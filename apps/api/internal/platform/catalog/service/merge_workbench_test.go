package service

import (
	"testing"
	"time"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type editionPair struct {
	src, dst                 int64
	srcRelease, dstRelease   int64
	srcTitle, dstVNDBRelease string
}

// The 211503 → 11529 shape: the source is a remake's own entry on Bangumi and
// EGS; VNDB lists that remake as a release of the target's VN, and both works
// carry the same download release.
func seedEditionPair(t *testing.T) editionPair {
	t.Helper()
	src := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "脅迫～終わらない明日～").ID
	dst := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "脅迫").ID
	addExternalRef(t, model.EntityTypeWork, src, model.SourceBangumi, "350885", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeWork, src, model.SourceErogameScape, "226", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeWork, dst, model.SourceVNDB, "v4302", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeWork, dst, model.SourceBangumi, "114894", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeWork, dst, model.SourceErogameScape, "4871", model.LinkKindExact)
	addWorkTitle(t, src, "ja", "脅迫～終わらない明日～", model.WorkTitleKindOfficial)
	addWorkTitle(t, src, "zh-Hans", "胁迫～没有终结的明日～", model.WorkTitleKindOfficial)
	addWorkTitle(t, dst, "ja", "脅迫", model.WorkTitleKindOfficial)

	sr := createRelease(t, src, 2005, 7, 15)
	dr := createRelease(t, dst, 2005, 7, 15)
	require.NoError(t, testDB.Exec(`UPDATE catalog_release SET kind = ?, platform = 'win' WHERE id = ?`, model.ReleaseKindDigital, sr.ID).Error)
	require.NoError(t, testDB.Exec(`UPDATE catalog_release SET platform = 'win', extra = '{"vndb_id":"r74937"}' WHERE id = ?`, dr.ID).Error)
	return editionPair{src: src, dst: dst, srcRelease: sr.ID, dstRelease: dr.ID, srcTitle: "胁迫～没有终结的明日～", dstVNDBRelease: "r74937"}
}

func TestMergePreviewEditionPair(t *testing.T) {
	cleanTables(t)
	e := seedEditionPair(t)

	p, err := testMerge.PreviewMerge(t.Context(), model.EntityTypeWork, e.src, e.dst)
	require.NoError(t, err)
	assert.Empty(t, p.Blockers)

	fates := map[string]string{}
	for _, a := range p.Anchors {
		fates[a.Side+":"+a.SourceKey+":"+a.ExternalID] = a.Fate
	}
	assert.Equal(t, FateDemotedRelated, fates["source:bangumi:350885"], "a second Bangumi subject becomes a related link")
	assert.Equal(t, FateDemotedRelated, fates["source:erogamescape:226"])
	assert.Equal(t, FateStays, fates["target:vndb:v4302"])

	var dup *PreviewRelease
	for i := range p.Releases {
		if p.Releases[i].ID == e.srcRelease {
			dup = &p.Releases[i]
		}
	}
	require.NotNil(t, dup)
	assert.Equal(t, e.dstRelease, dup.DuplicateOf, "a digital release matches a default-kind release on the same platform and date")
	assert.Equal(t, "platform_date", dup.DuplicateBy)

	titleFate := map[string]string{}
	for _, tt := range p.Titles {
		if tt.Side == ResolutionSource {
			titleFate[tt.Title] = tt.Fate
		}
	}
	assert.Equal(t, FateBecomesAlias, titleFate["脅迫～終わらない明日～"], "the target already shows a ja title")
	assert.Equal(t, FateMoves, titleFate[e.srcTitle], "the target has no zh-Hans title")

	assert.Contains(t, p.Facts, "VNDB 只收录了「脅迫」（v4302），「脅迫～終わらない明日～」在 VNDB 没有独立条目。")
	require.Len(t, p.Warnings, 2, "each demoted anchor is explained")
}

func TestMergePreviewBlockers(t *testing.T) {
	cleanTables(t)
	game := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "game").ID
	var novelMedium int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_medium WHERE key = 'novel'`).Scan(&novelMedium).Error)
	novel := createWorkX(t, novelMedium, model.ContentRatingAllAges, model.WorkStatusLive, "novel").ID

	p, err := testMerge.PreviewMerge(t.Context(), model.EntityTypeWork, novel, game)
	require.NoError(t, err)
	require.Len(t, p.Blockers, 1)
	assert.Equal(t, "cross_medium", p.Blockers[0].Code)

	require.NoError(t, testDB.Exec(`UPDATE catalog_work SET deleted_at = now() WHERE id = ?`, novel).Error)
	p, err = testMerge.PreviewMerge(t.Context(), model.EntityTypeWork, novel, game)
	require.NoError(t, err)
	assert.Equal(t, "not_live", p.Blockers[0].Code)
}

func TestMergeRequestChoicesApply(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()
	e := seedEditionPair(t)

	_, err := testMerge.RequestMerge(ctx, model.EntityTypeWork, e.src, e.dst, 42, "edition duplicate",
		MergeChoices{Fields: map[string]string{"display_name": "nope"}})
	require.ErrorIs(t, err, ErrMergeChoice)
	_, err = testMerge.RequestMerge(ctx, model.EntityTypeWork, e.src, e.dst, 42, "edition duplicate",
		MergeChoices{HideReleases: []int64{e.dstRelease}})
	require.ErrorIs(t, err, ErrMergeChoice, "only the source's own releases can be hidden")

	p, err := testMerge.RequestMerge(ctx, model.EntityTypeWork, e.src, e.dst, 42, "edition duplicate",
		MergeChoices{HideReleases: []int64{e.srcRelease}})
	require.NoError(t, err)
	require.NoError(t, testMerge.AmendMergeRequest(ctx, p.ID, 7, MergeChoices{
		Fields: map[string]string{"display_name": ResolutionSource}, HideReleases: []int64{e.srcRelease},
	}))
	got, err := testMerge.GetMergeRequest(ctx, p.ID)
	require.NoError(t, err)
	require.NotNil(t, got.AmendedBy)
	assert.EqualValues(t, 7, *got.AmendedBy)
	choices, err := MergeChoicesOf(got)
	require.NoError(t, err)
	assert.Equal(t, ResolutionSource, choices.Fields["display_name"])

	approveAndForceExecutable(t, p.ID)
	require.NoError(t, testMerge.ExecuteMerge(ctx, p.ID, nil))

	var name string
	require.NoError(t, testDB.Raw(`SELECT display_name FROM catalog_work WHERE id = ?`, e.dst).Scan(&name).Error)
	assert.Equal(t, "脅迫～終わらない明日～", name, "the reviewer chose the source's name")
	var hidden bool
	require.NoError(t, testDB.Raw(`SELECT deleted_at IS NOT NULL FROM catalog_release WHERE id = ? AND work_id = ?`,
		e.srcRelease, e.dst).Scan(&hidden).Error)
	assert.True(t, hidden, "the duplicate release moved to the target hidden")
}

func TestUndoMergeKeepsTheOriginalID(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()
	e := seedEditionPair(t)
	older := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "older dup").ID
	require.NoError(t, testDB.Transaction(func(tx *gorm.DB) error {
		return repository.InsertRedirect(tx, model.EntityTypeWork, older, e.src, nil, "earlier merge")
	}))
	require.NoError(t, testDB.Exec(`UPDATE catalog_redirect SET merged_at = now() - interval '10 days' WHERE old_id = ?`, older).Error)
	require.NoError(t, testDB.Create(&model.CatalogUserWorkRating{ActorUID: 501, WorkID: e.src, Overall: 8, Recommend: 1}).Error)

	p, err := testMerge.RequestMerge(ctx, model.EntityTypeWork, e.src, e.dst, 42, "edition duplicate",
		MergeChoices{Fields: map[string]string{"display_name": ResolutionSource}, HideReleases: []int64{e.srcRelease}})
	require.NoError(t, err)
	approveAndForceExecutable(t, p.ID)
	require.NoError(t, testMerge.ExecuteMerge(ctx, p.ID, nil))

	var flattenedAt time.Time
	require.NoError(t, testDB.Raw(`SELECT merged_at FROM catalog_redirect WHERE old_id = ?`, older).Scan(&flattenedAt).Error)
	assert.WithinDuration(t, time.Now(), flattenedAt, time.Minute, "a flattened redirect re-surfaces on the feed")

	rep, err := testMerge.UndoMerge(ctx, p.ID, 9, "different editions after all")
	require.NoError(t, err)

	var alive bool
	require.NoError(t, testDB.Raw(`SELECT deleted_at IS NULL AND status = ? FROM catalog_work WHERE id = ?`,
		model.WorkStatusLive, e.src).Scan(&alive).Error)
	assert.True(t, alive, "the source is live again under its own id")

	var release struct {
		WorkID int64
		Hidden bool
	}
	require.NoError(t, testDB.Raw(`SELECT work_id, deleted_at IS NOT NULL AS hidden FROM catalog_release WHERE id = ?`,
		e.srcRelease).Scan(&release).Error)
	assert.Equal(t, e.src, release.WorkID)
	assert.False(t, release.Hidden, "the release hidden by the merge is shown again")

	var kinds []int16
	require.NoError(t, testDB.Raw(`SELECT link_kind FROM catalog_external_ref WHERE entity_type = ? AND entity_id = ? ORDER BY external_id`,
		model.EntityTypeWork, e.src).Scan(&kinds).Error)
	assert.Equal(t, []int16{model.LinkKindExact, model.LinkKindExact}, kinds, "the demoted anchors are identities again")

	var titles []string
	require.NoError(t, testDB.Raw(`SELECT title FROM catalog_work_title WHERE work_id = ? ORDER BY title`, e.src).Scan(&titles).Error)
	assert.ElementsMatch(t, []string{"脅迫～終わらない明日～", e.srcTitle}, titles)
	var copies int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_work_title WHERE work_id = ? AND title = ?`,
		e.dst, "脅迫～終わらない明日～").Scan(&copies).Error)
	assert.Zero(t, copies, "the alias copy the merge added to the target is gone")

	var ratingWork int64
	require.NoError(t, testDB.Raw(`SELECT work_id FROM catalog_user_work_rating WHERE actor_uid = 501`).Scan(&ratingWork).Error)
	assert.Equal(t, e.src, ratingWork)

	var name string
	require.NoError(t, testDB.Raw(`SELECT display_name FROM catalog_work WHERE id = ?`, e.dst).Scan(&name).Error)
	assert.Equal(t, "脅迫", name, "the target's own name is back")
	assert.Contains(t, rep.RevertedFields, "display_name")

	redirects := repository.NewRedirectRepository(testDB)
	gone, err := redirects.Get(ctx, model.EntityTypeWork, e.src)
	require.NoError(t, err)
	assert.Nil(t, gone, "a revoked redirect no longer resolves")
	var revoked struct {
		CurrentID int64
		Revoked   bool
	}
	require.NoError(t, testDB.Raw(`SELECT current_id, revoked_at IS NOT NULL AS revoked FROM catalog_redirect WHERE old_id = ?`,
		e.src).Scan(&revoked).Error)
	assert.Equal(t, e.src, revoked.CurrentID)
	assert.True(t, revoked.Revoked)
	back, err := redirects.Get(ctx, model.EntityTypeWork, older)
	require.NoError(t, err)
	require.NotNil(t, back)
	assert.Equal(t, e.src, back.CurrentID, "the earlier merge into the source points at it again")

	got, err := testMerge.GetMergeRequest(ctx, p.ID)
	require.NoError(t, err)
	assert.Equal(t, model.ProposalStatusUndone, got.Status)
	require.NotNil(t, got.UndoneBy)

	_, err = testMerge.UndoMerge(ctx, p.ID, 9, "again")
	require.ErrorIs(t, err, ErrProposalState)
}

func TestRedirectFeedListsRevokedRedirects(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()
	e := seedEditionPair(t)
	p, err := testMerge.RequestMerge(ctx, model.EntityTypeWork, e.src, e.dst, 42, "dup", MergeChoices{})
	require.NoError(t, err)
	approveAndForceExecutable(t, p.ID)
	require.NoError(t, testMerge.ExecuteMerge(ctx, p.ID, nil))
	_, err = testMerge.UndoMerge(ctx, p.ID, 9, "undo")
	require.NoError(t, err)

	items, _, err := testResolve.RedirectsSince(ctx, nil, repository.RedirectCursor{}, 100)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.True(t, items[0].Revoked)
	assert.Equal(t, e.src, items[0].CurrentID)
}

func TestUndoMergeRefusals(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()
	e := seedEditionPair(t)
	p, err := testMerge.RequestMerge(ctx, model.EntityTypeWork, e.src, e.dst, 42, "dup", MergeChoices{})
	require.NoError(t, err)
	approveAndForceExecutable(t, p.ID)
	require.NoError(t, testMerge.ExecuteMerge(ctx, p.ID, nil))

	require.NoError(t, testDB.Exec(`UPDATE catalog_merge_proposal SET executed_at = now() - interval '400 days' WHERE id = ?`, p.ID).Error)
	_, err = testMerge.UndoMerge(ctx, p.ID, 9, "late")
	require.ErrorIs(t, err, ErrUndoExpired)

	require.NoError(t, testDB.Exec(`UPDATE catalog_merge_proposal SET executed_at = now() WHERE id = ?`, p.ID).Error)
	require.NoError(t, testDB.Exec(`DELETE FROM catalog_merge_movement WHERE proposal_id = ?`, p.ID).Error)
	_, err = testMerge.UndoMerge(ctx, p.ID, 9, "no record")
	require.ErrorIs(t, err, ErrUndoUnavailable)
}
