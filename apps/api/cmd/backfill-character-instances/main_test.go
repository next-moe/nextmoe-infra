package main

import (
	"fmt"
	"os"
	"testing"
	"time"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	srcv "api/internal/platform/catalog/srcvndb"
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
		dbtest.SkipMain("cmd/backfill-character-instances")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("cmd/backfill-character-instances", "cannot connect to test database: %v", err)
	}
	for _, step := range []struct {
		name string
		run  func(*gorm.DB) error
	}{
		{"catalog migrate", migrate.Run},
		{"catalog seed", seed.Run},
		{"src_vndb schema", srcv.EnsureSchema},
	} {
		if err := step.run(db); err != nil {
			fmt.Fprintf(os.Stderr, "FAIL: %s: %v\n", step.name, err)
			os.Exit(1)
		}
	}
	testDB = db
	os.Exit(m.Run())
}

func clean(t *testing.T) {
	t.Helper()
	for _, table := range []string{"catalog_external_ref", "catalog_character", "src_vndb.chars"} {
		require.NoError(t, testDB.Exec("TRUNCATE "+table+" RESTART IDENTITY CASCADE").Error)
	}
}

func mkChar(t *testing.T, name string, vndbIDs ...string) int64 {
	t.Helper()
	c := model.CatalogCharacter{DisplayName: name}
	require.NoError(t, testDB.Create(&c).Error)
	for _, id := range vndbIDs {
		require.NoError(t, testDB.Exec(`
			INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by)
			VALUES (?, ?, (SELECT id FROM catalog_source WHERE key = 'vndb'), ?, ?, 'rule:test')`,
			model.EntityTypeCharacter, c.ID, id, model.LinkKindExact).Error)
	}
	return c.ID
}

func mkVndbChar(t *testing.T, id, main string) {
	t.Helper()
	require.NoError(t, testDB.Create(&srcv.Char{ID: id, Main: main, IngestedAt: time.Now()}).Error)
}

func instanceOf(t *testing.T, id int64) *int64 {
	t.Helper()
	var c model.CatalogCharacter
	require.NoError(t, testDB.First(&c, id).Error)
	return c.InstanceOf
}

func TestFillsInstanceOfFromTheVndbMain(t *testing.T) {
	clean(t)
	root := mkChar(t, "本体", "c1")
	inst := mkChar(t, "分身", "c2")
	mkVndbChar(t, "c1", "")
	mkVndbChar(t, "c2", "c1")

	dry, err := run(testDB, false)
	require.NoError(t, err)
	assert.EqualValues(t, 1, dry.WouldChange)
	assert.Nil(t, instanceOf(t, inst), "a dry run writes nothing")

	st, err := run(testDB, true)
	require.NoError(t, err)
	assert.EqualValues(t, 1, st.Written)
	require.NotNil(t, instanceOf(t, inst))
	assert.Equal(t, root, *instanceOf(t, inst))

	again, err := run(testDB, true)
	require.NoError(t, err)
	assert.Zero(t, again.Written)
	assert.EqualValues(t, 1, again.AlreadyOK)
}

func TestInstanceWhoseIDsNameTwoMainsIsLeftAlone(t *testing.T) {
	clean(t)
	mkChar(t, "Lucy Parker", "c10")
	second := mkChar(t, "Lucy", "c20")
	merged := mkChar(t, "Lucy", "c11", "c21")
	mkVndbChar(t, "c10", "")
	mkVndbChar(t, "c20", "")
	mkVndbChar(t, "c11", "c10")
	mkVndbChar(t, "c21", "c20")
	require.NoError(t, testDB.Model(&model.CatalogCharacter{}).Where("id = ?", merged).
		Update("instance_of", second).Error)

	for range 2 {
		st, err := run(testDB, true)
		require.NoError(t, err)
		assert.EqualValues(t, 1, st.Conflicting)
		assert.Zero(t, st.WouldChange)
		assert.Zero(t, st.Written, "neither main may overwrite the other")
		require.NotNil(t, instanceOf(t, merged))
		assert.Equal(t, second, *instanceOf(t, merged))
	}
}

func TestMergedInstanceWhoseIDsAgreeIsStillFilled(t *testing.T) {
	clean(t)
	root := mkChar(t, "本体", "c30")
	merged := mkChar(t, "分身", "c31", "c32")
	mkVndbChar(t, "c30", "")
	mkVndbChar(t, "c31", "c30")
	mkVndbChar(t, "c32", "c30")

	st, err := run(testDB, true)
	require.NoError(t, err)
	assert.Zero(t, st.Conflicting)
	assert.EqualValues(t, 1, st.Written)
	require.NotNil(t, instanceOf(t, merged))
	assert.Equal(t, root, *instanceOf(t, merged))
}
