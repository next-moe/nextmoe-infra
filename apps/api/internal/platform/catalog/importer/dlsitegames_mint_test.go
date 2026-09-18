package importer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/titlekey"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTwoHitsMintQuarantinedWithCandidates(t *testing.T) {
	requireDLGamesDB(t)
	w1 := seedLiveWork(t, "TwoHitGameTitle")
	w2 := seedLiveWork(t, "TwoHitGameTitle")
	insertDL(t, "RJTWO01", "TwoHitGameTitle", "RG1", "3", "ADV", "", "{}")
	st := runDLGames(t, false, 0, "")
	assert.Equal(t, 1, st.QuarantinedGroups)
	assert.Equal(t, 2, st.CandidatesPlanned)
	got := workOfSKU(t, "RJTWO01")
	require.NotZero(t, got)
	assert.Equal(t, model.WorkStatusQuarantine, workStatusOf(t, got))
	assert.Equal(t, int64(2), scalarInt(t, `SELECT count(*) FROM catalog_match_candidate WHERE entity_type = 5 AND status = 0 AND (a_id = `+itoa64(got)+` OR b_id = `+itoa64(got)+`)`))
	_ = w1
	_ = w2
}

func TestCandidatesCappedAtThree(t *testing.T) {
	requireDLGamesDB(t)
	for i := 0; i < 5; i++ {
		seedLiveWork(t, "FiveHitGameTitle")
	}
	insertDL(t, "RJCAP01", "FiveHitGameTitle", "RG1", "3", "ADV", "", "{}")
	st := runDLGames(t, false, 0, "")
	assert.Equal(t, 1, st.QuarantinedGroups)
	assert.Equal(t, 3, st.CandidatesPlanned)
	got := workOfSKU(t, "RJCAP01")
	assert.Equal(t, int64(3), scalarInt(t, `SELECT count(*) FROM catalog_match_candidate WHERE a_id = `+itoa64(got)+` OR b_id = `+itoa64(got)))
	var ids []int64
	require.NoError(t, testDB.Raw(`SELECT CASE WHEN a_id = ? THEN b_id ELSE a_id END FROM catalog_match_candidate WHERE a_id = ? OR b_id = ? ORDER BY 1`, got, got, got).Scan(&ids).Error)
	require.Len(t, ids, 3)
	assert.Equal(t, ids[0], ids[0])
	for i := 1; i < len(ids); i++ {
		assert.Greater(t, ids[i], ids[i-1])
	}
	var all []int64
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_work WHERE display_name = 'FiveHitGameTitle' AND id <> ? ORDER BY id`, got).Scan(&all).Error)
	require.GreaterOrEqual(t, len(all), 5)
	assert.Equal(t, all[:3], ids)
}

func TestNoHitMintsLiveWithEveryRow(t *testing.T) {
	requireDLGamesDB(t)
	insertDLFull(t, "RJNOH01", "NoHitMintGameTitle", "ノヒットカナ", "RGNOH", "NoHit Circle", "3", "ADV", "2021-04-02 00:00:00+00",
		`{"creaters":{"voice_by":[{"id":"8101","name":"Voice X","classification":"voice_by"}]}}`)
	st := runDLGames(t, false, 0, "")
	assert.Equal(t, 1, st.MintedGroups)
	assert.Zero(t, st.QuarantinedGroups)
	wid := workOfSKU(t, "RJNOH01")
	require.NotZero(t, wid)
	assert.Equal(t, model.WorkStatusLive, workStatusOf(t, wid))
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_work WHERE id = `+itoa64(wid)+` AND display_name = 'NoHitMintGameTitle' AND olang = 'ja' AND medium_id = 1 AND content_rating = 2`))
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_work_title WHERE work_id = `+itoa64(wid)+` AND kind = 0 AND title = 'NoHitMintGameTitle' AND lang = 'ja'`))
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_work_title WHERE work_id = `+itoa64(wid)+` AND kind = 3 AND title = 'ノヒットカナ'`))
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_release WHERE work_id = `+itoa64(wid)+` AND kind = 1 AND released_y = 2021 AND released_m = 4 AND released_d = 2`))
	assert.Equal(t, ruleDLsiteGameImport, matchedBySKU(t, "RJNOH01"))
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_work_label WHERE work_id = `+itoa64(wid)))
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_credit WHERE work_id = `+itoa64(wid)+` AND source_id = 4`))
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_revision WHERE entity_type = 5 AND entity_id = `+itoa64(wid)+` AND action = 5`))
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_revision WHERE entity_type = 5 AND entity_id = `+itoa64(wid)+` AND snapshot ? 'work' AND snapshot ? 'titles'`), "the work revision snapshots the work")
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_revision r JOIN catalog_external_ref x ON x.entity_id = r.entity_id AND x.entity_type = 6 AND x.external_id = 'RJNOH01' WHERE r.entity_type = 6 AND r.action = 5`))
}

func TestMintDisplayNameIsStripped(t *testing.T) {
	requireDLGamesDB(t)
	raw := "【スマホ版】スライムバスター・リミテッド"
	insertDL(t, "RJSTR01", raw, "RG1", "3", "ADV", "", "{}")
	st := runDLGames(t, false, 0, "")
	assert.Equal(t, 1, st.MintedGroups)
	wid := workOfSKU(t, "RJSTR01")
	var display string
	require.NoError(t, testDB.Raw(`SELECT display_name FROM catalog_work WHERE id = ?`, wid).Scan(&display).Error)
	assert.Equal(t, titlekey.Strip(raw), display)
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_work_title WHERE work_id = `+itoa64(wid)+` AND kind = 0 AND title = '`+titlekey.Strip(raw)+`'`))
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_work_title WHERE work_id = `+itoa64(wid)+` AND kind = 3 AND title = '`+raw+`'`))
}

func TestAttachAddsSearchHintOnlyForNewKey(t *testing.T) {
	requireDLGamesDB(t)
	w := seedLiveWork(t, "SameGameTitleAA")
	seedDLRef(t, w, "RJHINTA")
	insertDL(t, "RJHINTA", "SameGameTitleAA", "RG1", "3", "ADV", "", "{}")
	insertDL(t, "RJHINTB", "SameGameTitleAA", "RG1", "3", "ADV", "", editionsJSON("RJHINTA", "RJHINTB"))
	insertDL(t, "RJHINTC", "CompletelyDifferentTitleBB", "RG1", "3", "ADV", "", editionsJSON("RJHINTA", "RJHINTC"))
	st := runDLGames(t, false, 0, "")
	assert.Equal(t, 1, st.EditionGroups)
	assert.Equal(t, w, workOfSKU(t, "RJHINTB"))
	assert.Equal(t, w, workOfSKU(t, "RJHINTC"))
	assert.Zero(t, scalarInt(t, `SELECT count(*) FROM catalog_work_title WHERE work_id = `+itoa64(w)+` AND kind = 3 AND title = 'SameGameTitleAA'`))
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_work_title WHERE work_id = `+itoa64(w)+` AND kind = 3 AND title = 'CompletelyDifferentTitleBB'`))
}

func TestIntraBatchMintsOnePrimaryAndAttachesTheRest(t *testing.T) {
	requireDLGamesDB(t)
	insertDL(t, "RJBAT01", "AlphaBridgeGame", "RG1", "3", "ADV", "2021-05-01", `{"language_editions":[{"workno":"RJBAT01","lang":"JPN"}]}`)
	insertDL(t, "RJBAT02", "【スマホ版】AlphaBridgeGame", "RG1", "3", "ADV", "2010-01-01", "{}")
	st := runDLGames(t, false, 0, "")
	assert.Equal(t, 1, st.MintedGroups)
	assert.Equal(t, 1, st.FoldedGroups)
	w1 := workOfSKU(t, "RJBAT01")
	w2 := workOfSKU(t, "RJBAT02")
	assert.Equal(t, w1, w2)
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_work WHERE id = `+itoa64(w1)))
	var display string
	require.NoError(t, testDB.Raw(`SELECT display_name FROM catalog_work WHERE id = ?`, w1).Scan(&display).Error)
	assert.Equal(t, titlekey.Strip("AlphaBridgeGame"), display)
}

func TestSetQuarantinedIfAnyGroupHits(t *testing.T) {
	requireDLGamesDB(t)
	w := seedLiveWork(t, "ZetaExistingGame")
	insertDL(t, "RJSET01", "OmegaBridgeGame", "RG1", "3", "ADV", "2020-01-01", `{"language_editions":[{"workno":"RJSET01","lang":"JPN"}]}`)
	insertDL(t, "RJSET02", "ZetaExistingGame", "RG1", "3", "ADV", "2010-01-01", `{"alt_name":"OmegaBridgeGame"}`)
	st := runDLGames(t, false, 0, "")
	assert.Equal(t, 1, st.MintedGroups)
	assert.Equal(t, 1, st.FoldedGroups)
	assert.Equal(t, 1, st.QuarantinedGroups)
	got := workOfSKU(t, "RJSET01")
	assert.Equal(t, got, workOfSKU(t, "RJSET02"))
	assert.Equal(t, model.WorkStatusQuarantine, workStatusOf(t, got))
	a, b := w, got
	if b < a {
		a, b = b, a
	}
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_match_candidate WHERE a_id = `+itoa64(a)+` AND b_id = `+itoa64(b)))
}

func TestReceiptsMatchWrites(t *testing.T) {
	requireDLGamesDB(t)
	host := seedLiveWork(t, "ReceiptHostGame")
	seedDLRef(t, host, "RJRCPT0")
	insertDL(t, "RJRCPT0", "ReceiptHostGame", "RG1", "3", "ADV", "", "{}")
	insertDL(t, "RJRCPT1", "ReceiptAttachGame", "RG1", "3", "ADV", "", editionsJSON("RJRCPT0", "RJRCPT1"))
	insertDL(t, "RJRCPT2", "ReceiptMintGameXX", "RG1", "3", "ADV", "", "{}")
	path := filepath.Join(t.TempDir(), "receipts.jsonl")
	st := runDLGames(t, false, 0, path)
	assert.Equal(t, 1, st.EditionGroups)
	assert.Equal(t, 1, st.MintedGroups)
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	var actions []DLsiteGamesReceipt
	for _, line := range splitNonEmpty(string(b)) {
		var r DLsiteGamesReceipt
		require.NoError(t, json.Unmarshal([]byte(line), &r))
		actions = append(actions, r)
	}
	seen := map[string]bool{}
	for _, r := range actions {
		for _, wn := range r.Worknos {
			seen[wn] = true
		}
	}
	assert.True(t, seen["RJRCPT1"])
	assert.True(t, seen["RJRCPT2"])
	assert.Equal(t, host, workOfSKU(t, "RJRCPT1"))
	assert.NotZero(t, workOfSKU(t, "RJRCPT2"))
}

func TestHoldoutReportCountsAttachesAgainstTruth(t *testing.T) {
	requireDLGamesDB(t)
	truth := seedLiveWork(t, "HoldoutTruthGame")
	seedCircle(t, truth, "RGHOLD1", "Hold Circle")
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by)
		VALUES (5, ?, 2, 'vhold1', 0, 'rule:vndb-work-import')`, truth).Error)
	seedDLRef(t, truth, "RJHOLD1")
	insertDL(t, "RJHOLD1", "HoldoutTruthGame", "RGHOLD1", "3", "ADV", "", "{}")

	host := seedLiveWork(t, "HoldoutHostOther")
	wrong := seedLiveWork(t, "HoldoutWrongGame")
	seedCircle(t, wrong, "RGHOLD2", "Wrong Circle")
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by)
		VALUES (5, ?, 2, 'vhold2', 0, 'rule:vndb-work-import')`, host).Error)
	seedDLRef(t, host, "RJHOLD2")
	insertDL(t, "RJHOLD2", "HoldoutWrongGame", "RGHOLD2", "3", "ADV", "", "{}")

	im := New(testDB, nil, Options{DryRun: true})
	rep, err := im.ReportDLsiteGamesHoldout(testDB)
	require.NoError(t, err)
	assert.Equal(t, 2, rep.N)
	assert.Equal(t, 1, rep.AttachCorrect)
	assert.Equal(t, 1, rep.AttachWrong)
	require.NotEmpty(t, rep.Wrong)
	assert.Equal(t, "holdout_wrong", rep.Wrong[0].Action)
	assert.Equal(t, []string{"RJHOLD2"}, rep.Wrong[0].Worknos)
	assert.Equal(t, wrong, rep.Wrong[0].WorkID)
}

func splitNonEmpty(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func TestSameTitleOtherCircleIsNotFolded(t *testing.T) {
	requireDLGamesDB(t)
	insertDL(t, "RJCIR01", "SharedDoujinTitle", "RGA", "3", "RPG", "2021-05-01", "{}")
	insertDL(t, "RJCIR02", "SharedDoujinTitle", "RGB", "3", "RPG", "2021-06-01", "{}")
	st := runDLGames(t, false, 0, "")
	assert.Equal(t, 2, st.MintedGroups)
	assert.Zero(t, st.FoldedGroups)
	assert.NotEqual(t, workOfSKU(t, "RJCIR01"), workOfSKU(t, "RJCIR02"))
}

func TestReleaseDateComesFromTheAPIString(t *testing.T) {
	requireDLGamesDB(t)
	insertDL(t, "RJDAY01", "DayBoundaryGame", "RGD", "3", "ADV", "2023-05-14 15:30:00+00", `{"regist_date":"2023-05-15 00:00:00"}`)
	insertDL(t, "RJDAY02", "NoApiDateGame", "RGE", "3", "ADV", "2023-05-14 15:30:00+00", "{}")
	insertDL(t, "RJDAY03", "ApiStringWinsGame", "RGF", "3", "ADV", "2023-05-15 00:00:00+00", `{"regist_date":"2023-05-16 00:00:00"}`)
	runDLGames(t, false, 0, "")
	day := func(sku string) string {
		var s string
		require.NoError(t, testDB.Raw(`SELECT lpad(rel.released_y::text,4,'0')||'-'||lpad(rel.released_m::text,2,'0')||'-'||lpad(rel.released_d::text,2,'0')
			FROM catalog_external_ref r JOIN catalog_release rel ON rel.id = r.entity_id
			WHERE r.entity_type = 6 AND r.source_id = 4 AND r.external_id = ?`, sku).Scan(&s).Error)
		return s
	}
	assert.Equal(t, "2023-05-15", day("RJDAY01"))
	assert.Equal(t, "2023-05-15", day("RJDAY02"), "without the API string the stored instant is read in JST")
	assert.Equal(t, "2023-05-16", day("RJDAY03"), "the API string wins over the stored instant")
}

func TestAnchorOnDeletedReleaseIsNeverReanchored(t *testing.T) {
	requireDLGamesDB(t)
	w := seedLiveWork(t, "OldHostGame")
	seedDLRef(t, w, "RJDEL01")
	require.NoError(t, testDB.Exec(`UPDATE catalog_release SET deleted_at = now() WHERE work_id = ?`, w).Error)
	insertDL(t, "RJDEL01", "ReanchoredAfterDeleteGame", "RGX", "3", "ADV", "2020-01-01", "{}")
	insertDL(t, "RJDEL02", "FreshNeighbourGame", "RGY", "3", "ADV", "2020-01-01", "{}")
	st := runDLGames(t, false, 0, "")
	assert.Equal(t, 1, st.Population)
	assert.Equal(t, 1, st.MintedGroups)
	assert.Zero(t, st.Errors)
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_external_ref WHERE entity_type = 6 AND source_id = 4 AND external_id = 'RJDEL01'`))
	assert.NotZero(t, workOfSKU(t, "RJDEL02"))
}
