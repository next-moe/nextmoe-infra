package docsync

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/ratingmodel"
	"api/internal/platform/catalog/srcbangumi"

	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
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

func TestNonVNWorksNeedVNEvidence(t *testing.T) {
	truncateRankTables(t)
	require.NoError(t, srcbangumi.EnsureSchema(facetTestDB))
	for _, tbl := range []string{"catalog_external_ref", "catalog_release", "src_bangumi.subject"} {
		require.NoError(t, facetTestDB.Exec("TRUNCATE "+tbl+" CASCADE").Error, tbl)
	}
	source := func(key string) int16 {
		var id int16
		require.NoError(t, facetTestDB.Raw(`SELECT id FROM catalog_source WHERE key = ?`, key).Scan(&id).Error)
		require.NotZero(t, id, key)
		return id
	}
	ref := func(entityType int16, entity int64, key, ext string) {
		require.NoError(t, facetTestDB.Create(&model.CatalogExternalRef{
			EntityType: entityType, EntityID: entity, SourceID: source(key), ExternalID: ext, MatchedBy: "test",
		}).Error)
	}
	subject := func(id int64, meta, tags string) {
		require.NoError(t, facetTestDB.Create(&srcbangumi.Subject{
			ID: id, Type: 4, MetaTags: datatypes.JSON(meta), Tags: datatypes.JSON(tags),
		}).Error)
	}
	work := func(name string) int64 { return mkWork(t, name, model.WorkStatusLive, galgameMedium) }

	vndb := work("VNDB")
	ref(model.EntityTypeWork, vndb, "vndb", "v1")
	dlsiteRelease := work("DLsiteRelease")
	rel := &model.CatalogRelease{WorkID: dlsiteRelease}
	require.NoError(t, facetTestDB.Create(rel).Error)
	ref(model.EntityTypeRelease, rel.ID, "dlsite", "RJ1")
	claimed := work("Claimed")
	require.NoError(t, facetTestDB.Model(&model.CatalogWork{}).Where("id = ?", claimed).Update("site", "kungal").Error)
	galgameTag := work("BangumiGalgame")
	ref(model.EntityTypeWork, galgameTag, "bangumi", "11")
	subject(11, `["Galgame","PC"]`, `[]`)
	novelGame := work("BangumiADV")
	ref(model.EntityTypeWork, novelGame, "bangumi", "12")
	subject(12, `["ADV","全年龄","PC"]`, `[]`)
	folk := work("BangumiFolk")
	ref(model.EntityTypeWork, folk, "bangumi", "13")
	subject(13, `["PC"]`, `[{"name":"黄油","count":5}]`)
	eroge := work("Eroge")
	require.NoError(t, facetTestDB.Model(&model.CatalogWork{}).Where("id = ?", eroge).Update("content_rating", model.ContentRatingR18).Error)

	rpg := work("RPG")
	ref(model.EntityTypeWork, rpg, "bangumi", "21")
	subject(21, `["RPG","PC"]`, `[{"name":"galgame","count":2}]`)
	adventure := work("Adventure")
	ref(model.EntityTypeWork, adventure, "bangumi", "22")
	subject(22, `["ADV","PS4"]`, `[]`)
	matureOnSteam := work("MatureOnSteam")
	require.NoError(t, facetTestDB.Model(&model.CatalogWork{}).Where("id = ?", matureOnSteam).Update("content_rating", model.ContentRatingR18).Error)
	ref(model.EntityTypeWork, matureOnSteam, "steam", "1174180")
	bare := work("Bare")
	dead := work("DeadVNDBLink")
	require.NoError(t, facetTestDB.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeWork, EntityID: dead, SourceID: source("vndb"), ExternalID: "v9", MatchedBy: "test",
		DeadAt: func() *time.Time { n := time.Now(); return &n }(),
	}).Error)

	got, err := loadNonVNWorks(facetTestDB, nil)
	require.NoError(t, err)
	require.Equal(t, map[int64]bool{rpg: true, adventure: true, matureOnSteam: true, bare: true, dead: true}, got)

	got, err = loadNonVNWorks(facetTestDB, scope{vndb, rpg})
	require.NoError(t, err)
	require.Equal(t, map[int64]bool{rpg: true}, got)
}
