package importer

import (
	"fmt"
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedExactWork(t *testing.T, source int16, extID int64) int64 {
	t.Helper()
	var workID int64
	require.NoError(t, testDB.Raw(`INSERT INTO catalog_work (medium_id, site, product_work_id, olang, display_name, content_rating, status, extra, field_provenance, display_nsfw)
		VALUES (1,'galgame_wiki',?, 'ja','w',0,0,'{}','{}',false) RETURNING id`, extID).Scan(&workID).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by)
		VALUES (5, ?, ?, ?, 0, 'rule:test-work-anchor')`, workID, source, fmt.Sprint(extID)).Error)
	return workID
}

func seedBangumiChar(t *testing.T, id int64, name string) {
	t.Helper()
	require.NoError(t, testDB.Exec(`INSERT INTO src_bangumi.character (id, role, name, infobox_raw, parse_error, summary, comments, collects, parser_version, ingested_at)
		VALUES (?,1,?,'','','',0,0,'v',now())`, id, name).Error)
}

func TestRosterBangumiWave(t *testing.T) {
	clean(t)
	work := seedExactWork(t, 3, 100)

	seedBangumiChar(t, 50, "渚")
	seedBangumiChar(t, 51, "モブ")
	seedBangumiChar(t, 52, "秋生")
	require.NoError(t, testDB.Exec(`INSERT INTO src_bangumi.subject_character (character_id, subject_id, type, item_order)
		VALUES (50,100,1,0),(51,100,4,1),(52,999,1,0)`).Error)

	st, err := New(testDB, nil, Options{Source: "bangumi"}).RunRoster("bangumi")
	require.NoError(t, err)
	assert.Equal(t, 2, st.CharactersCreated, "chars 50+51 created; 52 out of gate")
	assert.Equal(t, 2, st.EdgesWritten)
	assert.Zero(t, st.SkippedNoWorkAnchor)
	assert.Zero(t, st.Errors)

	assert.Equal(t, int64(model.WorkCharacterKindMain),
		scalarInt(t, fmt.Sprintf(`SELECT kind FROM catalog_work_character wc
			JOIN catalog_external_ref r ON r.entity_type=4 AND r.entity_id=wc.character_id AND r.source_id=3 AND r.external_id='50'
			WHERE wc.work_id=%d`, work)))
	assert.Equal(t, int64(model.WorkCharacterKindUnknown),
		scalarInt(t, fmt.Sprintf(`SELECT kind FROM catalog_work_character wc
			JOIN catalog_external_ref r ON r.entity_type=4 AND r.entity_id=wc.character_id AND r.source_id=3 AND r.external_id='51'
			WHERE wc.work_id=%d`, work)))

	var anchorRule string
	require.NoError(t, testDB.Raw(`SELECT matched_by FROM catalog_external_ref WHERE entity_type=4 AND source_id=3 AND external_id='50'`).Scan(&anchorRule).Error)
	assert.Equal(t, "rule:bangumi-character-import", anchorRule)

	var mb string
	require.NoError(t, testDB.Raw(`SELECT matched_by FROM catalog_work_character LIMIT 1`).Scan(&mb).Error)
	assert.Equal(t, "import:character-roster-bangumi", mb)

	assert.Zero(t, scalarInt(t, `SELECT count(*) FROM catalog_person`), "no person rows")
	assert.Zero(t, scalarInt(t, `SELECT count(*) FROM catalog_credit`), "roster does not touch credits")
	assert.Zero(t, scalarInt(t, `SELECT count(*) FROM catalog_external_ref WHERE entity_type=4 AND source_id=3 AND external_id='52'`))

	st2, err := New(testDB, nil, Options{Source: "bangumi"}).RunRoster("bangumi")
	require.NoError(t, err)
	assert.Zero(t, st2.CharactersCreated)
	assert.Zero(t, st2.EdgesWritten)
	assert.Equal(t, 2, st2.Already, "both edges already present")
}

func TestRosterEGWave(t *testing.T) {
	clean(t)
	work := seedExactWork(t, 5, 7)
	testDB.Exec(`INSERT INTO characters (id, raw) VALUES (800,'{"name":"EGキャラ"}'),(801,'{"name":"別キャラ"}')`)
	testDB.Exec(`INSERT INTO appearances (game, character_id) VALUES (7,800),(8,801)`)

	st, err := New(testDB, testDB, Options{Source: "eg"}).RunRoster("eg")
	require.NoError(t, err)
	assert.Equal(t, 1, st.CharactersCreated)
	assert.Equal(t, 1, st.EdgesWritten)
	assert.Equal(t, 1, st.SkippedNoWorkAnchor, "game 8 out of gate")

	assert.Equal(t, int64(model.WorkCharacterKindUnknown),
		scalarInt(t, fmt.Sprintf(`SELECT kind FROM catalog_work_character WHERE work_id=%d`, work)))
	var mb string
	require.NoError(t, testDB.Raw(`SELECT matched_by FROM catalog_work_character WHERE work_id=?`, work).Scan(&mb).Error)
	assert.Equal(t, "import:character-roster-eg", mb)
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_external_ref WHERE entity_type=4 AND source_id=5 AND external_id='800' AND link_kind=0 AND matched_by='rule:eg-character-import'`))

	st2, err := New(testDB, testDB, Options{Source: "eg"}).RunRoster("eg")
	require.NoError(t, err)
	assert.Zero(t, st2.CharactersCreated)
	assert.Zero(t, st2.EdgesWritten)
	assert.Equal(t, 1, st2.Already)
}

func TestRosterReusesExistingCharacter(t *testing.T) {
	clean(t)
	work := seedExactWork(t, 3, 100)
	var charID int64
	require.NoError(t, testDB.Raw(`INSERT INTO catalog_character (display_name, lang, description, field_provenance)
		VALUES ('既存','ja','','{}') RETURNING id`).Scan(&charID).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by)
		VALUES (4, ?, 3, '60', 0, 'rule:bangumi-character-import')`, charID).Error)

	seedBangumiChar(t, 60, "既存")
	require.NoError(t, testDB.Exec(`INSERT INTO src_bangumi.subject_character (character_id, subject_id, type, item_order) VALUES (60,100,2,0)`).Error)

	st, err := New(testDB, nil, Options{Source: "bangumi"}).RunRoster("bangumi")
	require.NoError(t, err)
	assert.Zero(t, st.CharactersCreated, "existing character reused, not recreated")
	assert.Equal(t, 1, st.EdgesWritten)
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_external_ref WHERE entity_type=4 AND source_id=3 AND external_id='60'`))
	assert.Equal(t, int64(model.WorkCharacterKindSecondary),
		scalarInt(t, fmt.Sprintf(`SELECT kind FROM catalog_work_character WHERE work_id=%d AND character_id=%d`, work, charID)))
}

func seedCastMember(t *testing.T, workID int64, name, matchedBy string) int64 {
	t.Helper()
	var charID int64
	require.NoError(t, testDB.Raw(`INSERT INTO catalog_character (display_name, lang, description, field_provenance)
		VALUES (?,'ja','','{}') RETURNING id`, name).Scan(&charID).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_work_character (work_id, character_id, kind, spoiler, matched_by)
		VALUES (?,?,0,0,?)`, workID, charID, matchedBy).Error)
	return charID
}

func TestBangumiRosterDoesNotCastBesideAnotherSource(t *testing.T) {
	clean(t)
	cast := seedExactWork(t, 3, 100)
	seedCastMember(t, cast, "渚", ruleRosterVNDB)
	empty := seedExactWork(t, 3, 101)

	seedBangumiChar(t, 50, "渚")
	seedBangumiChar(t, 51, "早苗")
	require.NoError(t, testDB.Exec(`INSERT INTO src_bangumi.subject_character (character_id, subject_id, type, item_order)
		VALUES (50,100,1,0),(51,101,1,0)`).Error)

	dry, err := New(testDB, nil, Options{Source: "bangumi", DryRun: true}).RunRoster("bangumi")
	require.NoError(t, err)
	assert.Equal(t, 1, dry.SkippedForeignRoster)
	assert.Equal(t, 1, dry.CharactersCreated, "a character seen only on a skipped work is not minted")

	st, err := New(testDB, nil, Options{Source: "bangumi"}).RunRoster("bangumi")
	require.NoError(t, err)
	assert.Equal(t, 1, st.SkippedForeignRoster)
	assert.Equal(t, 1, st.EdgesWritten)
	assert.Equal(t, int64(1), scalarInt(t, fmt.Sprintf(`SELECT count(*) FROM catalog_work_character WHERE work_id=%d`, cast)),
		"the VNDB cast is not joined by a second 渚")
	assert.Equal(t, int64(1), scalarInt(t, fmt.Sprintf(`SELECT count(*) FROM catalog_work_character WHERE work_id=%d`, empty)))
	assert.Zero(t, scalarInt(t, `SELECT count(*) FROM catalog_external_ref WHERE entity_type=4 AND source_id=3 AND external_id='50'`))

	again, err := New(testDB, nil, Options{Source: "bangumi"}).RunRoster("bangumi")
	require.NoError(t, err)
	assert.Equal(t, 1, again.Already, "the lane's own cast does not count as foreign")
	assert.Zero(t, again.EdgesWritten)
}

func TestEGRosterDoesNotCastBesideAnotherSource(t *testing.T) {
	clean(t)
	cast := seedExactWork(t, 5, 7)
	seedCastMember(t, cast, "EGキャラ", ruleRosterBangumi)
	empty := seedExactWork(t, 5, 8)
	testDB.Exec(`INSERT INTO characters (id, raw) VALUES (800,'{"name":"EGキャラ"}'),(801,'{"name":"別キャラ"}')`)
	testDB.Exec(`INSERT INTO appearances (game, character_id) VALUES (7,800),(8,801)`)

	st, err := New(testDB, testDB, Options{Source: "eg"}).RunRoster("eg")
	require.NoError(t, err)
	assert.Equal(t, 1, st.SkippedForeignRoster)
	assert.Equal(t, 1, st.CharactersCreated)
	assert.Equal(t, 1, st.EdgesWritten)
	assert.Equal(t, int64(1), scalarInt(t, fmt.Sprintf(`SELECT count(*) FROM catalog_work_character WHERE work_id=%d`, cast)))
	assert.Equal(t, int64(1), scalarInt(t, fmt.Sprintf(`SELECT count(*) FROM catalog_work_character WHERE work_id=%d`, empty)))
}
