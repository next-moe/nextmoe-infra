package tagparents

import (
	"context"
	"os"
	"testing"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	"api/internal/platform/catalog/srcvndb"
	"api/internal/testsupport/dbtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDB *gorm.DB

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("jobs/tagparents")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("jobs/tagparents", "cannot connect to test database: %v", err)
	}
	if err := migrate.Run(db); err != nil {
		dbtest.SkipMainf("jobs/tagparents", "catalog migrate failed: %v", err)
	}
	if err := seed.Run(db); err != nil {
		dbtest.SkipMainf("jobs/tagparents", "catalog seed failed: %v", err)
	}
	if err := srcvndb.EnsureSchema(db); err != nil {
		dbtest.SkipMainf("jobs/tagparents", "src_vndb schema failed: %v", err)
	}
	testDB = db
	os.Exit(m.Run())
}

func clean(t *testing.T) {
	t.Helper()
	for _, table := range []string{
		"catalog_tag_parent", "catalog_tag_source_map", "catalog_tag",
		"src_vndb.tags", "src_vndb.tags_parents",
	} {
		require.NoError(t, testDB.Exec("TRUNCATE "+table+" RESTART IDENTITY CASCADE").Error)
	}
}

func mkTag(t *testing.T, name string) int64 {
	t.Helper()
	tag := model.CatalogTag{Name: name, Tier: model.TagTierCore, Kind: model.TagKindContent}
	require.NoError(t, testDB.Create(&tag).Error)
	return tag.ID
}

func mapVNDB(t *testing.T, sourceName string, tagID int64) {
	t.Helper()
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_tag_source_map (source_id, source_name, tag_id)
		SELECT id, ?, ? FROM catalog_source WHERE key = 'vndb'`, sourceName, tagID).Error)
}

func mkSrcTag(t *testing.T, id, name, alias string) {
	t.Helper()
	require.NoError(t, testDB.Exec(`INSERT INTO src_vndb.tags
		(id, cat, defaultspoil, searchable, applicable, name, alias, description)
		VALUES (?, 'cont', 0, true, true, ?, ?, '')`, id, name, alias).Error)
}

func mkSrcParent(t *testing.T, id, parent string) {
	t.Helper()
	require.NoError(t, testDB.Exec(`INSERT INTO src_vndb.tags_parents (id, parent, main) VALUES (?, ?, true)`, id, parent).Error)
}

func setTagMap(m map[string]string) func() {
	prev := tagMap
	tagMap = func() map[string]string { return m }
	return func() { tagMap = prev }
}

func edges(t *testing.T) map[Edge]int16 {
	t.Helper()
	var rows []struct {
		TagID    int64
		ParentID int64
		Origin   int16
	}
	require.NoError(t, testDB.Raw(`SELECT tag_id, parent_id, origin FROM catalog_tag_parent`).Scan(&rows).Error)
	out := map[Edge]int16{}
	for _, r := range rows {
		out[Edge{Tag: r.TagID, Parent: r.ParentID}] = r.Origin
	}
	return out
}

func TestSyncProjectsThroughUnmappedAndSameCanonicalParents(t *testing.T) {
	clean(t)
	ctx := context.Background()
	theme, ntr, avoidable := mkTag(t, "主题"), mkTag(t, "NTR"), mkTag(t, "可避免NTR")
	mapVNDB(t, "主题", theme)
	mapVNDB(t, "NTR", ntr)
	mapVNDB(t, "可避免NTR", avoidable)

	mkSrcTag(t, "g1", "Theme", "")
	mkSrcTag(t, "g2", "Drama", "")
	mkSrcTag(t, "g3", "Netorare", "")
	mkSrcTag(t, "g4", "Netorare Variant", "")
	mkSrcTag(t, "g5", "Avoidable Netorare", "Optional NTR")
	mkSrcParent(t, "g2", "g1")
	mkSrcParent(t, "g3", "g2")
	mkSrcParent(t, "g4", "g3")
	mkSrcParent(t, "g5", "g4")

	tm := map[string]string{"Theme": "主题", "Netorare": "NTR", "Netorare Variant": "NTR", "Optional NTR": "可避免NTR"}
	restore := setTagMap(tm)
	defer restore()

	st, err := Sync(ctx, testDB, false)
	require.NoError(t, err)
	assert.Equal(t, 5, st.VNDBTags)
	assert.Equal(t, 4, st.Mapped)
	assert.Equal(t, 2, st.Added)
	assert.Empty(t, edges(t), "a dry run writes nothing")

	_, err = Sync(ctx, testDB, true)
	require.NoError(t, err)
	assert.Equal(t, map[Edge]int16{
		{Tag: ntr, Parent: theme}:     model.TagParentOriginVNDB,
		{Tag: avoidable, Parent: ntr}: model.TagParentOriginVNDB,
	}, edges(t))

	st, err = Sync(ctx, testDB, true)
	require.NoError(t, err)
	assert.Equal(t, 0, st.Added)
	assert.Equal(t, 0, st.Deleted)
}

func TestSyncDropsStaleVNDBEdgesAndLeavesCuratedOnes(t *testing.T) {
	clean(t)
	defer setTagMap(nil)()
	ctx := context.Background()
	a, b, c := mkTag(t, "甲"), mkTag(t, "乙"), mkTag(t, "丙")
	mapVNDB(t, "A", a)
	mapVNDB(t, "B", b)
	mkSrcTag(t, "g1", "A", "")
	mkSrcTag(t, "g2", "B", "")
	require.NoError(t, testDB.Create(&[]model.CatalogTagParent{
		{TagID: a, ParentID: b, Origin: model.TagParentOriginVNDB},
		{TagID: c, ParentID: b, Origin: model.TagParentOriginCurated, Note: "reviewed"},
	}).Error)

	st, err := Sync(ctx, testDB, true)
	require.NoError(t, err)
	assert.Equal(t, 1, st.Deleted)
	assert.Equal(t, map[Edge]int16{{Tag: c, Parent: b}: model.TagParentOriginCurated}, edges(t))
}

func TestSyncSkipsAnEdgeThatWouldCloseALoop(t *testing.T) {
	clean(t)
	defer setTagMap(nil)()
	ctx := context.Background()
	a, b := mkTag(t, "甲"), mkTag(t, "乙")
	mapVNDB(t, "A", a)
	mapVNDB(t, "B", b)
	mkSrcTag(t, "g1", "A", "")
	mkSrcTag(t, "g2", "B", "")
	mkSrcParent(t, "g1", "g2")
	require.NoError(t, testDB.Create(&model.CatalogTagParent{TagID: b, ParentID: a, Origin: model.TagParentOriginCurated, Note: "reviewed"}).Error)

	st, err := Sync(ctx, testDB, true)
	require.NoError(t, err)
	assert.Equal(t, 1, st.Cycles)
	assert.Equal(t, map[Edge]int16{{Tag: b, Parent: a}: model.TagParentOriginCurated}, edges(t))
}

func TestApplyCurated(t *testing.T) {
	clean(t)
	ctx := context.Background()
	guro, gore, lieki := mkTag(t, "色情怪诞（Guro）"), mkTag(t, "血腥暴力"), mkTag(t, "猎奇")
	require.NoError(t, testDB.Create(&model.CatalogTagParent{TagID: guro, ParentID: gore, Origin: model.TagParentOriginVNDB}).Error)

	recs := []CuratedRec{
		{TagID: guro, Tag: "色情怪诞（Guro）", ParentID: lieki, Parent: "猎奇", Op: OpAdd, Reason: "guro is a kind of 猎奇"},
		{TagID: guro, Tag: "色情怪诞（Guro）", ParentID: gore, Parent: "血腥暴力", Op: OpAdd, Reason: "keep it"},
		{TagID: lieki, Tag: "猎奇", ParentID: guro, Parent: "色情怪诞（Guro）", Op: OpAdd, Reason: "loop"},
		{TagID: lieki, Tag: "猎奇（旧名）", ParentID: gore, Parent: "血腥暴力", Op: OpAdd, Reason: "stale name"},
		{TagID: gore, Tag: "血腥暴力", ParentID: lieki, Parent: "猎奇", Op: OpAdd},
	}
	st, err := ApplyCurated(ctx, testDB, recs, false)
	require.NoError(t, err)
	assert.Equal(t, 1, st.Added)
	assert.Equal(t, 1, st.Endorsed)
	assert.Equal(t, 3, st.Errors, "the loop, the stale name and the missing reason are refused")
	assert.Len(t, edges(t), 1, "a dry run writes nothing")

	_, err = ApplyCurated(ctx, testDB, recs, true)
	require.NoError(t, err)
	assert.Equal(t, map[Edge]int16{
		{Tag: guro, Parent: lieki}: model.TagParentOriginCurated,
		{Tag: guro, Parent: gore}:  model.TagParentOriginCurated,
	}, edges(t))

	st, err = ApplyCurated(ctx, testDB, []CuratedRec{
		{TagID: guro, Tag: "色情怪诞（Guro）", ParentID: lieki, Parent: "猎奇", Op: OpRemove},
	}, true)
	require.NoError(t, err)
	assert.Equal(t, 1, st.Removed)
	assert.Len(t, edges(t), 1)
}

func TestApplyCuratedRefusesToRemoveAVNDBEdge(t *testing.T) {
	clean(t)
	a, b := mkTag(t, "甲"), mkTag(t, "乙")
	require.NoError(t, testDB.Create(&model.CatalogTagParent{TagID: a, ParentID: b, Origin: model.TagParentOriginVNDB}).Error)
	st, err := ApplyCurated(context.Background(), testDB, []CuratedRec{
		{TagID: a, Tag: "甲", ParentID: b, Parent: "乙", Op: OpRemove},
	}, true)
	require.NoError(t, err)
	assert.Equal(t, 1, st.Errors)
	assert.Len(t, edges(t), 1)
}
