package main

import (
	"os"
	"testing"
	"time"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	"api/internal/testsupport/dbtest"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDB *gorm.DB

// The forum database lives on the same server and the importer holds it as a
// second handle, so the suite passes one handle twice and creates the forum
// tables alongside the catalog schema. Neither name exists in kun_catalog, so
// nothing here can shadow a real table.
func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("cmd/import-favorites")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("cmd/import-favorites", "cannot connect to test database: %v", err)
	}
	if err := migrate.Run(db); err != nil {
		dbtest.SkipMainf("cmd/import-favorites", "catalog migration failed: %v", err)
	}
	// The fixture needs catalog_medium, which migrate.Run does not write.
	// Without this the suite only passed against a database that already held
	// real catalog data.
	if err := seed.Run(db); err != nil {
		dbtest.SkipMainf("cmd/import-favorites", "catalog seed failed: %v", err)
	}
	testDB = db
	os.Exit(m.Run())
}

const sourceSchema = `
DROP TABLE IF EXISTS galgame_collection_item, galgame_collection, galgame_favorite,
                     user_patch_favorite_relation, patch CASCADE;
CREATE TABLE galgame_collection (
  id bigserial PRIMARY KEY, user_id integer NOT NULL, name varchar(60) NOT NULL,
  description varchar(500) NOT NULL DEFAULT '', visibility varchar(16) NOT NULL DEFAULT 'public',
  is_default boolean NOT NULL DEFAULT false, item_count integer NOT NULL DEFAULT 0,
  created timestamptz NOT NULL DEFAULT now(), updated timestamptz NOT NULL DEFAULT now());
CREATE TABLE galgame_collection_item (
  id bigserial PRIMARY KEY, collection_id bigint NOT NULL, galgame_id integer NOT NULL,
  user_id integer NOT NULL, created timestamptz NOT NULL DEFAULT now(),
  updated timestamptz NOT NULL DEFAULT now());
`

func at(day int) time.Time {
	return time.Date(2024, 3, day, 12, 0, 0, 0, time.UTC)
}

type fixture struct {
	live, merged, survivor int64
}

func seedFixture(t *testing.T) fixture {
	t.Helper()
	require.NoError(t, testDB.Exec(sourceSchema).Error)
	require.NoError(t, testDB.Exec(`TRUNCATE catalog_user_folder_item, catalog_user_folder,
		catalog_user_folder_import RESTART IDENTITY CASCADE`).Error)

	var fx fixture
	newWork := func(status int16) int64 {
		w := model.CatalogWork{Status: status, DisplayName: "importer fixture", MediumID: 1, OLang: "ja"}
		require.NoError(t, testDB.Create(&w).Error)
		return w.ID
	}
	fx.live = newWork(model.WorkStatusLive)
	fx.survivor = newWork(model.WorkStatusLive)
	fx.merged = newWork(model.WorkStatusMerged)
	require.NoError(t, testDB.Exec(
		`INSERT INTO catalog_redirect (entity_type, old_id, current_id) VALUES (?, ?, ?)`,
		entityTypeWork, fx.merged, fx.survivor).Error)
	return fx
}

func loadedImporter(t *testing.T, apply bool) *importer {
	t.Helper()
	imp := newImporter(testDB, testDB, apply, 2)
	require.NoError(t, imp.loadState())
	return imp
}

func TestImportForumCollectionsPreservesTimestampsAndFollowsMerges(t *testing.T) {
	fx := seedFixture(t)
	require.NoError(t, testDB.Exec(`
		INSERT INTO galgame_collection (id, user_id, name, description, visibility, is_default, created, updated)
		VALUES (1, 7, '', '', 'public', true, ?, ?), (2, 7, '二周目', 'note', 'private', false, ?, ?)`,
		at(1), at(2), at(3), at(4)).Error)
	require.NoError(t, testDB.Exec(`
		INSERT INTO galgame_collection_item (collection_id, galgame_id, user_id, created, updated)
		VALUES (1, ?, 7, ?, ?), (2, ?, 7, ?, ?)`,
		fx.live, at(5), at(6), fx.merged, at(7), at(8)).Error)

	imp := loadedImporter(t, true)
	c, err := imp.importForumCollections()
	require.NoError(t, err)
	require.Equal(t, 2, c.FoldersCreated)
	require.Equal(t, 2, c.ItemsInserted)
	require.Equal(t, 1, c.Redirected, "the merged work must arrive through catalog_redirect")
	require.Zero(t, c.SkippedNotLive)

	var folders []model.CatalogUserFolder
	require.NoError(t, testDB.Order("id").Find(&folders).Error)
	require.Len(t, folders, 2)
	require.Empty(t, folders[0].Name, "an unnamed forum default stays unnamed")
	require.True(t, folders[0].IsDefault)
	require.Equal(t, at(1).UTC(), folders[0].CreatedAt.UTC(), "the folder keeps its source created date")
	require.Equal(t, model.FolderVisibilityPublic, folders[0].Visibility)
	require.Equal(t, model.FolderVisibilityPrivate, folders[1].Visibility)

	var items []model.CatalogUserFolderItem
	require.NoError(t, testDB.Order("folder_id").Find(&items).Error)
	require.Len(t, items, 2)
	require.Equal(t, at(5).UTC(), items[0].CreatedAt.UTC())
	require.Equal(t, fx.survivor, items[1].WorkID, "a merged work lands on its survivor")

	require.NoError(t, imp.recountItems())
	require.NoError(t, testDB.Order("id").Find(&folders).Error)
	require.Equal(t, 1, folders[0].ItemCount)
	require.Equal(t, 1, folders[1].ItemCount)

	// Re-running must reuse both folders and write no second copy.
	again := loadedImporter(t, true)
	c2, err := again.importForumCollections()
	require.NoError(t, err)
	require.Equal(t, 2, c2.FoldersReused)
	require.Zero(t, c2.FoldersCreated)
	require.Zero(t, c2.ItemsInserted)
	require.Equal(t, 2, c2.ItemsMerged)
	var n int64
	require.NoError(t, testDB.Model(&model.CatalogUserFolder{}).Count(&n).Error)
	require.Equal(t, int64(2), n)
}

// The 2026-09-07 runs created unnamed default folders that never came from a
// forum collection (the since-removed flat lanes minted them), and those
// folders persist. A forum default collection for such a user must adopt the
// existing default rather than land a second one beside it — nothing in the
// schema keeps a user to one default folder.
func TestForumDefaultAdoptsAPreexistingDefaultFolder(t *testing.T) {
	seedFixture(t)
	preexisting := model.CatalogUserFolder{OwnerUID: 4, Visibility: model.FolderVisibilityPrivate, IsDefault: true}
	require.NoError(t, testDB.Create(&preexisting).Error)
	require.NoError(t, testDB.Exec(`
		INSERT INTO galgame_collection (id, user_id, name, description, visibility, is_default, created, updated)
		VALUES (1, 4, '我的收藏', '', 'private', true, ?, ?)`, at(2), at(3)).Error)

	imp := loadedImporter(t, true)
	c, err := imp.importForumCollections()
	require.NoError(t, err)
	require.Equal(t, 1, c.FoldersReused)
	require.Zero(t, c.FoldersCreated)

	var folders []model.CatalogUserFolder
	require.NoError(t, testDB.Where("owner_uid = ?", 4).Find(&folders).Error)
	require.Len(t, folders, 1, "a user must never end with two default folders")
	require.Equal(t, "我的收藏", folders[0].Name, "the adopted folder takes the source name")
	require.Equal(t, at(2).UTC(), folders[0].CreatedAt.UTC())

	var prov []model.CatalogUserFolderImport
	require.NoError(t, testDB.Find(&prov).Error)
	require.Len(t, prov, 1)
	require.Equal(t, folders[0].ID, prov[0].FolderID)
}

func TestDryRunWritesNothing(t *testing.T) {
	fx := seedFixture(t)
	require.NoError(t, testDB.Exec(`
		INSERT INTO galgame_collection (id, user_id, name, visibility, is_default, created, updated)
		VALUES (1, 3, 'x', 'public', true, ?, ?)`, at(1), at(1)).Error)
	require.NoError(t, testDB.Exec(`
		INSERT INTO galgame_collection_item (collection_id, galgame_id, user_id, created, updated)
		VALUES (1, ?, 3, ?, ?)`, fx.live, at(1), at(1)).Error)

	imp := loadedImporter(t, false)
	c, err := imp.importForumCollections()
	require.NoError(t, err)
	require.Equal(t, 1, c.FoldersCreated, "a dry run still reports what it would create")
	require.Equal(t, 1, c.ItemsInserted)

	var n int64
	require.NoError(t, testDB.Model(&model.CatalogUserFolder{}).Count(&n).Error)
	require.Zero(t, n)
	require.NoError(t, testDB.Model(&model.CatalogUserFolderItem{}).Count(&n).Error)
	require.Zero(t, n)
}

// Two source items whose galgame ids merged into one survivor resolve to the
// same (folder, work) pair; with both in one batch Postgres would abort the
// statement with "ON CONFLICT DO UPDATE command cannot affect row a second
// time" — how the since-removed moyu lane died on its first 2026-09-07
// production flush. The batch fold has to keep absorbing the duplicate.
func TestOneBatchNeverCarriesAPairTwice(t *testing.T) {
	fx := seedFixture(t)
	require.NoError(t, testDB.Exec(`
		INSERT INTO galgame_collection (id, user_id, name, visibility, is_default, created, updated)
		VALUES (1, 11, '', 'public', true, ?, ?)`, at(1), at(1)).Error)
	require.NoError(t, testDB.Exec(`
		INSERT INTO galgame_collection_item (collection_id, galgame_id, user_id, created, updated)
		VALUES (1, ?, 11, ?, ?), (1, ?, 11, ?, ?)`,
		fx.merged, at(5), at(6), fx.survivor, at(2), at(9)).Error)

	imp := loadedImporter(t, true)
	c, err := imp.importForumCollections()
	require.NoError(t, err)
	require.Equal(t, 1, c.ItemsInserted)
	require.Equal(t, 1, c.ItemsMerged)

	var items []model.CatalogUserFolderItem
	require.NoError(t, testDB.Where("work_id = ?", fx.survivor).Find(&items).Error)
	require.Len(t, items, 1)
	require.Equal(t, at(2).UTC(), items[0].CreatedAt.UTC(), "folding keeps the earliest created")
	require.Equal(t, at(9).UTC(), items[0].UpdatedAt.UTC(), "folding keeps the latest updated")
}
