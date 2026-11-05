package service

import (
	"testing"
	"time"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/ratingsummary"
	"api/internal/platform/markdown"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type cookedSummary struct {
	ShortSummaryHTML       string
	ShortSummaryText       string
	ShortSummaryHasSpoiler bool
	ShortSummaryCooker     int16
	UpdatedAt              time.Time
	ChangedAt              time.Time
}

func storedSummary(t *testing.T, id int64) cookedSummary {
	t.Helper()
	var c cookedSummary
	require.NoError(t, testDB.Raw(`SELECT short_summary_html, short_summary_text, short_summary_has_spoiler, short_summary_cooker,
		updated_at, changed_at FROM catalog_user_work_rating WHERE id = ?`, id).Scan(&c).Error)
	return c
}

func TestRatingSummaryIsCookedOnWrite(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()
	svc := NewUserRatingService(testDB)
	w := createWork(t, "rating-summary-cook")

	rec, err := svc.Set(ctx, 21, w.ID, "c", "kungal", rating(8, "**好**\n结局||全员幸存||"))
	require.NoError(t, err)
	assert.Equal(t, "<p><strong>好</strong><br>\n结局<span class=\"kun-spoiler kun-spoiler-hidden\">全员幸存</span></p>", rec.SummaryHTML)
	assert.Equal(t, "好\n结局███", rec.SummaryText)
	assert.True(t, rec.SummaryHasSpoiler)
	assert.EqualValues(t, markdown.ShortVersion, storedSummary(t, rec.ID).ShortSummaryCooker)

	rec, err = svc.Set(ctx, 21, w.ID, "c", "kungal", rating(8, "改了"))
	require.NoError(t, err)
	assert.Equal(t, "<p>改了</p>", rec.SummaryHTML)
	assert.Equal(t, "改了", rec.SummaryText)
	assert.False(t, rec.SummaryHasSpoiler)

	rec, err = svc.Set(ctx, 21, w.ID, "c", "kungal", rating(8, ""))
	require.NoError(t, err)
	assert.Empty(t, rec.SummaryHTML)
	assert.Empty(t, rec.SummaryText)
}

func TestCookRatingSummariesLeavesTheClocksAlone(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()
	svc := NewUserRatingService(testDB)
	w := createWork(t, "rating-summary-backfill")
	rec, err := svc.Set(ctx, 22, w.ID, "c", "kungal", rating(7, "*旧*评论"))
	require.NoError(t, err)
	require.NoError(t, testDB.Exec(`UPDATE catalog_user_work_rating
		SET short_summary_html = '', short_summary_text = '', short_summary_cooker = 0,
		    updated_at = now() - interval '3 days', changed_at = now() - interval '2 days' WHERE id = ?`, rec.ID).Error)
	before := storedSummary(t, rec.ID)

	st, err := ratingsummary.Cook(ctx, testDB, ratingsummary.Scope{})
	require.NoError(t, err)
	assert.Equal(t, 1, st.Cooked)
	after := storedSummary(t, rec.ID)
	assert.Equal(t, "<p><em>旧</em>评论</p>", after.ShortSummaryHTML)
	assert.Equal(t, "旧评论", after.ShortSummaryText)
	assert.EqualValues(t, markdown.ShortVersion, after.ShortSummaryCooker)
	assert.True(t, before.UpdatedAt.Equal(after.UpdatedAt), "updated_at stays the author's")
	assert.True(t, before.ChangedAt.Equal(after.ChangedAt), "the deploy cook moves no mirror cursor")

	st, err = ratingsummary.Cook(ctx, testDB, ratingsummary.Scope{})
	require.NoError(t, err)
	assert.Zero(t, st.Scanned, "a current cooker version is not cooked again")

	require.NoError(t, testDB.Exec(`UPDATE catalog_user_work_rating SET short_summary_html = 'stale' WHERE id = ?`, rec.ID).Error)
	st, err = ratingsummary.Cook(ctx, testDB, ratingsummary.Scope{EveryRow: true, DryRun: true})
	require.NoError(t, err)
	assert.Equal(t, 1, st.Cooked)
	assert.Equal(t, "stale", storedSummary(t, rec.ID).ShortSummaryHTML, "a dry run writes nothing")
	_, err = ratingsummary.Cook(ctx, testDB, ratingsummary.Scope{EveryRow: true})
	require.NoError(t, err)
	assert.Equal(t, "<p><em>旧</em>评论</p>", storedSummary(t, rec.ID).ShortSummaryHTML, "every-row mode repairs a row cooked from an older edit")
}

func TestBumpRatingSummariesStampsOnlyRatingsWithASummary(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()
	svc := NewUserRatingService(testDB)
	w := createWork(t, "rating-summary-bump")
	with, err := svc.Set(ctx, 23, w.ID, "c", "kungal", rating(7, "有评论"))
	require.NoError(t, err)
	without, err := svc.Set(ctx, 24, w.ID, "c", "kungal", rating(6, ""))
	require.NoError(t, err)
	require.NoError(t, testDB.Exec(`UPDATE catalog_user_work_rating SET changed_at = now() - interval '1 hour'`).Error)
	untouched := storedSummary(t, without.ID).ChangedAt
	cursor := time.Now().Add(-time.Minute)

	st, err := ratingsummary.Bump(ctx, testDB, 1, 0, false)
	require.NoError(t, err)
	assert.Equal(t, 1, st.Bumped)
	assert.True(t, storedSummary(t, with.ID).ChangedAt.After(cursor))
	assert.True(t, storedSummary(t, without.ID).ChangedAt.Equal(untouched))

	ageRatings(t)
	changes, err := svc.Changes(ctx, cursor.Add(-time.Minute), 0, 10)
	require.NoError(t, err)
	require.Len(t, changes, 1)
	assert.Equal(t, with.ID, changes[0].ID)
	assert.Equal(t, "<p>有评论</p>", changes[0].Rating.SummaryHTML)
}

func TestUndoMergeCooksRatingsFromASnapshotOlderThanTheColumns(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()
	e := seedEditionPair(t)
	ratings := NewUserRatingService(testDB)

	const uid = int64(611)
	srcOld, err := ratings.Set(ctx, uid, e.src, "c", "kungal", rating(6, "源||剧透||"))
	require.NoError(t, err)
	_, err = ratings.Set(ctx, uid, e.dst, "c", "kungal", rating(9, "**新**"))
	require.NoError(t, err)
	require.NoError(t, testDB.Exec(`UPDATE catalog_user_work_rating SET updated_at = now() - interval '2 days' WHERE id = ?`, srcOld.ID).Error)

	p, err := testMerge.RequestMerge(ctx, model.EntityTypeWork, e.src, e.dst, 42, "edition duplicate", MergeChoices{})
	require.NoError(t, err)
	approveAndForceExecutable(t, p.ID)
	require.NoError(t, testMerge.ExecuteMerge(ctx, p.ID, nil))
	require.NoError(t, testDB.Exec(`UPDATE catalog_merge_movement
		SET "row" = "row" - 'short_summary_html' - 'short_summary_text' - 'short_summary_has_spoiler' - 'short_summary_cooker'
		WHERE proposal_id = ? AND table_name = 'catalog_user_work_rating'`, p.ID).Error)

	_, err = testMerge.UndoMerge(ctx, p.ID, 9, "different editions after all")
	require.NoError(t, err)
	restored := storedSummary(t, srcOld.ID)
	assert.Equal(t, "<p>源<span class=\"kun-spoiler kun-spoiler-hidden\">剧透</span></p>", restored.ShortSummaryHTML)
	assert.Equal(t, "源███", restored.ShortSummaryText)
	assert.True(t, restored.ShortSummaryHasSpoiler)
	assert.EqualValues(t, markdown.ShortVersion, restored.ShortSummaryCooker)
}
