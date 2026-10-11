package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/require"
)

func rating(overall int16, summary string) RatingInput {
	return RatingInput{Overall: overall, Recommend: model.RecommendYes, SpoilerLevel: model.SpoilerNone, ShortSummary: summary}
}

type nextmoeRow struct {
	Score        float64
	VoteCount    int
	Distribution json.RawMessage
	Stats        json.RawMessage
}

func nextmoeRating(t *testing.T, workID int64) *nextmoeRow {
	t.Helper()
	var rows []nextmoeRow
	require.NoError(t, testDB.Raw(`SELECT r.score, r.vote_count, r.distribution, r.stats
		FROM catalog_work_rating r JOIN catalog_source s ON s.id = r.source_id
		WHERE s.key = 'nextmoe' AND r.work_id = ?`, workID).Scan(&rows).Error)
	if len(rows) == 0 {
		return nil
	}
	return &rows[0]
}

func ageRatings(t *testing.T) {
	t.Helper()
	require.NoError(t, testDB.Exec(`UPDATE catalog_user_work_rating SET changed_at = changed_at - interval '1 minute'`).Error)
	require.NoError(t, testDB.Exec(`UPDATE catalog_user_work_rating_tombstone SET deleted_at = deleted_at - interval '1 minute'`).Error)
}

func TestRatingLifecycleKeepsIdAndLeavesTombstone(t *testing.T) {
	cleanTables(t)
	ctx := context.Background()
	svc := NewUserRatingService(testDB)
	w := createWork(t, "rating-lifecycle")

	in := rating(8, "good")
	in.Aspects.Story = i16(9)
	rec, err := svc.Set(ctx, 11, w.ID, "kungal-client", "kungal", in)
	require.NoError(t, err)
	require.Equal(t, int16(8), rec.Overall)
	require.Equal(t, int16(9), *rec.Aspects.Story)
	require.Nil(t, rec.Aspects.Art)
	require.Equal(t, "kungal-client", rec.ClientID)

	again, err := svc.Set(ctx, 11, w.ID, "letmoe-client", "letmoe", in)
	require.NoError(t, err)
	require.Equal(t, rec.ID, again.ID)
	require.True(t, again.UpdatedAt.Equal(rec.UpdatedAt), "an unchanged PUT must not move updated_at")

	in.Overall = 6
	edited, err := svc.Set(ctx, 11, w.ID, "letmoe-client", "letmoe", in)
	require.NoError(t, err)
	require.Equal(t, rec.ID, edited.ID)
	require.Equal(t, "kungal-client", edited.ClientID, "the origin stays the first writer")
	require.True(t, edited.UpdatedAt.After(rec.UpdatedAt))

	require.NoError(t, svc.DeleteMine(ctx, 11, w.ID))
	_, err = svc.Get(ctx, rec.ID)
	require.ErrorIs(t, err, ErrRatingNotFound)
	require.Nil(t, nextmoeRating(t, w.ID))

	ageRatings(t)
	changes, err := svc.Changes(ctx, time.Time{}, 0, 10)
	require.NoError(t, err)
	require.Len(t, changes, 1)
	require.True(t, changes[0].Gone)
	require.Equal(t, rec.ID, changes[0].ID)
	require.Equal(t, w.ID, changes[0].WorkID)

	back, err := svc.Set(ctx, 11, w.ID, "kungal-client", "kungal", in)
	require.NoError(t, err)
	require.NotEqual(t, rec.ID, back.ID, "a deleted id never comes back")
}

func TestRatingValidation(t *testing.T) {
	cleanTables(t)
	ctx := context.Background()
	svc := NewUserRatingService(testDB)
	w := createWork(t, "rating-validation")
	for name, tc := range map[string]struct {
		mutate func(*RatingInput)
		want   error
	}{
		"overall low":   {func(in *RatingInput) { in.Overall = 0 }, ErrRatingBadOverall},
		"overall high":  {func(in *RatingInput) { in.Overall = 11 }, ErrRatingBadOverall},
		"aspect":        {func(in *RatingInput) { in.Aspects.Voice = i16(0) }, ErrRatingBadAspect},
		"recommend":     {func(in *RatingInput) { in.Recommend = 0 }, ErrRatingBadRecommend},
		"spoiler":       {func(in *RatingInput) { in.SpoilerLevel = 3 }, ErrRatingBadSpoiler},
		"summary long":  {func(in *RatingInput) { in.ShortSummary = strings.Repeat("评", 2001) }, ErrRatingSummaryTooLong},
		"summary bytes": {func(in *RatingInput) { in.ShortSummary = "\xff" }, ErrRatingSummaryEncoding},
	} {
		in := rating(7, "")
		tc.mutate(&in)
		_, err := svc.Set(ctx, 1, w.ID, "c", "", in)
		require.ErrorIs(t, err, tc.want, name)
	}
	ok := rating(7, strings.Repeat("评", 2000))
	_, err := svc.Set(ctx, 1, w.ID, "c", "", ok)
	require.NoError(t, err, "2000 characters is the limit, not 2000 bytes")

	_, err = svc.Set(ctx, 1, w.ID+999, "c", "", rating(7, ""))
	require.ErrorIs(t, err, ErrRatingWorkUnavailable)
}

func TestRatingAggregateIsTheBayesianMean(t *testing.T) {
	cleanTables(t)
	ctx := context.Background()
	svc := NewUserRatingService(testDB)
	a, b := createWork(t, "aggregate-a"), createWork(t, "aggregate-b")
	_, err := svc.Set(ctx, 1, a.ID, "c", "", rating(8, ""))
	require.NoError(t, err)
	_, err = svc.Set(ctx, 2, a.ID, "c", "", rating(6, ""))
	require.NoError(t, err)
	_, err = svc.Set(ctx, 1, b.ID, "c", "", rating(10, ""))
	require.NoError(t, err)

	st, err := RefreshRatingAggregates(ctx, testDB, true)
	require.NoError(t, err)
	require.Equal(t, 2, st.Rated)

	// Network mean 8: a = (10*8 + 14) / 12, b = (10*8 + 10) / 11.
	ra := nextmoeRating(t, a.ID)
	require.NotNil(t, ra)
	require.InDelta(t, 7.83, ra.Score, 1e-9)
	require.Equal(t, 2, ra.VoteCount)
	require.JSONEq(t, `{"6":1,"8":1}`, string(ra.Distribution))
	require.JSONEq(t, `{"average":7,"stdev":1.41}`, string(ra.Stats))
	rb := nextmoeRating(t, b.ID)
	require.InDelta(t, 8.18, rb.Score, 1e-9)
	require.JSONEq(t, `{"average":10}`, string(rb.Stats), "one rating has no spread")

	again, err := RefreshRatingAggregates(ctx, testDB, true)
	require.NoError(t, err)
	require.Zero(t, again.Written, "a second pass over an unchanged population writes nothing")
}

func TestRatingAggregateDryRunWritesNothing(t *testing.T) {
	cleanTables(t)
	ctx := context.Background()
	w := createWork(t, "aggregate-dry")
	require.NoError(t, testDB.Create(&model.CatalogUserWorkRating{
		ActorUID: 1, WorkID: w.ID, Overall: 9, Recommend: model.RecommendYes, ClientID: "c",
	}).Error)
	st, err := RefreshRatingAggregates(ctx, testDB, false)
	require.NoError(t, err)
	require.Equal(t, 1, st.Written)
	require.Nil(t, nextmoeRating(t, w.ID))
}

func TestRatingMergeKeepsTheLaterRow(t *testing.T) {
	cleanTables(t)
	ctx := context.Background()
	svc := NewUserRatingService(testDB)
	src, dst := createWork(t, "merge-src"), createWork(t, "merge-dst")

	older, err := svc.Set(ctx, 1, dst.ID, "c", "", rating(4, "dst"))
	require.NoError(t, err)
	newer, err := svc.Set(ctx, 1, src.ID, "c", "", rating(9, "src"))
	require.NoError(t, err)
	stale, err := svc.Set(ctx, 2, src.ID, "c", "", rating(3, "src older"))
	require.NoError(t, err)
	fresh, err := svc.Set(ctx, 2, dst.ID, "c", "", rating(7, "dst newer"))
	require.NoError(t, err)
	only, err := svc.Set(ctx, 3, src.ID, "c", "", rating(5, "only src"))
	require.NoError(t, err)

	mergeNow(t, model.EntityTypeWork, src.ID, dst.ID)

	var left []model.CatalogUserWorkRating
	require.NoError(t, testDB.Order("actor_uid").Find(&left).Error)
	require.Len(t, left, 3)
	for _, r := range left {
		require.Equal(t, dst.ID, r.WorkID)
	}
	require.Equal(t, newer.ID, left[0].ID, "user 1's later rating keeps its id")
	require.Equal(t, fresh.ID, left[1].ID, "user 2's later rating keeps its id")
	require.Equal(t, only.ID, left[2].ID)

	var gone []int64
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_user_work_rating_tombstone ORDER BY id`).Scan(&gone).Error)
	require.ElementsMatch(t, []int64{older.ID, stale.ID}, gone)

	agg := nextmoeRating(t, dst.ID)
	require.NotNil(t, agg)
	require.Equal(t, 3, agg.VoteCount)
	require.Nil(t, nextmoeRating(t, src.ID))
}

func TestWorkRecordPutIsAllOrNothing(t *testing.T) {
	cleanTables(t)
	require.NoError(t, testDB.Exec("TRUNCATE catalog_user_work_state, catalog_user_playtime RESTART IDENTITY").Error)
	ctx := context.Background()
	svc := NewUserWorkRecordService(testDB)
	w := createWork(t, "work-record")
	minutes := 120
	comp := model.WorkCompletionMain
	full := WorkRecordInput{
		WorkState: &WorkStateInput{State: model.WorkStateDone, Completion: &comp},
		Minutes:   &minutes,
		Rating:    &RatingInput{Overall: 9, Recommend: model.RecommendStrongYes},
	}

	bad := full
	bad.Rating = &RatingInput{Overall: 11, Recommend: model.RecommendYes}
	_, err := svc.Put(ctx, 5, w.ID, "kungal", "kungal", bad)
	require.ErrorIs(t, err, ErrRatingBadOverall)
	got, err := svc.Get(ctx, 5, w.ID)
	require.NoError(t, err)
	require.Nil(t, got.WorkState)
	require.Nil(t, got.Playtime)
	require.Nil(t, got.Rating)

	rec, err := svc.Put(ctx, 5, w.ID, "kungal", "kungal", full)
	require.NoError(t, err)
	require.Equal(t, model.WorkStateDone, rec.WorkState.State)
	require.Equal(t, model.WorkCompletionMain, *rec.WorkState.Completion)
	require.Equal(t, 120, rec.Playtime.Minutes)
	require.Equal(t, int16(9), rec.Rating.Overall)

	_, err = NewUserPlaytimeService(testDB).Report(ctx, PlaytimeReport{ActorUID: 5, WorkID: w.ID, ClientID: "desktop", Minutes: 300})
	require.NoError(t, err)

	cleared, err := svc.Put(ctx, 5, w.ID, "kungal", "kungal", WorkRecordInput{
		WorkState: &WorkStateInput{State: model.WorkStateOnHold},
	})
	require.NoError(t, err)
	require.Equal(t, model.WorkStateOnHold, cleared.WorkState.State)
	require.Nil(t, cleared.WorkState.Completion)
	require.Nil(t, cleared.Rating)
	require.Equal(t, 300, cleared.Playtime.Minutes, "another app's playtime row stays")
	var mine int64
	require.NoError(t, testDB.Model(&model.CatalogUserPlaytime{}).
		Where("actor_uid = ? AND work_id = ? AND client_id = ?", 5, w.ID, "kungal").Count(&mine).Error)
	require.Zero(t, mine)
	var tombstones int64
	require.NoError(t, testDB.Model(&model.CatalogUserWorkRatingTombstone{}).Where("id = ?", rec.Rating.ID).Count(&tombstones).Error)
	require.Equal(t, int64(1), tombstones)
}

func TestRatingCarriesItsAuthorsStateAndSurfacesWhenItChanges(t *testing.T) {
	cleanTables(t)
	require.NoError(t, testDB.Exec("TRUNCATE catalog_user_work_state RESTART IDENTITY").Error)
	ctx := context.Background()
	ratings, states := NewUserRatingService(testDB), NewUserWorkStateService(testDB)
	w := createWork(t, "rating-state")
	rec, err := ratings.Set(ctx, 9, w.ID, "c", "", rating(8, ""))
	require.NoError(t, err)
	require.Nil(t, rec.AuthorState)

	changedAt := func() time.Time {
		var at time.Time
		require.NoError(t, testDB.Raw(`SELECT changed_at FROM catalog_user_work_rating WHERE id = ?`, rec.ID).Scan(&at).Error)
		return at
	}
	ageRatings(t)
	aged := changedAt()

	comp := model.WorkCompletionAll
	_, err = states.Set(ctx, 9, w.ID, model.WorkStateDone, &comp)
	require.NoError(t, err)
	touched := changedAt()
	require.True(t, touched.After(aged), "a state change surfaces the rating")
	got, err := ratings.Get(ctx, rec.ID)
	require.NoError(t, err)
	require.Equal(t, model.WorkStateDone, got.AuthorState.State)
	require.Equal(t, model.WorkCompletionAll, *got.AuthorState.Completion)
	require.True(t, got.UpdatedAt.Equal(rec.UpdatedAt), "a state change is not an edit of the rating")

	ageRatings(t)
	aged = changedAt()
	_, err = states.Set(ctx, 9, w.ID, model.WorkStateDone, &comp)
	require.NoError(t, err)
	require.True(t, changedAt().Equal(aged), "the same state again changes nothing")

	require.NoError(t, states.DeleteMine(ctx, 9, w.ID))
	require.True(t, changedAt().After(aged))
	got, err = ratings.Get(ctx, rec.ID)
	require.NoError(t, err)
	require.Nil(t, got.AuthorState)
}
