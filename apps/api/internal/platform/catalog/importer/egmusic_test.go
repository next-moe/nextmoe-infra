package importer

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func scalarIntA(t *testing.T, q string, args ...any) int64 {
	t.Helper()
	var n int64
	require.NoError(t, testDB.Raw(q, args...).Scan(&n).Error)
	return n
}

func seedEGRosettaWork(t *testing.T, game int64) int64 {
	t.Helper()
	var workID int64
	require.NoError(t, testDB.Raw(`INSERT INTO catalog_work (medium_id, site, product_work_id, olang, display_name, content_rating, status, extra, field_provenance, display_nsfw)
		VALUES (1,'galgame_wiki',?, 'ja','w',0,0,'{}','{}',false) RETURNING id`, game).Scan(&workID).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by)
		VALUES (5, ?, 5, ?, 0, 'rule:eg-vndb-rosetta')`, workID, strconv.FormatInt(game, 10)).Error)
	return workID
}

func TestEGMusicWave(t *testing.T) {
	clean(t)

	workA := seedEGRosettaWork(t, 7)
	workB := seedEGRosettaWork(t, 8)

	testDB.Exec(`INSERT INTO creaters (id, raw) VALUES
		(500,'{"name":"歌作曲家"}'), (501,'{"name":"作詞家"}'), (502,'{"name":"編曲家"}'), (503,'{"name":"圏外歌手"}')`)

	testDB.Exec(`INSERT INTO game_music (music, game) VALUES (10,7),(10,8),(11,8),(12,9)`)

	testDB.Exec(`INSERT INTO singers (raw, music, creater_id) VALUES
		('{"featuring":false}',10,500), ('{"featuring":true}',11,500), ('{"featuring":false}',12,503)`)
	testDB.Exec(`INSERT INTO composers (raw, music, creater_id) VALUES ('{"featuring":false}',10,500)`)
	testDB.Exec(`INSERT INTO lyricists (raw, music, creater_id) VALUES ('{"featuring":false}',10,501)`)
	testDB.Exec(`INSERT INTO arrangers (raw, music, creater_id) VALUES ('{"featuring":false}',11,502)`)

	st, err := New(testDB, testDB, Options{Source: "eg-music"}).Run("eg-music")
	require.NoError(t, err)
	assert.Equal(t, 3, st.NamesCreated, "creaters 500/501/502 (503 gated out, never created)")
	assert.Equal(t, 7, st.CreditsWritten, "singer 2 + composer 2 + lyric 2 + arrange 1")
	assert.Zero(t, st.Errors)

	assert.EqualValues(t, 2, scalarInt(t, `SELECT count(*) FROM catalog_credit WHERE source_id=5 AND role_id=286`), "vocal: (A,500)(B,500)")
	assert.EqualValues(t, 2, scalarInt(t, `SELECT count(*) FROM catalog_credit WHERE source_id=5 AND role_id=158`), "composer: (A,500)(B,500)")
	assert.EqualValues(t, 2, scalarInt(t, `SELECT count(*) FROM catalog_credit WHERE source_id=5 AND role_id=199`), "lyric: (A,501)(B,501)")
	assert.EqualValues(t, 1, scalarInt(t, `SELECT count(*) FROM catalog_credit WHERE source_id=5 AND role_id=115`), "arrange: (B,502)")

	assert.EqualValues(t, 1, scalarIntA(t, `SELECT count(*) FROM catalog_credit c
		JOIN catalog_external_ref r ON r.entity_type=1 AND r.entity_id=c.credit_name_id AND r.source_id=5 AND r.external_id='500'
		WHERE c.work_id=?  AND c.role_id=286`, workA))
	assert.EqualValues(t, 1, scalarIntA(t, `SELECT count(*) FROM catalog_credit c
		JOIN catalog_external_ref r ON r.entity_type=1 AND r.entity_id=c.credit_name_id AND r.source_id=5 AND r.external_id='500'
		WHERE c.work_id=? AND c.role_id=286`, workB))

	assert.EqualValues(t, "歌作曲家", scalarStr(t, `SELECT cn.name FROM catalog_credit_name cn
		JOIN catalog_external_ref r ON r.entity_type=1 AND r.entity_id=cn.id AND r.source_id=5 AND r.external_id='500' WHERE cn.person_id IS NULL`))
	assert.EqualValues(t, "rule:eg-creater-import", scalarStr(t, `SELECT matched_by FROM catalog_external_ref WHERE entity_type=1 AND source_id=5 AND external_id='500'`))
	assert.Zero(t, scalarInt(t, `SELECT count(*) FROM catalog_external_ref WHERE entity_type=1 AND source_id=5 AND external_id='503'`))
	assert.Zero(t, scalarInt(t, `SELECT count(*) FROM catalog_credit WHERE source_id=5 AND (character_id IS NOT NULL OR label_id IS NOT NULL)`))

	st2, err := New(testDB, testDB, Options{Source: "eg-music"}).Run("eg-music")
	require.NoError(t, err)
	assert.Zero(t, st2.NamesCreated)
	assert.Zero(t, st2.CreditsWritten)
	assert.Equal(t, 7, st2.Already, "all 7 credits already present")
}

func TestEGMusicSharesStaffNameSpace(t *testing.T) {
	clean(t)
	workID := seedEGRosettaWork(t, 7)

	testDB.Exec(`INSERT INTO creaters (id, raw) VALUES (500,'{"name":"兼任"}')`)
	testDB.Exec(`INSERT INTO staff (game, creater_id, shubetu) VALUES (7,500,1)`)
	_, err := New(testDB, testDB, Options{Source: "eg"}).Run("eg")
	require.NoError(t, err)
	nameID := scalarInt(t, `SELECT entity_id FROM catalog_external_ref WHERE entity_type=1 AND source_id=5 AND external_id='500'`)

	testDB.Exec(`INSERT INTO game_music (music, game) VALUES (20,7)`)
	testDB.Exec(`INSERT INTO singers (raw, music, creater_id) VALUES ('{"featuring":false}',20,500)`)
	st, err := New(testDB, testDB, Options{Source: "eg-music"}).Run("eg-music")
	require.NoError(t, err)
	assert.Zero(t, st.NamesCreated, "creater 500 already anchored by the staff lane → no new name")
	assert.Equal(t, 1, st.CreditsWritten)
	assert.EqualValues(t, nameID, scalarIntA(t, `SELECT credit_name_id FROM catalog_credit WHERE source_id=5 AND role_id=286 AND work_id=?`, workID),
		"the vocal credit reuses the staff-lane credit name")
	assert.EqualValues(t, 1, scalarInt(t, `SELECT count(*) FROM catalog_external_ref WHERE entity_type=1 AND source_id=5 AND external_id='500'`),
		"still exactly one anchor for creater 500")
}
