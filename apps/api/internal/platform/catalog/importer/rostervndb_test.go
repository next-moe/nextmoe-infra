package importer

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedVNDBWork(t *testing.T, vid string) int64 {
	t.Helper()
	pwid, err := strconv.ParseInt(strings.TrimPrefix(vid, "v"), 10, 64)
	require.NoError(t, err)
	var workID int64
	require.NoError(t, testDB.Raw(`INSERT INTO catalog_work (medium_id, site, product_work_id, olang, display_name, content_rating, status, extra, field_provenance, display_nsfw)
		VALUES (1,'galgame_wiki',?,'ja','w',0,0,'{}','{}',false) RETURNING id`, pwid).Scan(&workID).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by)
		VALUES (5, ?, 2, ?, 0, 'rule:test-vndb-work-anchor')`, workID, vid).Error)
	return workID
}

func seedVNDBChar(t *testing.T, id, native, romaji, image string) {
	t.Helper()
	require.NoError(t, testDB.Exec(`INSERT INTO src_vndb.chars (id, image, bloodt, cup_size, sex, spoil_sex, gender, spoil_gender, main, main_spoil, s_bust, s_waist, s_hip, birthday, height, description, ingested_at)
		VALUES (?,?,'','','','','','','',0,0,0,0,0,0,'',now())`, id, image).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO src_vndb.chars_names (id, lang, name, latin) VALUES (?, 'ja', ?, ?)`, id, native, romaji).Error)
}

func seedVNDBImage(t *testing.T, id string, sexual, violence int16) {
	t.Helper()
	require.NoError(t, testDB.Exec(`INSERT INTO src_vndb.images (id, width, height, c_votecount, c_sexual_avg, c_sexual_stddev, c_violence_avg, c_violence_stddev, c_weight)
		VALUES (?,250,300,5,?,0,?,0,1)`, id, sexual, violence).Error)
}

func seedCharVN(t *testing.T, charID, vid, role string, spoil int16) {
	t.Helper()
	require.NoError(t, testDB.Exec(`INSERT INTO src_vndb.chars_vns (id, vid, rid, role, spoil) VALUES (?,?,'',?,?)`, charID, vid, role, spoil).Error)
}

func TestRosterVNDBWave(t *testing.T) {
	clean(t)
	work := seedVNDBWork(t, "v100")
	seedVNDBChar(t, "c1", "主人公", "Shujinkou", "ch1")
	seedVNDBChar(t, "c2", "悪役", "Akuyaku", "ch2")
	seedVNDBChar(t, "c3", "端役", "Hayaku", "")
	seedVNDBImage(t, "ch1", 0, 0)
	seedVNDBImage(t, "ch2", 200, 0)
	seedCharVN(t, "c1", "v100", "main", 0)
	seedCharVN(t, "c2", "v100", "appears", 2)
	seedCharVN(t, "c3", "v999", "main", 0)

	st, err := New(testDB, nil, Options{Source: "vndb"}).RunRoster("vndb")
	require.NoError(t, err)
	assert.Equal(t, 2, st.CharactersCreated, "c1+c2 created; c3 out of gate")
	assert.Zero(t, st.AttachedExisting)
	assert.Equal(t, 2, st.EdgesWritten)
	assert.Equal(t, 1, st.PortraitCandidates, "only c1's portrait clears the threshold")
	assert.Zero(t, st.SkippedNoWorkAnchor)
	assert.Zero(t, st.Errors)

	c1 := vndbCharID(t, "c1")
	c2 := vndbCharID(t, "c2")
	assert.Equal(t, int64(model.WorkCharacterKindMain), scalarInt(t, fmt.Sprintf(`SELECT kind FROM catalog_work_character WHERE work_id=%d AND character_id=%d`, work, c1)))
	assert.Equal(t, int64(model.WorkCharacterKindAppears), scalarInt(t, fmt.Sprintf(`SELECT kind FROM catalog_work_character WHERE work_id=%d AND character_id=%d`, work, c2)))
	assert.Equal(t, int64(model.SpoilerNone), scalarInt(t, fmt.Sprintf(`SELECT spoiler FROM catalog_work_character WHERE work_id=%d AND character_id=%d`, work, c1)))
	assert.Equal(t, int64(model.SpoilerSevere), scalarInt(t, fmt.Sprintf(`SELECT spoiler FROM catalog_work_character WHERE work_id=%d AND character_id=%d`, work, c2)))

	var dn string
	require.NoError(t, testDB.Raw(`SELECT display_name FROM catalog_character WHERE id=?`, c1).Scan(&dn).Error)
	assert.Equal(t, "主人公", dn)
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_external_ref WHERE entity_type=4 AND source_id=2 AND external_id='c1' AND link_kind=0 AND matched_by='rule:vndb-character-import'`))
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_work_character WHERE matched_by='import:character-roster-vndb' AND character_id=`+fmt.Sprint(c1)))
	assert.Equal(t, int64(1), scalarInt(t, fmt.Sprintf(`SELECT count(*) FROM catalog_character_alias WHERE character_id=%d AND name='Shujinkou' AND kind=%d`, c1, model.AliasKindSpellingVariant)))

	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM src_vndb.portrait_backfill`))
	assert.Equal(t, int64(1), scalarInt(t, fmt.Sprintf(`SELECT count(*) FROM src_vndb.portrait_backfill WHERE catalog_character_id=%d AND image_id='ch1'`, c1)))

	assert.Zero(t, scalarInt(t, `SELECT count(*) FROM catalog_person`))
	assert.Zero(t, scalarInt(t, `SELECT count(*) FROM catalog_external_ref WHERE entity_type=4 AND source_id=2 AND external_id='c3'`))

	st2, err := New(testDB, nil, Options{Source: "vndb"}).RunRoster("vndb")
	require.NoError(t, err)
	assert.Zero(t, st2.CharactersCreated)
	assert.Zero(t, st2.AttachedExisting)
	assert.Zero(t, st2.EdgesWritten)
	assert.Zero(t, st2.AliasesCreated)
	assert.Equal(t, 2, st2.Already)
}

func TestRosterVNDBAttach(t *testing.T) {
	clean(t)
	shared := seedVNDBWork(t, "v200")
	fresh := seedVNDBWork(t, "v201")

	var existing int64
	require.NoError(t, testDB.Raw(`INSERT INTO catalog_character (display_name, lang, description, field_provenance)
		VALUES ('神尾観铃','ja','','{}') RETURNING id`).Scan(&existing).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by)
		VALUES (4, ?, 3, '2', 0, 'rule:bangumi-character-import')`, existing).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_character_alias (character_id, name, lang, kind, is_primary_for_locale, source_id, provenance)
		VALUES (?, 'Kamio Misuzu', 'ja', ?, false, 3, 0)`, existing, model.AliasKindSpellingVariant).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_work_character (work_id, character_id, kind, spoiler, matched_by, created_at, updated_at)
		VALUES (?, ?, ?, 0, 'import:character-roster-bangumi', now(), now())`, shared, existing, model.WorkCharacterKindSecondary).Error)

	seedVNDBChar(t, "c50", "神尾 観鈴", "Kamio Misuzu", "")
	seedCharVN(t, "c50", "v200", "primary", 0)
	seedCharVN(t, "c50", "v201", "primary", 0)

	st, err := New(testDB, nil, Options{Source: "vndb"}).RunRoster("vndb")
	require.NoError(t, err)
	assert.Zero(t, st.CharactersCreated, "matched an existing entity → no new row")
	assert.Equal(t, 1, st.AttachedExisting)
	assert.Equal(t, 1, st.EdgesWritten, "fresh work edge only")
	assert.Equal(t, 1, st.Already, "shared-work edge already exists → not overwritten")

	assert.Equal(t, existing, scalarInt(t, `SELECT entity_id FROM catalog_external_ref WHERE entity_type=4 AND source_id=2 AND external_id='c50' AND matched_by='rule:same-work-character-name'`))
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_character`))
	assert.Equal(t, int64(model.WorkCharacterKindSecondary), scalarInt(t, fmt.Sprintf(`SELECT kind FROM catalog_work_character WHERE work_id=%d AND character_id=%d`, shared, existing)))
	assert.Equal(t, int64(model.WorkCharacterKindMain), scalarInt(t, fmt.Sprintf(`SELECT kind FROM catalog_work_character WHERE work_id=%d AND character_id=%d`, fresh, existing)))

	st2, err := New(testDB, nil, Options{Source: "vndb"}).RunRoster("vndb")
	require.NoError(t, err)
	assert.Zero(t, st2.CharactersCreated)
	assert.Zero(t, st2.AttachedExisting)
	assert.Zero(t, st2.EdgesWritten)
	assert.Equal(t, 2, st2.Already)
}

func seedClaimingChar(t *testing.T, name, vid string, linkKind int16, deleted bool) int64 {
	t.Helper()
	var id int64
	require.NoError(t, testDB.Raw(`INSERT INTO catalog_character (display_name, lang, description, field_provenance)
		VALUES (?,'ja','','{}') RETURNING id`, name).Scan(&id).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by)
		VALUES (4, ?, 2, ?, ?, 'rule:test-claim')`, id, vid, linkKind).Error)
	if deleted {
		require.NoError(t, testDB.Exec(`UPDATE catalog_character SET deleted_at = now() WHERE id = ?`, id).Error)
	}
	return id
}

func TestRosterVNDBClaimedIDsAreNotReminted(t *testing.T) {
	clean(t)
	work := seedVNDBWork(t, "v300")

	seedVNDBChar(t, "c60", "生存者", "Seizonsha", "")
	seedVNDBChar(t, "c61", "亡霊", "Bourei", "")
	seedVNDBChar(t, "c62", "常連", "Jouren", "")
	for _, c := range []string{"c60", "c61", "c62"} {
		seedCharVN(t, c, "v300", "main", 0)
	}
	survivor := seedClaimingChar(t, "生存者", "c60", model.LinkKindProbable, false)
	seedClaimingChar(t, "亡霊", "c61", model.LinkKindProbable, true)
	anchored := seedClaimingChar(t, "常連", "c62", model.LinkKindExact, false)

	st, err := New(testDB, nil, Options{Source: "vndb"}).RunRoster("vndb")
	require.NoError(t, err)
	assert.Equal(t, 1, st.SkippedClaimedProbable, "c60 is claimed probable by an alive character")
	assert.Equal(t, 1, st.CharactersCreated, "only c61 (merged-away holder) is minted")
	assert.Zero(t, st.SkippedRetiredExactSquat)
	assert.Zero(t, st.AttachedExisting)
	assert.Equal(t, 2, st.EdgesWritten, "c61 + c62; c60 gets no edge at all")
	assert.Zero(t, st.Errors, "the skipped plan is dropped up front, not counted as an error")

	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_external_ref WHERE entity_type=4 AND source_id=2 AND external_id='c60'`))
	assert.Equal(t, int64(model.LinkKindProbable), scalarInt(t, `SELECT link_kind FROM catalog_external_ref WHERE entity_type=4 AND source_id=2 AND external_id='c60'`))
	assert.Zero(t, scalarInt(t, fmt.Sprintf(`SELECT count(*) FROM catalog_work_character WHERE character_id=%d`, survivor)))
	assert.Zero(t, scalarInt(t, fmt.Sprintf(`SELECT count(*) FROM catalog_character_alias WHERE character_id=%d`, survivor)))

	fresh := scalarInt(t, `SELECT entity_id FROM catalog_external_ref WHERE entity_type=4 AND source_id=2 AND external_id='c61' AND matched_by='rule:vndb-character-import'`)
	assert.NotZero(t, fresh)
	assert.Equal(t, int64(1), scalarInt(t, fmt.Sprintf(`SELECT count(*) FROM catalog_work_character WHERE work_id=%d AND character_id=%d`, work, fresh)))

	assert.Equal(t, int64(1), scalarInt(t, fmt.Sprintf(`SELECT count(*) FROM catalog_work_character WHERE work_id=%d AND character_id=%d`, work, anchored)))

	stDry, err := New(testDB, nil, Options{Source: "vndb", DryRun: true}).RunRoster("vndb")
	require.NoError(t, err)
	assert.Equal(t, 1, stDry.SkippedClaimedProbable)
	assert.Zero(t, stDry.CharactersCreated)
	assert.Equal(t, 2, stDry.EdgesWritten, "c61 + c62 only")
}

func TestRosterVNDBRetiredExactSquat(t *testing.T) {
	clean(t)
	seedVNDBWork(t, "v310")
	seedVNDBChar(t, "c70", "亡者", "Mouja", "")
	seedCharVN(t, "c70", "v310", "main", 0)
	dead := seedClaimingChar(t, "亡者", "c70", model.LinkKindExact, true)

	st, err := New(testDB, nil, Options{Source: "vndb"}).RunRoster("vndb")
	require.NoError(t, err)
	assert.Equal(t, 1, st.SkippedRetiredExactSquat)
	assert.Zero(t, st.SkippedClaimedProbable)
	assert.Zero(t, st.CharactersCreated)
	assert.Zero(t, st.EdgesWritten)
	assert.Zero(t, scalarInt(t, fmt.Sprintf(`SELECT count(*) FROM catalog_work_character WHERE character_id=%d`, dead)))
}

func vndbCharID(t *testing.T, extID string) int64 {
	t.Helper()
	return scalarInt(t, `SELECT entity_id FROM catalog_external_ref WHERE entity_type=4 AND source_id=2 AND external_id='`+extID+`'`)
}

func seedCreditOnlyChar(t *testing.T, workID int64, name string, source int16, ext string) int64 {
	t.Helper()
	var id, cn int64
	require.NoError(t, testDB.Raw(`INSERT INTO catalog_character (display_name, lang, description, field_provenance)
		VALUES (?,'ja','','{}') RETURNING id`, name).Scan(&id).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by)
		VALUES (4, ?, ?, ?, 0, 'rule:test')`, id, source, ext).Error)
	require.NoError(t, testDB.Raw(`INSERT INTO catalog_credit_name (name, lang, kind, link_visibility) VALUES ('声優'||?::text, 'ja', 0, 0) RETURNING id`, ext).Scan(&cn).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_credit (work_id, credit_name_id, role_id, character_id, spoiler, note, source_id)
		VALUES (?, ?, 1, ?, 0, '', ?)`, workID, cn, id, source).Error)
	return id
}

// Work 1894: the Bangumi credit lane had put 吉宮舞冬 on the cast before the
// VNDB roster ran, and the roster-only attach test minted 吉宮 舞冬 beside it.
func TestRosterVNDBAttachesToACreditOnlyCharacter(t *testing.T) {
	clean(t)
	work := seedVNDBWork(t, "v300")
	credited := seedCreditOnlyChar(t, work, "吉宮舞冬", 3, "220440")
	seedCreditOnlyChar(t, work, "少女", 2, "c999")

	seedVNDBChar(t, "c43843", "吉宮 舞冬", "Yoshimiya Mafuyu", "")
	seedCharVN(t, "c43843", "v300", "primary", 0)
	seedVNDBChar(t, "c1000", "少女", "", "")
	seedCharVN(t, "c1000", "v300", "side", 0)

	st, err := New(testDB, nil, Options{Source: "vndb"}).RunRoster("vndb")
	require.NoError(t, err)
	assert.Equal(t, 1, st.AttachedExisting)
	assert.Equal(t, 1, st.CharactersCreated, "少女 already carries another VNDB id")
	assert.Equal(t, credited, scalarInt(t, `SELECT entity_id FROM catalog_external_ref WHERE entity_type=4 AND source_id=2 AND external_id='c43843'`))
	assert.Equal(t, int64(1), scalarInt(t, fmt.Sprintf(`SELECT count(*) FROM catalog_work_character WHERE work_id=%d AND character_id=%d`, work, credited)))
}
