package main

import (
	"context"
	"encoding/csv"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeCSV(t *testing.T, rows [][]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "review.csv")
	f, err := os.Create(path)
	require.NoError(t, err)
	w := csv.NewWriter(f)
	require.NoError(t, w.WriteAll(rows))
	require.NoError(t, f.Close())
	return path
}

func TestApplyReviewedCSVWritesMachineRows(t *testing.T) {
	truncateCharacters(t)
	alice := seedCharacter(t, "アリス", nil)
	rejected := seedCharacter(t, "ひなた", nil)
	skipped := seedCharacter(t, "レナ", nil)

	path := writeCSV(t, [][]string{
		{"character_id", "name", "latin", "works", "uses", "proposed_zh", "model"},
		{itoa(alice), "アリス", "Alice", "w1", "3", "爱丽丝", "glm-5.2"},
		{itoa(rejected), "ひなた", "", "w2", "2", "", "glm-5.2"},   // reviewer blanked it
		{itoa(skipped), "レナ", "", "w3", "1", "SKIP", "glm-5.2"}, // model declined
		{"999999", "ゴースト", "", "", "0", "幽灵", "glm-5.2"},        // character gone
	})
	require.NoError(t, runApplyCSV(context.Background(), testDB, path, 0, 0))

	rows := loadAliases(t, alice)
	require.Len(t, rows, 1)
	assert.Equal(t, "爱丽丝", rows[0].Name)
	assert.Equal(t, "zh-Hans", rows[0].Lang)
	assert.EqualValues(t, 1, rows[0].Provenance, "a reviewed MT name is still machine provenance")
	assert.Equal(t, "glm-5.2", rows[0].MTModel)
	assert.False(t, rows[0].IsPrimary, "a machine name never claims the locale primary")

	assert.Empty(t, loadAliases(t, rejected))
	assert.Empty(t, loadAliases(t, skipped))
}

func TestApplyReviewedCSVDropsIdentityProposals(t *testing.T) {
	truncateCharacters(t)
	id := seedCharacter(t, "雪村時音", nil)
	path := writeCSV(t, [][]string{
		{"character_id", "name", "latin", "works", "uses", "proposed_zh", "model"},
		{itoa(id), "雪村時音", "", "", "1", "雪村時音", "glm-5.2"},
	})
	require.NoError(t, runApplyCSV(context.Background(), testDB, path, 0, 0))
	assert.Empty(t, loadAliases(t, id), "an identity proposal is the passthrough lane's call, not the model's")
}

func TestApplyReviewedCSVIsIdempotent(t *testing.T) {
	truncateCharacters(t)
	id := seedCharacter(t, "アリス", nil)
	path := writeCSV(t, [][]string{
		{"character_id", "name", "latin", "works", "uses", "proposed_zh", "model"},
		{itoa(id), "アリス", "", "", "1", "爱丽丝", "glm-5.2"},
	})
	require.NoError(t, runApplyCSV(context.Background(), testDB, path, 0, 0))
	require.NoError(t, runApplyCSV(context.Background(), testDB, path, 0, 0))
	assert.Len(t, loadAliases(t, id), 1)
}

func TestLoadMTResidueSelectsOnlyTheResidue(t *testing.T) {
	truncateCharacters(t)
	kanji := seedCharacter(t, "雪村時音", nil) // passthrough's, not MT's
	named := seedCharacter(t, "時雨", nil)   // already has a zh name
	seedAlias(t, named, "时雨", "zh-Hans", 0, 0, true)
	kana := seedCharacter(t, "アリス", strPtr("Alice"))
	seedWorkWithCharacter(t, kana)

	cands, err := loadMTResidue(context.Background(), testDB, 0)
	require.NoError(t, err)
	require.Len(t, cands, 1, "only the kana name with a live work qualifies")
	assert.Equal(t, kana, cands[0].ID)
	assert.Equal(t, "Alice", cands[0].Latin)
	assert.EqualValues(t, 1, cands[0].Uses)
	assert.NotEmpty(t, cands[0].Works, "the roster work's title rides along as context")
	_ = kanji
}

func itoa(id int64) string {
	return strconv.FormatInt(id, 10)
}

func strPtr(s string) *string { return &s }

func seedWorkWithCharacter(t *testing.T, charID int64) int64 {
	t.Helper()
	var workID int64
	require.NoError(t, testDB.Raw(`INSERT INTO catalog_work
		(medium_id, olang, display_name, content_rating, status, display_nsfw,
		 extra, field_provenance, created_at, updated_at)
		VALUES (1, 'ja', 'テスト作品', 0, 0, false, '{}', '{}', now(), now()) RETURNING id`).Scan(&workID).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_work_title
		(work_id, lang, title, kind, provenance)
		VALUES (?, 'ja', 'テスト作品', 0, 0)`, workID).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_work_character
		(work_id, character_id, kind, spoiler, matched_by, created_at, updated_at)
		VALUES (?, ?, 0, 0, 'test', now(), now())`, workID, charID).Error)
	return workID
}
