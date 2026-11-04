package orglabels

import (
	"context"
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func repairSetup(t *testing.T) repairPools {
	t.Helper()
	if testDB == nil {
		t.Skip("no test db")
	}
	cleanAll(t)
	for _, s := range []string{
		`CREATE TABLE IF NOT EXISTS works (workno text, maker_id text, maker_name text)`,
		`TRUNCATE works, catalog_release_label, catalog_release, catalog_credit RESTART IDENTITY CASCADE`,
	} {
		require.NoError(t, testDB.Exec(s).Error)
	}
	return repairPools{catalog: testDB, eg: testDB, dlsite: testDB}
}

func exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	require.NoError(t, testDB.Exec(sql, args...).Error)
}

func labelRef(t *testing.T, label int64, source int16, ext string, kind int16, rule string) {
	t.Helper()
	exec(t, `INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by, created_at)
		VALUES (3, ?, ?, ?, ?, ?, now())`, label, source, ext, kind, rule)
}

func scalar(t *testing.T, sql string, args ...any) int64 {
	t.Helper()
	var n int64
	require.NoError(t, testDB.Raw(sql, args...).Scan(&n).Error)
	return n
}

func seedTranslatorCase(t *testing.T) {
	mkLabel(t, 753, "ぱんみみそふと", model.LabelKindDoujinCircle)
	labelRef(t, 753, sourceVNDB, "p18414", 0, ruleVNDBCoworks)
	labelRef(t, 753, sourceOfficialSite, "canneed.eu.org", 0, "rule:vndb-extlink")
	labelRef(t, 753, sourceTwitter, "panno_mimi", 0, ruleEGTwitter)
	exec(t, `INSERT INTO src_vndb.producers (id,type,lang,name,latin,alias,description) VALUES ('p18414','ng','zh','Can/need','','','')`)
	mkWork(t, 4829)
	exec(t, `INSERT INTO catalog_release (id, work_id, kind) VALUES (325819, 4829, 0)`)
	exec(t, `INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by, created_at)
		VALUES (6, 325819, 2, 'r161944', 0, 'rule:test', now())`)
	exec(t, `INSERT INTO src_vndb.releases_producers (id,pid,developer,publisher) VALUES ('r161944','p18414',false,true)`)
	exec(t, `INSERT INTO catalog_release_label (release_id, label_id, kind, source_id) VALUES (325819, 753, 1, 2)`)
	exec(t, `INSERT INTO catalog_work_label (work_id, label_id, kind, source_id) VALUES (4829, 753, 1, 2)`)
	exec(t, `INSERT INTO catalog_label_intro (label_id, lang, intro, source_id, provenance) VALUES (753, 'en', 'A new amateur group', 2, 0)`)
	exec(t, `INSERT INTO catalog_label_alias (label_id, name, lang, kind, is_primary_for_locale, provenance) VALUES
		(753, 'Can/need', '', 2, false, 0), (753, 'Panmimi Soft', '', 2, false, 0)`)
}

func TestRepairMovesATranslatorOffTheDeveloper(t *testing.T) {
	p := repairSetup(t)
	seedTranslatorCase(t)

	res, err := repairAll(context.Background(), p, []RepairDecision{
		{Action: "detach", LabelID: 753, Source: sourceVNDB, ExtID: "p18414", Reason: "zh fan translator"},
	}, true)
	require.NoError(t, err)
	require.Len(t, res, 1)
	r := res[0]
	require.Equal(t, "detached", r.Status, r.Err)
	assert.Equal(t, "minted", r.TargetHow)

	var name string
	require.NoError(t, testDB.Raw(`SELECT display_name FROM catalog_label WHERE id = ?`, r.TargetID).Scan(&name).Error)
	assert.Equal(t, "Can/need", name)
	assert.Zero(t, scalar(t, `SELECT count(*) FROM catalog_external_ref WHERE entity_type = 3 AND entity_id = 753 AND source_id = 2`))
	assert.EqualValues(t, 1, scalar(t, `SELECT count(*) FROM catalog_external_ref WHERE entity_type = 3 AND entity_id = ?
		AND source_id = 2 AND external_id = 'p18414' AND link_kind = 0`, r.TargetID))
	assert.EqualValues(t, 1, scalar(t, `SELECT count(*) FROM catalog_match_rejection WHERE entity_id = 753 AND external_id = 'p18414'`))

	assert.EqualValues(t, 1, scalar(t, `SELECT count(*) FROM catalog_release_label WHERE label_id = ?`, r.TargetID))
	assert.EqualValues(t, 1, scalar(t, `SELECT count(*) FROM catalog_work_label WHERE label_id = ?`, r.TargetID))
	assert.Zero(t, scalar(t, `SELECT count(*) FROM catalog_release_label WHERE label_id = 753`))
	assert.Zero(t, scalar(t, `SELECT count(*) FROM catalog_work_label WHERE label_id = 753`))
	assert.EqualValues(t, 1, scalar(t, `SELECT count(*) FROM catalog_label_intro WHERE label_id = ?`, r.TargetID))
	assert.Zero(t, scalar(t, `SELECT count(*) FROM catalog_label_intro WHERE label_id = 753`))

	assert.EqualValues(t, 1, scalar(t, `SELECT count(*) FROM catalog_external_ref WHERE entity_id = ? AND source_id = 9`, r.TargetID),
		"the website came from the vndb entry, so it leaves with it")
	assert.EqualValues(t, 1, scalar(t, `SELECT count(*) FROM catalog_external_ref WHERE entity_id = 753 AND source_id = 10`),
		"the twitter handle came from erogamescape and stays")
	assert.EqualValues(t, 1, scalar(t, `SELECT count(*) FROM catalog_revision WHERE entity_type = 3 AND entity_id = 753`))
	assert.EqualValues(t, 1, scalar(t, `SELECT count(*) FROM catalog_label_alias WHERE label_id = 753`), "Panmimi Soft stays")
	assert.EqualValues(t, 1, scalar(t, `SELECT count(*) FROM catalog_label_alias WHERE label_id = ? AND name = 'Can/need'`, r.TargetID))
}

func TestRepairDryRunWritesNothing(t *testing.T) {
	p := repairSetup(t)
	seedTranslatorCase(t)

	res, err := repairAll(context.Background(), p, []RepairDecision{
		{Action: "detach", LabelID: 753, Source: sourceVNDB, ExtID: "p18414", Reason: "x"},
	}, false)
	require.NoError(t, err)
	assert.Equal(t, "detached", res[0].Status)
	assert.EqualValues(t, 1, res[0].ReleaseRows)
	assert.EqualValues(t, 1, scalar(t, `SELECT count(*) FROM catalog_external_ref WHERE entity_id = 753 AND source_id = 2`))
	assert.EqualValues(t, 1, scalar(t, `SELECT count(*) FROM catalog_label`))
	assert.Zero(t, scalar(t, `SELECT count(*) FROM catalog_match_rejection`))
}

func TestRepairRehomesByNameAndPromotesTheRightAnchor(t *testing.T) {
	p := repairSetup(t)
	mkLabel(t, 21, "スパイク・チュンソフト", model.LabelKindGameBrand)
	mkLabel(t, 999, "Numskull Games", model.LabelKindPublisher)
	labelRef(t, 21, sourceVNDB, "p10451", 0, ruleVNDBCoworks)
	labelRef(t, 21, sourceVNDB, "p2807", model.LinkKindRelated, "rule:vndb-org-graph-name")
	exec(t, `INSERT INTO src_vndb.producers (id,type,lang,name,latin,alias,description) VALUES
		('p10451','co','en','Numskull Games','','',''), ('p2807','co','ja','スパイク・チュンソフト','Spike Chunsoft','','')`)

	res, err := repairAll(context.Background(), p, []RepairDecision{
		{Action: "detach", LabelID: 21, Source: sourceVNDB, ExtID: "p10451", Reason: "localiser"},
		{Action: "promote", LabelID: 21, Source: sourceVNDB, ExtID: "p2807", Reason: "the developer itself"},
	}, true)
	require.NoError(t, err)
	assert.Equal(t, "detached", res[0].Status, res[0].Err)
	assert.Equal(t, "name", res[0].TargetHow)
	assert.EqualValues(t, 999, res[0].TargetID)
	assert.Equal(t, "promoted", res[1].Status, res[1].Err)
	assert.EqualValues(t, 1, scalar(t, `SELECT count(*) FROM catalog_external_ref WHERE entity_id = 21 AND source_id = 2
		AND external_id = 'p2807' AND link_kind = 0`))
	assert.EqualValues(t, 1, scalar(t, `SELECT count(*) FROM catalog_external_ref WHERE entity_id = 999 AND source_id = 2
		AND external_id = 'p10451' AND link_kind = 0`))
}

func TestRepairKeepsWorksAnotherAnchorStillJustifies(t *testing.T) {
	p := repairSetup(t)
	mkLabel(t, 50, "ブランドA", model.LabelKindGameBrand)
	labelRef(t, 50, sourceEG, "1", 0, ruleEGCoworks)
	labelRef(t, 50, sourceEG, "2", 0, ruleEGCoworks)
	exec(t, `INSERT INTO brands (id, raw) VALUES (1, '{"brandname":"ブランドA","kind":"CORPORATION"}'), (2, '{"brandname":"ブランドB","kind":"CORPORATION"}')`)
	exec(t, `INSERT INTO games (id, brand_id) VALUES (10, 1), (20, 2), (30, 2)`)
	mkWork(t, 100)
	mkWork(t, 200)
	mkWorkAnchor(t, sourceEG, "10", 100)
	mkWorkAnchor(t, sourceEG, "20", 200)
	mkWorkAnchor(t, sourceEG, "30", 100)
	exec(t, `INSERT INTO catalog_work_label (work_id, label_id, kind, source_id) VALUES (100, 50, 3, 5), (200, 50, 3, 5)`)

	res, err := repairAll(context.Background(), p, []RepairDecision{
		{LabelID: 50, Source: sourceEG, ExtID: "2", Reason: "different brand", Action: "detach"},
	}, true)
	require.NoError(t, err)
	r := res[0]
	require.Equal(t, "detached", r.Status, r.Err)
	assert.Equal(t, "minted", r.TargetHow)
	assert.EqualValues(t, 1, r.WorkRows)
	assert.EqualValues(t, 1, scalar(t, `SELECT count(*) FROM catalog_work_label WHERE label_id = 50 AND work_id = 100`),
		"brand 1 still makes work 100")
	assert.EqualValues(t, 1, scalar(t, `SELECT count(*) FROM catalog_work_label WHERE label_id = ? AND work_id = 200`, r.TargetID))
	assert.Zero(t, scalar(t, `SELECT count(*) FROM catalog_work_label WHERE label_id = 50 AND work_id = 200`))
}

func TestRepairSendsTwoWrongAnchorsOfOneCompanyToOneLabel(t *testing.T) {
	p := repairSetup(t)
	mkLabel(t, 15479, "IDES", model.LabelKindGameBrand)
	labelRef(t, 15479, sourceVNDB, "p2389", 0, ruleVNDBCoworks)
	labelRef(t, 15479, sourceEG, "3339", 0, ruleEGCoworks)
	exec(t, `INSERT INTO src_vndb.producers (id,type,lang,name,latin,alias,description) VALUES ('p2389','co','ja','ダイナミックプロ','Dynamic Production','','')`)
	exec(t, `INSERT INTO brands (id, raw) VALUES (3339, '{"brandname":"ダイナミックプロダクション","kind":"CORPORATION"}')`)

	ref := &struct {
		Source int16  `json:"src"`
		ExtID  string `json:"ext"`
	}{sourceVNDB, "p2389"}
	res, err := repairAll(context.Background(), p, []RepairDecision{
		{Action: "detach", LabelID: 15479, Source: sourceVNDB, ExtID: "p2389", Reason: "anime company"},
		{Action: "detach", LabelID: 15479, Source: sourceEG, ExtID: "3339", Reason: "anime company", TargetRef: ref},
	}, true)
	require.NoError(t, err)
	require.Equal(t, "minted", res[0].TargetHow, res[0].Err)
	require.Equal(t, "manual", res[1].TargetHow, res[1].Err)
	assert.Equal(t, res[0].TargetID, res[1].TargetID)
	assert.EqualValues(t, 2, scalar(t, `SELECT count(*) FROM catalog_label`), "IDES and one Dynamic Production, not two")
}
