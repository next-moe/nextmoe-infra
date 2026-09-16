package getchuintros

import (
	"context"
	"fmt"
	"os"
	"testing"

	"api/internal/jobs/workpop"
	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	"api/internal/platform/catalog/srcvndb"
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
		fmt.Fprintln(os.Stderr, "SKIP: no TEST_DATABASE_DSN — DB-backed getchuintros tests will skip individually")
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
	if err := srcvndb.EnsureSchema(db); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: src_vndb schema: %v\n", err)
		os.Exit(1)
	}
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS items (getchu_id text PRIMARY KEY, story text)`).Error; err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: staging items table: %v\n", err)
		os.Exit(1)
	}
	testDB = db
	os.Exit(m.Run())
}

const (
	getchuSource = int16(17)
	getchuRule   = "rule:vndb-extlink-getchu"
)

func clean(t *testing.T) {
	t.Helper()
	if testDB == nil {
		dbtest.Skip(t)
	}
	for _, table := range []string{"catalog_external_ref", "catalog_release", "items", "src_vndb.releases_vn"} {
		require.NoError(t, testDB.Exec("TRUNCATE "+table+" CASCADE").Error)
	}
	for _, table := range []string{"catalog_work_intro", "catalog_work"} {
		require.NoError(t, testDB.Exec("TRUNCATE "+table+" RESTART IDENTITY CASCADE").Error)
	}
}

var nextProductWorkID int64 = 970000

func mkWork(t *testing.T, name string, pop workpop.Population) int64 {
	t.Helper()
	w := model.CatalogWork{MediumID: 1, OLang: "ja", DisplayName: name}
	if pop == workpop.Claimed || pop == workpop.Published {
		site := "kungal"
		w.Site = &site
	}
	if pop == workpop.Published {
		nextProductWorkID++
		pid := nextProductWorkID
		w.ProductWorkID = &pid
	}
	require.NoError(t, testDB.Create(&w).Error)
	return w.ID
}

func mkAnchoredRelease(t *testing.T, workID int64, getchuID string) int64 {
	t.Helper()
	rel := model.CatalogRelease{WorkID: workID, Kind: 0}
	require.NoError(t, testDB.Create(&rel).Error)
	require.NoError(t, testDB.Exec(`
		INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by)
		VALUES (?,?,?,?,?,?)`,
		model.EntityTypeRelease, rel.ID, getchuSource, getchuID, model.LinkKindExact, getchuRule).Error)
	return rel.ID
}

func mkVndbRelease(t *testing.T, releaseID int64, rid string, vids ...string) {
	t.Helper()
	require.NoError(t, testDB.Exec(`
		INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by)
		VALUES (?, ?, (SELECT id FROM catalog_source WHERE key = 'vndb'), ?, ?, 'rule:test')`,
		model.EntityTypeRelease, releaseID, rid, model.LinkKindExact).Error)
	for _, vid := range vids {
		require.NoError(t, testDB.Exec(
			`INSERT INTO src_vndb.releases_vn (id, vid, rtype) VALUES (?, ?, 'complete')`, rid, vid).Error)
	}
}

func mkStagingItem(t *testing.T, getchuID, story string) {
	t.Helper()
	require.NoError(t, testDB.Exec(
		`INSERT INTO items (getchu_id, story) VALUES (?,?)`, getchuID, story).Error)
}

func mkIntro(t *testing.T, workID int64, lang string, source int16) {
	t.Helper()
	require.NoError(t, testDB.Create(&model.CatalogWorkIntro{
		WorkID: workID, Lang: lang, Intro: "already here", SourceID: source}).Error)
}

func run(t *testing.T, apply bool, pop workpop.Population) *Stats {
	t.Helper()
	st, err := Run(context.Background(), Opts{
		DSN: testDSN, GetchuDSN: testDSN, Apply: apply, Population: pop})
	require.NoError(t, err)
	return st
}

func introOf(t *testing.T, workID int64, lang string) (string, int16, bool) {
	t.Helper()
	var row struct {
		Intro    string `gorm:"column:intro"`
		SourceID int16  `gorm:"column:source_id"`
	}
	res := testDB.Raw(`SELECT intro, source_id FROM catalog_work_intro WHERE work_id = ? AND lang = ?`,
		workID, lang).Scan(&row)
	require.NoError(t, res.Error)
	return row.Intro, row.SourceID, res.RowsAffected > 0
}

func TestFillsMissingJapanese(t *testing.T) {
	clean(t)
	work := mkWork(t, "空の作品", workpop.Published)
	mkAnchoredRelease(t, work, "1001")
	mkStagingItem(t, "1001", "彼が目を覚ますと、そこは肌色の世界だった。")

	dry := run(t, false, workpop.Published)
	assert.Equal(t, 1, dry.Planned)
	assert.Equal(t, 0, dry.Written, "a dry run decides but writes nothing")
	_, _, found := introOf(t, work, "ja")
	assert.False(t, found)

	st := run(t, true, workpop.Published)
	assert.Equal(t, 1, st.Written)
	intro, source, found := introOf(t, work, "ja")
	require.True(t, found)
	assert.Equal(t, "彼が目を覚ますと、そこは肌色の世界だった。", intro)
	assert.Equal(t, getchuSource, source)
}

func TestSkipsWorkThatAlreadyReadsInJapanese(t *testing.T) {
	clean(t)
	work := mkWork(t, "既に日本語あり", workpop.Published)
	mkAnchoredRelease(t, work, "1002")
	mkStagingItem(t, "1002", "getchu の紹介文")
	mkIntro(t, work, "ja", 3)

	st := run(t, true, workpop.Published)
	assert.Equal(t, 1, st.SkipHasJa)
	assert.Equal(t, 0, st.Written)
	intro, source, _ := introOf(t, work, "ja")
	assert.Equal(t, "already here", intro, "the incumbent text is untouched")
	assert.Equal(t, int16(3), source)
}

func TestChineseIntroDoesNotBlockTheJapaneseFill(t *testing.T) {
	clean(t)
	work := mkWork(t, "中文あり", workpop.Published)
	mkAnchoredRelease(t, work, "1003")
	mkStagingItem(t, "1003", "日本語の紹介")
	mkIntro(t, work, "zh-Hans", 3)

	st := run(t, true, workpop.Published)
	assert.Equal(t, 1, st.Written)
	_, _, found := introOf(t, work, "ja")
	assert.True(t, found)
}

func TestSecondApplyIsAZeroWriteNoOp(t *testing.T) {
	clean(t)
	work := mkWork(t, "二度目", workpop.Published)
	mkAnchoredRelease(t, work, "1004")
	mkStagingItem(t, "1004", "本文")

	first := run(t, true, workpop.Published)
	require.Equal(t, 1, first.Written)

	second := run(t, true, workpop.Published)
	assert.Equal(t, 0, second.Written)
	assert.Equal(t, 0, second.Conflict, "the preload skips before the write is attempted")
	assert.Equal(t, 1, second.SkipHasJa)
}

func TestBundleReleaseDoesNotTestifyForTheWork(t *testing.T) {
	clean(t)
	bundled := mkWork(t, "同梱先", workpop.Published)
	mkVndbRelease(t, mkAnchoredRelease(t, bundled, "6001"), "r6001", "v1", "v2")
	mkStagingItem(t, "6001", "パッケージの看板作品の物語")

	single := mkWork(t, "単独", workpop.Published)
	mkVndbRelease(t, mkAnchoredRelease(t, single, "6002"), "r6002", "v3")
	mkStagingItem(t, "6002", "この作品の物語")

	st := run(t, true, workpop.Published)
	assert.Equal(t, 1, st.SkipBundle)
	assert.Equal(t, 1, st.Written)
	_, _, found := introOf(t, bundled, "ja")
	assert.False(t, found, "a release VNDB files under two VNs says nothing about either")
	_, _, found = introOf(t, single, "ja")
	assert.True(t, found, "a single-VN release still testifies")
}

func TestPopulationNarrows(t *testing.T) {
	clean(t)
	for i, pop := range []workpop.Population{workpop.Bodyless, workpop.Claimed, workpop.Published} {
		id := mkWork(t, fmt.Sprintf("w%d", i), pop)
		gid := fmt.Sprintf("200%d", i)
		mkAnchoredRelease(t, id, gid)
		mkStagingItem(t, gid, "本文")
	}

	assert.Equal(t, 1, run(t, false, workpop.Published).Planned)
	assert.Equal(t, 2, run(t, false, workpop.Claimed).Planned, "published works are claimed too")
	assert.Equal(t, 3, run(t, false, workpop.All).Planned)
}

func TestUnreachableWorkIsCountedNotHidden(t *testing.T) {
	clean(t)
	work := mkWork(t, "story なし", workpop.Published)
	mkAnchoredRelease(t, work, "3001")
	mkStagingItem(t, "3001", "")

	st := run(t, true, workpop.Published)
	assert.Equal(t, 1, st.Works)
	assert.Equal(t, 1, st.NoStory)
	assert.Equal(t, 0, st.Written)
}

func TestProbableAnchorIsNotRead(t *testing.T) {
	clean(t)
	work := mkWork(t, "probable のみ", workpop.Published)
	rel := model.CatalogRelease{WorkID: work, Kind: 0}
	require.NoError(t, testDB.Create(&rel).Error)
	require.NoError(t, testDB.Exec(`
		INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by)
		VALUES (?,?,?,?,?,?)`,
		model.EntityTypeRelease, rel.ID, getchuSource, "4001", model.LinkKindProbable, getchuRule).Error)
	mkStagingItem(t, "4001", "本文")

	st := run(t, true, workpop.Published)
	assert.Equal(t, 0, st.Works)
	assert.Equal(t, 0, st.Written)
}

func TestWrittenWorkIsTouchedAndSkippedWorkIsNot(t *testing.T) {
	clean(t)
	filled := mkWork(t, "書かれる", workpop.Published)
	mkAnchoredRelease(t, filled, "5001")
	mkStagingItem(t, "5001", "本文")

	skipped := mkWork(t, "飛ばされる", workpop.Published)
	mkAnchoredRelease(t, skipped, "5002")
	mkStagingItem(t, "5002", "本文")
	mkIntro(t, skipped, "ja", 3)

	var before []struct {
		ID        int64 `gorm:"column:id"`
		UpdatedAt any   `gorm:"column:updated_at"`
	}
	require.NoError(t, testDB.Raw(`SELECT id, updated_at FROM catalog_work ORDER BY id`).Scan(&before).Error)

	require.Equal(t, 1, run(t, true, workpop.Published).Written)

	var after []struct {
		ID        int64 `gorm:"column:id"`
		UpdatedAt any   `gorm:"column:updated_at"`
	}
	require.NoError(t, testDB.Raw(`SELECT id, updated_at FROM catalog_work ORDER BY id`).Scan(&after).Error)
	require.Len(t, after, 2)
	assert.NotEqual(t, before[0].UpdatedAt, after[0].UpdatedAt, "the filled work moved")
	assert.Equal(t, before[1].UpdatedAt, after[1].UpdatedAt, "the skipped work did not")
}

func TestBothDSNsAreRequired(t *testing.T) {
	_, err := Run(context.Background(), Opts{GetchuDSN: "x"})
	require.Error(t, err)
	_, err = Run(context.Background(), Opts{DSN: "x"})
	require.Error(t, err)
}
