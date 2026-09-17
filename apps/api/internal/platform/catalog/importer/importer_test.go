package importer

import (
	"fmt"
	"os"
	"testing"

	"api/internal/platform/catalog/llmsuggest"
	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	srcb "api/internal/platform/catalog/srcbangumi"
	srcv "api/internal/platform/catalog/srcvndb"
	"api/internal/testsupport/dbtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDB *gorm.DB

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("catalog/importer")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("catalog/importer", "no test db: %v", err)
	}
	for _, step := range []func(*gorm.DB) error{migrate.Run, seed.Run, srcb.EnsureSchema, srcv.EnsureSchema, llmsuggest.EnsureSchema} {
		if err := step(db); err != nil {
			dbtest.SkipMainf("catalog/importer", "setup: %v", err)
		}
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS creaters (id int, raw jsonb)`,
		`CREATE TABLE IF NOT EXISTS characters (id int, raw jsonb)`,
		`CREATE TABLE IF NOT EXISTS staff (game bigint, creater_id bigint, shubetu int)`,
		`CREATE TABLE IF NOT EXISTS appearances (game bigint, character_id bigint)`,
		`CREATE TABLE IF NOT EXISTS appearance_actors (game bigint, character_id bigint, actor_id bigint)`,
		`CREATE TABLE IF NOT EXISTS game_music (raw jsonb, music int, game int)`,
		`CREATE TABLE IF NOT EXISTS singers   (raw jsonb, music int, creater_id int)`,
		`CREATE TABLE IF NOT EXISTS lyricists (raw jsonb, music int, creater_id int)`,
		`CREATE TABLE IF NOT EXISTS composers (raw jsonb, music int, creater_id int)`,
		`CREATE TABLE IF NOT EXISTS arrangers (raw jsonb, music int, creater_id int)`,
		`CREATE TABLE IF NOT EXISTS works (workno text, work_name text, work_name_kana text, maker_id text, maker_name text, age_category text, work_type_string text, status text, regist_date timestamptz, product_json jsonb)`,
		`CREATE TABLE IF NOT EXISTS games (id bigint, vndb text, dlsite_id text)`,
		`ALTER TABLE games ADD COLUMN IF NOT EXISTS dlsite_id text`,
		`ALTER TABLE games ADD COLUMN IF NOT EXISTS vndb text`,
		`ALTER TABLE games ADD COLUMN IF NOT EXISTS gamename text`,
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			dbtest.SkipMainf("catalog/importer", "%s: %v", s, err)
		}
	}
	testDB = db
	os.Exit(m.Run())
}

func clean(t *testing.T) {
	t.Helper()
	tables := []string{
		"catalog_credit", "catalog_work_character", "catalog_match_candidate", "catalog_external_ref", "catalog_revision",
		"catalog_credit_name", "catalog_label", "catalog_character_alias", "catalog_character", "catalog_work",
		"src_bangumi.subject_person", "src_bangumi.subject_character", "src_bangumi.person_character",
		"src_bangumi.person", "src_bangumi.character", "src_bangumi.subject_relation", "src_bangumi.subject",
		"src_vndb.chars", "src_vndb.chars_names", "src_vndb.chars_vns", "src_vndb.images", "src_vndb.portrait_backfill",
		"src_vndb.staff", "src_vndb.staff_alias", "src_vndb.vn_staff", "src_vndb.vn_seiyuu", "src_vndb.vn_relations",
		"catalog_release", "catalog_work_title", "catalog_work_label", "catalog_work_relation",
		"src_llm.bid_identity_verdict", "creaters", "characters", "staff", "appearances", "appearance_actors", "works", "games",
		"game_music", "singers", "lyricists", "composers", "arrangers",
	}
	for _, tb := range tables {
		require.NoError(t, testDB.Exec("TRUNCATE "+tb+" RESTART IDENTITY CASCADE").Error)
	}
}

func seedAnchoredWork(t *testing.T, bid int64) int64 {
	t.Helper()
	var workID int64
	require.NoError(t, testDB.Raw(`INSERT INTO catalog_work (medium_id, site, product_work_id, olang, display_name, content_rating, status, extra, field_provenance, display_nsfw)
		VALUES (1,'galgame_wiki',?, 'ja','w',0,0,'{}','{}',false) RETURNING id`, bid).Scan(&workID).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by)
		VALUES (5, ?, 3, ?, 0, 'rule:wiki-bid-typed')`, workID, fmt.Sprint(bid)).Error)
	return workID
}

func TestBangumiWave(t *testing.T) {
	clean(t)
	anchoredWork := seedAnchoredWork(t, 100)
	suspectWork := seedAnchoredWork(t, 200)
	require.NoError(t, testDB.Exec(`INSERT INTO src_llm.bid_identity_verdict (work_id, galgame_id, bid, layer, input_hash, model, prompt_version)
		VALUES (?, 200, 200, 'suspect', 'h200', 'm', 'p')`, suspectWork).Error)
	var probableWork int64
	require.NoError(t, testDB.Raw(`INSERT INTO catalog_work (medium_id, site, product_work_id, olang, display_name, content_rating, status, extra, field_provenance, display_nsfw)
		VALUES (1,'galgame_wiki',300,'ja','w',0,0,'{}','{}',false) RETURNING id`).Scan(&probableWork).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by)
		VALUES (5, ?, 3, '300', 1, 'rule:probable-guess')`, probableWork).Error)

	testDB.Exec(`INSERT INTO src_bangumi.person (id, type, name, career, infobox_raw, parse_error, summary, comments, collects, parser_version, ingested_at)
		VALUES (10,1,'麻枝准','[]','',' ','',0,0,'v',now()), (20,2,'Key','[]','','','',0,0,'v',now()), (30,1,'VA太郎','[]','','','',0,0,'v',now())`)
	testDB.Exec(`INSERT INTO src_bangumi.subject_person (person_id, subject_id, position, appear_eps) VALUES (10,100,1001,''),(20,100,1001,''),(10,200,1001,''),(10,300,1001,'')`)
	testDB.Exec(`INSERT INTO src_bangumi.character (id, role, name, infobox_raw, parse_error, summary, comments, collects, parser_version, ingested_at) VALUES (50,1,'渚','','','',0,0,'v',now())`)
	testDB.Exec(`INSERT INTO src_bangumi.subject_character (character_id, subject_id, type, item_order) VALUES (50,100,1,0)`)
	testDB.Exec(`INSERT INTO src_bangumi.person_character (person_id, subject_id, character_id, type, summary) VALUES (30,100,50,1,'')`)
	var roleID int64
	require.NoError(t, testDB.Raw(`SELECT role_id FROM catalog_source_role_map WHERE source_id=3 LIMIT 1`).Scan(&roleID).Error)
	testDB.Exec(`INSERT INTO catalog_source_role_map (source_id, source_role, role_id, note) VALUES (3, '4:1001', ?, '') ON CONFLICT DO NOTHING`, roleID)

	st, err := New(testDB, nil, Options{Source: "bangumi"}).Run("bangumi")
	require.NoError(t, err)
	assert.Equal(t, 3, st.NamesCreated, "3 credit_names: writer, company, VA")
	assert.Equal(t, 1, st.LabelsCreated, "company → label")
	assert.Equal(t, 1, st.CharactersCreated)
	assert.Equal(t, 4, st.CreditsWritten)

	var orphaned int64
	testDB.Raw(`SELECT count(*) FROM catalog_credit_name WHERE person_id IS NOT NULL`).Scan(&orphaned)
	assert.Zero(t, orphaned, "all names orphan")
	var persons int64
	testDB.Table("catalog_person").Count(&persons)
	assert.Zero(t, persons, "no person rows created")

	var anchorRule string
	require.NoError(t, testDB.Raw(`SELECT matched_by FROM catalog_external_ref WHERE entity_type=1 AND source_id=3 AND external_id='10'`).Scan(&anchorRule).Error)
	assert.Equal(t, "rule:bangumi-person-import", anchorRule)

	var companyLabels int64
	testDB.Raw(`SELECT count(*) FROM catalog_credit c JOIN catalog_external_ref r ON r.entity_type=1 AND r.entity_id=c.credit_name_id AND r.source_id=3 AND r.external_id='20' WHERE c.label_id IS NOT NULL`).Scan(&companyLabels)
	assert.Equal(t, int64(1), companyLabels)

	var suspectCredits, probableCredits int64
	testDB.Raw(`SELECT count(*) FROM catalog_credit WHERE work_id = ?`, suspectWork).Scan(&suspectCredits)
	assert.Equal(t, int64(1), suspectCredits, "suspect-verdict work gets credits under the exact-anchor gate")
	testDB.Raw(`SELECT count(*) FROM catalog_credit WHERE work_id = ?`, probableWork).Scan(&probableCredits)
	assert.Zero(t, probableCredits, "probable-only work stays gated out")
	var anchoredCredits int64
	testDB.Raw(`SELECT count(*) FROM catalog_credit WHERE work_id = ?`, anchoredWork).Scan(&anchoredCredits)
	assert.Equal(t, int64(3), anchoredCredits)

	var vaNote string
	require.NoError(t, testDB.Raw(`SELECT note FROM catalog_credit WHERE role_id=1 AND character_id IS NOT NULL`).Scan(&vaNote).Error)
	assert.Equal(t, "主角", vaNote)

	var revs int64
	testDB.Raw(`SELECT count(*) FROM catalog_revision WHERE action=?`, model.RevisionActionImported).Scan(&revs)
	assert.Equal(t, int64(5), revs)

	st2, err := New(testDB, nil, Options{Source: "bangumi"}).Run("bangumi")
	require.NoError(t, err)
	assert.Zero(t, st2.NamesCreated)
	assert.Zero(t, st2.CharactersCreated)
	assert.Zero(t, st2.CreditsWritten)
	assert.Equal(t, 4, st2.Already, "all 4 credits already present")
}

func TestBangumiChunkedLoad(t *testing.T) {
	clean(t)
	restore := bidChunkSize
	bidChunkSize = 1
	defer func() { bidChunkSize = restore }()

	seedAnchoredWork(t, 100)
	seedAnchoredWork(t, 200)
	seedAnchoredWork(t, 300)
	testDB.Exec(`INSERT INTO src_bangumi.person (id, type, name, career, infobox_raw, parse_error, summary, comments, collects, parser_version, ingested_at)
		VALUES (10,1,'共有作家','[]','','','',0,0,'v',now()), (20,1,'別作家','[]','','','',0,0,'v',now())`)
	testDB.Exec(`INSERT INTO src_bangumi.subject_person (person_id, subject_id, position, appear_eps) VALUES (10,100,1001,''),(20,200,1001,''),(10,300,1001,'')`)
	var roleID int64
	require.NoError(t, testDB.Raw(`SELECT role_id FROM catalog_source_role_map WHERE source_id=3 LIMIT 1`).Scan(&roleID).Error)
	testDB.Exec(`INSERT INTO catalog_source_role_map (source_id, source_role, role_id, note) VALUES (3, '4:1001', ?, '') ON CONFLICT DO NOTHING`, roleID)

	st, err := New(testDB, nil, Options{Source: "bangumi"}).Run("bangumi")
	require.NoError(t, err)
	assert.Equal(t, 2, st.NamesCreated, "person 10 deduped across chunks 1 and 3 → 2 names")
	assert.Equal(t, 3, st.CreditsWritten, "all three subjects across three chunks contribute a credit")

	var total int64
	testDB.Raw(`SELECT count(*) FROM catalog_credit`).Scan(&total)
	assert.EqualValues(t, 3, total)
}

func TestUnmappedRoleSkipped(t *testing.T) {
	clean(t)
	seedAnchoredWork(t, 100)
	testDB.Exec(`INSERT INTO src_bangumi.person (id, type, name, career, infobox_raw, parse_error, summary, comments, collects, parser_version, ingested_at) VALUES (10,1,'X','[]','','','',0,0,'v',now())`)
	testDB.Exec(`INSERT INTO src_bangumi.subject_person (person_id, subject_id, position, appear_eps) VALUES (10,100,999999,'')`)

	st, err := New(testDB, nil, Options{Source: "bangumi"}).Run("bangumi")
	require.NoError(t, err)
	assert.Equal(t, 1, st.NamesCreated, "the name is still created")
	assert.Equal(t, 1, st.SkippedUnmappedRole)
	assert.Zero(t, st.CreditsWritten, "unmapped position → no credit")
}

func TestEGWaveAndCandidates(t *testing.T) {
	clean(t)
	var workID int64
	require.NoError(t, testDB.Raw(`INSERT INTO catalog_work (medium_id, site, product_work_id, olang, display_name, content_rating, status, extra, field_provenance, display_nsfw)
		VALUES (1,'galgame_wiki',7,'ja','w',0,0,'{}','{}',false) RETURNING id`).Scan(&workID).Error)
	testDB.Exec(`INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by) VALUES (5,?,5,'7',1,'rule:eg-vndb-rosetta')`, workID)
	testDB.Exec(`INSERT INTO creaters (id, raw) VALUES (500, '{"name":"絵師","twitter_username":"SharedHandle"}'), (501,'{"name":"声太"}')`)
	testDB.Exec(`INSERT INTO characters (id, raw) VALUES (800, '{"name":"EGキャラ"}')`)
	testDB.Exec(`INSERT INTO staff (game, creater_id, shubetu) VALUES (7,500,1)`)
	testDB.Exec(`INSERT INTO appearance_actors (game, character_id, actor_id) VALUES (7,800,501)`)

	seedAnchoredWork(t, 100)
	testDB.Exec(`INSERT INTO src_bangumi.person (id, type, name, career, infobox_raw, infobox_parsed, parse_error, summary, comments, collects, parser_version, ingested_at)
		VALUES (10,1,'絵師B','[]','', '{"Type":"Crt","Fields":[{"Key":"Twitter","Value":"@SharedHandle","Items":null,"Array":false,"Null":false}]}'::jsonb, '','',0,0,'v',now())`)
	testDB.Exec(`INSERT INTO src_bangumi.subject_person (person_id, subject_id, position, appear_eps) VALUES (10,100,1001,'')`)
	var roleID int64
	testDB.Raw(`SELECT role_id FROM catalog_source_role_map WHERE source_id=3 LIMIT 1`).Scan(&roleID)
	testDB.Exec(`INSERT INTO catalog_source_role_map (source_id, source_role, role_id, note) VALUES (3,'4:1001',?,'') ON CONFLICT DO NOTHING`, roleID)

	im := New(testDB, testDB, Options{Source: "all"})
	_, err := im.Run("all")
	require.NoError(t, err)

	var egCredits int64
	testDB.Raw(`SELECT count(*) FROM catalog_credit WHERE source_id=5`).Scan(&egCredits)
	assert.Equal(t, int64(2), egCredits, "1 staff + 1 VA")
	var egVA int64
	testDB.Raw(`SELECT count(*) FROM catalog_credit WHERE source_id=5 AND role_id=1 AND character_id IS NOT NULL`).Scan(&egVA)
	assert.Equal(t, int64(1), egVA)

	cs, err := im.RunCandidates()
	require.NoError(t, err)
	var cands int64
	testDB.Raw(`SELECT count(*) FROM catalog_match_candidate WHERE entity_type=1 AND reason=0`).Scan(&cands)
	assert.Equal(t, int64(1), cands, "one shared-handle candidate")
	assert.GreaterOrEqual(t, cs.Twitter, 1)
}

func TestDLsiteWave(t *testing.T) {
	clean(t)
	testDB.Exec(`INSERT INTO works (workno, work_name, work_name_kana, maker_id, maker_name, age_category, work_type_string, status, regist_date, product_json)
		VALUES ('RJ001','作品A','サクヒンエー','RG100','サークルX','3','ボイス・ASMR','fetched','2020-05-10',
		'{"creaters":{"voice_by":[{"id":"9001","name":"声優花","classification":"voice_by"}],"illust_by":[{"id":"9002","name":"絵師","classification":"illust_by"}]}}'::jsonb)`)
	testDB.Exec(`INSERT INTO works (workno, work_name, work_name_kana, maker_id, maker_name, age_category, work_type_string, status, product_json)
		VALUES ('RJ002','作品B','','BG200','会社Y','1','ボイス・ASMR','fetched','{"creaters":{}}'::jsonb)`)
	testDB.Exec(`INSERT INTO works (workno, work_name, work_type_string, status, maker_id, age_category, product_json) VALUES ('RJ003','漫画','マンガ','fetched','RG100','3','{}'::jsonb)`)

	st, err := New(testDB, nil, Options{}).RunDLsite(testDB)
	require.NoError(t, err)
	assert.Equal(t, 2, st.WorksCreated)
	assert.Equal(t, 2, st.ReleasesCreated)
	assert.Equal(t, 3, st.TitlesCreated)
	assert.Equal(t, 2, st.LabelsCreated)
	assert.Equal(t, 2, st.NamesCreated)
	assert.Equal(t, 2, st.CreditsWritten)
	assert.Zero(t, st.Stubs)

	assert.Equal(t, int64(2), scalarInt(t, `SELECT count(*) FROM catalog_work WHERE medium_id=5 AND site IS NULL`))
	assert.Equal(t, int64(2), scalarInt(t, `SELECT count(*) FROM catalog_external_ref WHERE entity_type=6 AND source_id=4 AND link_kind=0 AND matched_by='rule:dlsite-work-import'`))
	assert.Zero(t, scalarInt(t, `SELECT count(*) FROM catalog_external_ref WHERE entity_type=5 AND source_id=4`))
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_work w JOIN catalog_release rel ON rel.work_id=w.id
		JOIN catalog_external_ref r ON r.entity_type=6 AND r.entity_id=rel.id AND r.external_id='RJ001' WHERE w.content_rating=2`))
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_work_title WHERE kind=3 AND title='サクヒンエー'`))
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_label l JOIN catalog_external_ref r ON r.entity_type=3 AND r.entity_id=l.id AND r.external_id='RG100' WHERE l.kind=4`))
	assert.Equal(t, int64(1), scalarInt(t, `SELECT count(*) FROM catalog_label l JOIN catalog_external_ref r ON r.entity_type=3 AND r.entity_id=l.id AND r.external_id='BG200' WHERE l.kind=2`))
	assert.Equal(t, int64(2), scalarInt(t, `SELECT count(*) FROM catalog_credit_name cn JOIN catalog_external_ref r ON r.entity_type=1 AND r.entity_id=cn.id AND r.source_id=4 AND r.link_kind=0 AND r.matched_by='rule:dlsite-creater-import' WHERE cn.person_id IS NULL`))

	st2, err := New(testDB, nil, Options{}).RunDLsite(testDB)
	require.NoError(t, err)
	assert.Zero(t, st2.WorksCreated)
	assert.Equal(t, 2, st2.Already)
}

func scalarInt(t *testing.T, q string) int64 {
	t.Helper()
	var n int64
	require.NoError(t, testDB.Raw(q).Scan(&n).Error)
	return n
}
