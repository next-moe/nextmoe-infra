package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"api/internal/platform/catalog/migrate"
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
		dbtest.SkipMain("cmd/ingest-trait-zh")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		dbtest.SkipMainf("cmd/ingest-trait-zh", "cannot connect to test database: %v", err)
	}
	if err := migrate.Run(db); err != nil {
		dbtest.SkipMainf("cmd/ingest-trait-zh", "catalog migrate failed: %v", err)
	}
	testDB = db
	os.Exit(m.Run())
}

func seedTraits(t *testing.T, rows [][4]string) map[string]int64 {
	t.Helper()
	require.NoError(t, testDB.Exec(`TRUNCATE catalog_character_trait RESTART IDENTITY CASCADE`).Error)
	ids := map[string]int64{}
	for _, r := range rows {
		var id int64
		require.NoError(t, testDB.Raw(`INSERT INTO catalog_character_trait
			(vndb_tid, name, name_zh, name_zh_provenance, group_tid, gorder, default_spoil,
			 sexual, searchable, applicable, alias, description, created_at, updated_at)
			VALUES (?, ?, ?, ?::smallint, '', 0, 0, false, true, true, '', '', now(), now())
			RETURNING id`, r[0], r[1], r[2], r[3]).Scan(&id).Error)
		ids[r[1]] = id
	}
	return ids
}

func loadZh(t *testing.T, id int64) (string, int16) {
	t.Helper()
	var row struct {
		NameZh     string `gorm:"column:name_zh"`
		Provenance int16  `gorm:"column:name_zh_provenance"`
	}
	require.NoError(t, testDB.Raw(`SELECT name_zh, name_zh_provenance FROM catalog_character_trait WHERE id = ?`, id).Scan(&row).Error)
	return row.NameZh, row.Provenance
}

func TestAutoMigrateAddsTheZhColumns(t *testing.T) {
	var cols []struct {
		Column   string `gorm:"column:column_name"`
		Type     string `gorm:"column:data_type"`
		Nullable string `gorm:"column:is_nullable"`
		Default  string `gorm:"column:column_default"`
	}
	require.NoError(t, testDB.Raw(`SELECT column_name, data_type, is_nullable, coalesce(column_default,'') AS column_default
		FROM information_schema.columns
		WHERE table_name = 'catalog_character_trait' AND column_name IN ('name_zh','name_zh_provenance')
		ORDER BY column_name`).Scan(&cols).Error)
	require.Len(t, cols, 2)

	assert.Equal(t, "name_zh", cols[0].Column)
	assert.Equal(t, "text", cols[0].Type)
	assert.Equal(t, "NO", cols[0].Nullable)
	assert.Contains(t, cols[0].Default, "''")

	assert.Equal(t, "name_zh_provenance", cols[1].Column)
	assert.Equal(t, "smallint", cols[1].Type)
	assert.Equal(t, "NO", cols[1].Nullable)
	assert.Contains(t, cols[1].Default, "0")
}

func TestAutoMigrateBackfillsAPopulatedTable(t *testing.T) {
	seedTraits(t, [][4]string{{"i1", "Hair", "毛发", "0"}})
	require.NoError(t, testDB.Exec(`ALTER TABLE catalog_character_trait
		DROP COLUMN name_zh, DROP COLUMN name_zh_provenance`).Error)

	require.NoError(t, migrate.Run(testDB))

	var got struct {
		NameZh     string `gorm:"column:name_zh"`
		Provenance int16  `gorm:"column:name_zh_provenance"`
	}
	require.NoError(t, testDB.Raw(`SELECT name_zh, name_zh_provenance FROM catalog_character_trait WHERE vndb_tid = 'i1'`).Scan(&got).Error)
	assert.Equal(t, "", got.NameZh)
	assert.Equal(t, int16(0), got.Provenance)
}

func TestApplyFillsEmptyAndUpgradesMachineButNeverOverwritesCurated(t *testing.T) {
	ids := seedTraits(t, [][4]string{
		{"i100", "Ahoge", "", "0"},
		{"i101", "Kuudere", "冷娇机翻", "1"},
		{"i102", "Tsundere", "傲娇", "0"},
		{"i103", "Deredere", "痴情", "0"},
		{"i104", "Not In Dictionary", "", "0"},
	})
	proposals := []pair{
		{En: "Ahoge", Zh: "呆毛"},
		{En: "Kuudere", Zh: "冷娇"},
		{En: "Tsundere", Zh: "傲娇"},
		{En: "Deredere", Zh: "一见钟情"},
		{En: "Ghost Trait", Zh: "幽灵"},
	}

	rows, err := loadTraits(context.Background(), testDB)
	require.NoError(t, err)
	writes := plan(proposals, rows, provenanceCurated)
	c := summarise(proposals, writes)
	assert.Equal(t, 4, c.Matched, "the ghost key must not match anything")
	assert.Equal(t, 2, c.Write)
	assert.Equal(t, 1, c.Same)
	assert.Equal(t, 1, c.Conflict)

	n, err := applyWrites(context.Background(), testDB, writes, provenanceCurated)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	zh, prov := loadZh(t, ids["Ahoge"])
	assert.Equal(t, "呆毛", zh)
	assert.Equal(t, provenanceCurated, prov)

	zh, prov = loadZh(t, ids["Kuudere"])
	assert.Equal(t, "冷娇", zh, "a machine guess yields to the curated dictionary")
	assert.Equal(t, provenanceCurated, prov)

	zh, _ = loadZh(t, ids["Tsundere"])
	assert.Equal(t, "傲娇", zh)

	zh, prov = loadZh(t, ids["Deredere"])
	assert.Equal(t, "痴情", zh, "a curated value is NEVER overwritten — the conflict is reported instead")
	assert.Equal(t, provenanceCurated, prov)

	zh, _ = loadZh(t, ids["Not In Dictionary"])
	assert.Equal(t, "", zh)
}

func TestApplyIsIdempotent(t *testing.T) {
	seedTraits(t, [][4]string{{"i200", "Ahoge", "", "0"}})
	proposals := []pair{{En: "Ahoge", Zh: "呆毛"}}

	for i, want := range []int{1, 0} {
		rows, err := loadTraits(context.Background(), testDB)
		require.NoError(t, err)
		n, err := applyWrites(context.Background(), testDB, plan(proposals, rows, provenanceCurated), provenanceCurated)
		require.NoError(t, err)
		assert.Equal(t, want, n, "run %d", i+1)
	}
}

func TestApplyWritesEveryRowSharingAName(t *testing.T) {
	ids := seedTraits(t, [][4]string{
		{"i300", "Red", "", "0"},
		{"i301", "Red2", "", "0"},
	})
	require.NoError(t, testDB.Exec(`UPDATE catalog_character_trait SET name = 'Red' WHERE vndb_tid = 'i301'`).Error)

	rows, err := loadTraits(context.Background(), testDB)
	require.NoError(t, err)
	n, err := applyWrites(context.Background(), testDB, plan([]pair{{En: "Red", Zh: "红色"}}, rows, provenanceCurated), provenanceCurated)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	for _, name := range []string{"Red", "Red2"} {
		zh, _ := loadZh(t, ids[name])
		assert.Equal(t, "红色", zh)
	}
}

func TestApplyCSVWritesMachineProvenance(t *testing.T) {
	ids := seedTraits(t, [][4]string{
		{"i400", "Alraune", "", "0"},
		{"i401", "Bovine", "", "0"},
		{"i402", "Balaclava", "巴拉克拉法帽", "0"},
	})
	path := filepath.Join(t.TempDir(), "review.csv")
	require.NoError(t, writeReviewCSV(path, []csvRow{
		{TraitID: ids["Alraune"], VndbTID: "i400", Name: "Alraune", Group: "Role", Description: "a plant girl", ProposedZh: "花妖"},
		{TraitID: ids["Bovine"], VndbTID: "i401", Name: "Bovine", Group: "Body", Description: "", ProposedZh: ""},
		{TraitID: ids["Balaclava"], VndbTID: "i402", Name: "Balaclava", Group: "Clothes", Description: "", ProposedZh: "头套"},
	}))

	pairs, err := readReviewCSV(path)
	require.NoError(t, err)
	require.Len(t, pairs, 2, "an empty proposal is the reviewer's reject and is dropped")

	rows, err := loadTraits(context.Background(), testDB)
	require.NoError(t, err)
	writes := plan(pairs, rows, provenanceMachine)
	n, err := applyWrites(context.Background(), testDB, writes, provenanceMachine)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	zh, prov := loadZh(t, ids["Alraune"])
	assert.Equal(t, "花妖", zh)
	assert.Equal(t, provenanceMachine, prov)

	zh, _ = loadZh(t, ids["Bovine"])
	assert.Equal(t, "", zh)

	zh, prov = loadZh(t, ids["Balaclava"])
	assert.Equal(t, "巴拉克拉法帽", zh, "the machine lane cannot overwrite curated text either")
	assert.Equal(t, provenanceCurated, prov)
	assert.Equal(t, 1, summarise(pairs, writes).Conflict)
}

func TestLoadMTCandidatesOnlyTheEmptyOnes(t *testing.T) {
	seedTraits(t, [][4]string{
		{"i500", "Body", "身体", "0"},
		{"i501", "Toned", "", "0"},
		{"i502", "Tanned", "晒黑", "1"},
	})
	require.NoError(t, testDB.Exec(`UPDATE catalog_character_trait SET group_tid = 'i500',
		description = 'This character is [url=http://x]toned[/url].' WHERE vndb_tid = 'i501'`).Error)

	cands, err := loadMTCandidates(context.Background(), testDB, 0)
	require.NoError(t, err)
	require.Len(t, cands, 1, "a filled name_zh is not a candidate, machine-made or not")
	assert.Equal(t, "Toned", cands[0].Name)
	assert.Equal(t, "Body", cands[0].GroupName)
	assert.Equal(t, "This character is toned.", plainDescription(cands[0].Description))
	assert.Contains(t, userMessage(cands[0]), "所属分类: Body")
}

func TestApplyNeverRefillsANameZhAPersonEdited(t *testing.T) {
	ids := seedTraits(t, [][4]string{
		{"i110", "Cleared", "", "1"},
		{"i111", "Edited", "人工译名", "1"},
	})
	require.NoError(t, testDB.Exec(`UPDATE catalog_character_trait SET field_provenance = ?::jsonb WHERE id IN ?`,
		`{"name_zh":[{"source":"curated","at":"2026-10-19T00:00:00Z"}]}`, []int64{ids["Cleared"], ids["Edited"]}).Error)
	proposals := []pair{{En: "Cleared", Zh: "机翻甲"}, {En: "Edited", Zh: "机翻乙"}}

	rows, err := loadTraits(context.Background(), testDB)
	require.NoError(t, err)
	writes := plan(proposals, rows, provenanceMachine)
	assert.Equal(t, 2, summarise(proposals, writes).Human)
	for i := range writes {
		writes[i].Decision = decWrite
	}
	n, err := applyWrites(context.Background(), testDB, writes, provenanceMachine)
	require.NoError(t, err)
	assert.Zero(t, n, "the write itself refuses a human-held name, even when a plan says write")

	zh, _ := loadZh(t, ids["Cleared"])
	assert.Equal(t, "", zh, "a name a person cleared stays cleared")
	zh, _ = loadZh(t, ids["Edited"])
	assert.Equal(t, "人工译名", zh)
}
