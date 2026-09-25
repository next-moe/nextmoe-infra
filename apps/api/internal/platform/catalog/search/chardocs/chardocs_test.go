package chardocs

import (
	"math"
	"os"
	"reflect"
	"testing"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	"api/internal/testsupport/dbtest"

	"gorm.io/datatypes"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	glogger "gorm.io/gorm/logger"
)

var testDB *gorm.DB

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("catalog/search/chardocs")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: glogger.Default.LogMode(glogger.Silent)})
	if err != nil {
		dbtest.SkipMainf("catalog/search/chardocs", "cannot connect to test database: %v", err)
	}
	if err := migrate.Run(db); err != nil {
		dbtest.SkipMainf("catalog/search/chardocs", "catalog migration failed: %v", err)
	}
	if err := seed.Run(db); err != nil {
		dbtest.SkipMainf("catalog/search/chardocs", "catalog seeding failed: %v", err)
	}
	testDB = db
	os.Exit(m.Run())
}

func TestBuildTraitClosureGenderPopularity(t *testing.T) {
	empty := datatypes.JSON([]byte("{}"))
	for _, table := range []string{
		"catalog_character_trait_link", "catalog_character_trait_parent",
		"catalog_work_character", "catalog_work_popularity",
		"catalog_character_trait", "catalog_character", "catalog_work",
	} {
		if err := testDB.Exec("TRUNCATE " + table + " RESTART IDENTITY CASCADE").Error; err != nil {
			t.Fatalf("truncate %s: %v", table, err)
		}
	}

	trait := func(tid, name string, sexual bool) *model.CatalogCharacterTrait {
		t.Helper()
		row := &model.CatalogCharacterTrait{
			VndbTID: tid, Name: name, GroupTID: "i89000", Alias: "", Description: "",
			Sexual: sexual, Searchable: true, Applicable: true,
		}
		if err := testDB.Create(row).Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	parent := func(child, par int64) {
		t.Helper()
		if err := testDB.Create(&model.CatalogCharacterTraitParent{TraitID: child, ParentID: par}).Error; err != nil {
			t.Fatal(err)
		}
	}
	p := trait("i89001", "P", false)
	c := trait("i89002", "C", false)
	g := trait("i89003", "G", false)
	a := trait("i89004", "A", false)
	b := trait("i89005", "B", false)
	mp := trait("i89006", "M", false)
	s := trait("i89007", "S", true)
	q := trait("i89008", "Q", false)
	parent(c.ID, p.ID)
	parent(g.ID, c.ID)
	parent(mp.ID, a.ID)
	parent(mp.ID, b.ID)
	if err := testDB.Exec(model.TraitSexualFamilySQL).Error; err != nil {
		t.Fatal(err)
	}

	female := model.GenderFemale
	ch := &model.CatalogCharacter{
		DisplayName: "chardocs", Lang: "ja", Gender: &female,
		Extra: empty, FieldProvenance: empty,
	}
	if err := testDB.Create(ch).Error; err != nil {
		t.Fatal(err)
	}
	link := func(traitID int64, spoiler int16) {
		t.Helper()
		if err := testDB.Create(&model.CatalogCharacterTraitLink{
			CharacterID: ch.ID, TraitID: traitID, SpoilerLevel: spoiler,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	link(g.ID, model.SpoilerNone)
	link(mp.ID, model.SpoilerNone)
	link(s.ID, model.SpoilerNone)
	link(q.ID, model.SpoilerNone)
	link(p.ID, model.SpoilerMild)

	wMain := &model.CatalogWork{
		MediumID: 1, OLang: "ja", DisplayName: "main-pop",
		ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive,
		Extra: empty, FieldProvenance: empty,
	}
	wOther := &model.CatalogWork{
		MediumID: 1, OLang: "ja", DisplayName: "other-pop",
		ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive,
		Extra: empty, FieldProvenance: empty,
	}
	if err := testDB.Create(wMain).Error; err != nil {
		t.Fatal(err)
	}
	if err := testDB.Create(wOther).Error; err != nil {
		t.Fatal(err)
	}
	if err := testDB.Create(&model.CatalogWorkPopularity{
		WorkID: wMain.ID, SourceID: 2, Metric: model.PopularityMetricBgmCollect, Value: 100,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := testDB.Create(&model.CatalogWorkPopularity{
		WorkID: wOther.ID, SourceID: 2, Metric: model.PopularityMetricDownloads, Value: 50,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := testDB.Create(&model.CatalogWorkCharacter{
		WorkID: wMain.ID, CharacterID: ch.ID, Kind: model.WorkCharacterKindMain, Spoiler: model.SpoilerNone,
		MatchedBy: "import:test", FieldProvenance: empty,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := testDB.Create(&model.CatalogWorkCharacter{
		WorkID: wOther.ID, CharacterID: ch.ID, Kind: model.WorkCharacterKindSecondary, Spoiler: model.SpoilerNone,
		MatchedBy: "import:test", FieldProvenance: empty,
	}).Error; err != nil {
		t.Fatal(err)
	}

	ctx := t.Context()
	loaded, err := Load(ctx, testDB)
	if err != nil {
		t.Fatal(err)
	}
	docs, err := loaded.Build(ctx, testDB, []Row{{
		ID: ch.ID, DisplayName: ch.DisplayName, Lang: ch.Lang, Gender: ch.Gender,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 {
		t.Fatalf("docs %d", len(docs))
	}
	d := docs[0]
	if d.CatalogID != ch.ID {
		t.Fatalf("catalog_id %d", d.CatalogID)
	}
	if d.Gender == nil || *d.Gender != model.GenderFemale {
		t.Fatalf("gender %+v", d.Gender)
	}
	wantAll := []int64{p.ID, c.ID, g.ID, a.ID, b.ID, mp.ID, s.ID, q.ID}
	if !reflect.DeepEqual(d.TraitIDs, sortedIDs(setOf(wantAll))) {
		t.Fatalf("trait_ids %v want %v", d.TraitIDs, sortedIDs(setOf(wantAll)))
	}
	wantSFW := []int64{p.ID, c.ID, g.ID, a.ID, b.ID, mp.ID, q.ID}
	if !reflect.DeepEqual(d.TraitIDsSFW, sortedIDs(setOf(wantSFW))) {
		t.Fatalf("trait_ids_sfw %v want %v (sexual link excluded, spoiler-1 excluded)", d.TraitIDsSFW, sortedIDs(setOf(wantSFW)))
	}
	if containsID(d.TraitIDsSFW, s.ID) {
		t.Fatal("sexual_family trait must not appear in trait_ids_sfw")
	}
	wantPop := math.Log1p(125)
	if d.Popularity != wantPop {
		t.Fatalf("popularity %v want log1p(125)=%v", d.Popularity, wantPop)
	}
}

func setOf(ids []int64) map[int64]struct{} {
	m := map[int64]struct{}{}
	for _, id := range ids {
		m[id] = struct{}{}
	}
	return m
}

func containsID(ids []int64, want int64) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
