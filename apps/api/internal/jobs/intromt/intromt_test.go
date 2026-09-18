package intromt

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
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
		fmt.Fprintln(os.Stderr, "SKIP: no TEST_DATABASE_DSN — DB-backed intromt tests will skip individually")
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
	for _, table := range []string{"catalog_external_ref", "catalog_release"} {
		require.NoError(t, testDB.Exec("TRUNCATE "+table+" CASCADE").Error)
	}
	for _, table := range []string{"catalog_work_intro", "catalog_work_popularity", "catalog_work"} {
		require.NoError(t, testDB.Exec("TRUNCATE "+table+" RESTART IDENTITY CASCADE").Error)
	}
}

func mkWork(t *testing.T, medium int16, name string, site *string) int64 {
	t.Helper()
	w := model.CatalogWork{MediumID: medium, OLang: "ja", DisplayName: name, Site: site}
	require.NoError(t, testDB.Create(&w).Error)
	return w.ID
}

var nextProductWorkID int64 = 900000

func mkPublished(t *testing.T, medium int16, name string, site *string) int64 {
	t.Helper()
	nextProductWorkID++
	pid := nextProductWorkID
	w := model.CatalogWork{MediumID: medium, OLang: "ja", DisplayName: name, Site: site, ProductWorkID: &pid}
	require.NoError(t, testDB.Create(&w).Error)
	return w.ID
}

func mkGetchuAnchor(t *testing.T, workID int64, getchu, vndb int16, getchuID string) {
	t.Helper()
	rel := model.CatalogRelease{WorkID: workID, Kind: 0}
	require.NoError(t, testDB.Create(&rel).Error)
	require.NoError(t, testDB.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeRelease, EntityID: rel.ID, SourceID: getchu,
		ExternalID: getchuID, LinkKind: model.LinkKindExact, MatchedBy: "import:test"}).Error)
	_ = vndb
}

func mkIntro(t *testing.T, workID int64, lang, intro string, source int16) {
	t.Helper()
	require.NoError(t, testDB.Create(&model.CatalogWorkIntro{
		WorkID: workID, Lang: lang, Intro: intro, SourceID: source, Provenance: 0,
	}).Error)
}

func mkPop(t *testing.T, workID int64, source, metric int16, value int64) {
	t.Helper()
	require.NoError(t, testDB.Create(&model.CatalogWorkPopularity{
		WorkID: workID, SourceID: source, Metric: metric, Value: value,
	}).Error)
}

func introCount(t *testing.T, where string, args ...any) int64 {
	t.Helper()
	var n int64
	require.NoError(t, testDB.Raw("SELECT count(*) FROM catalog_work_intro "+where, args...).Scan(&n).Error)
	return n
}

func reg(t *testing.T) (medium, dlsite, bangumi int16) {
	t.Helper()
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_medium WHERE key='galgame'`).Scan(&medium).Error)
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_source WHERE key='dlsite'`).Scan(&dlsite).Error)
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_source WHERE key='bangumi'`).Scan(&bangumi).Error)
	require.NotZero(t, medium)
	require.NotZero(t, dlsite)
	require.NotZero(t, bangumi)
	return
}

type fakeTranslator struct {
	model string
	calls int
	fn    func(ja string) string
	gloss Glossary
	err   error
}

func (f *fakeTranslator) Translate(_ context.Context, ja string, gloss Glossary) (string, string, error) {
	f.calls++
	f.gloss = gloss
	if f.err != nil {
		return "", "", f.err
	}
	return f.fn(ja), f.model, nil
}

func TestPilotEndToEnd(t *testing.T) {
	clean(t)
	ctx := context.Background()
	medium, dlsite, bangumi := reg(t)
	claimed := "kungal"

	wInsert := mkWork(t, medium, "ja-no-zh", nil)
	wHasZh := mkWork(t, medium, "ja-has-zh-src", nil)
	wClaimed := mkWork(t, medium, "claimed", &claimed)
	wNoJa := mkWork(t, medium, "no-ja", nil)

	mkIntro(t, wInsert, "ja", "これはあらすじです。", bangumi)
	mkIntro(t, wHasZh, "ja", "日本語のあらすじ。", bangumi)
	mkIntro(t, wHasZh, "zh-Hans", "已有的中文简介。", bangumi)
	mkIntro(t, wClaimed, "ja", "claimed ja", bangumi)
	mkIntro(t, wNoJa, "en", "english only", bangumi)

	mkPop(t, wInsert, dlsite, model.PopularityMetricDownloads, 500)

	tr := &fakeTranslator{model: "test-mt", fn: func(ja string) string { return "[译] " + ja }}

	st, err := Run(ctx, nil, Opts{DSN: testDSN})
	require.NoError(t, err)
	assert.Equal(t, 1, st.Candidates, "only wInsert qualifies (has ja, no zh source, bodyless)")
	assert.Equal(t, 1, st.WouldInsert)
	assert.Equal(t, 0, st.WouldRetranslate)
	assert.Equal(t, 0, st.Inserted, "dry writes nothing")
	assert.EqualValues(t, 5, introCount(t, ""), "dry writes nothing (5 intro fixtures)")
	require.Len(t, st.Samples, 1)
	assert.Empty(t, st.Samples[0].Zh, "dry captures ja only, no translation")

	st, err = Run(ctx, tr, Opts{DSN: testDSN, Apply: true})
	require.NoError(t, err)
	assert.Equal(t, 1, st.Inserted)
	assert.Equal(t, 0, st.Refused)
	assert.Equal(t, 0, st.Errors)
	assert.Equal(t, 1, tr.calls, "exactly one translation (the single candidate)")

	var row model.CatalogWorkIntro
	require.NoError(t, testDB.Where("work_id=? AND lang='zh-Hans'", wInsert).First(&row).Error)
	assert.EqualValues(t, 1, row.Provenance, "machine row")
	assert.Equal(t, bangumi, row.SourceID, "attributed to the source ja row's source_id")
	assert.Equal(t, "[译] これはあらすじです。", row.Intro)
	assert.Equal(t, hashSource("これはあらすじです。"), row.SrcHash)
	assert.Equal(t, "test-mt", row.MTModel)
	assert.EqualValues(t, 0, introCount(t, "WHERE work_id=? AND lang='zh-Hans'", wClaimed), "claimed work gets no machine row")
	assert.EqualValues(t, 6, introCount(t, ""), "5 fixtures + 1 machine insert")

	tr2 := &fakeTranslator{model: "test-mt", fn: func(ja string) string { return "SHOULD-NOT-BE-CALLED" }}
	st, err = Run(ctx, tr2, Opts{DSN: testDSN, Apply: true})
	require.NoError(t, err)
	assert.Equal(t, 1, st.SkipUnchanged)
	assert.Zero(t, st.Inserted+st.Retranslated)
	assert.Equal(t, 0, tr2.calls, "unchanged source → no LLM call")

	require.NoError(t, testDB.Exec(`UPDATE catalog_work_intro SET intro=? WHERE work_id=? AND lang='ja'`,
		"あらすじが変わった。", wInsert).Error)
	tr3 := &fakeTranslator{model: "test-mt-v2", fn: func(ja string) string { return "[新译] " + ja }}
	st, err = Run(ctx, tr3, Opts{DSN: testDSN, Apply: true})
	require.NoError(t, err)
	assert.Equal(t, 1, st.WouldRetranslate)
	assert.Equal(t, 1, st.Retranslated)
	assert.Equal(t, 1, tr3.calls)
	var updated model.CatalogWorkIntro
	require.NoError(t, testDB.Where("work_id=? AND lang='zh-Hans'", wInsert).First(&updated).Error)
	assert.Equal(t, "[新译] あらすじが変わった。", updated.Intro, "re-translated in place")
	assert.Equal(t, hashSource("あらすじが変わった。"), updated.SrcHash, "src_hash follows the new source")
	assert.Equal(t, "test-mt-v2", updated.MTModel)
	assert.EqualValues(t, 1, introCount(t, "WHERE work_id=? AND lang='zh-Hans'", wInsert), "still one zh-Hans row (upsert, not a second row)")
}

func TestNeverOverwriteSource(t *testing.T) {
	clean(t)
	ctx := context.Background()
	medium, dlsite, bangumi := reg(t)
	w := mkWork(t, medium, "src-zh-guard", nil)
	mkIntro(t, w, "zh-Hans", "人工/源中文,不可覆盖。", bangumi)
	mkMachineIntro(t, w, "zh-Hans", "别的来源的机翻", dlsite, "stray-hash")

	r := &runner{db: testDB, tr: nil, stats: &Stats{}}
	rows, pruned, err := r.writeMachine(ctx, candidate{WorkID: w, JaSourceID: bangumi}, "机翻不该落地", "deadbeef", "test-mt")
	require.NoError(t, err)
	assert.EqualValues(t, 0, rows, "DO UPDATE WHERE provenance=1 refuses the source row")
	assert.Zero(t, pruned, "a refused write prunes nothing")
	assert.EqualValues(t, 1, introCount(t,
		"WHERE work_id=? AND lang='zh-Hans' AND provenance=1 AND source_id=?", w, dlsite))

	var row model.CatalogWorkIntro
	require.NoError(t, testDB.Where("work_id=? AND lang='zh-Hans'", w).First(&row).Error)
	assert.Equal(t, "人工/源中文,不可覆盖。", row.Intro, "source text intact")
	assert.EqualValues(t, 0, row.Provenance, "still a source row")
}

func TestPopularityOrdering(t *testing.T) {
	clean(t)
	ctx := context.Background()
	medium, dlsite, bangumi := reg(t)

	wHi := mkWork(t, medium, "hi-downloads", nil)
	wMid := mkWork(t, medium, "mid-downloads", nil)
	wWish := mkWork(t, medium, "wishlist-only", nil)
	for _, w := range []int64{wHi, wMid, wWish} {
		mkIntro(t, w, "ja", fmt.Sprintf("あらすじ%d", w), bangumi)
	}
	mkPop(t, wHi, dlsite, model.PopularityMetricDownloads, 9000)
	mkPop(t, wMid, dlsite, model.PopularityMetricDownloads, 100)
	mkPop(t, wWish, dlsite, model.PopularityMetricWishlist, 8000)

	reg2, err := resolveRegistry(ctx, testDB)
	require.NoError(t, err)
	cands, err := loadCandidates(ctx, testDB, reg2, PopulationBodyless, SourceJa, 5000, 2, nil)
	require.NoError(t, err)
	require.Len(t, cands, 2, "--limit 2 keeps the two most popular")
	assert.Equal(t, wHi, cands[0].WorkID, "9000 downloads first")
	assert.Equal(t, wWish, cands[1].WorkID)
}

func TestClaimedPopulation(t *testing.T) {
	clean(t)
	ctx := context.Background()
	medium, _, bangumi := reg(t)
	var curated int16
	require.NoError(t, testDB.Raw(
		`SELECT id FROM catalog_source WHERE key = 'curated'`).Scan(&curated).Error)
	require.NotZero(t, curated)
	claimed := "kungal"
	formerSite := "galgame_wiki"

	wA := mkWork(t, medium, "claimed-a", &claimed)
	wB := mkWork(t, medium, "claimed-b", &claimed)
	wZh := mkWork(t, medium, "claimed-has-zh", &claimed)
	wFormer := mkWork(t, medium, "former-site", &formerSite)
	wBodyless := mkWork(t, medium, "bodyless", nil)

	mkIntro(t, wA, "ja", "認領作品Aのあらすじ。", curated)
	mkIntro(t, wB, "ja", "認領作品Bのあらすじ。", curated)
	mkIntro(t, wZh, "ja", "中文既存のあらすじ。", curated)
	mkIntro(t, wZh, "zh-Hans", "已有的中文简介。", bangumi)
	mkIntro(t, wFormer, "ja", "旧サイトのあらすじ。", curated)
	mkIntro(t, wBodyless, "ja", "ボディレスのあらすじ。", bangumi)

	st, err := Run(ctx, nil, Opts{DSN: testDSN, Population: PopulationClaimed})
	require.NoError(t, err)
	assert.Equal(t, 3, st.Candidates, "zh-present and bodyless works are excluded; a stale site value is still a claim")

	reg2, err := resolveRegistry(ctx, testDB)
	require.NoError(t, err)
	cands, err := loadCandidates(ctx, testDB, reg2, PopulationClaimed, SourceJa, 5000, 0, nil)
	require.NoError(t, err)
	require.Len(t, cands, 3)
	assert.Equal(t, wA, cands[0].WorkID, "no popularity → work_id ASC")
	assert.Equal(t, wB, cands[1].WorkID)
	assert.Equal(t, wFormer, cands[2].WorkID, "a stale site value is still a claim")
	assert.Equal(t, curated, cands[0].JaSourceID, "chosen ja row is the curated source row")

	tr := &fakeTranslator{model: "claimed-mt", fn: func(ja string) string { return "[译] " + ja }}
	st, err = Run(ctx, tr, Opts{DSN: testDSN, Apply: true, Population: PopulationClaimed})
	require.NoError(t, err)
	assert.Equal(t, 3, st.Inserted)
	assert.Zero(t, st.Refused)
	assert.Zero(t, st.Errors)

	var row model.CatalogWorkIntro
	require.NoError(t, testDB.Where("work_id=? AND lang='zh-Hans'", wA).First(&row).Error)
	assert.EqualValues(t, 1, row.Provenance, "machine row")
	assert.Equal(t, curated, row.SourceID, "attributed to the curated ja row's source_id")
	assert.Equal(t, "[译] 認領作品Aのあらすじ。", row.Intro)
	assert.EqualValues(t, 1, introCount(t, "WHERE work_id=? AND lang='zh-Hans'", wFormer), "a stale site value is still a claim")
	assert.EqualValues(t, 0, introCount(t, "WHERE work_id=? AND lang='zh-Hans'", wBodyless), "bodyless work untouched by the claimed lane")

	st, err = Run(ctx, nil, Opts{DSN: testDSN})
	require.NoError(t, err)
	assert.Equal(t, 1, st.Candidates, "empty Population defaults to the bodyless pilot lane")

	_, err = Run(ctx, nil, Opts{DSN: testDSN, Population: "everything"})
	assert.ErrorContains(t, err, "unknown population")
}

func TestHTTPTranslator(t *testing.T) {
	var gotAuth, gotPath string
	var gotBody chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		_, _ = io.WriteString(w, `{"model":"deepseek-chat","choices":[{"message":{"role":"assistant","content":"  这是译文。  "},"finish_reason":"stop"}]}`)
	}))
	defer srv.Close()

	tr := NewHTTPTranslator(srv.URL, "sekret", "deepseek-chat", 512)
	require.True(t, tr.Configured())
	zh, model, err := tr.Translate(context.Background(), "これは原文です。", nil)
	require.NoError(t, err)
	assert.Equal(t, "这是译文。", zh, "content trimmed, plain text is the translation")
	assert.Equal(t, "deepseek-chat", model)
	assert.Equal(t, "Bearer sekret", gotAuth)
	assert.Equal(t, "/chat/completions", gotPath)
	require.Len(t, gotBody.Messages, 2)
	assert.Equal(t, TranslateSystemPrompt, gotBody.Messages[0].Content, "pinned system prompt sent verbatim")
	assert.Equal(t, "これは原文です。", gotBody.Messages[1].Content, "ja source as user content")
	assert.EqualValues(t, 0, gotBody.Temperature, "faithful/deterministic")
}

func TestHTTPTranslatorErrors(t *testing.T) {
	origSchedule := retrySchedule
	retrySchedule = []time.Duration{time.Millisecond}
	defer func() { retrySchedule = origSchedule }()

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, "boom")
	}))
	defer bad.Close()
	tr := NewHTTPTranslator(bad.URL, "t", "m", 64)
	_, _, err := tr.Translate(context.Background(), "x", nil)
	assert.Error(t, err)

	var hits int
	limited := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		if hits == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = io.WriteString(w, `{"model":"m","choices":[{"message":{"role":"assistant","content":"译"},"finish_reason":"stop"}]}`)
	}))
	defer limited.Close()
	zh, _, err := NewHTTPTranslator(limited.URL, "t", "m", 64).Translate(context.Background(), "x", nil)
	require.NoError(t, err, "429 retried to success")
	assert.Equal(t, "译", zh)
	assert.Equal(t, 2, hits)

	trunc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"model":"m","choices":[{"message":{"role":"assistant","content":"残りは途中で"},"finish_reason":"length"}]}`)
	}))
	defer trunc.Close()
	_, _, err = NewHTTPTranslator(trunc.URL, "t", "m", 64).Translate(context.Background(), "x", nil)
	assert.ErrorContains(t, err, "finish_reason", "partial output refused")

	assert.False(t, NewHTTPTranslator("", "t", "m", 64).Configured(), "no base → not configured")
	assert.False(t, NewHTTPTranslator("http://x/v1", "", "m", 64).Configured(), "no token → not configured")
}

func TestConcurrentApply(t *testing.T) {
	clean(t)
	ctx := context.Background()
	medium, dlsite, bangumi := reg(t)

	const n = 40
	for i := range n {
		w := mkWork(t, medium, fmt.Sprintf("conc-%d", i), nil)
		mkIntro(t, w, "ja", fmt.Sprintf("あらすじ %d。", i), bangumi)
		mkPop(t, w, dlsite, model.PopularityMetricDownloads, int64(1000-i))
	}

	tr := &slowFakeTranslator{model: "conc-mt"}
	st, err := Run(ctx, tr, Opts{DSN: testDSN, Apply: true, Workers: 8})
	require.NoError(t, err)
	assert.Equal(t, n, st.Candidates)
	assert.Equal(t, n, st.Inserted)
	assert.Zero(t, st.Errors)
	assert.Zero(t, st.Refused)
	assert.EqualValues(t, n, introCount(t, "WHERE provenance = 1"))
	assert.EqualValues(t, n, tr.calls.Load(), "one gateway call per candidate")

	st, err = Run(ctx, tr, Opts{DSN: testDSN, Apply: true, Workers: 8})
	require.NoError(t, err)
	assert.Equal(t, n, st.SkipUnchanged)
	assert.Zero(t, st.Inserted)
	assert.EqualValues(t, n, tr.calls.Load(), "skips never dial the gateway")
}

type slowFakeTranslator struct {
	model string
	calls atomic.Int64
}

func (f *slowFakeTranslator) Translate(_ context.Context, ja string, _ Glossary) (string, string, error) {
	f.calls.Add(1)
	time.Sleep(2 * time.Millisecond)
	return "[译] " + ja, f.model, nil
}

func TestMockTranslatorDeterminism(t *testing.T) {
	m := MockTranslator{Model: "stub"}
	a1, mdl, err := m.Translate(context.Background(), "同じ原文", nil)
	require.NoError(t, err)
	a2, _, _ := m.Translate(context.Background(), "同じ原文", nil)
	b, _, _ := m.Translate(context.Background(), "違う原文", nil)
	assert.Equal(t, a1, a2, "same source → same output (idempotent)")
	assert.NotEqual(t, a1, b, "different source → different output (re-translate trigger)")
	assert.True(t, strings.HasPrefix(mdl, "mock:"), "obvious mock model id")
}

func TestEnglishLaneIsALastResort(t *testing.T) {
	clean(t)
	ctx := context.Background()
	medium, _, bangumi := reg(t)
	var curated, getchu, vndb int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_source WHERE key='curated'`).Scan(&curated).Error)
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_source WHERE key='getchu'`).Scan(&getchu).Error)
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_source WHERE key='vndb'`).Scan(&vndb).Error)
	require.NotZero(t, getchu, "the seed must carry the getchu source row")
	site := "kungal"

	wTarget := mkPublished(t, medium, "en-only-orphan", &site)
	mkIntro(t, wTarget, "en", "A quiet town where nothing ever happens.", curated)

	wHasJa := mkPublished(t, medium, "en-and-ja", &site)
	mkIntro(t, wHasJa, "en", "An English blurb.", curated)
	mkIntro(t, wHasJa, "ja", "日本語のあらすじ。", bangumi)

	wGetchu := mkPublished(t, medium, "en-but-getchu-anchored", &site)
	mkIntro(t, wGetchu, "en", "Another English blurb.", curated)
	mkGetchuAnchor(t, wGetchu, getchu, vndb, "1117747")

	wHasZh := mkPublished(t, medium, "en-and-zh", &site)
	mkIntro(t, wHasZh, "en", "Yet another English blurb.", curated)
	mkIntro(t, wHasZh, "zh-Hans", "已有的中文简介。", bangumi)

	st, err := Run(ctx, nil, Opts{DSN: testDSN, Population: PopulationPublished, SourceLang: SourceEn})
	require.NoError(t, err)
	assert.Equal(t, 2, st.Candidates,
		"en-only works are candidates (getchu anchor no longer excludes); ja-having and zh-having works are not")

	reg2, err := resolveRegistry(ctx, testDB)
	require.NoError(t, err)
	cands, err := loadCandidates(ctx, testDB, reg2, PopulationPublished, SourceEn, 5000, 0, nil)
	require.NoError(t, err)
	require.Len(t, cands, 2)
	ids := []int64{cands[0].WorkID, cands[1].WorkID}
	assert.ElementsMatch(t, []int64{wTarget, wGetchu}, ids)
	assert.Equal(t, "A quiet town where nothing ever happens.", cands[0].JaText,
		"the lane carries the ENGLISH source text through the same field")

	tr := &fakeTranslator{model: "en-mt", fn: func(src string) string { return "[译] " + src }}
	st, err = Run(ctx, tr, Opts{DSN: testDSN, Apply: true, Population: PopulationPublished, SourceLang: SourceEn})
	require.NoError(t, err)
	assert.Equal(t, 2, st.Inserted)
	assert.Zero(t, st.Errors)

	var row model.CatalogWorkIntro
	require.NoError(t, testDB.Where("work_id=? AND lang='zh-Hans'", wTarget).First(&row).Error)
	assert.EqualValues(t, 1, row.Provenance, "machine row")
	assert.Equal(t, curated, row.SourceID, "attributed to the English row's source_id")
	assert.EqualValues(t, 0, introCount(t, "WHERE work_id=? AND lang='zh-Hans'", wHasJa),
		"the ja-having work must be left for the japanese path")

	st, err = Run(ctx, tr, Opts{DSN: testDSN, Apply: true, Population: PopulationPublished, SourceLang: SourceEn})
	require.NoError(t, err)
	assert.Zero(t, st.Inserted, "fill-missing is idempotent")
}

func TestOlangGateExcludesNonJapaneseOriginals(t *testing.T) {
	clean(t)
	ctx := context.Background()
	medium, _, bangumi := reg(t)
	site := "kungal"

	wJa := mkPublished(t, medium, "olang-ja", &site)
	mkIntro(t, wJa, "ja", "日本語のあらすじ。", bangumi)
	require.NoError(t, testDB.Exec(`UPDATE catalog_work SET olang='ja' WHERE id=?`, wJa).Error)

	wUnset := mkPublished(t, medium, "olang-unset", &site)
	mkIntro(t, wUnset, "ja", "原語未申告のあらすじ。", bangumi)

	wEn := mkPublished(t, medium, "olang-en", &site)
	mkIntro(t, wEn, "ja", "英語原作の日本語紹介。", bangumi)
	require.NoError(t, testDB.Exec(`UPDATE catalog_work SET olang='en' WHERE id=?`, wEn).Error)

	reg2, err := resolveRegistry(ctx, testDB)
	require.NoError(t, err)
	cands, err := loadCandidates(ctx, testDB, reg2, PopulationPublished, SourceJa, 0, 0, nil)
	require.NoError(t, err)
	require.Len(t, cands, 2, "olang=ja and unset pass; a declared non-ja original is excluded")
	ids := []int64{cands[0].WorkID, cands[1].WorkID}
	assert.ElementsMatch(t, []int64{wJa, wUnset}, ids)
}

func TestPublishedPopulationExcludesDrafts(t *testing.T) {
	clean(t)
	ctx := context.Background()
	medium, _, _ := reg(t)
	var curated int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_source WHERE key='curated'`).Scan(&curated).Error)
	site := "kungal"

	wLive := mkPublished(t, medium, "live", &site)
	mkIntro(t, wLive, "en", "Live work.", curated)
	wDraft := mkPublished(t, medium, "draft", &site)
	mkIntro(t, wDraft, "en", "Draft work.", curated)
	require.NoError(t, testDB.Model(&model.CatalogWork{}).Where("id = ?", wDraft).
		Update("claim_state", model.ClaimStateDraft).Error)

	st, err := Run(ctx, nil, Opts{DSN: testDSN, Population: PopulationPublished, SourceLang: SourceEn})
	require.NoError(t, err)
	assert.Equal(t, 1, st.Candidates, "the draft claim is not on the public face")
}

func TestSanitizeSourceStripsUpstreamMarkup(t *testing.T) {
	cases := []struct{ in, want string }{
		{"[Toono Shiki](/c72) hears of murders in [Tsukihime](/v7).",
			"Toono Shiki hears of murders in Tsukihime."},
		{"A DLC starring [Nachi](/c55103).", "A DLC starring Nachi."},
		{"See [url=https://example.com/x]the site[/url] for more.",
			"See the site for more."},
		{"[spoiler]She dies.[/spoiler]", "She dies."},
		{"[raw]魔法少女[/raw]", "魔法少女"},
		{"[docs](https://example.com/d)", "[docs](https://example.com/d)"},
		{"ごく普通の日本語のあらすじ。", "ごく普通の日本語のあらすじ。"},
		{"", ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, sanitizeSource(c.in), c.in)
	}
}

func TestSanitizeChangesHashOnlyForDirtyText(t *testing.T) {
	clean := "ごく普通のあらすじ。"
	assert.Equal(t, hashSource(clean), hashSource(sanitizeSource(clean)),
		"a clean source must keep its hash or the whole corpus re-translates")

	dirty := "[Toono Shiki](/c72) hears of murders."
	assert.NotEqual(t, hashSource(dirty), hashSource(sanitizeSource(dirty)),
		"a dirty source must change hash so the lane re-translates it")
}
