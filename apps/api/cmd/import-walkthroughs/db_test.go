package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	"api/internal/platform/catalog/walkthrough"
	"api/internal/testsupport/dbtest"

	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDB *gorm.DB

func TestMain(m *testing.M) {
	if dsn, ok := dbtest.DSN(); ok {
		db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			dbtest.SkipMainf("import-walkthroughs", "connect: %v", err)
		}
		for _, step := range []func(*gorm.DB) error{migrate.Run, seed.Run, func(db *gorm.DB) error {
			return ensureStage(context.Background(), db)
		}} {
			if err := step(db); err != nil {
				dbtest.SkipMainf("import-walkthroughs", "setup: %v", err)
			}
		}
		testDB = db
	}
	os.Exit(m.Run())
}

func freshDB(t *testing.T) *gorm.DB {
	t.Helper()
	if testDB == nil {
		dbtest.Skip(t)
	}
	for _, table := range []string{
		"catalog_walkthrough_character", "catalog_walkthrough", "catalog_work_character", "catalog_character_alias",
		"catalog_character", "catalog_revision", "catalog_external_ref", "catalog_release", "catalog_work_title", "catalog_work",
		"wt_stage.extraction", "wt_stage.translation", "wt_stage.match",
	} {
		require.NoError(t, testDB.Exec("TRUNCATE "+table+" RESTART IDENTITY CASCADE").Error)
	}
	return testDB
}

func mkWork(t *testing.T, db *gorm.DB, medium, title string) int64 {
	t.Helper()
	var mediumID int16
	require.NoError(t, db.Raw(`SELECT id FROM catalog_medium WHERE key = ?`, medium).Scan(&mediumID).Error)
	require.NotZero(t, mediumID, medium)
	w := &model.CatalogWork{
		MediumID: mediumID, OLang: "ja", DisplayName: title,
		ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive,
	}
	require.NoError(t, db.Create(w).Error)
	require.NoError(t, db.Create(&model.CatalogWorkTitle{
		WorkID: w.ID, Lang: "ja", Title: title, Kind: model.WorkTitleKindOfficial,
	}).Error)
	return w.ID
}

func mkDLsiteRef(t *testing.T, db *gorm.DB, workID int64, code string) {
	t.Helper()
	require.NoError(t, db.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeWork, EntityID: workID, SourceID: sourceDLsite,
		ExternalID: code, LinkKind: model.LinkKindExact, MatchedBy: "test",
	}).Error)
}

func mkCharacter(t *testing.T, db *gorm.DB, workID int64, name, matchedBy string) int64 {
	t.Helper()
	c := &model.CatalogCharacter{DisplayName: name, Lang: "ja", FieldProvenance: datatypes.JSON(`{}`)}
	require.NoError(t, db.Create(c).Error)
	require.NoError(t, db.Exec(`INSERT INTO catalog_work_character (work_id, character_id, kind, spoiler, matched_by, created_at, updated_at)
VALUES (?, ?, 0, 0, ?, now(), now())`, workID, c.ID, matchedBy).Error)
	return c.ID
}

func chartOf(chars ...walkthrough.Character) walkthrough.Chart {
	return walkthrough.Chart{
		Characters: chars, Sections: []walkthrough.Section{}, Legend: []walkthrough.Legend{}, Entries: []walkthrough.Entry{},
		Saves: []walkthrough.Save{}, Endings: []walkthrough.Ending{}, Segments: []walkthrough.Segment{},
	}
}

func mkWalkthrough(t *testing.T, db *gorm.DB, workID int64, url string, c walkthrough.Chart, prov string) int64 {
	t.Helper()
	raw, err := json.Marshal(c)
	require.NoError(t, err)
	w := &model.CatalogWalkthrough{
		WorkID: workID, SourceID: model.SourceSeiya, SourceURL: url, ChartVersion: walkthrough.ChartVersion,
		Chart: datatypes.JSON(raw), SourceHash: "h", ExtractModel: "test", FieldProvenance: datatypes.JSON(prov),
	}
	require.NoError(t, db.Create(w).Error)
	return w.ID
}

func mkLink(t *testing.T, db *gorm.DB, walkthroughID int64, key string, characterID int64, matchedBy string) {
	t.Helper()
	require.NoError(t, db.Create(&model.CatalogWalkthroughCharacter{
		WalkthroughID: walkthroughID, ChartKey: key, CharacterID: characterID, MatchedBy: matchedBy,
	}).Error)
}

func linksOf(t *testing.T, db *gorm.DB, walkthroughID int64) map[string]model.CatalogWalkthroughCharacter {
	t.Helper()
	var rows []model.CatalogWalkthroughCharacter
	require.NoError(t, db.Where("walkthrough_id = ?", walkthroughID).Find(&rows).Error)
	out := map[string]model.CatalogWalkthroughCharacter{}
	for _, r := range rows {
		out[r.ChartKey] = r
	}
	return out
}

func stageEntry(t *testing.T, db *gorm.DB, entryID, workID int64, c walkthrough.Chart, pins map[string]characterPin) {
	t.Helper()
	raw, err := json.Marshal(c)
	require.NoError(t, err)
	require.NoError(t, db.Exec(`INSERT INTO wt_stage.extraction (entry_id, prompt_version, model, input_hash, verdict, chart)
VALUES (?, 'v3', 'test', 'h', 'ok', ?::jsonb)`, entryID, string(raw)).Error)
	require.NoError(t, storeMatch(context.Background(), db, matchResult{
		EntryID: entryID, WorkID: &workID, Method: matchTitle, Characters: pins,
	}))
}
