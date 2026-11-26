package importer

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedRosterChar(t *testing.T, workID int64, name string, bgmID string) int64 {
	t.Helper()
	var id int64
	require.NoError(t, testDB.Raw(`INSERT INTO catalog_character (display_name, lang, description, field_provenance)
		VALUES (?,'ja','','{}') RETURNING id`, name).Scan(&id).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by)
		VALUES (4, ?, 3, ?, 0, 'rule:bangumi-character-import')`, id, bgmID).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_work_character (work_id, character_id, kind, spoiler, matched_by, field_provenance)
		VALUES (?, ?, 2, 0, 'import:character-roster-bangumi', '{}')`, workID, id).Error)
	return id
}

// Work 2452 (Timepiece Ensemble): the EG credit lane minted テトメト 237605 and
// 蔵前 こなみ beside roster characters 2118 and 蔵前こなみ.
func TestEGCreditsAttachToTheRosterCharacter(t *testing.T) {
	clean(t)
	work := seedExactWork(t, 5, 7)
	tetomet := seedRosterChar(t, work, "テトメト", "22860")
	konami := seedRosterChar(t, work, "蔵前こなみ", "22858")
	twinA := seedRosterChar(t, work, "な な", "1")
	seedRosterChar(t, work, "なな", "2")
	testDB.Exec(`INSERT INTO creaters (id, raw) VALUES (501,'{"name":"藤咲ウサ"}'),(502,'{"name":"羽鳥いち"}'),(503,'{"name":"声"}'),(504,'{"name":"新人"}')`)
	testDB.Exec(`INSERT INTO characters (id, raw) VALUES (2244,'{"name":"テトメト"}'),(2242,'{"name":"蔵前 こなみ"}'),(2250,'{"name":"なな"}'),(2251,'{"name":"新キャラ"}')`)
	testDB.Exec(`INSERT INTO appearance_actors (game, character_id, actor_id) VALUES (7,2244,501),(7,2242,502),(7,2250,503),(7,2251,504)`)

	st, err := New(testDB, testDB, Options{Source: "eg"}).Run("eg")
	require.NoError(t, err)
	assert.Equal(t, 2, st.CharactersAttached)
	assert.Equal(t, 2, st.CharactersCreated, "なな folds onto two roster characters, 新キャラ onto none")

	for ext, want := range map[string]int64{"2244": tetomet, "2242": konami} {
		assert.Equal(t, want, scalarInt(t, fmt.Sprintf(
			`SELECT entity_id FROM catalog_external_ref WHERE entity_type=4 AND source_id=5 AND external_id='%s' AND matched_by='rule:same-work-character-name'`, ext)), ext)
		assert.Equal(t, want, scalarInt(t, fmt.Sprintf(
			`SELECT c.character_id FROM catalog_credit c JOIN catalog_external_ref r ON r.entity_type=4 AND r.entity_id=c.character_id AND r.source_id=5 AND r.external_id='%s' WHERE c.work_id=%d`, ext, work)), ext)
	}
	assert.NotEqual(t, twinA, scalarInt(t, `SELECT entity_id FROM catalog_external_ref WHERE entity_type=4 AND source_id=5 AND external_id='2250'`))
	assert.Equal(t, int64(4+2), scalarInt(t, `SELECT count(*) FROM catalog_character`))

	st2, err := New(testDB, testDB, Options{Source: "eg"}).Run("eg")
	require.NoError(t, err)
	assert.Zero(t, st2.CharactersAttached)
	assert.Zero(t, st2.CharactersCreated)
}
