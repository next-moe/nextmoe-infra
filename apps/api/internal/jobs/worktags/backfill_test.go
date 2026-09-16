package worktags

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	srcb "api/internal/platform/catalog/srcbangumi"
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
		dbtest.SkipMain("jobs/worktags")
	}
	db, err := gorm.Open(postgres.Open(testDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("jobs/worktags", "cannot connect to test database: %v", err)
	}
	if err := migrate.Run(db); err != nil {
		dbtest.SkipMainf("jobs/worktags", "catalog migrate failed: %v", err)
	}
	if err := seed.Run(db); err != nil {
		dbtest.SkipMainf("jobs/worktags", "catalog seed failed: %v", err)
	}
	if err := srcb.EnsureSchema(db); err != nil {
		dbtest.SkipMainf("jobs/worktags", "src_bangumi schema failed: %v", err)
	}
	testDB = db
	os.Exit(m.Run())
}

func clean(t *testing.T) {
	t.Helper()
	for _, table := range []string{
		"catalog_work_tag", "catalog_external_ref", "catalog_work", "src_bangumi.subject",
	} {
		require.NoError(t, testDB.Exec("TRUNCATE "+table+" RESTART IDENTITY CASCADE").Error)
	}
}

func mkWork(t *testing.T, medium int16, name string, site *string) int64 {
	t.Helper()
	w := model.CatalogWork{MediumID: medium, OLang: "ja", DisplayName: name, Site: site}
	require.NoError(t, testDB.Create(&w).Error)
	return w.ID
}

func mkAnchor(t *testing.T, workID int64, externalID string, source, kind int16, matchedBy string) {
	t.Helper()
	require.NoError(t, testDB.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeWork, EntityID: workID, SourceID: source,
		ExternalID: externalID, LinkKind: kind, MatchedBy: matchedBy,
	}).Error)
}

func mkSubject(t *testing.T, id int64, tags string) {
	t.Helper()
	sub := srcb.Subject{
		ID: id, Type: 4, Name: fmt.Sprintf("subject-%d", id), NameCN: "",
		InfoboxRaw: "", ParseError: "", Summary: "", Date: "",
		ParserVersion: srcb.ParserVersion, IngestedAt: time.Now(),
	}
	if tags != "" {
		sub.Tags = []byte(tags)
	}
	require.NoError(t, testDB.Create(&sub).Error)
}

func tagCount(t *testing.T, where string, args ...any) int64 {
	t.Helper()
	var n int64
	require.NoError(t, testDB.Raw("SELECT count(*) FROM catalog_work_tag "+where, args...).Scan(&n).Error)
	return n
}

func TestBackfillWorkTags(t *testing.T) {
	clean(t)
	ctx := context.Background()
	reg, err := resolveRegistry(ctx, testDB)
	require.NoError(t, err)

	claimed := "galgame_wiki"

	wGood := mkWork(t, reg.galgameMedium, "tags-good", nil)
	mkSubject(t, 201, `[{"name":"百合","count":24},{"name":"拔作","count":1},{"name":"  PC  ","count":5},{"name":"百合","count":30},{"name":"   ","count":3},{"count":9}]`)
	mkAnchor(t, wGood, "201", reg.bangumiSource, model.LinkKindExact, ruleTitleYear)

	wNull := mkWork(t, reg.galgameMedium, "tags-null", nil)
	mkSubject(t, 202, "")
	mkAnchor(t, wNull, "202", reg.bangumiSource, model.LinkKindExact, ruleTitleYear)

	wEmpty := mkWork(t, reg.galgameMedium, "tags-empty", nil)
	mkSubject(t, 203, `[]`)
	mkAnchor(t, wEmpty, "203", reg.bangumiSource, model.LinkKindExact, ruleTitleYear)

	wObject := mkWork(t, reg.galgameMedium, "tags-object", nil)
	mkSubject(t, 204, `{"name":"百合","count":9}`)
	mkAnchor(t, wObject, "204", reg.bangumiSource, model.LinkKindExact, ruleTitleYear)

	wClaimed := mkWork(t, reg.galgameMedium, "tags-claimed", &claimed)
	mkSubject(t, 205, `[{"name":"百合","count":7}]`)
	mkAnchor(t, wClaimed, "205", reg.bangumiSource, model.LinkKindExact, ruleTitleYear)

	wProbable := mkWork(t, reg.galgameMedium, "tags-probable", nil)
	mkSubject(t, 206, `[{"name":"百合","count":7}]`)
	mkAnchor(t, wProbable, "206", reg.bangumiSource, model.LinkKindProbable, "rule:bgm-title-only")

	wGood2 := mkWork(t, reg.galgameMedium, "tags-good2", nil)
	mkSubject(t, 207, `[{"name":"百合","count":2}]`)
	mkAnchor(t, wGood2, "207", reg.bangumiSource, model.LinkKindExact, ruleTitleYear)

	st, err := Run(ctx, Opts{DSN: testDSN})
	require.NoError(t, err)
	assert.Equal(t, 6, st.Candidates, "probable excluded; claimed admitted (T2)")
	assert.Equal(t, 2, st.NoTags, "NULL + empty array")
	assert.Equal(t, 1, st.NotArray)
	assert.Equal(t, 2, st.NameBlank, "whitespace-only + missing name")
	assert.Equal(t, 1, st.DupCollapsed, "the second 百合 collapsed")
	assert.Equal(t, 5, st.Planned, "wGood 3 + wGood2 1 + wClaimed 1 (T2 admits claimed)")
	assert.Equal(t, 3, st.DistinctNames, "百合 / PC / 拔作")
	assert.Zero(t, st.Written+st.Conflict+st.Errors)
	assert.EqualValues(t, 0, tagCount(t, ""), "dry run writes nothing")
	require.GreaterOrEqual(t, len(st.Samples), 3)
	assert.Equal(t, Sample{WorkID: wGood, SubjectID: 201, Name: "百合", Count: 30}, st.Samples[0],
		"duplicate collapsed to the HIGHEST count")
	assert.Equal(t, "PC", st.Samples[1].Name, "name whitespace-trimmed")
	assert.Equal(t, Sample{WorkID: wGood, SubjectID: 201, Name: "拔作", Count: 1}, st.Samples[2],
		"count=1 stored too (store-all, no threshold)")
	require.NotEmpty(t, st.TopNames)
	assert.Equal(t, NameFreq{Name: "百合", Works: 3}, st.TopNames[0])

	st, err = Run(ctx, Opts{DSN: testDSN, Apply: true})
	require.NoError(t, err)
	assert.Equal(t, 5, st.Written)
	assert.Zero(t, st.Conflict+st.Errors)
	assert.EqualValues(t, 5, tagCount(t, ""))

	var rows []model.CatalogWorkTag
	require.NoError(t, testDB.Where("work_id = ?", wGood).Order("count DESC, name").Find(&rows).Error)
	require.Len(t, rows, 3)
	assert.Equal(t, "百合", rows[0].Name)
	assert.Equal(t, 30, rows[0].Count)
	assert.Equal(t, reg.bangumiSource, rows[0].SourceID)
	assert.Equal(t, "PC", rows[1].Name)
	assert.Equal(t, 5, rows[1].Count)
	assert.Equal(t, "拔作", rows[2].Name)
	assert.Equal(t, 1, rows[2].Count)
	assert.EqualValues(t, 1, tagCount(t, "WHERE work_id = ?", wGood2))
	assert.EqualValues(t, 1, tagCount(t, "WHERE work_id = ?", wClaimed),
		"T2: the claimed work's bgm folksonomy row now materialises")
	assert.EqualValues(t, 0, tagCount(t, "WHERE work_id = ?", wProbable),
		"probable anchors never materialise")

	st, err = Run(ctx, Opts{DSN: testDSN, Apply: true})
	require.NoError(t, err)
	assert.Zero(t, st.Written+st.Errors, "second pass writes zero")
	assert.Equal(t, 5, st.Conflict)
	assert.EqualValues(t, 5, tagCount(t, ""), "row count unchanged")
}

func TestNonTitleYearExactAnchorEntersCandidates(t *testing.T) {
	clean(t)
	ctx := context.Background()
	reg, err := resolveRegistry(ctx, testDB)
	require.NoError(t, err)

	wGated := mkWork(t, reg.galgameMedium, "tags-gated", nil)
	mkSubject(t, 501, `[{"name":"純愛","count":12}]`)
	mkAnchor(t, wGated, "501", reg.bangumiSource, model.LinkKindExact, "rule:bgm-type4-gated")

	wProbable := mkWork(t, reg.galgameMedium, "tags-probable", nil)
	mkSubject(t, 502, `[{"name":"純愛","count":9}]`)
	mkAnchor(t, wProbable, "502", reg.bangumiSource, model.LinkKindProbable, "rule:bgm-title-only")

	st, err := Run(ctx, Opts{DSN: testDSN, Apply: true})
	require.NoError(t, err)
	assert.Equal(t, 1, st.Candidates, "the gated-rule exact anchor is now a candidate; probable stays out")
	assert.EqualValues(t, 1, tagCount(t, "WHERE work_id = ?", wGated))
	assert.EqualValues(t, 0, tagCount(t, "WHERE work_id = ?", wProbable))
}

func TestClaimedMaterialisesAndDSNRequired(t *testing.T) {
	clean(t)
	ctx := context.Background()
	reg, err := resolveRegistry(ctx, testDB)
	require.NoError(t, err)

	claimed := "galgame_wiki"
	wClaimed := mkWork(t, reg.galgameMedium, "claimed-direct", &claimed)

	w := &writer{db: testDB, stats: &Stats{}}
	w.write(ctx, plannedRow{WorkID: wClaimed, SourceID: reg.bangumiSource, Name: "百合", Count: 3}, true)
	assert.Equal(t, 1, w.stats.Written, "T2: the claimed work materialises")
	w.write(ctx, plannedRow{WorkID: wClaimed, SourceID: reg.bangumiSource, Name: "百合", Count: 3}, true)
	assert.Equal(t, 1, w.stats.Conflict, "ON CONFLICT refuses the duplicate")
	w.write(ctx, plannedRow{WorkID: wClaimed, SourceID: reg.bangumiSource, Name: "拔作", Count: 1}, true)
	assert.Equal(t, 2, w.stats.Written, "same work+source, different name → distinct row")
	assert.EqualValues(t, 2, tagCount(t, ""))

	_, err = Run(ctx, Opts{})
	require.Error(t, err)
}

func backdate(t *testing.T) time.Time {
	t.Helper()
	ts := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, testDB.Exec(`UPDATE catalog_work SET updated_at = ?`, ts).Error)
	return ts
}

func workUpdatedAt(t *testing.T, id int64) time.Time {
	t.Helper()
	var ts time.Time
	require.NoError(t, testDB.Raw(`SELECT updated_at FROM catalog_work WHERE id = ?`, id).Scan(&ts).Error)
	return ts
}

func TestTagWriteTouchesHostWork(t *testing.T) {
	clean(t)
	ctx := context.Background()
	reg, err := resolveRegistry(ctx, testDB)
	require.NoError(t, err)

	wTagged := mkWork(t, reg.galgameMedium, "touch-tagged", nil)
	mkSubject(t, 701, `[{"name":"純愛","count":12}]`)
	mkAnchor(t, wTagged, "701", reg.bangumiSource, model.LinkKindExact, ruleTitleYear)

	wUntagged := mkWork(t, reg.galgameMedium, "touch-untagged", nil)
	stamp := backdate(t)

	_, err = Run(ctx, Opts{DSN: testDSN})
	require.NoError(t, err)
	assert.True(t, workUpdatedAt(t, wTagged).Equal(stamp), "dry run must not touch")

	st, err := Run(ctx, Opts{DSN: testDSN, Apply: true})
	require.NoError(t, err)
	require.Equal(t, 1, st.Written)
	touched := workUpdatedAt(t, wTagged)
	assert.True(t, touched.After(stamp), "the tag write bumped its host work")
	assert.True(t, workUpdatedAt(t, wUntagged).Equal(stamp), "an unwritten work stays put")

	st, err = Run(ctx, Opts{DSN: testDSN, Apply: true})
	require.NoError(t, err)
	require.Zero(t, st.Written)
	require.Equal(t, 1, st.Conflict)
	assert.True(t, workUpdatedAt(t, wTagged).Equal(touched), "a no-op re-run must not drift the watermark")
}

func TestNewRowInheritsTheSexualVerdictOfItsName(t *testing.T) {
	clean(t)
	ctx := context.Background()
	reg, err := resolveRegistry(ctx, testDB)
	require.NoError(t, err)

	judged := mkWork(t, reg.galgameMedium, "judged", nil)
	require.NoError(t, testDB.Create(&model.CatalogWorkTag{
		WorkID: judged, Name: "巨乳", Count: 3, SourceID: reg.bangumiSource, Sexual: true,
	}).Error)
	fresh := mkWork(t, reg.galgameMedium, "fresh", nil)
	mkSubject(t, 301, `[{"name":"巨乳","count":4},{"name":"百合","count":2}]`)
	mkAnchor(t, fresh, "301", reg.bangumiSource, model.LinkKindExact, ruleTitleYear)

	st, err := Run(ctx, Opts{DSN: testDSN, Apply: true})
	require.NoError(t, err)
	assert.Equal(t, 2, st.Written)
	assert.Equal(t, 1, st.SexualInherited)
	assert.EqualValues(t, 1, tagCount(t, "WHERE work_id = ? AND name = ? AND sexual", fresh, "巨乳"),
		"classify-tag-safety judged 巨乳 once, per source and name")
	assert.EqualValues(t, 1, tagCount(t, "WHERE work_id = ? AND name = ? AND NOT sexual", fresh, "百合"))
}
