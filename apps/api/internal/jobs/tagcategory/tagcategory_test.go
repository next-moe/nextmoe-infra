package tagcategory

import (
	"context"
	"os"
	"testing"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	"api/internal/platform/catalog/srcvndb"
	"api/internal/testsupport/dbtest"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDB *gorm.DB

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("jobs/tagcategory")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("jobs/tagcategory", "cannot connect to test database: %v", err)
	}
	if err := migrate.Run(db); err != nil {
		dbtest.SkipMainf("jobs/tagcategory", "catalog migrate failed: %v", err)
	}
	if err := seed.Run(db); err != nil {
		dbtest.SkipMainf("jobs/tagcategory", "catalog seed failed: %v", err)
	}
	if err := srcvndb.EnsureSchema(db); err != nil {
		dbtest.SkipMainf("jobs/tagcategory", "src_vndb schema failed: %v", err)
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

func mkTag(t *testing.T, name string, sexual bool) int64 {
	t.Helper()
	tag := model.CatalogTag{Name: name, Tier: model.TagTierCore, Kind: model.TagKindContent, Sexual: sexual}
	require.NoError(t, testDB.Create(&tag).Error)
	return tag.ID
}

func mkSrc(t *testing.T, id, name string, parents ...string) {
	t.Helper()
	require.NoError(t, testDB.Exec(`INSERT INTO src_vndb.tags
		(id, cat, defaultspoil, searchable, applicable, name, alias, description)
		VALUES (?, 'cont', 0, true, true, ?, '', '')`, id, name).Error)
	for _, p := range parents {
		require.NoError(t, testDB.Exec(`INSERT INTO src_vndb.tags_parents (id, parent, main) VALUES (?, ?, true)`, id, p).Error)
	}
}

func mapVNDB(t *testing.T, sourceName string, tagID int64) {
	t.Helper()
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_tag_source_map (source_id, source_name, tag_id)
		SELECT id, ?, ? FROM catalog_source WHERE key = 'vndb'`, sourceName, tagID).Error)
}

func categoryOf(t *testing.T, id int64) *int16 {
	t.Helper()
	var tag model.CatalogTag
	require.NoError(t, testDB.First(&tag, id).Error)
	return tag.CategoryID
}

func TestDerive(t *testing.T) {
	clean(t)
	ctx := context.Background()
	for _, r := range [][]string{
		{"g1", "Theme"}, {"g20", "Character"}, {"g674", "Style"},
		{"g2", "Fantasy", "g1"}, {"g23", "Sexual Content", "g1"}, {"g399", "Presentation", "g674"},
		{"g9001", "live-cat Magic", "g2"},
		{"g9002", "live-cat Elf Heroine", "g2", "g20"},
		{"g9003", "live-cat Pixel Art", "g399"},
		{"g9004", "live-cat Bath Scene", "g2"},
		{"g9005", "live-cat Group Act", "g23"},
	} {
		mkSrc(t, r[0], r[1], r[2:]...)
	}
	magic, elf, pixel := mkTag(t, "live-cat-magic", false), mkTag(t, "live-cat-elf", false), mkTag(t, "live-cat-pixel", false)
	bath, group, unsourced := mkTag(t, "live-cat-bath", true), mkTag(t, "live-cat-group", false), mkTag(t, "live-cat-unsourced", false)
	reviewed := mkTag(t, "live-cat-reviewed", false)
	for name, id := range map[string]int64{
		"live-cat Magic": magic, "live-cat Elf Heroine": elf, "live-cat Pixel Art": pixel,
		"live-cat Bath Scene": bath, "live-cat Group Act": group,
	} {
		mapVNDB(t, name, id)
	}
	mkSrc(t, "g9006", "live-cat Reviewed", "g2")
	mapVNDB(t, "live-cat Reviewed", reviewed)
	_, err := ApplyCurated(ctx, testDB, []CuratedRec{{TagID: reviewed, Tag: "live-cat-reviewed", Category: ""}}, true)
	require.NoError(t, err)

	st, err := Derive(ctx, testDB, false)
	require.NoError(t, err)
	require.Equal(t, 6, st.Candidates, "the reviewed tag is not a candidate")
	require.Nil(t, categoryOf(t, magic), "a dry run writes nothing")

	st, err = Derive(ctx, testDB, true)
	require.NoError(t, err)
	require.Equal(t, 5, st.Derived)
	for id, want := range map[int64]int16{
		magic: model.TagCategoryGenre,
		elf:   model.TagCategoryCharacter,
		pixel: model.TagCategoryPresentation,
		bath:  model.TagCategoryAdult,
		group: model.TagCategoryAdult,
	} {
		got := categoryOf(t, id)
		require.NotNil(t, got, "tag %d", id)
		require.Equal(t, want, *got, "tag %d", id)
	}
	require.Nil(t, categoryOf(t, unsourced))
	require.Nil(t, categoryOf(t, reviewed), "a reviewed 其他 stays empty")
}

func TestApplyCurated(t *testing.T) {
	clean(t)
	ctx := context.Background()
	a, b := mkTag(t, "live-cat-a", false), mkTag(t, "live-cat-b", false)

	_, err := ApplyCurated(ctx, testDB, []CuratedRec{
		{TagID: a, Tag: "live-cat-a", Category: "genre"},
		{TagID: b, Tag: "wrong-name", Category: "genre"},
	}, true)
	require.ErrorContains(t, err, "nothing written")
	require.Nil(t, categoryOf(t, a), "one bad record blocks the whole file")

	_, err = ApplyCurated(ctx, testDB, []CuratedRec{{TagID: a, Tag: "live-cat-a", Category: "nope"}}, true)
	require.ErrorContains(t, err, "unknown category")

	recs := []CuratedRec{
		{TagID: a, Tag: "live-cat-a", Category: "character"},
		{TagID: b, Tag: "live-cat-b", Category: ""},
	}
	st, err := ApplyCurated(ctx, testDB, recs, true)
	require.NoError(t, err)
	require.Equal(t, CuratedStats{Records: 2, Changed: 1, Stamped: 1}, *st)
	require.Equal(t, model.TagCategoryCharacter, *categoryOf(t, a))

	st, err = ApplyCurated(ctx, testDB, recs, true)
	require.NoError(t, err)
	require.Equal(t, CuratedStats{Records: 2, Unchanged: 2}, *st)

	var head string
	require.NoError(t, testDB.Raw(`SELECT field_provenance -> 'category_id' -> 0 ->> 'source' FROM catalog_tag WHERE id = ?`, a).Scan(&head).Error)
	require.Equal(t, "curated", head)
}
