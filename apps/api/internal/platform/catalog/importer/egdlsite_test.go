package importer

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

func seedClaimedWork(t *testing.T, title string) int64 {
	return seedClaimedWorkPW(t, title, 9999)
}

func seedClaimedWorkPW(t *testing.T, title string, pw int64) int64 {
	t.Helper()
	site := "galgame_wiki"
	w := model.CatalogWork{
		MediumID: mediumGalgame, Site: &site, ProductWorkID: &pw, OLang: "ja",
		DisplayName: title, Status: model.WorkStatusLive,
		Extra: datatypes.JSON(`{}`), FieldProvenance: datatypes.JSON(`{}`),
	}
	require.NoError(t, testDB.Create(&w).Error)
	require.NoError(t, testDB.Create(&model.CatalogWorkTitle{WorkID: w.ID, Lang: "ja", Title: title, Kind: model.WorkTitleKindOfficial}).Error)
	return w.ID
}

func TestEGDLsiteWave(t *testing.T) {
	if testDB == nil {
		t.Skip("no test db")
	}
	clean(t)

	w := seedClaimedWork(t, "既存タイトル")
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_external_ref
		(entity_type, entity_id, source_id, external_id, link_kind, matched_by)
		VALUES (5, ?, 5, '100', 1, 'rule:eg-vndb-rosetta')`, w).Error)

	require.NoError(t, testDB.Exec(`INSERT INTO games (id, dlsite_id) VALUES
		(100,'RJ0ATT'), (200,'RJ0MINT'), (300,'RJ0AMB'), (301,'RJ0AMB'), (400,'RJ0MISS')`).Error)

	require.NoError(t, testDB.Exec(`INSERT INTO works (workno, work_name, work_name_kana, maker_id, maker_name, age_category, work_type_string, status, product_json) VALUES
		('RJ0ATT','付属作品','','RG100','同人サークル','3','アドベンチャー','fetched','{"creaters":{"voice_by":[{"id":"7001","name":"声優X","classification":"voice_by"}]}}'::jsonb),
		('RJ0MINT','新作','しんさく','VG200','ブランド社','2','ロールプレイング','fetched','{"creaters":{"voice_by":[{"id":"7002","name":"声優Y","classification":"voice_by"}]}}'::jsonb),
		('RJ0AMB','曖昧','','RG300','','1','アドベンチャー','fetched','{"creaters":[]}'::jsonb)`).Error)

	st, err := New(testDB, testDB, Options{}).RunEGDLsite(testDB)
	require.NoError(t, err)
	assert.Equal(t, 1, st.Attached)
	assert.Equal(t, 1, st.Minted)
	assert.Equal(t, 1, st.Ambiguous, "RJ0AMB claimed by two EG games")
	assert.Equal(t, 1, st.Missing, "RJ0MISS not in dlsite staging")
	assert.Zero(t, st.Already)

	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_release WHERE work_id=`+itoa64(w)), "one release attached to the claimed work")
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_external_ref r JOIN catalog_release rel ON rel.id=r.entity_id
		WHERE rel.work_id=`+itoa64(w)+` AND r.entity_type=6 AND r.source_id=4 AND r.link_kind=0 AND r.matched_by='rule:eg-dlsite-rosetta' AND r.external_id='RJ0ATT'`))
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_work_label WHERE work_id=`+itoa64(w)), "attribution edge")
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_credit WHERE work_id=`+itoa64(w)+` AND source_id=4`), "dlsite creater credit")
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_work_title WHERE work_id=`+itoa64(w)+` AND kind=3 AND title='付属作品'`), "search-hint title for the differing dlsite name")

	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_work WHERE medium_id=1 AND site IS NULL AND display_name='新作'`), "minted unclaimed galgame work")
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_external_ref WHERE entity_type=5 AND source_id=5 AND external_id='200' AND link_kind=1 AND matched_by='rule:eg-dlsite-rosetta'`), "probable EG work-ref")
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_external_ref WHERE entity_type=6 AND source_id=4 AND external_id='RJ0MINT' AND link_kind=0 AND matched_by='rule:eg-dlsite-rosetta'`), "release SKU anchor")

	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_label l JOIN catalog_external_ref r ON r.entity_type=3 AND r.entity_id=l.id AND r.source_id=4 AND r.external_id='VG200' WHERE l.kind=2`), "VJ commercial maker → publisher label")

	st2, err := New(testDB, testDB, Options{}).RunEGDLsite(testDB)
	require.NoError(t, err)
	assert.Equal(t, 2, st2.Already)
	assert.Zero(t, st2.Attached+st2.Minted+st2.ReleasesCreated+st2.EGRefsWritten)
}

func TestEGDLsiteResolveAmbiguous(t *testing.T) {
	if testDB == nil {
		t.Skip("no test db")
	}
	clean(t)

	w1 := seedClaimedWorkPW(t, "B1既存", 8001)
	w2 := seedClaimedWorkPW(t, "B3作品A", 8002)
	w3 := seedClaimedWorkPW(t, "B3作品B", 8003)
	rosetta := func(game, work int64) {
		require.NoError(t, testDB.Exec(`INSERT INTO catalog_external_ref
			(entity_type, entity_id, source_id, external_id, link_kind, matched_by)
			VALUES (5, ?, 5, ?, 1, 'rule:eg-vndb-rosetta')`, work, strconv.FormatInt(game, 10)).Error)
	}
	rosetta(500, w1)
	rosetta(700, w2)
	rosetta(701, w3)

	require.NoError(t, testDB.Exec(`INSERT INTO games (id, dlsite_id) VALUES
		(500,'RJ0B1'),(501,'RJ0B1'),
		(600,'RJ0B2'),(601,'RJ0B2'),
		(700,'RJ0B3'),(701,'RJ0B3')`).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO works (workno, work_name, work_name_kana, maker_id, maker_name, age_category, work_type_string, status, product_json) VALUES
		('RJ0B1','B1付属','','RG500','','3','アドベンチャー','fetched','{"creaters":[]}'::jsonb),
		('RJ0B2','B2新作','','RG600','','2','アドベンチャー','fetched','{"creaters":[]}'::jsonb),
		('RJ0B3','B3曖昧','','RG700','','1','アドベンチャー','fetched','{"creaters":[]}'::jsonb)`).Error)

	stOff, err := New(testDB, testDB, Options{}).RunEGDLsite(testDB)
	require.NoError(t, err)
	assert.Equal(t, 3, stOff.Ambiguous)
	assert.Zero(t, stOff.Attached+stOff.Minted+stOff.AmbB1+stOff.AmbB2+stOff.AmbConflicts)
	assert.Zero(t, scalarInt(t, `SELECT count(*) FROM catalog_release WHERE work_id=`+itoa64(w1)), "default off touches nothing")

	confPath := filepath.Join(t.TempDir(), "conf.tsv")
	st, err := New(testDB, testDB, Options{ResolveAmbiguous: true, ConflictsOut: confPath}).RunEGDLsite(testDB)
	require.NoError(t, err)
	assert.Zero(t, st.Ambiguous)
	assert.Equal(t, 1, st.AmbB1, "RJ0B1: one distinct work → attach")
	assert.Equal(t, 1, st.AmbB2, "RJ0B2: no matched claimant → mint")
	assert.Equal(t, 1, st.AmbConflicts, "RJ0B3: two distinct works → conflict")

	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_release WHERE work_id=`+itoa64(w1)))
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_external_ref WHERE entity_type=6 AND source_id=4 AND external_id='RJ0B1' AND link_kind=0 AND matched_by='rule:eg-dlsite-rosetta'`), "B1 SKU anchor")

	b2work := scalarInt(t, `SELECT id FROM catalog_work WHERE display_name='B2新作' AND site IS NULL`)
	require.NotZero(t, b2work)
	assert.Equal(t, int64(0), scalarInt(t, `SELECT count(*) FROM catalog_external_ref WHERE entity_type=5 AND entity_id=`+itoa64(b2work)+` AND source_id=5`), "B2 mint writes NO EG ref (unattributable)")
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_external_ref WHERE entity_type=6 AND source_id=4 AND external_id='RJ0B2' AND link_kind=0`), "B2 SKU anchor stands")

	assert.Equal(t, int64(0), scalarInt(t, `SELECT count(*) FROM catalog_external_ref WHERE entity_type=6 AND source_id=4 AND external_id='RJ0B3'`), "B3 writes nothing")
	confBytes, err := os.ReadFile(confPath)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(confBytes)), "\n")
	require.Len(t, lines, 2, "header + one conflict row")
	assert.Contains(t, lines[1], "RJ0B3")
	assert.Contains(t, lines[1], itoa64(w2), "conflict row carries both wiki works")
	assert.Contains(t, lines[1], itoa64(w3))

	st2, err := New(testDB, testDB, Options{ResolveAmbiguous: true}).RunEGDLsite(testDB)
	require.NoError(t, err)
	assert.Equal(t, 2, st2.Already, "B1 + B2 worknos now anchored")
	assert.Equal(t, 1, st2.AmbConflicts, "B3 remains a conflict")
	assert.Zero(t, st2.AmbB1+st2.AmbB2)
}

func itoa64(n int64) string {
	return strconv.FormatInt(n, 10)
}

func TestEGDLsiteQuarantinesATitleCollision(t *testing.T) {
	if testDB == nil {
		t.Skip("no test db")
	}
	clean(t)

	existing := seedExistingWork(t, "お姫様は特訓中R！！～性なる魔法修行～")
	require.NoError(t, testDB.Exec(`INSERT INTO games (id, dlsite_id) VALUES
		(800,'RJ0COL'), (801,'RJ0NEW'), (802,'RJ0TWA'), (803,'RJ0TWB')`).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO works (workno, work_name, work_name_kana, maker_id, maker_name, age_category, work_type_string, status, product_json) VALUES
		('RJ0COL','お姫様は特訓中R!!〜性なる魔法修行〜','','RG800','','3','アドベンチャー','fetched','{"creaters":[]}'::jsonb),
		('RJ0NEW','衝突しない同人新作','','RG801','','3','アドベンチャー','fetched','{"creaters":[]}'::jsonb),
		('RJ0TWA','双子タイトル－前編－','','RG802','','3','アドベンチャー','fetched','{"creaters":[]}'::jsonb),
		('RJ0TWB','双子タイトル〜前編〜','','RG803','','3','アドベンチャー','fetched','{"creaters":[]}'::jsonb)`).Error)

	dry, err := New(testDB, testDB, Options{DryRun: true}).RunEGDLsite(testDB)
	require.NoError(t, err)
	assert.Equal(t, 2, dry.Minted)
	assert.Equal(t, 1, dry.TitleCollisions)
	assert.Equal(t, 1, dry.Quarantined)
	assert.Equal(t, 2, dry.SkippedIntraCollision, "two pending SKUs spelling one title are both held back")
	assert.Zero(t, scalarInt(t, `SELECT count(*) FROM catalog_work WHERE site IS NULL AND id <> `+itoa64(existing)))

	st, err := New(testDB, testDB, Options{}).RunEGDLsite(testDB)
	require.NoError(t, err)
	assert.Equal(t, 2, st.Minted)
	assert.Equal(t, 1, st.Quarantined)

	workOfSKU := func(sku string) int64 {
		t.Helper()
		return scalarInt(t, `SELECT rel.work_id FROM catalog_external_ref r JOIN catalog_release rel ON rel.id = r.entity_id
			WHERE r.entity_type = 6 AND r.source_id = 4 AND r.external_id = '`+sku+`'`)
	}
	col := workOfSKU("RJ0COL")
	require.NotZero(t, col)
	assert.Equal(t, model.WorkStatusQuarantine, workStatusOf(t, col))
	a, b := existing, col
	if b < a {
		a, b = b, a
	}
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_match_candidate
		WHERE entity_type = 5 AND status = 0 AND a_id = `+itoa64(a)+` AND b_id = `+itoa64(b)))
	assert.Equal(t, model.WorkStatusLive, workStatusOf(t, workOfSKU("RJ0NEW")))
	assert.Zero(t, scalarInt(t, `SELECT count(*) FROM catalog_external_ref WHERE entity_type = 6 AND external_id IN ('RJ0TWA','RJ0TWB')`))

	again, err := New(testDB, testDB, Options{}).RunEGDLsite(testDB)
	require.NoError(t, err)
	assert.Equal(t, 2, again.Already)
	assert.Zero(t, again.Minted+again.Quarantined)
	assert.Equal(t, 2, again.SkippedIntraCollision)
}

func TestEGDLsiteGatesAndFilesTheEGName(t *testing.T) {
	if testDB == nil {
		t.Skip("no test db")
	}
	clean(t)

	existing := seedExistingWork(t, "リリィとイザベラの館")
	require.NoError(t, testDB.Exec(`INSERT INTO games (id, dlsite_id, gamename) VALUES
		(900,'RJ0DEC','リリィとイザベラの館'), (901,'RJ0ALI','メカクレカノジョ'), (902,'RJ0ODD','温泉へ行こう'),
		(903,'RJ0TW1','双子の館'), (904,'RJ0TW2','双子の館')`).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO works (workno, work_name, work_name_kana, maker_id, maker_name, age_category, work_type_string, status, product_json) VALUES
		('RJ0DEC','【ゲームのみ】リリィとイザベラの館','','RG900','','3','アドベンチャー','fetched','{"creaters":[]}'::jsonb),
		('RJ0ALI','メカクレカノジョ《WIN版》','','RG901','','3','アドベンチャー','fetched','{"creaters":[]}'::jsonb),
		('RJ0ODD','【豪華5特典】プリズナー～てんこ盛り完全版～','','RG902','','3','アドベンチャー','fetched','{"creaters":[]}'::jsonb),
		('RJ0TW1','【ゲームのみ】双子の館','','RG903','','3','アドベンチャー','fetched','{"creaters":[]}'::jsonb),
		('RJ0TW2','双子の館 DL版','','RG904','','3','アドベンチャー','fetched','{"creaters":[]}'::jsonb)`).Error)

	dry, err := New(testDB, testDB, Options{DryRun: true}).RunEGDLsite(testDB)
	require.NoError(t, err)
	assert.Equal(t, 3, dry.Minted)
	assert.Equal(t, 2, dry.SkippedIntraCollision, "two store names that EG files under one title are both held back")
	assert.Equal(t, 1, dry.TitleCollisions, "the store-decorated name hides a title the EG name matches")
	assert.Equal(t, 5, dry.TitlesCreated, "three store names and two EG aliases")

	st, err := New(testDB, testDB, Options{}).RunEGDLsite(testDB)
	require.NoError(t, err)
	assert.Equal(t, 1, st.Quarantined)

	workOfSKU := func(sku string) int64 {
		t.Helper()
		return scalarInt(t, `SELECT rel.work_id FROM catalog_external_ref r JOIN catalog_release rel ON rel.id = r.entity_id
			WHERE r.entity_type = 6 AND r.source_id = 4 AND r.external_id = '`+sku+`'`)
	}
	aliases := func(work int64) int64 {
		t.Helper()
		return scalarInt(t, `SELECT count(*) FROM catalog_work_title WHERE kind = 1 AND work_id = `+itoa64(work))
	}
	dec, ali, odd := workOfSKU("RJ0DEC"), workOfSKU("RJ0ALI"), workOfSKU("RJ0ODD")
	assert.Equal(t, model.WorkStatusQuarantine, workStatusOf(t, dec))
	a, b := existing, dec
	if b < a {
		a, b = b, a
	}
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_match_candidate
		WHERE entity_type = 5 AND status = 0 AND a_id = `+itoa64(a)+` AND b_id = `+itoa64(b)))
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_work_title
		WHERE kind = 1 AND title = 'リリィとイザベラの館' AND work_id = `+itoa64(dec)))
	assert.Equal(t, model.WorkStatusLive, workStatusOf(t, ali))
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_work_title
		WHERE kind = 1 AND title = 'メカクレカノジョ' AND work_id = `+itoa64(ali)))
	assert.Equal(t, model.WorkStatusLive, workStatusOf(t, odd))
	assert.Zero(t, aliases(odd), "an EG name sharing nothing with the store name is not filed")
}

func TestEGDLsiteBackfillsTheEGNameAlias(t *testing.T) {
	if testDB == nil {
		t.Skip("no test db")
	}
	clean(t)

	minted := func(display string, game int64, linkKind int16) int64 {
		t.Helper()
		w := seedExistingWork(t, display)
		require.NoError(t, testDB.Exec(`INSERT INTO catalog_external_ref
			(entity_type, entity_id, source_id, external_id, link_kind, matched_by)
			VALUES (5, ?, 5, ?, ?, 'rule:eg-dlsite-rosetta')`, w, strconv.FormatInt(game, 10), linkKind).Error)
		return w
	}
	decorated := minted("なついろにっき。製品版", 950, model.LinkKindProbable)
	short := minted("MY…", 951, model.LinkKindProbable)
	translated := minted("Maid of the Dead", 952, model.LinkKindProbable)
	confirmed := minted("メカクレカノジョ《WIN版》", 953, model.LinkKindExact)
	require.NoError(t, testDB.Exec(`INSERT INTO games (id, gamename) VALUES
		(950,'なついろにっき。'), (951,'MY… 懐疑編'), (952,'メイド・オブ・ザ・デッド'), (953,'メカクレカノジョ')`).Error)

	dry, err := New(testDB, testDB, Options{DryRun: true}).RunEGDLsite(testDB)
	require.NoError(t, err)
	assert.Equal(t, 2, dry.EGAliases)
	assert.Zero(t, scalarInt(t, `SELECT count(*) FROM catalog_work_title WHERE kind = 1`))

	st, err := New(testDB, testDB, Options{}).RunEGDLsite(testDB)
	require.NoError(t, err)
	assert.Equal(t, 2, st.EGAliases)
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_work_title
		WHERE kind = 1 AND provenance = 0 AND lang = 'ja' AND title = 'なついろにっき。' AND work_id = `+itoa64(decorated)))
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_work_title
		WHERE kind = 1 AND title = 'メカクレカノジョ' AND work_id = `+itoa64(confirmed)), "a ref the judge confirmed to exact still names the game")
	assert.Zero(t, scalarInt(t, `SELECT count(*) FROM catalog_work_title WHERE kind = 1 AND work_id IN (`+
		itoa64(short)+`,`+itoa64(translated)+`)`), "a two-letter store name and a translation file nothing")

	again, err := New(testDB, testDB, Options{}).RunEGDLsite(testDB)
	require.NoError(t, err)
	assert.Zero(t, again.EGAliases)
	dryAgain, err := New(testDB, testDB, Options{DryRun: true}).RunEGDLsite(testDB)
	require.NoError(t, err)
	assert.Zero(t, dryAgain.EGAliases, "a dry run counts only the aliases still missing")
}
