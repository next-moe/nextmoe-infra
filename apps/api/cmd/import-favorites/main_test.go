package main

import (
	"fmt"
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

// The three source databases live on one server and the importer holds them as
// three handles, so the suite passes one handle three times and creates the
// forum and moyu tables alongside the catalog schema. Neither name exists in
// kun_catalog, so nothing here can shadow a real table.
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
	// The fixture needs catalog_medium and the vndb catalog_source row, neither
	// of which migrate.Run writes. Without this the suite only passed against a
	// database that already held real catalog data.
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
CREATE TABLE galgame_favorite (
  id serial PRIMARY KEY, galgame_id integer NOT NULL, user_id integer NOT NULL,
  created timestamptz NOT NULL DEFAULT now(), updated timestamptz NOT NULL DEFAULT now());
CREATE TABLE patch (id serial PRIMARY KEY, vndb_id varchar(107) NOT NULL);
CREATE TABLE user_patch_favorite_relation (
  id serial PRIMARY KEY, user_id integer NOT NULL, galgame_id integer NOT NULL,
  created timestamptz NOT NULL DEFAULT now(), updated timestamptz NOT NULL DEFAULT now());
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
	require.NoError(t, testDB.Exec(`DELETE FROM catalog_external_ref WHERE external_id LIKE 'vtest%'`).Error)

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

	var vndbSource int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_source WHERE key = 'vndb'`).Scan(&vndbSource).Error)
	require.NotZero(t, vndbSource, "the vndb source registry row must be seeded")
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_external_ref
		(entity_type, entity_id, source_id, external_id, link_kind, matched_by)
		VALUES (?, ?, ?, 'vtest1', 0, 'test')`, entityTypeWork, fx.live, vndbSource).Error)
	return fx
}

func loadedImporter(t *testing.T, apply bool) *importer {
	t.Helper()
	imp := newImporter(testDB, testDB, testDB, apply, 2)
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

func TestImportFlatLanesShareOneDefaultFolderAndTakeTheEarliestDate(t *testing.T) {
	fx := seedFixture(t)
	// The same work favourited on both sites, moyu first and earlier.
	require.NoError(t, testDB.Exec(`INSERT INTO patch (id, vndb_id) VALUES (1, 'vtest1'), (2, 'pending-9'), (3, ?)`,
		fmt.Sprintf("wiki-%d", fx.survivor)).Error)
	require.NoError(t, testDB.Exec(`
		INSERT INTO user_patch_favorite_relation (user_id, galgame_id, created, updated)
		VALUES (9, 1, ?, ?), (9, 2, ?, ?), (9, 3, ?, ?)`,
		at(1), at(2), at(1), at(1), at(1), at(1)).Error)
	require.NoError(t, testDB.Exec(`
		INSERT INTO galgame_favorite (galgame_id, user_id, created, updated)
		VALUES (?, 9, ?, ?)`, fx.live, at(5), at(9)).Error)

	imp := loadedImporter(t, true)
	moyu, err := imp.importMoyu()
	require.NoError(t, err)
	require.Equal(t, 1, moyu.FoldersCreated, "a moyu-only user gets one unnamed default folder")
	require.Equal(t, 2, moyu.ItemsInserted)
	require.Equal(t, 1, moyu.SkippedPending, "a pending- placeholder is not a work")
	require.Zero(t, moyu.SkippedNoAnchor)

	flat, err := imp.importForumFlat()
	require.NoError(t, err)
	require.Zero(t, flat.FoldersCreated, "the second lane reuses the default folder")
	require.Equal(t, 1, flat.ItemsMerged, "the cross-site duplicate is one membership, not two")

	var items []model.CatalogUserFolderItem
	require.NoError(t, testDB.Where("work_id = ?", fx.live).Find(&items).Error)
	require.Len(t, items, 1)
	require.Equal(t, at(1).UTC(), items[0].CreatedAt.UTC(), "earliest created wins")
	require.Equal(t, at(9).UTC(), items[0].UpdatedAt.UTC(), "the sync watermark never travels backwards")

	var folders []model.CatalogUserFolder
	require.NoError(t, testDB.Find(&folders).Error)
	require.Len(t, folders, 1)
	require.True(t, folders[0].IsDefault)
}

// A flat lane running before the collection lane creates the default folder
// first. Nothing in the schema stops a second default from landing beside it.
func TestForumDefaultAdoptsAFolderAFlatLaneAlreadyCreated(t *testing.T) {
	fx := seedFixture(t)
	require.NoError(t, testDB.Exec(`INSERT INTO patch (id, vndb_id) VALUES (1, 'vtest1')`).Error)
	require.NoError(t, testDB.Exec(`
		INSERT INTO user_patch_favorite_relation (user_id, galgame_id, created, updated)
		VALUES (4, 1, ?, ?)`, at(1), at(1)).Error)
	require.NoError(t, testDB.Exec(`
		INSERT INTO galgame_collection (id, user_id, name, description, visibility, is_default, created, updated)
		VALUES (1, 4, '我的收藏', '', 'private', true, ?, ?)`, at(2), at(3)).Error)

	imp := loadedImporter(t, true)
	_, err := imp.importMoyu()
	require.NoError(t, err)
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
	require.EqualValues(t, fx.live, fx.live)
}

// A flat favorite of a work the owner already filed in a named folder must not
// appear a second time in their default folder — it merges into the folder that
// holds it, and drags created_at back if this site favourited it first.
func TestFlatFavoriteMergesIntoTheFolderThatAlreadyHoldsTheWork(t *testing.T) {
	fx := seedFixture(t)
	require.NoError(t, testDB.Exec(`
		INSERT INTO galgame_collection (id, user_id, name, visibility, is_default, created, updated)
		VALUES (1, 5, '', 'public', true, ?, ?), (2, 5, '二周目', 'private', false, ?, ?)`,
		at(1), at(1), at(1), at(1)).Error)
	require.NoError(t, testDB.Exec(`
		INSERT INTO galgame_collection_item (collection_id, galgame_id, user_id, created, updated)
		VALUES (2, ?, 5, ?, ?)`, fx.live, at(8), at(8)).Error)
	require.NoError(t, testDB.Exec(`
		INSERT INTO galgame_favorite (galgame_id, user_id, created, updated)
		VALUES (?, 5, ?, ?)`, fx.live, at(2), at(3)).Error)

	imp := loadedImporter(t, true)
	_, err := imp.importForumCollections()
	require.NoError(t, err)
	flat, err := imp.importForumFlat()
	require.NoError(t, err)
	require.Zero(t, flat.ItemsInserted, "the work is already filed; nothing new is created")
	require.Equal(t, 1, flat.ItemsMerged)

	var items []model.CatalogUserFolderItem
	require.NoError(t, testDB.Where("work_id = ?", fx.live).Find(&items).Error)
	require.Len(t, items, 1, "the default folder must not get a second copy")
	require.Equal(t, at(2).UTC(), items[0].CreatedAt.UTC(),
		"the flat row favourited it first, so its date wins")

	var def model.CatalogUserFolder
	require.NoError(t, testDB.Where("owner_uid = ? AND is_default", 5).First(&def).Error)
	require.NotEqual(t, def.ID, items[0].FolderID, "it stays in the named folder")
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

// Two moyu patches carrying one vndb id resolve to one work, so the lane emits
// the same (folder, work) pair twice; with both in one batch Postgres aborts the
// statement with "ON CONFLICT DO UPDATE command cannot affect row a second
// time". That is how the 2026-09-07 production run died on its first moyu
// flush, after both forum lanes had already been written.
func TestOneBatchNeverCarriesAPairTwice(t *testing.T) {
	fx := seedFixture(t)
	require.NoError(t, testDB.Exec(`INSERT INTO patch (id, vndb_id) VALUES (1, 'vtest1'), (2, 'vtest1')`).Error)
	require.NoError(t, testDB.Exec(`
		INSERT INTO user_patch_favorite_relation (id, user_id, galgame_id, created, updated)
		VALUES (1, 11, 1, ?, ?), (2, 11, 2, ?, ?)`, at(5), at(6), at(2), at(9)).Error)

	imp := loadedImporter(t, true)
	c, err := imp.importMoyu()
	require.NoError(t, err)
	require.Equal(t, 1, c.ItemsInserted)
	require.Equal(t, 1, c.ItemsMerged)

	var items []model.CatalogUserFolderItem
	require.NoError(t, testDB.Where("work_id = ?", fx.live).Find(&items).Error)
	require.Len(t, items, 1)
	require.Equal(t, at(2).UTC(), items[0].CreatedAt.UTC(), "folding keeps the earliest created")
	require.Equal(t, at(9).UTC(), items[0].UpdatedAt.UTC(), "folding keeps the latest updated")
}
