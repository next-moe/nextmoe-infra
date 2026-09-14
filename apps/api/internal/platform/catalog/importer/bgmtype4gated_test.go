package importer

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedSubject(t *testing.T, id int64, name, nameCN, metaTags, tags string, nsfw bool) {
	t.Helper()
	mt := "NULL"
	if metaTags != "" {
		mt = "'" + metaTags + "'::jsonb"
	}
	tg := "NULL"
	if tags != "" {
		tg = "'" + tags + "'::jsonb"
	}
	require.NoError(t, testDB.Exec(`INSERT INTO src_bangumi.subject
		(id, type, name, name_cn, infobox_raw, parse_error, platform, summary, nsfw, date, series, score, rank, meta_tags, tags, parser_version, ingested_at)
		VALUES (?,4,?,?,'','',4001,'',?, '', false, 0, 0, `+mt+`, `+tg+`, 'v', now())`,
		id, name, nameCN, nsfw).Error)
}

func TestBgmType4GatedWave(t *testing.T) {
	clean(t)
	require.NoError(t, testDB.Exec(`ALTER TABLE games ADD COLUMN IF NOT EXISTS gamename text`).Error)

	seedSubject(t, 1001, "絶対×小夜曲エターナル", "", `["PC","VN","游戏","R18"]`, "", true)
	seedSubject(t, 1002, "青空アドベンチャーPCゲーム", "", `["PC","ADV","R18","游戏"]`, "", false)
	seedSubject(t, 1005, "明示ギャルゲー作品", "", `["Galgame","PC","游戏"]`, "", false)
	seedSubject(t, 1006, "同人ゲーム民間伝承作品", "", `["PC","游戏"]`, `[{"name":"galgame","count":5}]`, false)
	seedSubject(t, 1008, "クロスソース限定作品", "", `["PC","游戏"]`, "", false)
	seedSubject(t, 1014, "", "中文限定galgame作品", `["Galgame","PC","游戏"]`, "", false)

	seedSubject(t, 1003, "西方冒険物語ADV作品", "", `["PC","ADV","游戏"]`, "", false)
	seedSubject(t, 1004, "鉄砲撃ちまくりオンライン", "", `["PC","FPS","游戏"]`, "", false)
	seedSubject(t, 1007, "弱票同人ゲーム作品", "", `["PC","游戏"]`, `[{"name":"galgame","count":2}]`, false)

	seedSubject(t, 1009, "家庭用専売乙女ゲーム", "主机专卖乙女", `["Galgame","NS","游戏"]`, "", false)

	seedSubject(t, 1010, "既存衝突タイトル作品", "", `["Galgame","PC","游戏"]`, "", false)
	existing1010 := seedExistingWork(t, "既存衝突タイトル作品")

	seedSubject(t, 1011, "重複同名ゲーム作品", "", `["Galgame","PC","游戏"]`, "", false)
	seedSubject(t, 1012, "重複同名ゲーム作品", "", `["Galgame","PC","游戏"]`, "", false)

	seedSubject(t, 1013, "既に錨定済み作品", "", `["Galgame","PC","游戏"]`, "", false)
	seedAnchoredWork(t, 1013)

	require.NoError(t, testDB.Exec(`INSERT INTO works (workno, work_name, work_type_string, status)
		VALUES ('RJ900','クロスソース限定作品','アドベンチャー','fetched')`).Error)

	dry, err := New(testDB, testDB, Options{DryRun: true}).RunBgmType4Gated(testDB)
	require.NoError(t, err)
	assert.Equal(t, 13, dry.PoolTotal, "S1..S12 + S14 (S13 anchored, out of pool)")
	assert.Equal(t, 1, dry.ExcludedConsoleMobile, "S9 console-exclusive")
	assert.Equal(t, 12, dry.EligiblePool)
	assert.Equal(t, 2, dry.SigP)
	assert.Equal(t, 6, dry.SigT, "S5,S6,S10,S11,S12,S14")
	assert.Equal(t, 1, dry.SigX, "S8")
	assert.Equal(t, 9, dry.GatedTotal)
	assert.Equal(t, 1, dry.TitleCollisions, "S10")
	assert.Equal(t, 1, dry.ToQuarantine, "S10 planned as quarantine")
	assert.Equal(t, 2, dry.SkippedIntraCollision, "S11+S12")
	assert.Equal(t, 6, dry.ToCreate)
	assert.Zero(t, dry.WorksCreated, "dry writes nothing")
	assert.Len(t, dry.CollisionSamples, 1)
	assert.Equal(t, int64(1010), dry.CollisionSamples[0].SubjectID)

	st, err := New(testDB, testDB, Options{}).RunBgmType4Gated(testDB)
	require.NoError(t, err)
	assert.Equal(t, 7, st.WorksCreated)
	assert.Equal(t, 1, st.Quarantined)
	assert.Equal(t, 7, st.AnchorsCreated)
	assert.Equal(t, 7, st.RevisionsCreated)
	assert.Equal(t, 7, st.TitlesCreated, "5 ja-only live + S14 zh-Hans-only + S10 quarantine")

	assert.Equal(t, int64(6), scalarInt(t, `SELECT count(*) FROM catalog_work w
		JOIN catalog_external_ref e ON e.entity_id=w.id AND e.entity_type=5 AND e.matched_by='rule:bgm-type4-gated' AND e.link_kind=0
		WHERE w.medium_id=1 AND w.site IS NULL AND w.status=0`))
	assert.Equal(t, int64(7), scalarInt(t, `SELECT count(*) FROM catalog_revision WHERE entity_type=5 AND action=5 AND revision=1
		AND entity_id IN (SELECT entity_id FROM catalog_external_ref WHERE matched_by='rule:bgm-type4-gated')`))

	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_work w
		JOIN catalog_external_ref e ON e.entity_id=w.id AND e.external_id='1001' AND e.matched_by='rule:bgm-type4-gated'
		WHERE w.content_rating=2 AND w.olang='ja'`))
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_work_title t
		JOIN catalog_external_ref e ON e.entity_id=t.work_id AND e.external_id='1001' AND e.matched_by='rule:bgm-type4-gated'
		WHERE t.lang='ja' AND t.kind=0 AND t.title='絶対×小夜曲エターナル'`))
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_work w
		JOIN catalog_external_ref e ON e.entity_id=w.id AND e.external_id='1014' AND e.matched_by='rule:bgm-type4-gated'
		WHERE w.olang='zh-Hans'`))
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_work_title t
		JOIN catalog_external_ref e ON e.entity_id=t.work_id AND e.external_id='1014' AND e.matched_by='rule:bgm-type4-gated'
		WHERE t.lang='zh-Hans' AND t.title='中文限定galgame作品'`))
	assert.Zero(t, scalarInt(t, `SELECT count(*) FROM catalog_external_ref WHERE matched_by='rule:bgm-type4-gated' AND external_id IN ('1009','1011','1012','1013','1003','1004','1007')`))
	q1010 := workIDByBgmExt(t, "1010")
	require.NotZero(t, q1010)
	assert.Equal(t, model.WorkStatusQuarantine, workStatusOf(t, q1010))
	a, b := existing1010, q1010
	if b < a {
		a, b = b, a
	}
	assert.Equal(t, int64(1), countWhere(t, `SELECT count(*) FROM catalog_match_candidate WHERE entity_type = ? AND a_id = ? AND b_id = ? AND reason = ? AND status = ?`,
		model.EntityTypeWork, a, b, model.CandidateReasonNameNormEqual, model.CandidateStatusPending))

	st2, err := New(testDB, testDB, Options{}).RunBgmType4Gated(testDB)
	require.NoError(t, err)
	assert.Zero(t, st2.WorksCreated)
	assert.Zero(t, st2.ToCreate)
	assert.Equal(t, 6, dry.ToCreate)
}

func TestBgmType4GatedASCIITightening(t *testing.T) {
	clean(t)
	require.NoError(t, testDB.Exec(`ALTER TABLE games ADD COLUMN IF NOT EXISTS gamename text`).Error)
	require.NoError(t, testDB.Exec(`TRUNCATE src_vndb.releases_titles`).Error)

	seedSubject(t, 2001, "Manhunt Generic Title", "", `["PC","游戏"]`, "", false)
	seedDLsiteGame(t, "Manhunt Generic Title")
	seedSubject(t, 2002, "Ascii Multi Corpus Game", "", `["PC","游戏"]`, "", false)
	seedEGGame(t, 9002, "Ascii Multi Corpus Game")
	seedVNDBTitle(t, "r2", "Ascii Multi Corpus Game")
	seedSubject(t, 2003, "日本語限定タイトル作品", "", `["PC","游戏"]`, "", false)
	seedDLsiteGame(t, "日本語限定タイトル作品")
	seedSubject(t, 2004, "Ascii With P Support", "", `["PC","VN","游戏"]`, "", false)
	seedDLsiteGame(t, "Ascii With P Support")
	seedSubject(t, 2005, "Ascii With T Support", "", `["Galgame","PC","游戏"]`, "", false)
	seedDLsiteGame(t, "Ascii With T Support")

	dry, err := New(testDB, testDB, Options{DryRun: true}).RunBgmType4Gated(testDB)
	require.NoError(t, err)
	assert.Equal(t, 5, dry.EligiblePool)
	assert.Equal(t, 1, dry.SkippedASCIIXOnly, "A1 dropped")
	assert.Equal(t, 4, dry.SigX, "A2,A3,A4,A5 keep effective X; A1 dropped")
	assert.Equal(t, 4, dry.GatedTotal, "A2,A3,A4,A5")
	assert.Equal(t, 4, dry.ToCreate)
	require.Len(t, dry.ASCIIDroppedSamples, 1)
	assert.Equal(t, int64(2001), dry.ASCIIDroppedSamples[0].SubjectID)
	require.Len(t, dry.ASCIISurvivorSamples, 1)
	assert.Equal(t, int64(2002), dry.ASCIISurvivorSamples[0].SubjectID)

	st, err := New(testDB, testDB, Options{}).RunBgmType4Gated(testDB)
	require.NoError(t, err)
	assert.Equal(t, 4, st.WorksCreated)
	assert.Zero(t, scalarInt(t, `SELECT count(*) FROM catalog_external_ref WHERE matched_by='rule:bgm-type4-gated' AND external_id='2001'`))
	assert.Equal(t, int64(4), scalarInt(t, `SELECT count(*) FROM catalog_external_ref WHERE matched_by='rule:bgm-type4-gated' AND external_id IN ('2002','2003','2004','2005')`))
}

func seedDLsiteGame(t *testing.T, name string) {
	t.Helper()
	require.NoError(t, testDB.Exec(`INSERT INTO works (workno, work_name, work_type_string, status)
		VALUES (?, ?, 'アドベンチャー', 'fetched')`, "RJ"+name, name).Error)
}

func seedEGGame(t *testing.T, id int64, name string) {
	t.Helper()
	require.NoError(t, testDB.Exec(`INSERT INTO games (id, gamename) VALUES (?, ?)`, id, name).Error)
}

func seedVNDBTitle(t *testing.T, id, title string) {
	t.Helper()
	require.NoError(t, testDB.Exec(`INSERT INTO src_vndb.releases_titles (id, lang, mtl, title, latin)
		VALUES (?, 'ja', false, ?, '')`, id, title).Error)
}

// The 2026-07 gated wave minted ~4.9k duplicates because these two shapes both
// read as "no existing work": a wiki claim carries only a display_name, and a
// title that differs by one internal space compares unequal on raw title_norm.
func TestBgmType4GatedFoldedCollisions(t *testing.T) {
	clean(t)
	require.NoError(t, testDB.Exec(`ALTER TABLE games ADD COLUMN IF NOT EXISTS gamename text`).Error)

	seedSubject(t, 3001, "表示名だけの既存作品", "", `["Galgame","PC","游戏"]`, "", false)
	seedDisplayOnlyWork(t, "表示名だけの既存作品")

	seedSubject(t, 3002, "空白違いの既存作品", "", `["Galgame","PC","游戏"]`, "", false)
	seedExistingWork(t, "空白 違いの 既存作品")

	seedSubject(t, 3003, "衝突しない新規作品名", "", `["Galgame","PC","游戏"]`, "", false)

	dry, err := New(testDB, testDB, Options{DryRun: true}).RunBgmType4Gated(testDB)
	require.NoError(t, err)
	assert.Equal(t, 3, dry.GatedTotal)
	assert.Equal(t, 2, dry.TitleCollisions, "display-name-only and space-variant both collide")
	assert.Equal(t, 2, dry.ToQuarantine)
	assert.Equal(t, 1, dry.ToCreate)
	assert.Zero(t, dry.WorksCreated)

	st, err := New(testDB, testDB, Options{}).RunBgmType4Gated(testDB)
	require.NoError(t, err)
	assert.Equal(t, 3, st.WorksCreated)
	assert.Equal(t, 2, st.Quarantined)
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_external_ref
		WHERE matched_by='rule:bgm-type4-gated' AND external_id='3003'`))
	assert.Equal(t, int64(2), scalarInt(t, `SELECT count(*) FROM catalog_external_ref
		WHERE matched_by='rule:bgm-type4-gated' AND external_id IN ('3001','3002')`))
	assert.Equal(t, model.WorkStatusQuarantine, workStatusOf(t, workIDByBgmExt(t, "3001")))
	assert.Equal(t, model.WorkStatusQuarantine, workStatusOf(t, workIDByBgmExt(t, "3002")))
	assert.Equal(t, model.WorkStatusLive, workStatusOf(t, workIDByBgmExt(t, "3003")))
}

func TestBgmType4GatedTrailingWaveDashCollides(t *testing.T) {
	clean(t)
	require.NoError(t, testDB.Exec(`ALTER TABLE games ADD COLUMN IF NOT EXISTS gamename text`).Error)

	// work 16269 and bgm 222199 were one game held as two live works for seven
	// weeks: bangumi shipped the subtitle without its closing wave dash, so this
	// gate saw a new title and minted a twin instead of quarantining it.
	seedSubject(t, 3201, "隣人妄想～団地族の昼下がり", "", `["Galgame","PC","游戏"]`, "", false)
	seedExistingWork(t, "隣人妄想～団地族の昼下がり～")

	dry, err := New(testDB, testDB, Options{DryRun: true}).RunBgmType4Gated(testDB)
	require.NoError(t, err)
	assert.Equal(t, 1, dry.TitleCollisions, "a dropped closing wave dash is not a new title")
	assert.Equal(t, 1, dry.ToQuarantine)
	assert.Zero(t, dry.ToCreate)
}

func TestBgmType4GatedIntraCollisionFoldsSpace(t *testing.T) {
	clean(t)
	require.NoError(t, testDB.Exec(`ALTER TABLE games ADD COLUMN IF NOT EXISTS gamename text`).Error)

	seedSubject(t, 3101, "同一プール内衝突作品", "", `["Galgame","PC","游戏"]`, "", false)
	seedSubject(t, 3102, "同一 プール内 衝突作品", "", `["Galgame","PC","游戏"]`, "", false)

	dry, err := New(testDB, testDB, Options{DryRun: true}).RunBgmType4Gated(testDB)
	require.NoError(t, err)
	assert.Equal(t, 2, dry.SkippedIntraCollision, "two pool rows spelling one title must both stand down")
	assert.Zero(t, dry.ToCreate)
}

func TestBgmType4GatedHanFloorCollisions(t *testing.T) {
	clean(t)
	require.NoError(t, testDB.Exec(`ALTER TABLE games ADD COLUMN IF NOT EXISTS gamename text`).Error)

	seedSubject(t, 3201, "", "红楼梦", `["Galgame","PC","游戏"]`, "", false)
	seedDisplayOnlyWork(t, "红楼梦")

	seedSubject(t, 3202, "", "abc", `["Galgame","PC","游戏"]`, "", false)
	seedDisplayOnlyWork(t, "abc")

	dry, err := New(testDB, testDB, Options{DryRun: true}).RunBgmType4Gated(testDB)
	require.NoError(t, err)
	assert.Equal(t, 2, dry.GatedTotal)
	assert.Equal(t, 1, dry.TitleCollisions)
	assert.Equal(t, 1, dry.ToQuarantine)
	assert.Equal(t, 1, dry.ToCreate)
	require.Len(t, dry.CollisionSamples, 1)
	assert.Equal(t, int64(3201), dry.CollisionSamples[0].SubjectID)

	st, err := New(testDB, testDB, Options{}).RunBgmType4Gated(testDB)
	require.NoError(t, err)
	assert.Equal(t, 2, st.WorksCreated)
	assert.Equal(t, 1, st.Quarantined)
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_external_ref
		WHERE matched_by='rule:bgm-type4-gated' AND external_id='3202'`))
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_external_ref
		WHERE matched_by='rule:bgm-type4-gated' AND external_id='3201'`))
	assert.Equal(t, model.WorkStatusQuarantine, workStatusOf(t, workIDByBgmExt(t, "3201")))
}

func TestBgmType4GatedSoftDeletedTitleDoesNotCollide(t *testing.T) {
	clean(t)
	require.NoError(t, testDB.Exec(`ALTER TABLE games ADD COLUMN IF NOT EXISTS gamename text`).Error)

	dead := seedExistingWork(t, "削除済み衝突タイトル作品")
	require.NoError(t, testDB.Exec(`UPDATE catalog_work SET deleted_at = now() WHERE id = ?`, dead).Error)
	seedSubject(t, 3301, "削除済み衝突タイトル作品", "", `["Galgame","PC","游戏"]`, "", false)

	dry, err := New(testDB, testDB, Options{DryRun: true}).RunBgmType4Gated(testDB)
	require.NoError(t, err)
	assert.Zero(t, dry.TitleCollisions)
	assert.Zero(t, dry.ToQuarantine)
	assert.Equal(t, 1, dry.ToCreate)

	st, err := New(testDB, testDB, Options{}).RunBgmType4Gated(testDB)
	require.NoError(t, err)
	assert.Equal(t, 1, st.WorksCreated)
	assert.Zero(t, st.Quarantined)
	assert.Equal(t, model.WorkStatusLive, workStatusOf(t, workIDByBgmExt(t, "3301")))
}

func TestBgmType4GatedQuarantineMint(t *testing.T) {
	clean(t)
	require.NoError(t, testDB.Exec(`ALTER TABLE games ADD COLUMN IF NOT EXISTS gamename text`).Error)

	existing := seedExistingWork(t, "隔離ミント衝突作品名")
	seedSubject(t, 3401, "隔離ミント衝突作品名", "", `["Galgame","PC","游戏"]`, "", false)
	seedSubject(t, 3402, "隔離ミント新規作品名", "", `["Galgame","PC","游戏"]`, "", false)

	dry, err := New(testDB, testDB, Options{DryRun: true}).RunBgmType4Gated(testDB)
	require.NoError(t, err)
	assert.Equal(t, 1, dry.ToQuarantine)
	assert.Equal(t, 1, dry.ToCreate)
	assert.Zero(t, dry.WorksCreated)
	assert.Zero(t, scalarInt(t, `SELECT count(*) FROM catalog_external_ref WHERE matched_by='rule:bgm-type4-gated'`))

	st, err := New(testDB, testDB, Options{}).RunBgmType4Gated(testDB)
	require.NoError(t, err)
	assert.Equal(t, 2, st.WorksCreated)
	assert.Equal(t, 1, st.Quarantined)

	qID := workIDByBgmExt(t, "3401")
	require.NotZero(t, qID)
	assert.Equal(t, model.WorkStatusQuarantine, workStatusOf(t, qID))
	assert.Equal(t, int64(1), countWhere(t, `SELECT count(*) FROM catalog_external_ref
		WHERE entity_type = ? AND entity_id = ? AND source_id = ? AND external_id = ? AND link_kind = ?`,
		model.EntityTypeWork, qID, bangumiSource, "3401", model.LinkKindExact))
	a, b := existing, qID
	if b < a {
		a, b = b, a
	}
	assert.Equal(t, int64(1), countWhere(t, `SELECT count(*) FROM catalog_match_candidate
		WHERE entity_type = ? AND a_id = ? AND b_id = ? AND reason = ? AND status = ?`,
		model.EntityTypeWork, a, b, model.CandidateReasonNameNormEqual, model.CandidateStatusPending))
	assert.Equal(t, model.WorkStatusLive, workStatusOf(t, workIDByBgmExt(t, "3402")))
}

func TestBgmType4GatedLimitSpendsLiveFirst(t *testing.T) {
	clean(t)
	require.NoError(t, testDB.Exec(`ALTER TABLE games ADD COLUMN IF NOT EXISTS gamename text`).Error)

	_ = seedExistingWork(t, "予算衝突タイトル作品")
	seedSubject(t, 3501, "予算衝突タイトル作品", "", `["Galgame","PC","游戏"]`, "", false)
	seedSubject(t, 3502, "予算公開タイトル作品", "", `["Galgame","PC","游戏"]`, "", false)

	dry, err := New(testDB, testDB, Options{DryRun: true, Limit: 1}).RunBgmType4Gated(testDB)
	require.NoError(t, err)
	assert.Equal(t, 1, dry.ToCreate)
	assert.Equal(t, 1, dry.ToQuarantine)

	st, err := New(testDB, testDB, Options{Limit: 1}).RunBgmType4Gated(testDB)
	require.NoError(t, err)
	assert.Equal(t, 1, st.WorksCreated)
	assert.Zero(t, st.Quarantined)
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_external_ref
		WHERE matched_by='rule:bgm-type4-gated' AND external_id='3502'`))
	assert.Zero(t, scalarInt(t, `SELECT count(*) FROM catalog_external_ref
		WHERE matched_by='rule:bgm-type4-gated' AND external_id='3501'`))
}

func seedDisplayOnlyWork(t *testing.T, displayName string) {
	t.Helper()
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_work (medium_id, olang, display_name, content_rating, status, extra, field_provenance, display_nsfw)
		VALUES (1,'ja',?,0,0,'{}','{}',false)`, displayName).Error)
}

func seedExistingWork(t *testing.T, title string) int64 {
	t.Helper()
	var wid int64
	require.NoError(t, testDB.Raw(`INSERT INTO catalog_work (medium_id, olang, display_name, content_rating, status, extra, field_provenance, display_nsfw)
		VALUES (1,'ja',?,0,0,'{}','{}',false) RETURNING id`, title).Scan(&wid).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_work_title (work_id, lang, title, kind, provenance) VALUES (?,'ja',?,0,0)`, wid, title).Error)
	return wid
}

func workIDByBgmExt(t *testing.T, ext string) int64 {
	t.Helper()
	var id int64
	require.NoError(t, testDB.Raw(`SELECT entity_id FROM catalog_external_ref
		WHERE matched_by='rule:bgm-type4-gated' AND external_id=?`, ext).Scan(&id).Error)
	return id
}

func workStatusOf(t *testing.T, id int64) int16 {
	t.Helper()
	var status int16
	require.NoError(t, testDB.Raw(`SELECT status FROM catalog_work WHERE id = ?`, id).Scan(&status).Error)
	return status
}
