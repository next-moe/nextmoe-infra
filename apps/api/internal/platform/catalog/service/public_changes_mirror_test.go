package service

import (
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// changesFrom is changesSince with the gone bit kept: the whole point of these
// tests is which ids come back AND whether each is still part of the population.
func changesFrom(t *testing.T, cursor string) map[int64]bool {
	t.Helper()
	require.NoError(t, testDB.Exec(`UPDATE catalog_work SET updated_at = updated_at - interval '10 seconds'`).Error)
	page, err := newPublicSvc().Changes(t.Context(), cursor, 500)
	require.NoError(t, err)
	out := make(map[int64]bool, len(page.Items))
	for _, it := range page.Items {
		out[it.ID] = it.Gone
	}
	return out
}

func TestChangesFeedSurfacesDisplayAxisEdit(t *testing.T) {
	cleanTables(t)
	edited := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "display-axis")
	bystander := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "bystander")
	settleWorks(t)
	cursor := drainChanges(t)

	require.NoError(t, editspec.ApplyWorkFields(t.Context(), testDB, edited.ID,
		map[string]any{editspec.FieldWorkDisplayNSFW: true}))

	got := changesFrom(t, cursor)
	require.Contains(t, got, edited.ID, "an editor's display_nsfw flip is what the mirror channel exists for")
	assert.False(t, got[edited.ID], "the work is still served, only its display verdict moved")
	assert.NotContains(t, got, bystander.ID)
}

func TestChangesFeedSurfacesContentRatingEdit(t *testing.T) {
	cleanTables(t)
	edited := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "rating-axis")
	bystander := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "bystander")
	settleWorks(t)
	cursor := drainChanges(t)

	require.NoError(t, editspec.ApplyWorkFields(t.Context(), testDB, edited.ID,
		map[string]any{editspec.FieldWorkContentRating: float64(model.ContentRatingR18)}))

	got := changesFrom(t, cursor)
	require.Contains(t, got, edited.ID, "content_rating decides the verdict for unclaimed works")
	assert.NotContains(t, got, bystander.ID)
}

func TestChangesFeedSurfacesOLangEdit(t *testing.T) {
	cleanTables(t)
	edited := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "olang")
	bystander := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "bystander")
	settleWorks(t)
	cursor := drainChanges(t)

	require.NoError(t, editspec.ApplyWorkFields(t.Context(), testDB, edited.ID,
		map[string]any{editspec.FieldWorkOLang: "zh-Hant"}))

	got := changesFrom(t, cursor)
	require.Contains(t, got, edited.ID, "a site that routes on the original language mirrors olang off this feed")
	assert.False(t, got[edited.ID])
	assert.NotContains(t, got, bystander.ID)
}

func TestChangesFeedSurfacesClaimTransition(t *testing.T) {
	s := newLifecycle(t)
	work := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "claimed")
	bystander := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "bystander")
	product := int64(9001)
	act(t, s, work.ID, ClaimActionClaim, ClaimActionParams{Site: "kungal", ProductWorkID: &product, ActorUID: 7})

	settleWorks(t)
	cursor := drainChanges(t)

	act(t, s, work.ID, ClaimActionSubmit, ClaimActionParams{Site: "kungal", ActorUID: 7})

	got := changesFrom(t, cursor)
	require.Contains(t, got, work.ID, "the claim block is half of the display verdict")
	assert.False(t, got[work.ID])
	assert.NotContains(t, got, bystander.ID)
}

func TestChangesFeedMarksVanishedRowsGone(t *testing.T) {
	cleanTables(t)
	deleted := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "deleted")
	retired := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "retired")
	live := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "live")
	settleWorks(t)
	cursor := drainChanges(t)

	require.NoError(t, testDB.Exec(
		`UPDATE catalog_work SET deleted_at = now(), updated_at = now() WHERE id = ?`, deleted.ID).Error)
	require.NoError(t, testDB.Exec(
		`UPDATE catalog_work SET status = ?, updated_at = now() WHERE id = ?`,
		model.WorkStatusMerged, retired.ID).Error)
	require.NoError(t, repository.TouchWorks(t.Context(), testDB, []int64{live.ID}))

	got := changesFrom(t, cursor)
	assert.True(t, got[deleted.ID], "a soft-deleted work is gone")
	assert.True(t, got[retired.ID], "gone is the complement of the served population, not just deleted_at")
	require.Contains(t, got, live.ID)
	assert.False(t, got[live.ID])
}

func TestChangesFeedSurfacesMergedSourceAsGone(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()
	dst := createWork(t, "merge-target")
	src := createWork(t, "merge-source")
	settleWorks(t)
	cursor := drainChanges(t)

	p, err := testMerge.ProposeMerge(ctx, model.EntityTypeWork, src.ID, dst.ID, 7, "same work")
	require.NoError(t, err)
	approveAndForceExecutable(t, p.ID)
	require.NoError(t, testMerge.ExecuteMerge(ctx, p.ID, nil))

	got := changesFrom(t, cursor)
	require.Contains(t, got, src.ID, "an executed merge retires an id downstream still holds")
	assert.True(t, got[src.ID])
	require.Contains(t, got, dst.ID)
	assert.False(t, got[dst.ID])

	items, _, err := testResolve.RedirectsSince(ctx, nil, repository.RedirectCursor{}, 100)
	require.NoError(t, err)
	found := false
	for _, it := range items {
		if it.EntityType == model.EntityTypeWork && it.OldID == src.ID && it.CurrentID == dst.ID {
			found = true
		}
	}
	assert.True(t, found, "a gone entry must be repointable through the redirects feed")
}

func TestChangesFeedDoesNotRepeatUntouchedWorks(t *testing.T) {
	cleanTables(t)
	quiet := createWork(t, "quiet")
	touched := createWork(t, "touched")
	settleWorks(t)
	cursor := drainChanges(t)

	assert.Empty(t, changesFrom(t, cursor), "nothing was written, so the feed has nothing to say")

	require.NoError(t, repository.TouchWorks(t.Context(), testDB, []int64{touched.ID}))
	got := changesFrom(t, cursor)
	assert.Equal(t, map[int64]bool{touched.ID: false}, got,
		"the same cursor now yields exactly the written row")
	assert.NotContains(t, got, quiet.ID)
}

func TestChangesCursorMintedBeforeGoneStillResumes(t *testing.T) {
	cleanTables(t)
	first := createWork(t, "first")
	second := createWork(t, "second")
	settleWorks(t)

	var ts time.Time
	require.NoError(t, testDB.Raw(`SELECT updated_at FROM catalog_work WHERE id = ?`, first.ID).Scan(&ts).Error)
	// Hand-built rather than round-tripped through encodePublicCursor: the point
	// is that a cursor a consumer minted before this wave still decodes.
	legacy := base64.RawURLEncoding.EncodeToString([]byte(
		fmt.Sprintf(`{"s":"changes","id":%d,"u":%q}`, first.ID, ts.UTC().Format(time.RFC3339Nano))))

	page, err := newPublicSvc().Changes(t.Context(), legacy, 500)
	require.NoError(t, err)
	ids := make([]int64, 0, len(page.Items))
	for _, it := range page.Items {
		ids = append(ids, it.ID)
	}
	assert.Equal(t, []int64{second.ID}, ids, "an old cursor resumes where it left off")
}
