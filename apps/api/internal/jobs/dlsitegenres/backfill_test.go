package dlsitegenres

import (
	"context"
	"os"
	"testing"

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
	testDB    *gorm.DB
	testDSN   string
	dlTestDSN string
)

func TestMain(m *testing.M) {
	var ok bool
	testDSN, ok = dbtest.DSN()
	if !ok {
		dbtest.SkipMain("jobs/dlsitegenres")
	}
	db, err := gorm.Open(postgres.Open(testDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("jobs/dlsitegenres", "cannot connect to test database: %v", err)
	}
	if err := migrate.Run(db); err != nil {
		dbtest.SkipMainf("jobs/dlsitegenres", "catalog migrate failed: %v", err)
	}
	if err := seed.Run(db); err != nil {
		dbtest.SkipMainf("jobs/dlsitegenres", "catalog seed failed: %v", err)
	}
	if err := srcvndb.EnsureSchema(db); err != nil {
		dbtest.SkipMainf("jobs/dlsitegenres", "src_vndb schema failed: %v", err)
	}
	for _, ddl := range []string{
		`CREATE SCHEMA IF NOT EXISTS dlsitegenres_dl`,
		`CREATE TABLE IF NOT EXISTS dlsitegenres_dl.works (workno text PRIMARY KEY, product_json jsonb)`,
		`CREATE TABLE IF NOT EXISTS dlsitegenres_dl.genre_taxonomy (
			genre_id int NOT NULL, locale text NOT NULL, name text NOT NULL,
			PRIMARY KEY (genre_id, locale))`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			dbtest.SkipMainf("jobs/dlsitegenres", "mirror fixture failed: %v", err)
		}
	}
	dlTestDSN = testDSN + " options='-csearch_path=dlsitegenres_dl'"
	testDB = db
	os.Exit(m.Run())
}

func clean(t *testing.T) {
	t.Helper()
	for _, table := range []string{
		"catalog_work_tag", "catalog_external_ref", "catalog_release", "catalog_work",
		"dlsitegenres_dl.works", "dlsitegenres_dl.genre_taxonomy", "src_vndb.releases_vn",
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

func mkReleaseAnchor(t *testing.T, workID int64, externalID string, source, kind int16) int64 {
	t.Helper()
	rel := model.CatalogRelease{WorkID: workID, Kind: model.ReleaseKindDigital}
	require.NoError(t, testDB.Create(&rel).Error)
	require.NoError(t, testDB.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeRelease, EntityID: rel.ID, SourceID: source,
		ExternalID: externalID, LinkKind: kind, MatchedBy: "rule:test",
	}).Error)
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

func mkMirrorWork(t *testing.T, workno, genres string) {
	t.Helper()
	pj := `{}`
	if genres != "" {
		pj = `{"genres": ` + genres + `}`
	}
	require.NoError(t, testDB.Exec(
		`INSERT INTO dlsitegenres_dl.works (workno, product_json) VALUES (?, ?::jsonb)`,
		workno, pj).Error)
}

func mkTaxonomy(t *testing.T, genreID int, locale, name string) {
	t.Helper()
	require.NoError(t, testDB.Exec(
		`INSERT INTO dlsitegenres_dl.genre_taxonomy (genre_id, locale, name) VALUES (?, ?, ?)`,
		genreID, locale, name).Error)
}

func tagCount(t *testing.T, where string, args ...any) int64 {
	t.Helper()
	var n int64
	require.NoError(t, testDB.Raw("SELECT count(*) FROM catalog_work_tag "+where, args...).Scan(&n).Error)
	return n
}

func runOpts(apply bool) Opts {
	return Opts{DSN: testDSN, DlsiteDSN: dlTestDSN, Apply: apply}
}

func TestBackfillDlsiteGenres(t *testing.T) {
	clean(t)
	ctx := context.Background()
	reg, err := resolveRegistry(ctx, testDB)
	require.NoError(t, err)

	claimed := "galgame_wiki"

	mkTaxonomy(t, 226, "zh_CN", "女教师")
	mkTaxonomy(t, 226, "ja_JP", "女教師")
	mkTaxonomy(t, 113, "zh_CN", "强X")
	mkTaxonomy(t, 300, "zh_CN", "重复名")
	mkTaxonomy(t, 301, "zh_CN", "重复名")

	wFull := mkWork(t, reg.galgameMedium, "genres-full", nil)
	mkReleaseAnchor(t, wFull, "RJ200001", reg.dlsiteSource, model.LinkKindExact)
	mkMirrorWork(t, "RJ200001",
		`[{"id":226,"name":"女教師"},{"id":113,"name":"レイプ"},{"id":9999,"name":"退役ジャンル"}]`)

	wDup := mkWork(t, reg.galgameMedium, "genres-dup", nil)
	mkReleaseAnchor(t, wDup, "RJ200002", reg.dlsiteSource, model.LinkKindExact)
	mkMirrorWork(t, "RJ200002", `[{"id":300,"name":"旧名A"},{"id":301,"name":"旧名B"}]`)

	wBlank := mkWork(t, reg.galgameMedium, "genres-blank", nil)
	mkReleaseAnchor(t, wBlank, "RJ200003", reg.dlsiteSource, model.LinkKindExact)
	mkMirrorWork(t, "RJ200003", `[{"id":8888,"name":"   "},{"id":8887}]`)

	wEmpty := mkWork(t, reg.galgameMedium, "genres-empty", nil)
	mkReleaseAnchor(t, wEmpty, "RJ200004", reg.dlsiteSource, model.LinkKindExact)
	mkMirrorWork(t, "RJ200004", `[]`)

	wNull := mkWork(t, reg.galgameMedium, "genres-null", nil)
	mkReleaseAnchor(t, wNull, "RJ200005", reg.dlsiteSource, model.LinkKindExact)
	mkMirrorWork(t, "RJ200005", "")

	wObject := mkWork(t, reg.galgameMedium, "genres-object", nil)
	mkReleaseAnchor(t, wObject, "RJ200006", reg.dlsiteSource, model.LinkKindExact)
	mkMirrorWork(t, "RJ200006", `{"id":226,"name":"女教師"}`)

	wMissing := mkWork(t, reg.galgameMedium, "genres-missing", nil)
	mkReleaseAnchor(t, wMissing, "RJ200007", reg.dlsiteSource, model.LinkKindExact)

	wClaimed := mkWork(t, reg.galgameMedium, "genres-claimed", &claimed)
	mkReleaseAnchor(t, wClaimed, "RJ200008", reg.dlsiteSource, model.LinkKindExact)
	mkMirrorWork(t, "RJ200008", `[{"id":226,"name":"女教師"}]`)

	wAsmr := mkWork(t, 5, "genres-asmr", nil)
	mkReleaseAnchor(t, wAsmr, "RJ200009", reg.dlsiteSource, model.LinkKindExact)
	mkMirrorWork(t, "RJ200009", `[{"id":226,"name":"女教師"}]`)

	wProbable := mkWork(t, reg.galgameMedium, "genres-probable", nil)
	mkReleaseAnchor(t, wProbable, "RJ200010", reg.dlsiteSource, model.LinkKindProbable)
	mkMirrorWork(t, "RJ200010", `[{"id":226,"name":"女教師"}]`)

	st, err := Run(ctx, runOpts(false))
	require.NoError(t, err)
	assert.Equal(t, 4, st.TaxonomyRows, "zh_CN rows only — the ja_JP 226 row is pinned out")
	assert.Equal(t, 8, st.Candidates, "claimed included now; asmr + probable still excluded in SQL")
	assert.Equal(t, 1, st.MissingMirror)
	assert.Equal(t, 2, st.NoGenres, "empty array + missing key")
	assert.Equal(t, 1, st.NotArray)
	assert.Equal(t, 5, st.ZhHit, "226 + 113 + 300 + 301 + the claimed work's 226")
	assert.Equal(t, 1, st.JaFallback, "retired 9999")
	assert.Equal(t, 2, st.NameBlank, "whitespace-only + missing embedded name")
	assert.Equal(t, 1, st.DupCollapsed, "301's 重复名 collapsed into 300's")
	assert.Equal(t, 5, st.Planned, "wFull 3 + wDup 1 + wClaimed 1")
	assert.Equal(t, 4, st.DistinctNames)
	assert.Zero(t, st.Written+st.Conflict+st.Errors)
	assert.EqualValues(t, 0, tagCount(t, ""), "dry run writes nothing")
	require.Len(t, st.Samples, 5)
	assert.Equal(t, Sample{WorkID: wFull, Workno: "RJ200001", GenreID: 226, Name: "女教师", FromTaxonomy: true},
		st.Samples[0], "zh_CN taxonomy name wins over the embedded ja name AND the ja_JP taxonomy row")
	assert.Equal(t, Sample{WorkID: wFull, Workno: "RJ200001", GenreID: 113, Name: "强X", FromTaxonomy: true},
		st.Samples[1], "outdated embedded レイプ auto-corrected to the CURRENT official name by id")
	assert.Equal(t, Sample{WorkID: wFull, Workno: "RJ200001", GenreID: 9999, Name: "退役ジャンル", FromTaxonomy: false},
		st.Samples[2], "retired id falls back to the embedded ja name")
	require.Len(t, st.FallbackSamples, 1)
	assert.Equal(t, 9999, st.FallbackSamples[0].GenreID)

	require.NoError(t, testDB.Create(&model.CatalogWorkTag{
		WorkID: wFull, Name: "百合", Count: 24, SourceID: 1,
	}).Error)

	st, err = Run(ctx, runOpts(true))
	require.NoError(t, err)
	assert.Equal(t, 5, st.Written)
	assert.Zero(t, st.Conflict+st.Errors)
	assert.EqualValues(t, 6, tagCount(t, ""), "5 dlsite rows + the pre-existing bgm row")

	var rows []model.CatalogWorkTag
	require.NoError(t, testDB.Where("work_id = ?", wFull).Order(`count DESC, name`).Find(&rows).Error)
	require.Len(t, rows, 4)
	assert.Equal(t, "百合", rows[0].Name)
	assert.Equal(t, 24, rows[0].Count)
	for i, want := range []string{"女教师", "强X", "退役ジャンル"} {
		assert.Equal(t, want, rows[i+1].Name)
		assert.Equal(t, 0, rows[i+1].Count, "dlsite genres carry no votes")
		assert.Equal(t, reg.dlsiteSource, rows[i+1].SourceID)
	}
	assert.EqualValues(t, 0, tagCount(t, "WHERE name = ?", "レイプ"),
		"the outdated embedded name never lands")
	assert.EqualValues(t, 1, tagCount(t, "WHERE work_id = ?", wDup), "重复名 deduped to one row")
	assert.EqualValues(t, 1, tagCount(t, "WHERE work_id = ?", wClaimed),
		"a CLAIMED work materialises now — the claim guard is gone")
	assert.EqualValues(t, 0, tagCount(t, "WHERE work_id IN (?, ?)", wAsmr, wProbable),
		"off-domain / probable works never materialise")

	st, err = Run(ctx, runOpts(true))
	require.NoError(t, err)
	assert.Zero(t, st.Written+st.Errors, "second pass writes zero")
	assert.Equal(t, 5, st.Conflict)
	assert.EqualValues(t, 6, tagCount(t, ""), "row count unchanged")

	require.NoError(t, testDB.Exec(
		`UPDATE dlsitegenres_dl.works
		SET product_json = jsonb_set(product_json, '{genres}',
			(product_json->'genres') || '[{"id":226,"name":"女教師"}]'::jsonb)
		WHERE workno = 'RJ200002'`).Error)
	st, err = Run(ctx, runOpts(true))
	require.NoError(t, err)
	assert.Equal(t, 1, st.Written, "only the newly-appeared genre lands")
	assert.Equal(t, 5, st.Conflict)
	assert.EqualValues(t, 2, tagCount(t, "WHERE work_id = ?", wDup))
}

func TestClaimPeerWritesAndDSNRequired(t *testing.T) {
	clean(t)
	ctx := context.Background()
	reg, err := resolveRegistry(ctx, testDB)
	require.NoError(t, err)

	claimed := "galgame_wiki"
	wClaimed := mkWork(t, reg.galgameMedium, "claimed-direct", &claimed)
	wBody := mkWork(t, reg.galgameMedium, "bodyless-direct", nil)

	w := &writer{db: testDB, stats: &Stats{}}
	w.write(ctx, plannedRow{WorkID: wClaimed, SourceID: reg.dlsiteSource, Name: "女教师"}, true)
	assert.Equal(t, 1, w.stats.Written)
	assert.EqualValues(t, 1, tagCount(t, ""))

	w.write(ctx, plannedRow{WorkID: wBody, SourceID: reg.dlsiteSource, Name: "女教师"}, true)
	assert.Equal(t, 2, w.stats.Written)
	w.write(ctx, plannedRow{WorkID: wBody, SourceID: reg.dlsiteSource, Name: "女教师"}, true)
	assert.Equal(t, 1, w.stats.Conflict, "ON CONFLICT refuses the duplicate")
	w.write(ctx, plannedRow{WorkID: wBody, SourceID: 1, Name: "女教师"}, true)
	assert.Equal(t, 3, w.stats.Written, "same work+name, different source → distinct row")
	assert.EqualValues(t, 3, tagCount(t, ""))

	_, err = Run(ctx, Opts{DlsiteDSN: dlTestDSN})
	require.Error(t, err)
	_, err = Run(ctx, Opts{DSN: testDSN})
	require.Error(t, err)
}

func TestBundleReleaseLendsNoGenres(t *testing.T) {
	clean(t)
	ctx := context.Background()
	reg, err := resolveRegistry(ctx, testDB)
	require.NoError(t, err)
	mkTaxonomy(t, 226, "zh_CN", "女教师")
	mkTaxonomy(t, 113, "zh_CN", "强X")

	bundleOnly := mkWork(t, reg.galgameMedium, "bundle-only", nil)
	mkVndbRelease(t, mkReleaseAnchor(t, bundleOnly, "RJ300001", reg.dlsiteSource, model.LinkKindExact), "r1", "v1", "v2")
	mkMirrorWork(t, "RJ300001", `[{"id":113,"name":"レイプ"}]`)

	both := mkWork(t, reg.galgameMedium, "bundle-and-single", nil)
	mkVndbRelease(t, mkReleaseAnchor(t, both, "RJ300002", reg.dlsiteSource, model.LinkKindExact), "r2", "v3", "v4")
	mkMirrorWork(t, "RJ300002", `[{"id":113,"name":"レイプ"}]`)
	mkVndbRelease(t, mkReleaseAnchor(t, both, "RJ300003", reg.dlsiteSource, model.LinkKindExact), "r3", "v3")
	mkMirrorWork(t, "RJ300003", `[{"id":226,"name":"女教師"}]`)

	st, err := Run(ctx, runOpts(true))
	require.NoError(t, err)
	assert.Equal(t, 1, st.SkippedBundle)
	assert.Equal(t, 1, st.Written)
	assert.EqualValues(t, 0, tagCount(t, "WHERE work_id = ?", bundleOnly))
	assert.EqualValues(t, 1, tagCount(t, "WHERE work_id = ? AND name = ?", both, "女教师"),
		"the single-VN product testifies even though the bundle's workno sorts first")
}

func TestNewGenreRowInheritsTheSexualVerdictOfItsName(t *testing.T) {
	clean(t)
	ctx := context.Background()
	reg, err := resolveRegistry(ctx, testDB)
	require.NoError(t, err)
	mkTaxonomy(t, 1, "zh_CN", "巨乳/爆乳")
	mkTaxonomy(t, 2, "zh_CN", "学校/学园")

	judged := mkWork(t, reg.galgameMedium, "judged", nil)
	require.NoError(t, testDB.Create(&model.CatalogWorkTag{
		WorkID: judged, Name: "巨乳/爆乳", SourceID: reg.dlsiteSource, Sexual: true,
	}).Error)
	fresh := mkWork(t, reg.galgameMedium, "fresh", nil)
	mkReleaseAnchor(t, fresh, "RJ500001", reg.dlsiteSource, model.LinkKindExact)
	mkMirrorWork(t, "RJ500001", `[{"id":1,"name":"巨乳/爆乳"},{"id":2,"name":"学校/学園"}]`)

	dry, err := Run(ctx, runOpts(false))
	require.NoError(t, err)
	assert.Zero(t, dry.SexualInherited, "a dry run writes nothing, flags included")

	st, err := Run(ctx, runOpts(true))
	require.NoError(t, err)
	assert.Equal(t, 2, st.Written)
	assert.Equal(t, 1, st.SexualInherited)
	assert.EqualValues(t, 1, tagCount(t, "WHERE work_id = ? AND name = ? AND sexual", fresh, "巨乳/爆乳"))
	assert.EqualValues(t, 1, tagCount(t, "WHERE work_id = ? AND name = ? AND NOT sexual", fresh, "学校/学园"))
}
