package main

import (
	"fmt"
	"os"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	"api/internal/platform/editing"
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
		fmt.Fprintln(os.Stderr, "SKIP: no TEST_DATABASE_DSN — the DB-backed test here skips individually")
		os.Exit(m.Run())
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("cmd/catalog-char-xsrc", "cannot connect to test database: %v", err)
	}
	if err := migrate.Run(db); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: catalog migrate failed: %v\n", err)
		os.Exit(1)
	}
	if err := seed.Run(db); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: catalog seed failed: %v\n", err)
		os.Exit(1)
	}
	testDB = db
	os.Exit(m.Run())
}

// TestCreditsSuppressionExcludedOnEveryReadSite is this package's share of the
// R2c-1 read-site sweep: a VA bridge says "these two characters share a voice
// actor on the same work", which is the evidence a cross-source pairing is
// built from. A suppressed credit is the statement that the voicing is wrong,
// and a merge candidate derived from it hands the reviewer back the mistake
// they just recorded.
func TestCreditsSuppressionExcludedOnEveryReadSite(t *testing.T) {
	if testDB == nil {
		t.Skip("no catalog test DB")
	}
	for _, tbl := range []string{
		"catalog_credit", "catalog_credit_name", "catalog_character", "catalog_work",
		"edit_suppressed_row",
	} {
		require.NoError(t, testDB.Exec("TRUNCATE "+tbl+" RESTART IDENTITY CASCADE").Error)
	}
	const vaRole = int64(1)
	work := model.CatalogWork{MediumID: 1, OLang: "ja", DisplayName: "作品",
		ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive}
	require.NoError(t, testDB.Create(&work).Error)
	cn := model.CatalogCreditName{Name: "声優", Lang: "ja"}
	require.NoError(t, testDB.Create(&cn).Error)

	mkChar := func(name string) int64 {
		c := model.CatalogCharacter{DisplayName: name}
		require.NoError(t, testDB.Create(&c).Error)
		return c.ID
	}
	a, b := mkChar("キャラA"), mkChar("キャラB")
	for _, id := range []int64{a, b} {
		charID := id
		require.NoError(t, testDB.Create(&model.CatalogCredit{
			WorkID: work.ID, CreditNameID: cn.ID, RoleID: vaRole, CharacterID: &charID,
		}).Error)
	}

	bridges, err := loadVABridges(testDB)
	require.NoError(t, err)
	require.Len(t, bridges, 1, "the two characters share a VA on one work")

	require.NoError(t, testDB.Create(&editing.SuppressedRow{
		EntityType: editspec.TypeWork, EntityID: work.ID, FieldKey: editspec.FieldWorkCredits,
		IdentityKey: editspec.CreditIdentity(vaRole, cn.ID, b),
	}).Error)
	bridges, err = loadVABridges(testDB)
	require.NoError(t, err)
	assert.Empty(t, bridges, "a suppressed credit is not pairing evidence")
}

// Work 2452: EG-minted テトメト 237605 sat on the work through a voice credit
// only, so the roster-only pairing never set it beside the roster's 2118.
// same-work decides that pair; the judge gets the same-name credit pairs it
// cannot decide, and no credit-only pair on a looser tier.
func TestBuildPairsSeesCreditOnlyCharacters(t *testing.T) {
	if testDB == nil {
		t.Skip("no catalog test DB")
	}
	for _, tbl := range []string{
		"catalog_credit", "catalog_credit_name", "catalog_work_character", "catalog_external_ref",
		"catalog_character_alias", "catalog_character", "catalog_work", "edit_suppressed_row",
	} {
		require.NoError(t, testDB.Exec("TRUNCATE "+tbl+" RESTART IDENTITY CASCADE").Error)
	}
	work := model.CatalogWork{MediumID: 1, OLang: "ja", DisplayName: "Timepiece Ensemble",
		ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive}
	require.NoError(t, testDB.Create(&work).Error)
	cn := model.CatalogCreditName{Name: "藤咲ウサ", Lang: "ja"}
	require.NoError(t, testDB.Create(&cn).Error)
	mk := func(name string, source int16, ext string, roster bool) int64 {
		c := model.CatalogCharacter{DisplayName: name}
		require.NoError(t, testDB.Create(&c).Error)
		require.NoError(t, testDB.Create(&model.CatalogExternalRef{
			EntityType: model.EntityTypeCharacter, EntityID: c.ID, SourceID: source,
			ExternalID: ext, LinkKind: model.LinkKindExact, MatchedBy: "test",
		}).Error)
		if roster {
			require.NoError(t, testDB.Create(&model.CatalogWorkCharacter{
				WorkID: work.ID, CharacterID: c.ID, Kind: model.WorkCharacterKindSecondary,
				MatchedBy: "import:character-roster-bangumi",
			}).Error)
		} else {
			id := c.ID
			require.NoError(t, testDB.Create(&model.CatalogCredit{
				WorkID: work.ID, CreditNameID: cn.ID, RoleID: 1, CharacterID: &id,
			}).Error)
		}
		return c.ID
	}
	mk("テトメト", model.SourceBangumi, "22860", true)
	mk("テトメト", model.SourceErogameScape, "2244", false)
	kuroEG := mk("黒翼", model.SourceErogameScape, "2243", false)
	kuroBgm := mk("黒 翼", model.SourceBangumi, "22859", false)
	mk("黒羽", model.SourceVNDB, "c22036", false)

	decided, _, _, err := buildSameWork(testDB)
	require.NoError(t, err)
	require.Len(t, decided, 1, "テトメト")
	pairs, _, err := buildPairs(testDB, decided)
	require.NoError(t, err)
	require.Len(t, pairs, 1, "%+v", pairs)
	assert.Equal(t, [2]int64{kuroEG, kuroBgm}, [2]int64{pairs[0].A, pairs[0].B})
	assert.Equal(t, 1, pairs[0].Tier)
}

func TestSameWorkMergesTheClearCreditOnlyTwins(t *testing.T) {
	if testDB == nil {
		t.Skip("no catalog test DB")
	}
	for _, tbl := range []string{
		"catalog_credit", "catalog_credit_name", "catalog_work_character", "catalog_external_ref",
		"catalog_character_alias", "catalog_character", "catalog_work", "edit_suppressed_row",
	} {
		require.NoError(t, testDB.Exec("TRUNCATE "+tbl+" RESTART IDENTITY CASCADE").Error)
	}
	work := model.CatalogWork{MediumID: 1, OLang: "ja", DisplayName: "w",
		ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive}
	require.NoError(t, testDB.Create(&work).Error)
	cn := model.CatalogCreditName{Name: "声", Lang: "ja"}
	require.NoError(t, testDB.Create(&cn).Error)
	mk := func(name string, source int16, ext string, roster bool) int64 {
		c := model.CatalogCharacter{DisplayName: name}
		require.NoError(t, testDB.Create(&c).Error)
		require.NoError(t, testDB.Create(&model.CatalogExternalRef{
			EntityType: model.EntityTypeCharacter, EntityID: c.ID, SourceID: source,
			ExternalID: ext, LinkKind: model.LinkKindExact, MatchedBy: "test",
		}).Error)
		if roster {
			require.NoError(t, testDB.Create(&model.CatalogWorkCharacter{
				WorkID: work.ID, CharacterID: c.ID, Kind: model.WorkCharacterKindMain, MatchedBy: "import:character-roster-vndb",
			}).Error)
		} else {
			id := c.ID
			require.NoError(t, testDB.Create(&model.CatalogCredit{
				WorkID: work.ID, CreditNameID: cn.ID, RoleID: 1, CharacterID: &id,
			}).Error)
		}
		return c.ID
	}
	konami := mk("蔵前こなみ", model.SourceVNDB, "c24126", true)
	konamiEG := mk("蔵前 こなみ", model.SourceErogameScape, "2242", false)
	konamiBgm := mk("蔵前・こなみ", model.SourceBangumi, "1", false)
	nana := mk("なな", model.SourceVNDB, "c1", true)
	nanaBgm := mk("な な", model.SourceBangumi, "2", false)
	mk("ナナ", model.SourceVNDB, "c2", true)
	mk("ナナ", model.SourceVNDB, "c3", false)

	merges, _, st, err := buildSameWork(testDB)
	require.NoError(t, err)
	assert.Equal(t, map[int64]int64{nanaBgm: nana}, merges, "three 蔵前こなみ are no clear pair; the two ナナ share a source")
	assert.Equal(t, 1, st.Ambiguous)
	assert.Equal(t, 1, st.SharedSource)

	require.NoError(t, testDB.Exec(`DELETE FROM catalog_credit WHERE character_id = ?`, konamiBgm).Error)
	merges, _, _, err = buildSameWork(testDB)
	require.NoError(t, err)
	assert.Equal(t, map[int64]int64{nanaBgm: nana, konamiEG: konami}, merges)
}
