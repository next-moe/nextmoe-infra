package service

import (
	"context"
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/require"
)

func TestPersonSearchMatchesPublicNamesOnly(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()
	byDisplay := createPerson(t, "Hanazawa Display")
	byName := createPerson(t, "Someone Else")
	createCreditName(t, &byName.ID, "花澤香菜")
	hiddenOwner := createPerson(t, "Hidden Owner")
	hidden := createCreditName(t, &hiddenOwner.ID, "花澤 Secret")
	require.NoError(t, testDB.Exec(`UPDATE catalog_credit_name SET link_visibility = ? WHERE id = ?`,
		model.LinkVisibilityHidden, hidden.ID).Error)

	ids := func(q string) []int64 {
		t.Helper()
		page, err := svc.PersonsList(t.Context(), nil, q, "", 50)
		require.NoError(t, err)
		out := make([]int64, 0, len(page.Items))
		for _, it := range page.Items {
			out = append(out, it.ID)
		}
		return out
	}
	require.Equal(t, []int64{byDisplay.ID}, ids("hanazawa"), "display name, case-insensitive")
	require.Equal(t, []int64{byName.ID}, ids("花澤"), "a public credit name finds its person; a hidden link does not")
	require.Empty(t, ids("%"), "q= is a substring, not a pattern")
	require.Len(t, ids(""), 3)
}

func TestReleaseSearchMatchesTitleOrWorkName(t *testing.T) {
	cleanTables(t)
	ctx := context.Background()
	svc := newPublicSvcCDN()
	w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "Summer Pockets")
	claimLive(t, w.ID, 9601)
	titled := createRelease(t, w.ID, 2018, 6, 29)
	title := "Reflection Blue Limited"
	require.NoError(t, testDB.Model(titled).Update("title", title).Error)
	other := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "Winter Something")
	claimLive(t, other.ID, 9602)
	createRelease(t, other.ID, 2020, 1, 1)

	ids := func(q string) []int64 {
		t.Helper()
		feed, err := svc.ReleaseFeed(ctx, ReleaseFeedFilter{TitleQuery: q}, "", 20)
		require.NoError(t, err)
		out := make([]int64, 0, len(feed.Items))
		for _, it := range feed.Items {
			out = append(out, it.ID)
		}
		return out
	}
	require.Equal(t, []int64{titled.ID}, ids("reflection"), "release title")
	require.Equal(t, []int64{titled.ID}, ids("summer"), "work display name")
	require.Empty(t, ids("_"), "q= is a substring, not a pattern")
	n, _, _, err := svc.ReleaseFeedMeta(ctx, ReleaseFeedFilter{TitleQuery: "summer"})
	require.NoError(t, err)
	require.Equal(t, int64(1), n, "the total counts the same filter")

	byIndex := ReleaseFeedFilter{TitleQuery: "サマポケ", TitleWorkIDs: []int64{w.ID}}
	feed, err := svc.ReleaseFeed(ctx, byIndex, "", 20)
	require.NoError(t, err)
	require.Len(t, feed.Items, 1, "a work the search index matched brings its releases")
	require.Equal(t, titled.ID, feed.Items[0].ID)
	n, _, _, err = svc.ReleaseFeedMeta(ctx, byIndex)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
}
