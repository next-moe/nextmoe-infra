package service

import (
	"strings"
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/require"
)

func seedTagTree(t *testing.T) (root, mid, leaf, other int64) {
	t.Helper()
	root = createCanonicalTag(t, "NTR", model.TagTierCore, model.TagKindContent)
	mid = createCanonicalTag(t, "不可避NTR", model.TagTierCore, model.TagKindContent)
	leaf = createCanonicalTag(t, "完全不可避免NTR", model.TagTierLongtail, model.TagKindContent)
	other = createCanonicalTag(t, "纯爱", model.TagTierCore, model.TagKindContent)
	require.NoError(t, testDB.Create(&[]model.CatalogTagParent{
		{TagID: mid, ParentID: root, Origin: model.TagParentOriginVNDB},
		{TagID: leaf, ParentID: mid, Origin: model.TagParentOriginVNDB},
		{TagID: root, ParentID: leaf, Origin: model.TagParentOriginCurated, Note: "a loop the expansion must survive"},
	}).Error)
	return root, mid, leaf, other
}

func TestTagExclusionExpandsTreesOnly(t *testing.T) {
	cleanTables(t)
	cleanTagTables(t)
	svc := newPublicSvc()
	ctx := t.Context()
	root, mid, leaf, other := seedTagTree(t)

	got, err := svc.TagExclusion(ctx, []int64{other}, []int64{mid})
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{root, mid, leaf, other}, got, "the curated loop leaf→root brings root back in")

	got, err = svc.TagExclusion(ctx, []int64{root}, nil)
	require.NoError(t, err)
	require.Equal(t, []int64{root}, got, "an exact id never expands")

	got, err = svc.TagExclusion(ctx, nil, []int64{999999})
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestWorksListAndCalendarExcludeTags(t *testing.T) {
	cleanTables(t)
	cleanTagTables(t)
	svc := newPublicSvc()
	ctx := t.Context()

	parent := createCanonicalTag(t, "NTR", model.TagTierCore, model.TagKindContent)
	child := createCanonicalTag(t, "不可避NTR", model.TagTierCore, model.TagKindContent)
	other := createCanonicalTag(t, "纯爱", model.TagTierCore, model.TagKindContent)
	require.NoError(t, testDB.Create(&model.CatalogTagParent{TagID: child, ParentID: parent, Origin: model.TagParentOriginVNDB}).Error)
	for name, id := range map[string]int64{"寝取られ（不可避）": child, "純愛": other} {
		require.NoError(t, testDB.Create(&model.CatalogTagSourceMap{SourceID: srcBangumiSupply, SourceName: name, TagID: id}).Error)
	}

	mk := func(name string, day int16, tag string) int64 {
		t.Helper()
		w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, name)
		createRelease(t, w.ID, 2024, 6, day)
		if tag != "" {
			require.NoError(t, testDB.Create(&model.CatalogWorkTag{
				WorkID: w.ID, Name: tag, Count: 1, SourceID: srcBangumiSupply, Spoiler: 2,
			}).Error)
		}
		return w.ID
	}
	ntr := mk("NTR Work", 1, "寝取られ（不可避）")
	pure := mk("Pure Work", 2, "純愛")
	plain := mk("Untagged Work", 3, "")

	list := func(excl []int64) []int64 {
		t.Helper()
		page, err := svc.WorksList(ctx, WorksListFilter{Sort: "id", ExcludeTagIDs: excl}, "", 50)
		require.NoError(t, err)
		out := make([]int64, 0, len(page.Items))
		for _, it := range page.Items {
			out = append(out, it.ID)
		}
		return out
	}
	tree, err := svc.TagExclusion(ctx, nil, []int64{parent})
	require.NoError(t, err)
	exact, err := svc.TagExclusion(ctx, []int64{parent}, nil)
	require.NoError(t, err)

	require.Equal(t, []int64{ntr, pure, plain}, list(nil))
	require.Equal(t, []int64{pure, plain}, list(tree), "a spoiler-level tag still excludes")
	require.Equal(t, []int64{ntr, pure, plain}, list(exact), "exact on the parent leaves the child's works")
	require.Equal(t, []int64{ntr, plain}, list([]int64{other}))

	cal := func(excl []int64) CalendarFilter {
		return CalendarFilter{OLang: PublicOLang{All: true}, ExcludeTagIDs: excl}
	}
	require.Equal(t, []int64{ntr, pure, plain}, calIDs(t, svc, june2024(), cal(nil)))
	require.Equal(t, []int64{pure, plain}, calIDs(t, svc, june2024(), cal(tree)))
	n, _, err := svc.CalendarMeta(ctx, june2024(), cal(tree))
	require.NoError(t, err)
	require.EqualValues(t, 2, n)
	require.NotEqual(t, cal(tree).PopulationKey(), cal([]int64{other}).PopulationKey())

	_, _, found, err := svc.CalendarBounds(ctx, cal(tree))
	require.NoError(t, err)
	require.True(t, found)
	svc.totals.mu.Lock()
	defer svc.totals.mu.Unlock()
	needle := cal(tree).PopulationKey()
	for key := range svc.totals.entries {
		if strings.Contains(key, needle) {
			t.Fatalf("per-reader exclusion bounds cached under %s", key)
		}
	}
}
