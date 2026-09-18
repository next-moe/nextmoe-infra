package importer

import (
	"strconv"
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEGCreditsUseExactRefsOfAnyRule(t *testing.T) {
	clean(t)
	exact := seedWorkWithEGRef(t, 10, model.LinkKindExact, "rule:eg-dlsite-rosetta")
	probable := seedWorkWithEGRef(t, 11, model.LinkKindProbable, "rule:eg-vndb-rosetta")
	testDB.Exec(`INSERT INTO creaters (id, raw) VALUES (510,'{"name":"絵師A"}'), (511,'{"name":"絵師B"}')`)
	testDB.Exec(`INSERT INTO staff (game, creater_id, shubetu) VALUES (10,510,1), (11,511,1)`)

	st, err := New(testDB, testDB, Options{Source: "eg"}).Run("eg")
	require.NoError(t, err)
	assert.Equal(t, 1, st.NamesCreated)
	assert.Equal(t, 1, st.CreditsWritten)
	assert.EqualValues(t, 1, scalarIntA(t, `SELECT count(*) FROM catalog_credit WHERE work_id=? AND source_id=5`, exact))
	assert.Zero(t, scalarIntA(t, `SELECT count(*) FROM catalog_credit WHERE work_id=? AND source_id=5`, probable))
}

func TestEGMusicUsesExactRefs(t *testing.T) {
	clean(t)
	exact := seedWorkWithEGRef(t, 20, model.LinkKindExact, "rule:eg-dlsite-rosetta")
	probable := seedWorkWithEGRef(t, 21, model.LinkKindProbable, "rule:eg-vndb-rosetta")
	testDB.Exec(`INSERT INTO creaters (id, raw) VALUES (520,'{"name":"歌手"}')`)
	testDB.Exec(`INSERT INTO game_music (music, game) VALUES (30,20),(31,21)`)
	testDB.Exec(`INSERT INTO singers (raw, music, creater_id) VALUES ('{"featuring":false}',30,520), ('{"featuring":false}',31,520)`)

	st, err := New(testDB, testDB, Options{Source: "eg-music"}).Run("eg-music")
	require.NoError(t, err)
	assert.Equal(t, 1, st.NamesCreated)
	assert.Equal(t, 1, st.CreditsWritten)
	assert.EqualValues(t, 1, scalarIntA(t, `SELECT count(*) FROM catalog_credit WHERE work_id=? AND source_id=5`, exact))
	assert.Zero(t, scalarIntA(t, `SELECT count(*) FROM catalog_credit WHERE work_id=? AND source_id=5`, probable))
}

func TestEGDLsiteAttachesToAnyKnownGame(t *testing.T) {
	clean(t)
	w := seedClaimedWork(t, "関連付き")
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_external_ref
		(entity_type, entity_id, source_id, external_id, link_kind, matched_by)
		VALUES (5, ?, 5, '40', 2, 'rule:eg-xlink-twin')`, w).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO games (id, dlsite_id) VALUES (40,'RJ0REL')`).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO works (workno, work_name, work_name_kana, maker_id, maker_name, age_category, work_type_string, status, product_json) VALUES
		('RJ0REL','関連作品','','RG40','','3','アドベンチャー','fetched','{"creaters":[]}'::jsonb)`).Error)

	st, err := New(testDB, testDB, Options{}).RunEGDLsite(testDB)
	require.NoError(t, err)
	assert.Equal(t, 1, st.Attached)
	assert.Zero(t, st.Minted)
	assert.Zero(t, st.SkippedMultiWork)
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_release WHERE work_id=`+itoa64(w)))
	assert.Zero(t, scalarInt(t, `SELECT count(*) FROM catalog_work WHERE site IS NULL`))
}

func TestEGDLsiteSkipsGameOnTwoWorks(t *testing.T) {
	clean(t)
	w1 := seedClaimedWorkPW(t, "二作品A", 4101)
	w2 := seedClaimedWorkPW(t, "二作品B", 4102)
	for _, w := range []int64{w1, w2} {
		require.NoError(t, testDB.Exec(`INSERT INTO catalog_external_ref
			(entity_type, entity_id, source_id, external_id, link_kind, matched_by)
			VALUES (5, ?, 5, '41', 2, 'rule:eg-xlink-twin')`, w).Error)
	}
	require.NoError(t, testDB.Exec(`INSERT INTO games (id, dlsite_id) VALUES (41,'RJ0MUL')`).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO works (workno, work_name, work_name_kana, maker_id, maker_name, age_category, work_type_string, status, product_json) VALUES
		('RJ0MUL','二作品SKU','','RG41','','3','アドベンチャー','fetched','{"creaters":[]}'::jsonb)`).Error)

	st, err := New(testDB, testDB, Options{}).RunEGDLsite(testDB)
	require.NoError(t, err)
	assert.Equal(t, 1, st.SkippedMultiWork)
	assert.Zero(t, st.Attached)
	assert.Zero(t, st.Minted)
	assert.Zero(t, scalarInt(t, `SELECT count(*) FROM catalog_release`))
	assert.Zero(t, scalarInt(t, `SELECT count(*) FROM catalog_work WHERE site IS NULL`))
}

func seedWorkWithEGRef(t *testing.T, game int64, kind int16, matchedBy string) int64 {
	t.Helper()
	var workID int64
	require.NoError(t, testDB.Raw(`INSERT INTO catalog_work (medium_id, site, product_work_id, olang, display_name, content_rating, status, extra, field_provenance, display_nsfw)
		VALUES (1,'galgame_wiki',?, 'ja','w',0,0,'{}','{}',false) RETURNING id`, game).Scan(&workID).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by)
		VALUES (5, ?, 5, ?, ?, ?)`, workID, strconv.FormatInt(game, 10), kind, matchedBy).Error)
	return workID
}
