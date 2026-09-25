package main

import (
	"context"
	"path/filepath"
	"testing"

	"api/internal/platform/catalog/migrate"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type descSeed struct {
	TID, Name, NameZh, GroupTID, Desc, DescZh, Hash string
}

func seedDescTraits(t *testing.T, rows []descSeed) map[string]int64 {
	t.Helper()
	require.NoError(t, testDB.Exec(`TRUNCATE catalog_character_trait RESTART IDENTITY CASCADE`).Error)
	ids := map[string]int64{}
	for _, r := range rows {
		var id int64
		require.NoError(t, testDB.Raw(`INSERT INTO catalog_character_trait
			(vndb_tid, name, name_zh, name_zh_provenance, group_tid, gorder, default_spoil,
			 sexual, searchable, applicable, alias, description, description_zh, description_zh_source_hash, created_at, updated_at)
			VALUES (?, ?, ?, 0, ?, 0, 0, false, true, true, '', ?, ?, ?, now(), now())
			RETURNING id`, r.TID, r.Name, r.NameZh, r.GroupTID, r.Desc, r.DescZh, r.Hash).Scan(&id).Error)
		ids[r.TID] = id
	}
	return ids
}

func TestGoHashAgreesWithSQLOnMultibyte(t *testing.T) {
	desc := "该角色有呆毛。\n\nCafé ☕"
	ids := seedDescTraits(t, []descSeed{{TID: "i80001", Name: "Ahoge", Desc: desc}})
	var sqlHash string
	require.NoError(t, testDB.Raw(
		`SELECT `+descriptionHashSQL+` FROM catalog_character_trait WHERE id = ?`, ids["i80001"]).
		Scan(&sqlHash).Error)
	assert.Equal(t, sourceHash(desc), sqlHash)
}

func TestAutoMigrateAddsDescriptionZhColumns(t *testing.T) {
	var cols []struct {
		Column   string `gorm:"column:column_name"`
		Type     string `gorm:"column:data_type"`
		Nullable string `gorm:"column:is_nullable"`
		Default  string `gorm:"column:column_default"`
	}
	require.NoError(t, testDB.Raw(`SELECT column_name, data_type, is_nullable, coalesce(column_default,'') AS column_default
		FROM information_schema.columns
		WHERE table_name = 'catalog_character_trait'
		  AND column_name IN ('description_zh','description_zh_source_hash')
		ORDER BY column_name`).Scan(&cols).Error)
	require.Len(t, cols, 2)
	assert.Equal(t, "description_zh", cols[0].Column)
	assert.Equal(t, "text", cols[0].Type)
	assert.Equal(t, "NO", cols[0].Nullable)
	assert.Contains(t, cols[0].Default, "''")
	assert.Equal(t, "description_zh_source_hash", cols[1].Column)
	assert.Equal(t, "text", cols[1].Type)
	assert.Equal(t, "NO", cols[1].Nullable)
	assert.Contains(t, cols[1].Default, "''")
}

func TestAutoMigrateBackfillsDescriptionZh(t *testing.T) {
	seedDescTraits(t, []descSeed{{TID: "i80002", Name: "Hair", Desc: "root"}})
	require.NoError(t, testDB.Exec(`ALTER TABLE catalog_character_trait
		DROP COLUMN description_zh, DROP COLUMN description_zh_source_hash`).Error)
	require.NoError(t, migrate.Run(testDB))
	var got struct {
		Zh   string `gorm:"column:description_zh"`
		Hash string `gorm:"column:description_zh_source_hash"`
	}
	require.NoError(t, testDB.Raw(
		`SELECT description_zh, description_zh_source_hash FROM catalog_character_trait WHERE vndb_tid = 'i80002'`).
		Scan(&got).Error)
	assert.Equal(t, "", got.Zh)
	assert.Equal(t, "", got.Hash)
}

func TestLoadDescCandidatesSelection(t *testing.T) {
	old := "old english"
	cur := "current english"
	ids := seedDescTraits(t, []descSeed{
		{TID: "i80100", Name: "EmptyZh", Desc: cur},
		{TID: "i80101", Name: "Matching", Desc: cur, DescZh: "已译", Hash: sourceHash(cur)},
		{TID: "i80102", Name: "StaleHash", Desc: cur, DescZh: "旧译", Hash: sourceHash(old)},
		{TID: "i80103", Name: "BlankDesc", Desc: "   "},
	})

	cands, err := loadDescCandidates(context.Background(), testDB, 0, nil)
	require.NoError(t, err)
	got := map[string]bool{}
	for _, c := range cands {
		got[c.Row.Name] = true
	}
	assert.True(t, got["EmptyZh"])
	assert.True(t, got["StaleHash"])
	assert.False(t, got["Matching"])
	assert.False(t, got["BlankDesc"])

	forced, err := loadDescCandidates(context.Background(), testDB, 0, []int64{ids["i80101"]})
	require.NoError(t, err)
	require.Len(t, forced, 1)
	assert.Equal(t, "Matching", forced[0].Row.Name)

	blankForced, err := loadDescCandidates(context.Background(), testDB, 0, []int64{ids["i80103"]})
	require.NoError(t, err)
	assert.Empty(t, blankForced)
}

func TestPrepareDescItemGlossaryAndLinks(t *testing.T) {
	ids := seedDescTraits(t, []descSeed{
		{TID: "i12", Name: "Small breasts", NameZh: "贫乳", Desc: "linked"},
		{TID: "i80200", Name: "Hair", NameZh: "毛发", Desc: "group"},
		{TID: "i80201", Name: "Body", NameZh: "身体", GroupTID: "i80200", Desc: "parent"},
		{TID: "i80202", Name: "Toned", NameZh: "健美", GroupTID: "i80200",
			Desc: "This character is [url=/i12]i12[/url] and [url=https://vndb.org/i12]small breasts[/url]. See [url=https://example.com/x]site[/url]."},
		{TID: "i80203", Name: "Ghost", NameZh: "", Desc: "no zh"},
	})
	require.NoError(t, testDB.Exec(
		`INSERT INTO catalog_character_trait_parent (trait_id, parent_id) VALUES (?, ?)`,
		ids["i80202"], ids["i80201"]).Error)

	cands, err := loadDescCandidates(context.Background(), testDB, 0, []int64{ids["i80202"]})
	require.NoError(t, err)
	require.Len(t, cands, 1)
	item := cands[0]
	assert.Equal(t, "This character is Small breasts and small breasts. See site.", item.Prepared)
	assert.Equal(t, sourceHash(item.Row.Description), item.Hash)
	assert.Equal(t, []glossPair{
		{"Toned", "健美"},
		{"Hair", "毛发"},
		{"Body", "身体"},
		{"Small breasts", "贫乳"},
	}, item.Glossary)
}

func TestApplyDescCSVCountsAndDryRun(t *testing.T) {
	desc := "Hello 世界"
	hash := sourceHash(desc)
	ids := seedDescTraits(t, []descSeed{
		{TID: "i80300", Name: "WriteMe", Desc: desc},
		{TID: "i80301", Name: "Already", Desc: desc, DescZh: "已有", Hash: hash},
		{TID: "i80302", Name: "Changed", Desc: "new english", DescZh: "旧译", Hash: hash},
	})
	path := filepath.Join(t.TempDir(), "desc.csv")
	require.NoError(t, writeDescCSV(path, []descCSVRow{
		{TraitID: ids["i80300"], VndbTID: "i80300", Name: "WriteMe", SourceHash: hash, SourceText: desc, DescriptionZh: "你好世界"},
		{TraitID: ids["i80301"], VndbTID: "i80301", Name: "Already", SourceHash: hash, SourceText: desc, DescriptionZh: "已有"},
		{TraitID: ids["i80302"], VndbTID: "i80302", Name: "Changed", SourceHash: hash, SourceText: desc, DescriptionZh: "新译"},
		{TraitID: 999999, VndbTID: "i9", Name: "Ghost", SourceHash: hash, SourceText: desc, DescriptionZh: "幽灵"},
		{TraitID: ids["i80300"], VndbTID: "i80300", Name: "Empty", SourceHash: hash, SourceText: desc, DescriptionZh: "  "},
	}))

	rows, err := readDescCSV(path)
	require.NoError(t, err)
	writes, c, err := planDescWrites(context.Background(), testDB, rows)
	require.NoError(t, err)
	assert.Equal(t, 5, c.Rows)
	assert.Equal(t, 1, c.Write)
	assert.Equal(t, 1, c.Same)
	assert.Equal(t, 2, c.Stale)
	assert.Equal(t, 1, c.Empty)
	assert.Equal(t, []int64{ids["i80302"], 999999}, c.StaleIDs)
	require.Len(t, writes, 1)

	var before string
	require.NoError(t, testDB.Raw(`SELECT description_zh FROM catalog_character_trait WHERE id = ?`, ids["i80300"]).Scan(&before).Error)
	assert.Equal(t, "", before)

	n, err := applyDescWrites(context.Background(), testDB, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	require.NoError(t, testDB.Raw(`SELECT description_zh FROM catalog_character_trait WHERE id = ?`, ids["i80300"]).Scan(&before).Error)
	assert.Equal(t, "", before, "a dry plan must not write")

	n, err = applyDescWrites(context.Background(), testDB, writes)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	var got struct {
		Zh   string `gorm:"column:description_zh"`
		Hash string `gorm:"column:description_zh_source_hash"`
	}
	require.NoError(t, testDB.Raw(
		`SELECT description_zh, description_zh_source_hash FROM catalog_character_trait WHERE id = ?`, ids["i80300"]).
		Scan(&got).Error)
	assert.Equal(t, "你好世界", got.Zh)
	assert.Equal(t, hash, got.Hash)

	var staleZh string
	require.NoError(t, testDB.Raw(`SELECT description_zh FROM catalog_character_trait WHERE id = ?`, ids["i80302"]).Scan(&staleZh).Error)
	assert.Equal(t, "旧译", staleZh)

	_, c2, err := planDescWrites(context.Background(), testDB, rows)
	require.NoError(t, err)
	assert.Equal(t, 2, c2.Same)
	assert.Equal(t, 0, c2.Write)
}
