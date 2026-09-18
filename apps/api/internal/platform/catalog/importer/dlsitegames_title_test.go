package importer

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCorroboratedByCircleAttachesAllMembers(t *testing.T) {
	requireDLGamesDB(t)
	w := seedLiveWork(t, "CircleHitGameTitle")
	seedCircle(t, w, "RGCIRCLE", "Circle Name")
	insertDL(t, "RJCIR01", "CircleHitGameTitle", "RGCIRCLE", "3", "ADV", "", editionsJSON("RJCIR01", "RJCIR02"))
	insertDL(t, "RJCIR02", "CircleSiblingOther", "RGCIRCLE", "3", "ADV", "", editionsJSON("RJCIR01", "RJCIR02"))
	st := runDLGames(t, false, 0, "")
	assert.Equal(t, 1, st.TitleAttachedGroups)
	assert.Equal(t, w, workOfSKU(t, "RJCIR01"))
	assert.Equal(t, w, workOfSKU(t, "RJCIR02"))
	assert.Equal(t, ruleDLsiteTitleCircle, matchedBySKU(t, "RJCIR01"))
	assert.Equal(t, ruleDLsiteTitleCircle, matchedBySKU(t, "RJCIR02"))
}

func TestCorroboratedByDateAttaches(t *testing.T) {
	requireDLGamesDB(t)
	w := seedLiveWork(t, "DateHitGameTitle")
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_release (work_id, kind, released_y, released_m, released_d, extra)
		VALUES (?, 1, 2020, 1, 15, '{}')`, w).Error)
	insertDL(t, "RJDATE01", "DateHitGameTitle", "RG1", "3", "ADV", "2020-01-15 12:00:00+00", "{}")
	st := runDLGames(t, false, 0, "")
	assert.Equal(t, 1, st.TitleAttachedGroups)
	assert.Equal(t, w, workOfSKU(t, "RJDATE01"))
	assert.Equal(t, ruleDLsiteTitleDate, matchedBySKU(t, "RJDATE01"))
}

func TestCorroboratedByBangumiDateAttaches(t *testing.T) {
	requireDLGamesDB(t)
	w := seedLiveWork(t, "BgmDateHitGame")
	insertBgmInfobox(t, 4201, "", "2020-06-01")
	seedBgmExact(t, w, 4201)
	insertDL(t, "RJBGD01", "BgmDateHitGame", "RG1", "3", "ADV", "2020-06-01 00:00:00+00", "{}")
	st := runDLGames(t, false, 0, "")
	assert.Equal(t, 1, st.TitleAttachedGroups)
	assert.Equal(t, w, workOfSKU(t, "RJBGD01"))
	assert.Equal(t, ruleDLsiteTitleBgm, matchedBySKU(t, "RJBGD01"))
}

func TestUncorroboratedUniqueHitMintsQuarantined(t *testing.T) {
	requireDLGamesDB(t)
	w := seedLiveWork(t, "UncorrHitGameTitle")
	insertDL(t, "RJUNC01", "UncorrHitGameTitle", "RG1", "3", "ADV", "", "{}")
	st := runDLGames(t, false, 0, "")
	assert.Equal(t, 1, st.QuarantinedGroups)
	assert.Equal(t, 1, st.MintedGroups)
	assert.Zero(t, st.TitleAttachedGroups)
	got := workOfSKU(t, "RJUNC01")
	require.NotZero(t, got)
	assert.NotEqual(t, w, got)
	assert.Equal(t, model.WorkStatusQuarantine, workStatusOf(t, got))
	a, b := w, got
	if b < a {
		a, b = b, a
	}
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_match_candidate WHERE entity_type = 5 AND status = 0 AND a_id = `+itoa64(a)+` AND b_id = `+itoa64(b)))
}

func TestStrippedStoreTagStillHits(t *testing.T) {
	requireDLGamesDB(t)
	w := seedLiveWork(t, "スライムバスター・リミテッド")
	insertDL(t, "RJTAG01", "【スマホ版】スライムバスター・リミテッド", "RG1", "3", "ADV", "", "{}")
	st := runDLGames(t, false, 0, "")
	assert.Equal(t, 1, st.QuarantinedGroups)
	got := workOfSKU(t, "RJTAG01")
	require.NotZero(t, got)
	a, b := w, got
	if b < a {
		a, b = b, a
	}
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_match_candidate WHERE entity_type = 5 AND a_id = `+itoa64(a)+` AND b_id = `+itoa64(b)))
}

func TestSearchHintIsNotCorpus(t *testing.T) {
	requireDLGamesDB(t)
	w := seedLiveWork(t, "DifferentOfficialTitle")
	require.NoError(t, testDB.Exec(`UPDATE catalog_work SET display_name = 'DifferentDisplayNameXX' WHERE id = ?`, w).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_work_title (work_id, lang, title, kind, provenance) VALUES (?, 'ja', 'HintOnlyGameTitle', 3, 0)`, w).Error)
	insertDL(t, "RJHINT01", "HintOnlyGameTitle", "RG1", "3", "ADV", "", "{}")
	st := runDLGames(t, false, 0, "")
	assert.Equal(t, 1, st.MintedGroups)
	assert.Zero(t, st.QuarantinedGroups)
	got := workOfSKU(t, "RJHINT01")
	assert.NotEqual(t, w, got)
	assert.Equal(t, model.WorkStatusLive, workStatusOf(t, got))
}

func TestQuarantinedAndDeletedWorksAreNotCorpus(t *testing.T) {
	requireDLGamesDB(t)
	q := seedWorkStatus(t, "HiddenCorpusGameXX", model.WorkStatusQuarantine)
	d := seedLiveWork(t, "HiddenCorpusGameXX")
	require.NoError(t, testDB.Exec(`UPDATE catalog_work SET deleted_at = now() WHERE id = ?`, d).Error)
	insertDL(t, "RJHID01", "HiddenCorpusGameXX", "RG1", "3", "ADV", "", "{}")
	st := runDLGames(t, false, 0, "")
	assert.Equal(t, 1, st.MintedGroups)
	assert.Zero(t, st.QuarantinedGroups)
	got := workOfSKU(t, "RJHID01")
	assert.NotEqual(t, q, got)
	assert.NotEqual(t, d, got)
	assert.Equal(t, model.WorkStatusLive, workStatusOf(t, got))
}

func TestRejectionHonouredOnEveryWrite(t *testing.T) {
	requireDLGamesDB(t)
	host := seedLiveWork(t, "RejectHostGame")
	seedDLRef(t, host, "RJREJA")
	insertDL(t, "RJREJA", "RejectHostGame", "RG1", "3", "ADV", "", "{}")
	insertDL(t, "RJREJB", "RejectAttachGame", "RG1", "3", "ADV", "", editionsJSON("RJREJA", "RJREJB"))
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_match_rejection (entity_type, entity_id, source_id, external_id, reason)
		VALUES (5, ?, 4, 'RJREJB', 'not this work')`, host).Error)

	hit := seedLiveWork(t, "RejectCandidateGame")
	insertDL(t, "RJREJC", "RejectCandidateGame", "RG1", "3", "ADV", "", "{}")
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_match_rejection (entity_type, entity_id, source_id, external_id, reason)
		VALUES (5, ?, 4, 'RJREJC', 'not a candidate')`, hit).Error)

	st := runDLGames(t, false, 0, "")
	assert.GreaterOrEqual(t, st.RejectedSkips, 2)
	assert.Zero(t, workOfSKU(t, "RJREJB"))
	got := workOfSKU(t, "RJREJC")
	require.NotZero(t, got)
	assert.Equal(t, model.WorkStatusLive, workStatusOf(t, got), "a rejected work is no longer a hit, so nothing is left to quarantine against")
	assert.NotEqual(t, hit, got)
	a, b := hit, got
	if b < a {
		a, b = b, a
	}
	assert.Zero(t, scalarInt(t, `SELECT count(*) FROM catalog_match_candidate WHERE a_id = `+itoa64(a)+` AND b_id = `+itoa64(b)))
}
