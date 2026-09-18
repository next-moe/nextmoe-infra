package entityintromt

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	"api/internal/testsupport/dbtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	testDB  *gorm.DB
	testDSN string
)

func TestMain(m *testing.M) {
	var ok bool
	testDSN, ok = dbtest.DSN()
	if !ok {
		fmt.Fprintln(os.Stderr, "SKIP: no TEST_DATABASE_DSN — DB-backed entityintromt tests will skip individually")
		os.Exit(m.Run())
	}
	db, err := gorm.Open(postgres.Open(testDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL: cannot connect to the assigned test database")
		os.Exit(1)
	}
	if err := migrate.Run(db); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: catalog migrate failed: %v\n", err)
		os.Exit(1)
	}
	if err := seed.Run(db); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: catalog seed failed: %v\n", err)
		os.Exit(1)
	}
	testDB = db
	os.Exit(m.Run())
}

func clean(t *testing.T) {
	t.Helper()
	if testDB == nil {
		dbtest.Skip(t)
	}
	for _, table := range []string{
		"catalog_character_intro", "catalog_person_intro", "catalog_label_intro",
		"catalog_character", "catalog_person", "catalog_label",
	} {
		require.NoError(t, testDB.Exec("TRUNCATE "+table+" CASCADE").Error)
	}
}

func srcIDs(t *testing.T) (vndb, bangumi int16) {
	t.Helper()
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_source WHERE key='vndb'`).Scan(&vndb).Error)
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_source WHERE key='bangumi'`).Scan(&bangumi).Error)
	require.NotZero(t, vndb)
	require.NotZero(t, bangumi)
	return
}

func mkCharacter(t *testing.T, name string) int64 {
	t.Helper()
	c := model.CatalogCharacter{DisplayName: name, Lang: "ja"}
	require.NoError(t, testDB.Create(&c).Error)
	return c.ID
}

func mkPerson(t *testing.T, name string) int64 {
	t.Helper()
	p := model.CatalogPerson{DisplayName: name}
	require.NoError(t, testDB.Create(&p).Error)
	return p.ID
}

func mkLabel(t *testing.T, name string) int64 {
	t.Helper()
	l := model.CatalogLabel{DisplayName: name, Lang: "ja", Kind: 0}
	require.NoError(t, testDB.Create(&l).Error)
	return l.ID
}

func mkCharIntro(t *testing.T, charID int64, lang, intro string, source int16) {
	t.Helper()
	require.NoError(t, testDB.Create(&model.CatalogCharacterIntro{
		CharacterID: charID, Lang: lang, Intro: intro, SourceID: source, Provenance: 0,
	}).Error)
}

func mkPersonIntro(t *testing.T, personID int64, lang, intro string, source int16) {
	t.Helper()
	require.NoError(t, testDB.Create(&model.CatalogPersonIntro{
		PersonID: personID, Lang: lang, Intro: intro, SourceID: source, Provenance: 0,
	}).Error)
}

func mkLabelIntro(t *testing.T, labelID int64, lang, intro string, source int16) {
	t.Helper()
	require.NoError(t, testDB.Create(&model.CatalogLabelIntro{
		LabelID: labelID, Lang: lang, Intro: intro, SourceID: source, Provenance: 0,
	}).Error)
}

func charLane(t *testing.T) laneDef {
	t.Helper()
	l, err := selectLanes(LaneCharacter)
	require.NoError(t, err)
	return l[0]
}

type machineRow struct {
	Intro     string
	SourceID  int16
	SrcHash   string
	MTModel   string
	UpdatedAt time.Time
}

func readMachine(t *testing.T, charID int64) (machineRow, bool) {
	t.Helper()
	var rows []machineRow
	require.NoError(t, testDB.Raw(`
		SELECT intro, source_id, src_hash, mt_model, updated_at
		FROM catalog_character_intro
		WHERE character_id = ? AND lang = 'zh-Hans' AND provenance = 1`, charID).Scan(&rows).Error)
	if len(rows) == 0 {
		return machineRow{}, false
	}
	return rows[0], true
}

func TestDecide(t *testing.T) {
	const text = "テスト用の紹介文"
	hash := hashSource(text)

	dec, h := decide(candidate{Text: text}, false)
	assert.Equal(t, decInsert, dec, "no machine row yet")
	assert.Equal(t, hash, h)

	id := int64(7)
	dec, _ = decide(candidate{Text: text, MZhID: &id, MZhSrcHash: &hash}, false)
	assert.Equal(t, decSkipSame, dec, "machine row with the same source hash")

	stale := hashSource("older text")
	dec, h = decide(candidate{Text: text, MZhID: &id, MZhSrcHash: &stale}, false)
	assert.Equal(t, decRetrans, dec, "machine row whose source has changed")
	assert.Equal(t, hash, h, "the hash written back is always the CURRENT source's")
}

func TestSelectLanes(t *testing.T) {
	all, err := selectLanes("")
	require.NoError(t, err)
	assert.Len(t, all, 3)

	for _, key := range []string{LaneCharacter, LanePerson, LaneLabel} {
		one, err := selectLanes(key)
		require.NoError(t, err)
		require.Len(t, one, 1)
		assert.Equal(t, key, one[0].key)
	}

	_, err = selectLanes("work")
	assert.Error(t, err)
}

func TestLoadCandidates_SourcePreference(t *testing.T) {
	clean(t)
	vndb, bangumi := srcIDs(t)
	ctx := context.Background()
	lane := charLane(t)

	both := mkCharacter(t, "both")
	lo, hi := min(vndb, bangumi), max(vndb, bangumi)
	mkCharIntro(t, both, "en", "english text", lo)
	mkCharIntro(t, both, "ja", "日本語ハイ", hi)
	mkCharIntro(t, both, "ja", "日本語ロー", lo)

	enOnly := mkCharacter(t, "en only")
	mkCharIntro(t, enOnly, "en", "only english", lo)

	hasZh := mkCharacter(t, "has zh")
	mkCharIntro(t, hasZh, "ja", "日本語", lo)
	mkCharIntro(t, hasZh, "zh-Hans", "中文", hi)

	deleted := mkCharacter(t, "deleted")
	mkCharIntro(t, deleted, "ja", "日本語", lo)
	require.NoError(t, testDB.Delete(&model.CatalogCharacter{}, deleted).Error)

	blank := mkCharacter(t, "blank")
	mkCharIntro(t, blank, "ja", "   \n  ", lo)

	cands, err := loadCandidates(ctx, testDB, lane, 0, 0, nil)
	require.NoError(t, err)

	byID := map[int64]candidate{}
	for _, c := range cands {
		byID[c.EntityID] = c
	}
	require.Contains(t, byID, both)
	assert.Equal(t, string(SourceJa), byID[both].SrcLang)
	assert.Equal(t, "日本語ロー", byID[both].Text, "lowest source_id among the ja rows")
	assert.Equal(t, lo, byID[both].SourceID)

	require.Contains(t, byID, enOnly)
	assert.Equal(t, string(SourceEn), byID[enOnly].SrcLang)

	assert.NotContains(t, byID, hasZh, "a zh source row excludes the entity")
	assert.NotContains(t, byID, deleted, "soft-deleted entity excluded")
	assert.NotContains(t, byID, blank, "whitespace-only source dropped")
}

func TestApply_MockWritePath(t *testing.T) {
	clean(t)
	vndb, _ := srcIDs(t)
	ctx := context.Background()

	id := mkCharacter(t, "水橋 かおり")
	const srcText = "ヒロインの一人。明るく面倒見がよい。"
	mkCharIntro(t, id, "ja", srcText, vndb)

	opts := Opts{DSN: testDSN, Apply: true, Lane: LaneCharacter}
	st, err := Run(ctx, MockTranslator{}, opts)
	require.NoError(t, err)
	require.Len(t, st, 1)
	assert.Equal(t, 1, st[0].Candidates)
	assert.Equal(t, 1, st[0].WouldInsert)
	assert.Equal(t, 1, st[0].Inserted)
	assert.Equal(t, 0, st[0].Refused)
	assert.Equal(t, 0, st[0].Errors)

	row, ok := readMachine(t, id)
	require.True(t, ok, "a machine zh-Hans row must land")
	assert.Equal(t, vndb, row.SourceID, "attributed to the source row it was translated from")
	assert.Equal(t, hashSource(srcText), row.SrcHash)
	assert.Contains(t, row.MTModel, "mock:")
	assert.Contains(t, row.Intro, "rehearsal mock")
	firstWrite := row.UpdatedAt

	st, err = Run(ctx, MockTranslator{}, opts)
	require.NoError(t, err)
	assert.Equal(t, 1, st[0].SkipUnchanged)
	assert.Equal(t, 0, st[0].Inserted)
	assert.Equal(t, 0, st[0].Retranslated)

	again, ok := readMachine(t, id)
	require.True(t, ok)
	assert.True(t, again.UpdatedAt.Equal(firstWrite), "idempotent skip must not rewrite the row")

	const newText = "ヒロインの一人。実は生徒会長でもある。"
	require.NoError(t, testDB.Exec(
		`UPDATE catalog_character_intro SET intro = ? WHERE character_id = ? AND lang = 'ja' AND provenance = 0`,
		newText, id).Error)

	st, err = Run(ctx, MockTranslator{}, opts)
	require.NoError(t, err)
	assert.Equal(t, 1, st[0].WouldRetranslate)
	assert.Equal(t, 1, st[0].Retranslated)

	after, ok := readMachine(t, id)
	require.True(t, ok)
	assert.Equal(t, hashSource(newText), after.SrcHash)
	assert.NotEqual(t, row.Intro, after.Intro)
}

func TestApply_NeverOverwritesSourceRow(t *testing.T) {
	clean(t)
	vndb, bangumi := srcIDs(t)
	ctx := context.Background()

	id := mkCharacter(t, "guarded")
	mkCharIntro(t, id, "ja", "日本語の紹介", vndb)
	mkCharIntro(t, id, "zh-Hans", "人工翻译的中文简介", vndb)
	require.NoError(t, testDB.Create(&model.CatalogCharacterIntro{
		CharacterID: id, Lang: "zh-Hans", Intro: "别的来源的机翻", SourceID: bangumi,
		Provenance: model.IntroProvenanceMachine, SrcHash: "stray-hash", MTModel: "old-mt",
	}).Error)

	r := &runner{db: testDB, lane: charLane(t), stats: &LaneStats{Lane: LaneCharacter}}
	rows, pruned, err := r.writeMachine(ctx, candidate{EntityID: id, SourceID: vndb, Text: "日本語の紹介"},
		"机器译文", hashSource("日本語の紹介"), "mock:stub")
	require.NoError(t, err)
	assert.Zero(t, rows, "the DO UPDATE guard must refuse a provenance=0 row")
	assert.Zero(t, pruned, "a refused write prunes nothing")
	var strays int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_character_intro
		WHERE character_id = ? AND lang = 'zh-Hans' AND provenance = 1 AND source_id = ?`, id, bangumi).Scan(&strays).Error)
	assert.EqualValues(t, 1, strays)

	var kept struct {
		Intro      string
		Provenance int16
		MTModel    string
	}
	require.NoError(t, testDB.Raw(`
		SELECT intro, provenance, mt_model FROM catalog_character_intro
		WHERE character_id = ? AND lang = 'zh-Hans' AND source_id = ?`, id, vndb).Scan(&kept).Error)
	assert.Equal(t, "人工翻译的中文简介", kept.Intro)
	assert.Equal(t, int16(0), kept.Provenance)
	assert.Empty(t, kept.MTModel)
}

func TestApply_AllLanes(t *testing.T) {
	clean(t)
	vndb, _ := srcIDs(t)
	ctx := context.Background()

	ch := mkCharacter(t, "角色")
	mkCharIntro(t, ch, "ja", "主人公の幼なじみで、明るく世話好きなヒロイン。", vndb)
	pe := mkPerson(t, "人物")
	mkPersonIntro(t, pe, "en", "A voice actress.", vndb)
	la := mkLabel(t, "会社")
	mkLabelIntro(t, la, "ja", "美少女ゲームを中心に展開する老舗ブランド。", vndb)

	st, err := Run(ctx, MockTranslator{}, Opts{DSN: testDSN, Apply: true})
	require.NoError(t, err)
	require.Len(t, st, 3)
	for _, s := range st {
		assert.Equal(t, 1, s.Candidates, s.Lane)
		assert.Equal(t, 1, s.Inserted, s.Lane)
		assert.Zero(t, s.Errors, s.Lane)
	}
	assert.Equal(t, 1, st[1].FromEn, "the person lane's only source is English")

	for _, tbl := range []struct {
		table string
		id    int64
	}{{"catalog_character", ch}, {"catalog_person", pe}, {"catalog_label", la}} {
		var n int64
		require.NoError(t, testDB.Raw(
			"SELECT count(*) FROM "+tbl.table+" WHERE id = ? AND updated_at > created_at", tbl.id).Scan(&n).Error)
		assert.EqualValues(t, 1, n, tbl.table+" updated_at bumped")
	}
}
