package docsync

import (
	"context"
	"encoding/json"
	"testing"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/ratingmodel"

	"github.com/stretchr/testify/require"
)

func truncateRankTables(t *testing.T) {
	t.Helper()
	truncateFacetTables(t)
	for _, tbl := range []string{"catalog_work_rating", "catalog_work_playtime", "catalog_work_platform", "catalog_rating_model"} {
		require.NoError(t, facetTestDB.Exec("TRUNCATE "+tbl+" RESTART IDENTITY CASCADE").Error, tbl)
	}
}

func attachLabel(t *testing.T, work int64, labelKind, edgeKind int16) {
	t.Helper()
	l := &model.CatalogLabel{DisplayName: "L", Lang: "ja", Kind: labelKind}
	require.NoError(t, facetTestDB.Create(l).Error)
	require.NoError(t, facetTestDB.Create(&model.CatalogWorkLabel{WorkID: work, LabelID: l.ID, Kind: edgeKind}).Error)
}

func TestWorkCirculationPlatformsPlaytime(t *testing.T) {
	truncateRankTables(t)
	brand := mkWork(t, "Brand", model.WorkStatusLive, galgameMedium)
	both := mkWork(t, "BrandAndCircle", model.WorkStatusLive, galgameMedium)
	circle := mkWork(t, "Circle", model.WorkStatusLive, galgameMedium)
	publisherOnly := mkWork(t, "PublisherOnly", model.WorkStatusLive, galgameMedium)
	attachLabel(t, brand, model.LabelKindGameBrand, model.WorkLabelKindBrand)
	attachLabel(t, both, model.LabelKindGameBrand, model.WorkLabelKindDeveloper)
	attachLabel(t, both, model.LabelKindDoujinCircle, model.WorkLabelKindCircle)
	attachLabel(t, circle, model.LabelKindDoujinCircle, model.WorkLabelKindCircle)
	attachLabel(t, publisherOnly, model.LabelKindGameBrand, model.WorkLabelKindPublisher)

	circ, err := loadWorkCirculation(facetTestDB, nil)
	require.NoError(t, err)
	require.Equal(t, map[int64]string{brand: CirculationCommercial, both: CirculationCommercial, circle: CirculationDoujin}, circ)

	win := "win"
	require.NoError(t, facetTestDB.Create(&model.CatalogRelease{WorkID: brand, Platform: &win, Extra: []byte(`{"platforms":["win","swi"]}`)}).Error)
	require.NoError(t, facetTestDB.Create(&model.CatalogWorkPlatform{WorkID: circle, Platform: "ps4", SourceID: 3}).Error)
	plats, err := loadWorkPlatforms(facetTestDB, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"swi", "win"}, plats[brand])
	require.Equal(t, []string{"ps4"}, plats[circle])

	for _, p := range []struct {
		work    int64
		source  int16
		minutes int
	}{{brand, 2, 600}, {brand, 5, 1200}, {brand, 20, 300}, {circle, 2, 90}, {circle, 5, 150}} {
		require.NoError(t, facetTestDB.Create(&model.CatalogWorkPlaytime{WorkID: p.work, SourceID: p.source, Minutes: p.minutes}).Error)
	}
	pt, err := loadWorkPlaytime(facetTestDB, scope{brand, circle})
	require.NoError(t, err)
	require.Equal(t, map[int64]int{brand: 600, circle: 120}, pt)
}

func TestWorkRanksNeedAFittedModel(t *testing.T) {
	truncateRankTables(t)
	ctx := context.Background()
	var works []int64
	for i := range 12 {
		w := mkWork(t, "Rated", model.WorkStatusLive, galgameMedium)
		works = append(works, w)
		hist, _ := json.Marshal(map[string]int{"6": 2 + i%3, "8": 1 + i%4})
		require.NoError(t, facetTestDB.Create(&model.CatalogWorkRating{
			WorkID: w, SourceID: 3, Score: 7, VoteCount: 3 + i%3 + i%4, Distribution: hist,
		}).Error)
	}

	ranks, err := loadWorkRanks(facetTestDB, nil)
	require.NoError(t, err)
	require.Nil(t, ranks, "no model yet: documents carry no rank block")

	_, _, err = ratingmodel.FitAndSave(ctx, facetTestDB)
	require.NoError(t, err)
	ranks, err = loadWorkRanks(facetTestDB, scope{works[0]})
	require.NoError(t, err)
	require.Len(t, ranks, 1)
	b := ranks[works[0]]["bangumi"]
	require.Equal(t, 7.0, b[ratingmodel.FieldNative])
	require.Less(t, b[ratingmodel.FieldLower], b[ratingmodel.FieldMean])
	require.Greater(t, b[ratingmodel.FieldUpper], b[ratingmodel.FieldMean])
	require.Contains(t, b, ratingmodel.FieldMedian)
}
