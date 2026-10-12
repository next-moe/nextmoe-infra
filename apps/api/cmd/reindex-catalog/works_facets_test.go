package main

import (
	"context"
	"fmt"
	"os"
	"testing"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	"api/internal/testsupport/dbtest"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	glogger "gorm.io/gorm/logger"
)

var facetTestDB *gorm.DB

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("reindex-catalog suite")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: glogger.Default.LogMode(glogger.Silent)})
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL: cannot connect to the assigned test database")
		os.Exit(1)
	}
	if err := migrate.Run(db); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: catalog migration failed: %v\n", err)
		os.Exit(1)
	}
	if err := seed.Run(db); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: catalog seeding failed: %v\n", err)
		os.Exit(1)
	}
	facetTestDB = db
	release := acquireSuiteLock(db)
	code := m.Run()
	release()
	if err := assertRetiredCatalogTablesAbsent(db); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: retired catalog table gate: %v\n", err)
		code = 1
	}
	os.Exit(code)
}

var retiredCatalogTableNames = []string{
	"galgame_series",
	"galgame_tag",
	"galgame_tag_alias",
	"galgame_official",
	"galgame_official_alias",
	"galgame_engine",
	"galgame",
	"galgame_tag_edge",
	"galgame_alias",
	"galgame_tag_relation",
	"galgame_official_relation",
	"galgame_engine_relation",
	"galgame_link",
	"galgame_cover",
	"galgame_screenshot",
	"galgame_pr",
	"galgame_revision",
	"taxonomy_revision",
	"galgame_history",
	"galgame_contributor",
	"galgame_migrations",
	"galgame_message",
	"galgame_bangumi_meta",
	"galgame_vndb_meta",
	"galgame_eg_meta",
	"galgame_dlsite_meta",
	"galgame_stats",
}

func assertRetiredCatalogTablesAbsent(db *gorm.DB) error {
	var tables []string
	if err := db.Raw(`SELECT table_name FROM information_schema.tables
		WHERE table_schema = current_schema() AND table_name IN ?
		ORDER BY table_name`, retiredCatalogTableNames).Scan(&tables).Error; err != nil {
		return err
	}
	if len(tables) != 0 {
		return fmt.Errorf("unexpected tables: %v", tables)
	}
	return nil
}

const facetSuiteLockKey int64 = 0x65647473

func acquireSuiteLock(db *gorm.DB) func() {
	sqlDB, err := db.DB()
	if err != nil {
		return func() {}
	}
	ctx := context.Background()
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return func() {}
	}
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", facetSuiteLockKey); err != nil {
		_ = conn.Close()
		return func() {}
	}
	return func() {
		_, _ = conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", facetSuiteLockKey)
		_ = conn.Close()
	}
}

const galgameMedium int16 = 1

func truncateFacetTables(t *testing.T) {
	t.Helper()
	for _, tbl := range []string{
		"catalog_work_title", "catalog_work_intro",
		"catalog_work_tag", "catalog_tag_source_map", "catalog_tag",
		"catalog_work_label", "catalog_label", "catalog_work_engine", "catalog_engine",
		"catalog_series_member", "catalog_series", "catalog_release", "catalog_work",
	} {
		if err := facetTestDB.Exec("TRUNCATE " + tbl + " RESTART IDENTITY CASCADE").Error; err != nil {
			t.Fatalf("truncate %s: %v", tbl, err)
		}
	}
}

func mkWork(t *testing.T, name string, status int16, medium int16) int64 {
	t.Helper()
	w := &model.CatalogWork{
		MediumID: medium, OLang: "ja", DisplayName: name,
		ContentRating: model.ContentRatingAllAges, Status: status,
	}
	if err := facetTestDB.Create(w).Error; err != nil {
		t.Fatalf("create work %s: %v", name, err)
	}
	return w.ID
}

func TestWorksFacetLoadersScopeAndResolve(t *testing.T) {
	truncateFacetTables(t)

	live := mkWork(t, "LiveGalgame", model.WorkStatusLive, galgameMedium)
	stub := mkWork(t, "StubGalgame", model.WorkStatusStub, galgameMedium)
	asmr := mkWork(t, "AsmrWork", model.WorkStatusLive, 5)
	deleted := mkWork(t, "DeletedGalgame", model.WorkStatusLive, galgameMedium)
	if err := facetTestDB.Exec(`UPDATE catalog_work SET deleted_at = now() WHERE id = ?`, deleted).Error; err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	outOfScope := []int64{stub, asmr, deleted}

	tag := &model.CatalogTag{Name: "純愛", Tier: model.TagTierCore, Kind: model.TagKindContent}
	if err := facetTestDB.Create(tag).Error; err != nil {
		t.Fatalf("create tag: %v", err)
	}
	if err := facetTestDB.Create(&model.CatalogTagSourceMap{
		SourceID: 3, SourceName: "純愛", TagID: tag.ID,
	}).Error; err != nil {
		t.Fatalf("create tag map: %v", err)
	}
	for _, id := range append([]int64{live}, outOfScope...) {
		if err := facetTestDB.Create(&model.CatalogWorkTag{
			WorkID: id, Name: "純愛", Count: 1, SourceID: 3,
		}).Error; err != nil {
			t.Fatalf("attach mapped tag to %d: %v", id, err)
		}
	}
	if err := facetTestDB.Create(&model.CatalogWorkTag{
		WorkID: live, Name: "未マッピング", Count: 1, SourceID: 3,
	}).Error; err != nil {
		t.Fatalf("attach unmapped tag: %v", err)
	}

	label := &model.CatalogLabel{DisplayName: "Favorite", Lang: "en", Kind: model.LabelKindGameBrand}
	if err := facetTestDB.Create(label).Error; err != nil {
		t.Fatalf("create label: %v", err)
	}
	engine := &model.CatalogEngine{Name: "KiriKiri", Aliases: []byte("[]")}
	if err := facetTestDB.Create(engine).Error; err != nil {
		t.Fatalf("create engine: %v", err)
	}
	series := &model.CatalogSeries{DisplayName: "Sekai", SourceID: 4, ExternalID: "SRI0001"}
	if err := facetTestDB.Create(series).Error; err != nil {
		t.Fatalf("create series: %v", err)
	}
	for _, id := range append([]int64{live}, outOfScope...) {
		if err := facetTestDB.Create(&model.CatalogWorkLabel{
			WorkID: id, LabelID: label.ID, Kind: model.WorkLabelKindBrand,
		}).Error; err != nil {
			t.Fatalf("attach label to %d: %v", id, err)
		}
		if err := facetTestDB.Create(&model.CatalogWorkEngine{
			WorkID: id, EngineID: engine.ID, SourceID: 2,
		}).Error; err != nil {
			t.Fatalf("attach engine to %d: %v", id, err)
		}
		if err := facetTestDB.Create(&model.CatalogSeriesMember{
			WorkID: id, SeriesID: series.ID,
		}).Error; err != nil {
			t.Fatalf("attach series to %d: %v", id, err)
		}
	}

	mkRelease(t, live, 2024, 12, 25)
	mkRelease(t, live, 2020, 6, 0)
	mkRelease(t, live, 0, 0, 0)
	for _, id := range outOfScope {
		mkRelease(t, id, 1999, 1, 1)
	}

	facets, err := loadWorksFacets(facetTestDB)
	if err != nil {
		t.Fatalf("loadWorksFacets: %v", err)
	}

	if got := facets.tagIDs[live]; len(got) != 1 || got[0] != tag.ID {
		t.Fatalf("tag ids = %v, want [%d] — canonical only, the unmapped source tag must vanish", got, tag.ID)
	}
	if got := facets.labelIDs[live]; len(got) != 1 || got[0] != label.ID {
		t.Fatalf("label ids = %v, want [%d]", got, label.ID)
	}
	if got := facets.engineIDs[live]; len(got) != 1 || got[0] != engine.ID {
		t.Fatalf("engine ids = %v, want [%d]", got, engine.ID)
	}
	if got := facets.seriesIDs[live]; len(got) != 1 || got[0] != series.ID {
		t.Fatalf("series ids = %v, want [%d]", got, series.ID)
	}
	if got := facets.releasedOrd[live]; got != 20200600 {
		t.Fatalf("released ordinal = %d, want 20200600 (earliest DATED release, month precision)", got)
	}

	for _, id := range outOfScope {
		if len(facets.tagIDs[id])+len(facets.labelIDs[id])+len(facets.engineIDs[id])+len(facets.seriesIDs[id]) != 0 {
			t.Fatalf("out-of-population work %d produced facet rows", id)
		}
		if facets.releasedOrd[id] != 0 {
			t.Fatalf("out-of-population work %d produced a release ordinal", id)
		}
	}

	bare := mkWork(t, "Undated", model.WorkStatusLive, galgameMedium)
	mkRelease(t, bare, 0, 0, 0)
	facets, err = loadWorksFacets(facetTestDB)
	if err != nil {
		t.Fatalf("reload facets: %v", err)
	}
	if _, present := facets.releasedOrd[bare]; present {
		t.Fatalf("undated work %d must have NO release ordinal entry", bare)
	}

	counts, err := loadEdgeWorkCounts(facetTestDB, reindexTagEdge)
	if err != nil {
		t.Fatalf("tag work counts: %v", err)
	}
	if counts[tag.ID] != 1 {
		t.Fatalf("tag work count = %d, want 1 (population-scoped, DISTINCT)", counts[tag.ID])
	}
	counts, err = loadEdgeWorkCounts(facetTestDB, reindexLabelEdge)
	if err != nil {
		t.Fatalf("label work counts: %v", err)
	}
	if counts[label.ID] != 1 {
		t.Fatalf("label work count = %d, want 1 (population-scoped, DISTINCT)", counts[label.ID])
	}
}

func mkRelease(t *testing.T, workID int64, y, m, d int16) {
	t.Helper()
	r := &model.CatalogRelease{WorkID: workID, Kind: model.ReleaseKindDefault}
	if y != 0 {
		r.ReleasedY = &y
	}
	if m != 0 {
		r.ReleasedM = &m
	}
	if d != 0 {
		r.ReleasedD = &d
	}
	if err := facetTestDB.Create(r).Error; err != nil {
		t.Fatalf("create release: %v", err)
	}
}
