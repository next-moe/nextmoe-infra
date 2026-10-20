package tagsafety

import (
	"context"
	"fmt"
	"os"
	"testing"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	"api/internal/testsupport/dbtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

var testDB *gorm.DB

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		fmt.Fprintln(os.Stderr, "SKIP: no TEST_DATABASE_DSN — DB-backed tagsafety tests will skip individually")
		os.Exit(m.Run())
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL: cannot connect to the assigned test database")
		os.Exit(1)
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

func TestSetWorkTagSexualSkipsCuratedLane(t *testing.T) {
	if testDB == nil {
		dbtest.Skip(t)
	}
	require.NoError(t, testDB.Exec(`TRUNCATE catalog_work_tag, catalog_work RESTART IDENTITY CASCADE`).Error)

	var curated, bangumi int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_source WHERE key = 'curated'`).Scan(&curated).Error)
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_source WHERE key = 'bangumi'`).Scan(&bangumi).Error)
	require.NotZero(t, curated)
	require.NotZero(t, bangumi)

	w := model.CatalogWork{MediumID: 1, OLang: "ja", DisplayName: "tag-safety"}
	require.NoError(t, testDB.Create(&w).Error)
	require.NoError(t, testDB.Create(&model.CatalogWorkTag{
		WorkID: w.ID, Name: "調教", SourceID: curated, Spoiler: 0, Sexual: false,
	}).Error)
	require.NoError(t, testDB.Create(&model.CatalogWorkTag{
		WorkID: w.ID, Name: "調教", SourceID: bangumi, Spoiler: 0, Sexual: false,
	}).Error)

	g := &gormWriter{db: testDB, sources: map[string]int16{"curated": curated, "bangumi": bangumi}}
	ctx := context.Background()

	n, err := g.setWorkTagSexual(ctx, "curated", "調教")
	require.NoError(t, err)
	assert.Zero(t, n, "a curated work-tag edge is not flipped")

	n, err = g.setWorkTagSexual(ctx, "bangumi", "調教")
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)

	var human, machine bool
	require.NoError(t, testDB.Raw(`SELECT sexual FROM catalog_work_tag WHERE work_id = ? AND source_id = ?`,
		w.ID, curated).Scan(&human).Error)
	require.NoError(t, testDB.Raw(`SELECT sexual FROM catalog_work_tag WHERE work_id = ? AND source_id = ?`,
		w.ID, bangumi).Scan(&machine).Error)
	assert.False(t, human)
	assert.True(t, machine)
}

func TestVocabularyFlagsLeaveHumanEditsAlone(t *testing.T) {
	if testDB == nil {
		dbtest.Skip(t)
	}
	require.NoError(t, testDB.Exec(`TRUNCATE catalog_tag RESTART IDENTITY CASCADE`).Error)
	stamp := `{"sexual":[{"source":"curated","at":"2026-10-19T00:00:00Z"}],"tier":[{"source":"curated","at":"2026-10-19T00:00:00Z"}]}`
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_tag (name, tier, kind, sexual, field_provenance, created_at, updated_at)
		VALUES ('百合', ?, 0, false, ?::jsonb, now(), now()), ('凌辱', ?, 0, false, '{}'::jsonb, now(), now())`,
		model.TagTierCore, stamp, model.TagTierCore).Error)

	g := &gormWriter{db: testDB}
	ctx := context.Background()
	for _, name := range []string{"百合", "凌辱"} {
		_, err := g.setTagSexual(ctx, name)
		require.NoError(t, err)
		_, err = g.setTagHidden(ctx, name)
		require.NoError(t, err)
	}

	var human, machine model.CatalogTag
	require.NoError(t, testDB.Where("name = ?", "百合").First(&human).Error)
	require.NoError(t, testDB.Where("name = ?", "凌辱").First(&machine).Error)
	assert.False(t, human.Sexual, "a sexual flag a person cleared stays cleared")
	assert.Equal(t, model.TagTierCore, human.Tier, "a tier a person set is not hidden by the classifier")
	assert.True(t, machine.Sexual)
	assert.Equal(t, model.TagTierHidden, machine.Tier)
}
