package main

import (
	"bytes"
	"context"
	"errors"
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

const (
	roleProgramming = int64(238)
	roleArtWorker   = int64(316)
)

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("cmd/refine-staff-notes")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("cmd/refine-staff-notes", "cannot connect to test database: %v", err)
	}
	if err := migrate.Run(db); err != nil {
		dbtest.SkipMainf("cmd/refine-staff-notes", "catalog migrate failed: %v", err)
	}
	if err := seed.Run(db); err != nil {
		dbtest.SkipMainf("cmd/refine-staff-notes", "catalog seed failed: %v", err)
	}
	testDB = db
	os.Exit(m.Run())
}

type staffFixture struct {
	work                      int64
	curatedName, upstreamName int64
	curatedID, upstreamID     int64
}

func seedStaffNotes(t *testing.T, note string) staffFixture {
	t.Helper()
	for _, tbl := range []string{
		"catalog_credit", "catalog_credit_name", "catalog_work", "edit_suppressed_row",
	} {
		require.NoError(t, testDB.Exec("TRUNCATE "+tbl+" RESTART IDENTITY CASCADE").Error)
	}
	var f staffFixture
	w := model.CatalogWork{MediumID: 1, OLang: "ja", DisplayName: "作品",
		ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive}
	require.NoError(t, testDB.Create(&w).Error)
	f.work = w.ID

	mkName := func(name string) int64 {
		cn := model.CatalogCreditName{Name: name, Lang: "ja"}
		require.NoError(t, testDB.Create(&cn).Error)
		return cn.ID
	}
	f.curatedName, f.upstreamName = mkName("人手スタッフ"), mkName("上流スタッフ")

	mk := func(nameID int64, source int16) int64 {
		c := model.CatalogCredit{WorkID: f.work, CreditNameID: nameID,
			RoleID: otherStaffRoleID, Note: note, SourceID: &source}
		require.NoError(t, testDB.Create(&c).Error)
		return c.ID
	}
	f.curatedID, f.upstreamID = mk(f.curatedName, 12), mk(f.upstreamName, 2)
	return f
}

func roleOf(t *testing.T, creditID int64) int64 {
	t.Helper()
	var c model.CatalogCredit
	require.NoError(t, testDB.First(&c, creditID).Error)
	return c.RoleID
}

func TestRefineStaffNotesSkipsCuratedLane(t *testing.T) {
	t.Run("Update", func(t *testing.T) {
		f := seedStaffNotes(t, "programming")
		var out bytes.Buffer
		require.NoError(t, run(context.Background(), testDB, true, &out))

		assert.Equal(t, roleProgramming, roleOf(t, f.upstreamID), "the importer's row is reclassified")
		assert.EqualValues(t, otherStaffRoleID, roleOf(t, f.curatedID),
			"a hand-written credit is not the machine's to re-file")
		var n int64
		require.NoError(t, testDB.Model(&model.CatalogCredit{}).Count(&n).Error)
		assert.EqualValues(t, 2, n, "an UPDATE-only note must not create rows")
	})

	t.Run("Insert", func(t *testing.T) {
		// A composite note resolves to two roles: the first is an UPDATE, the
		// rest are INSERT ... SELECT, and that SELECT copies source_id verbatim.
		f := seedStaffNotes(t, "programming, cg")
		var out bytes.Buffer
		require.NoError(t, run(context.Background(), testDB, true, &out))

		var copies []model.CatalogCredit
		require.NoError(t, testDB.Where("role_id = ?", roleArtWorker).Find(&copies).Error)
		require.Len(t, copies, 1, "only the importer's row is copied to the extra role")
		assert.Equal(t, f.upstreamName, copies[0].CreditNameID)
		require.NotNil(t, copies[0].SourceID)
		assert.EqualValues(t, 2, *copies[0].SourceID)

		var curatedRows int64
		require.NoError(t, testDB.Model(&model.CatalogCredit{}).
			Where("credit_name_id = ?", f.curatedName).Count(&curatedRows).Error)
		assert.EqualValues(t, 1, curatedRows,
			"the machine must not mint a source_id=12 row: the curated apply deletes it on the next save")
		assert.EqualValues(t, otherStaffRoleID, roleOf(t, f.curatedID))
	})
}

func TestRefineStaffNotesRekeysInSameTxn(t *testing.T) {
	f := seedStaffNotes(t, "programming")
	oldKey := editspec.CreditIdentity(otherStaffRoleID, f.upstreamName, 0)
	newKey := editspec.CreditIdentity(roleProgramming, f.upstreamName, 0)
	require.NoError(t, testDB.Create(&editing.SuppressedRow{
		EntityType: editspec.TypeWork, EntityID: f.work,
		FieldKey: editspec.FieldWorkCredits, IdentityKey: oldKey,
	}).Error)

	keys := func() []string {
		var out []string
		require.NoError(t, testDB.Model(&editing.SuppressedRow{}).
			Where("entity_id = ?", f.work).Pluck("identity_key", &out).Error)
		return out
	}

	t.Run("RollingBackTakesTheKeyWithTheRow", func(t *testing.T) {
		reg := editing.NewRegistry()
		require.NoError(t, editspec.RegisterAll(reg, testDB))
		boom := errors.New("boom")
		err := testDB.Transaction(func(tx *gorm.DB) error {
			if _, _, _, err := reclassifyNote(context.Background(), tx, reg, "programming", roleProgramming); err != nil {
				return err
			}
			return boom
		})
		require.ErrorIs(t, err, boom)
		assert.EqualValues(t, otherStaffRoleID, roleOf(t, f.upstreamID))
		assert.Equal(t, []string{oldKey}, keys(), "the key may never survive a rolled-back move")
	})

	var out bytes.Buffer
	require.NoError(t, run(context.Background(), testDB, true, &out))
	assert.Equal(t, roleProgramming, roleOf(t, f.upstreamID))
	assert.Equal(t, []string{newKey}, keys(), "the suppression follows the row it is about")
	assert.Contains(t, out.String(), "rekey=1/0")

	// The point of the rekey: the predicate still hides the very row the person
	// said was wrong.
	var visible []int64
	require.NoError(t, testDB.Raw(`SELECT c.id FROM catalog_credit c
		WHERE c.work_id = ? AND `+editspec.NotSuppressedCreditSQL("c")+` ORDER BY c.id`, f.work).
		Scan(&visible).Error)
	assert.Equal(t, []int64{f.curatedID}, visible)
}

func TestRefineStaffNotesSkipsACorrectedNote(t *testing.T) {
	f := seedStaffNotes(t, "programming")
	require.NoError(t, testDB.Exec(`UPDATE catalog_credit SET field_provenance =
		'{"note":[{"source":"curated","at":"2026-10-21T00:00:00Z"}]}'::jsonb WHERE id = ?`, f.upstreamID).Error)

	require.NoError(t, run(context.Background(), testDB, true, &bytes.Buffer{}))

	assert.EqualValues(t, otherStaffRoleID, roleOf(t, f.upstreamID),
		"a note a person wrote is not the importer's text to reclassify by")
}
