package getchuchars

import (
	"context"
	"fmt"
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
		os.Exit(m.Run())
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL: cannot connect to the assigned test database")
		os.Exit(1)
	}
	for _, step := range []func(*gorm.DB) error{migrate.Run, seed.Run, srcvndb.EnsureSchema} {
		if err := step(db); err != nil {
			fmt.Fprintf(os.Stderr, "FAIL: test schema: %v\n", err)
			os.Exit(1)
		}
	}
	testDB = db
	os.Exit(m.Run())
}

func mkCastedWork(t *testing.T, getchuID, rid string, vids []string, name string) int64 {
	t.Helper()
	w := model.CatalogWork{MediumID: 1, OLang: "ja", DisplayName: "w" + getchuID}
	require.NoError(t, testDB.Create(&w).Error)
	rel := model.CatalogRelease{WorkID: w.ID}
	require.NoError(t, testDB.Create(&rel).Error)
	ch := model.CatalogCharacter{DisplayName: name}
	require.NoError(t, testDB.Create(&ch).Error)
	require.NoError(t, testDB.Create(&model.CatalogWorkCharacter{WorkID: w.ID, CharacterID: ch.ID}).Error)
	require.NoError(t, testDB.Exec(`
		INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by) VALUES
		(?, ?, (SELECT id FROM catalog_source WHERE key = 'getchu'), ?, ?, 'rule:test'),
		(?, ?, (SELECT id FROM catalog_source WHERE key = 'vndb'), ?, ?, 'rule:test')`,
		entityTypeRelease, rel.ID, getchuID, linkKindExact,
		entityTypeRelease, rel.ID, rid, linkKindExact).Error)
	for _, vid := range vids {
		require.NoError(t, testDB.Exec(
			`INSERT INTO src_vndb.releases_vn (id, vid, rtype) VALUES (?, ?, 'complete')`, rid, vid).Error)
	}
	return ch.ID
}

func TestRosterMarksBundleProducts(t *testing.T) {
	if testDB == nil {
		dbtest.Skip(t)
	}
	for _, table := range []string{
		"catalog_work_character", "catalog_character", "catalog_external_ref",
		"catalog_release", "catalog_work", "src_vndb.releases_vn",
	} {
		require.NoError(t, testDB.Exec("TRUNCATE "+table+" RESTART IDENTITY CASCADE").Error)
	}
	mkCastedWork(t, "7001", "r7001", []string{"v1", "v2"}, "美咲")
	single := mkCastedWork(t, "7002", "r7002", []string{"v3"}, "美咲")

	ctx := context.Background()
	source, err := SourceID(ctx, testDB)
	require.NoError(t, err)
	roster, err := loadRoster(ctx, testDB, source)
	require.NoError(t, err)
	bundle := map[string]bool{}
	for _, r := range roster {
		bundle[r.GetchuID] = r.Bundle
	}
	assert.Equal(t, map[string]bool{"7001": true, "7002": false}, bundle)

	got, st := match([]getchuChar{
		{GetchuID: "7001", Name: "美咲", Profile: "p"},
		{GetchuID: "7002", Name: "美咲", Profile: "p"},
	}, buildIndex(roster))
	assert.Equal(t, 1, st.Bundle)
	require.Len(t, got, 1)
	assert.Equal(t, single, got[0].CharacterID)
}
