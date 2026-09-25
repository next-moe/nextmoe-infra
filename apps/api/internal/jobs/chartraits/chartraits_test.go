package chartraits

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

var (
	testDB  *gorm.DB
	testDSN string
)

func TestMain(m *testing.M) {
	var ok bool
	testDSN, ok = dbtest.DSN()
	if !ok {
		dbtest.SkipMain("jobs/chartraits")
	}
	db, err := gorm.Open(postgres.Open(testDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("jobs/chartraits", "cannot connect to test database: %v", err)
	}
	if err := migrate.Run(db); err != nil {
		dbtest.SkipMainf("jobs/chartraits", "catalog migrate failed: %v", err)
	}
	if err := seed.Run(db); err != nil {
		dbtest.SkipMainf("jobs/chartraits", "catalog seed failed: %v", err)
	}
	if err := srcvndb.EnsureSchema(db); err != nil {
		dbtest.SkipMainf("jobs/chartraits", "src_vndb schema failed: %v", err)
	}
	testDB = db
	os.Exit(m.Run())
}

func clean(t *testing.T) {
	t.Helper()
	for _, table := range []string{
		"catalog_character_trait_link", "catalog_character_trait_parent", "catalog_character_trait",
		"catalog_external_ref", "catalog_character",
		"src_vndb.traits", "src_vndb.traits_parents", "src_vndb.chars_traits",
	} {
		require.NoError(t, testDB.Exec("TRUNCATE "+table+" RESTART IDENTITY CASCADE").Error)
	}
}

func mkSrcTrait(t *testing.T, id, gid, name string, sexual bool, defaultSpoil int16) {
	t.Helper()
	require.NoError(t, testDB.Exec(`INSERT INTO src_vndb.traits
		(id, gid, gorder, defaultspoil, sexual, searchable, applicable, name, alias, description)
		VALUES (?, ?, 0, ?, ?, true, true, ?, '', '')`, id, gid, defaultSpoil, sexual, name).Error)
}

func mkSrcParent(t *testing.T, id, parent string) {
	t.Helper()
	require.NoError(t, testDB.Exec(`INSERT INTO src_vndb.traits_parents (id, parent, main) VALUES (?, ?, true)`, id, parent).Error)
}

func mkSrcLink(t *testing.T, charID, tid string, spoil int16, lie bool) {
	t.Helper()
	require.NoError(t, testDB.Exec(`INSERT INTO src_vndb.chars_traits (id, tid, spoil, lie)
		VALUES (?, ?, ?, ?)`, charID, tid, spoil, lie).Error)
}

func mkChar(t *testing.T, name, vndbID string) int64 {
	t.Helper()
	ch := model.CatalogCharacter{DisplayName: name, Lang: "ja"}
	require.NoError(t, testDB.Create(&ch).Error)
	require.NoError(t, testDB.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeCharacter, EntityID: ch.ID, SourceID: 2,
		ExternalID: vndbID, LinkKind: model.LinkKindExact, MatchedBy: "rule:test",
	}).Error)
	return ch.ID
}

func TestImportCharacterTraits(t *testing.T) {
	clean(t)

	mkSrcTrait(t, "i1", "", "Hair", false, 0)
	mkSrcTrait(t, "i10", "i1", "Blond Hair", false, 0)
	mkSrcTrait(t, "i11", "i1", "Long Hair", false, 0)
	mkSrcTrait(t, "i43", "", "Engages in (Sexual)", true, 2)
	mkSrcTrait(t, "i50", "i43", "Sexual Trait X", true, 2)
	mkSrcParent(t, "i10", "i1")
	mkSrcParent(t, "i11", "i1")
	mkSrcParent(t, "i50", "i43")

	chA := mkChar(t, "A", "c1")
	chB := mkChar(t, "B", "c2")
	mkSrcLink(t, "c1", "i10", 0, false)
	mkSrcLink(t, "c1", "i50", 2, false)
	mkSrcLink(t, "c2", "i11", 1, true)

	ctx := context.Background()
	opts := Opts{DSN: testDSN}

	st, err := Run(ctx, opts)
	require.NoError(t, err)
	assert.Equal(t, 5, st.VocabTotal)
	assert.Equal(t, 5, st.VocabWritten, "all new in dry plan")
	assert.Equal(t, 0, st.EdgesTotal, "edges unresolvable before vocab lands (dry-before-first-apply)")
	assert.Equal(t, 3, st.LinksSeen, "link plan joins the SRC vocab — honest before first apply")
	var n int64
	require.NoError(t, testDB.Table("catalog_character_trait").Count(&n).Error)
	assert.Zero(t, n, "dry run must not write")

	opts.Apply = true
	st, err = Run(ctx, opts)
	require.NoError(t, err)
	assert.Equal(t, 5, st.VocabWritten)
	assert.Equal(t, 3, st.EdgesTotal)
	assert.Equal(t, 3, st.EdgesAdded)
	assert.Equal(t, 0, st.EdgesDeleted)
	assert.Equal(t, 3, st.LinksSeen)
	assert.Equal(t, 3, st.LinksWritten)
	assert.Zero(t, st.Errors)

	require.NoError(t, testDB.Table("catalog_character_trait_link").Where("character_id = ?", chA).Count(&n).Error)
	assert.EqualValues(t, 2, n, "chA carries its two links")
	var link model.CatalogCharacterTraitLink
	require.NoError(t, testDB.Where("character_id = ?", chB).First(&link).Error)
	assert.EqualValues(t, 1, link.SpoilerLevel)
	assert.True(t, link.Lie)
	var trait model.CatalogCharacterTrait
	require.NoError(t, testDB.Where("vndb_tid = 'i50'").First(&trait).Error)
	assert.True(t, trait.Sexual)
	assert.True(t, trait.SexualFamily)
	assert.Equal(t, "i43", trait.GroupTID)

	st, err = Run(ctx, opts)
	require.NoError(t, err)
	assert.Zero(t, st.VocabWritten)
	assert.Equal(t, 5, st.VocabUnchanged)
	assert.Zero(t, st.EdgesAdded+st.EdgesDeleted)
	assert.Zero(t, st.LinksWritten)
	assert.Equal(t, 3, st.LinksUnchanged)

	require.NoError(t, testDB.Exec(`UPDATE src_vndb.chars_traits SET spoil = 2 WHERE id = 'c2' AND tid = 'i11'`).Error)
	require.NoError(t, testDB.Exec(`UPDATE src_vndb.traits SET name = 'Blonde Hair' WHERE id = 'i10'`).Error)
	require.NoError(t, testDB.Exec(`DELETE FROM src_vndb.traits_parents WHERE id = 'i11'`).Error)
	st, err = Run(ctx, opts)
	require.NoError(t, err)
	assert.Equal(t, 1, st.VocabWritten, "renamed trait updates in place")
	assert.Equal(t, 1, st.LinksWritten, "regraded spoiler updates in place")
	assert.Equal(t, 0, st.EdgesAdded)
	assert.Equal(t, 1, st.EdgesDeleted, "stale edge removed — the dump is the truth")
	link = model.CatalogCharacterTraitLink{}
	require.NoError(t, testDB.Where("character_id = ?", chB).First(&link).Error)
	assert.EqualValues(t, 2, link.SpoilerLevel)

	require.NoError(t, testDB.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeCharacter, EntityID: chA, SourceID: 2,
		ExternalID: "c99", LinkKind: model.LinkKindExact, MatchedBy: "rule:test",
	}).Error)
	mkSrcLink(t, "c99", "i10", 0, false)
	mkSrcLink(t, "c99", "i11", 0, false)
	st, err = Run(ctx, opts)
	require.NoError(t, err)
	assert.Equal(t, 4, st.LinksSeen, "chA i10+i11+i50 (deduped) + chB i11")
	assert.Equal(t, 1, st.LinksWritten, "only the genuinely new (chA, i11) row")
	require.NoError(t, testDB.Table("catalog_character_trait_link").Where("character_id = ?", chA).Count(&n).Error)
	assert.EqualValues(t, 3, n, "no dupes from the double anchor")
}
