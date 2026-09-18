package importer

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func requireDLGamesDB(t *testing.T) {
	t.Helper()
	if testDB == nil {
		t.Skip("no test db")
	}
	clean(t)
}

func insertDL(t *testing.T, workno, name, maker, age, workType, regist, productJSON string) {
	t.Helper()
	insertDLFull(t, workno, name, "", maker, maker, age, workType, regist, productJSON)
}

func insertDLFull(t *testing.T, workno, name, kana, maker, makerName, age, workType, regist, productJSON string) {
	t.Helper()
	if productJSON == "" {
		productJSON = "{}"
	}
	require.NoError(t, testDB.Exec(`INSERT INTO works
		(workno, work_name, work_name_kana, maker_id, maker_name, age_category, work_type, work_type_string, status, regist_date, product_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, '', 'fetched', NULLIF(?,'')::timestamptz, ?::jsonb)`,
		workno, name, kana, maker, makerName, age, workType, regist, productJSON).Error)
}

func runDLGames(t *testing.T, dry bool, limit int, receipts string) DLsiteGamesStats {
	t.Helper()
	im := New(testDB, nil, Options{DryRun: dry, Limit: limit})
	st, err := im.RunDLsiteGamesWith(testDB, DLsiteGamesRun{Receipts: receipts})
	require.NoError(t, err)
	return st
}

func seedDLRef(t *testing.T, workID int64, workno string) {
	t.Helper()
	var relID int64
	require.NoError(t, testDB.Raw(`INSERT INTO catalog_release (work_id, kind, extra) VALUES (?, 1, '{}') RETURNING id`, workID).Scan(&relID).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by)
		VALUES (6, ?, 4, ?, 0, 'rule:eg-dlsite-rosetta')`, relID, workno).Error)
}

func workOfSKU(t *testing.T, sku string) int64 {
	t.Helper()
	return scalarInt(t, `SELECT rel.work_id FROM catalog_external_ref r JOIN catalog_release rel ON rel.id = r.entity_id
		WHERE r.entity_type = 6 AND r.source_id = 4 AND r.external_id = '`+sku+`'`)
}

func seedLiveWork(t *testing.T, title string) int64 {
	t.Helper()
	return seedExistingWork(t, title)
}

func seedWorkStatus(t *testing.T, title string, status int16) int64 {
	t.Helper()
	var wid int64
	require.NoError(t, testDB.Raw(`INSERT INTO catalog_work (medium_id, olang, display_name, content_rating, status, extra, field_provenance, display_nsfw)
		VALUES (1,'ja',?,0,?, '{}','{}',false) RETURNING id`, title, status).Scan(&wid).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_work_title (work_id, lang, title, kind, provenance) VALUES (?, 'ja', ?, 0, 0)`, wid, title).Error)
	return wid
}

func seedCircle(t *testing.T, workID int64, makerID, name string) {
	t.Helper()
	var lid int64
	require.NoError(t, testDB.Raw(`INSERT INTO catalog_label (display_name, lang, kind) VALUES (?, 'ja', 4) RETURNING id`, name).Scan(&lid).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by)
		VALUES (3, ?, 4, ?, 0, 'rule:dlsite-maker-import')`, lid, makerID).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_work_label (work_id, label_id, kind) VALUES (?, ?, 0)`, workID, lid).Error)
}

func seedBgmExact(t *testing.T, workID, bid int64) {
	t.Helper()
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by)
		VALUES (5, ?, 3, ?, 0, 'rule:wiki-bid-typed')`, workID, fmt.Sprint(bid)).Error)
}

func insertBgmInfobox(t *testing.T, id int64, infobox, date string) {
	t.Helper()
	require.NoError(t, testDB.Exec(`INSERT INTO src_bangumi.subject
		(id, type, name, name_cn, infobox_raw, parse_error, platform, summary, nsfw, date, series, score, rank, parser_version, ingested_at)
		VALUES (?, 4, 's', '', ?, '', 0, '', false, ?, false, 0, 0, 'v', now())`, id, infobox, date).Error)
}

func matchedBySKU(t *testing.T, sku string) string {
	t.Helper()
	var rule string
	require.NoError(t, testDB.Raw(`SELECT matched_by FROM catalog_external_ref WHERE entity_type = 6 AND source_id = 4 AND external_id = ?`, sku).Scan(&rule).Error)
	return rule
}

func editionsJSON(worknos ...string) string {
	s := `{"editions":[`
	for i, w := range worknos {
		if i > 0 {
			s += ","
		}
		s += `{"workno":"` + w + `"}`
	}
	return s + `]}`
}
