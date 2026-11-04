package vndbshots

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	srcv "api/internal/platform/catalog/srcvndb"
	"api/internal/testsupport/dbtest"
	"api/pkg/imageclient"

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
		fmt.Fprintln(os.Stderr, "SKIP: no TEST_DATABASE_DSN — DB-backed vndbshots tests will skip individually")
		os.Exit(m.Run())
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
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
	if err := srcv.EnsureSchema(db); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: src_vndb schema failed: %v\n", err)
		os.Exit(1)
	}
	testDB = db
	os.Exit(m.Run())
}

func setup(t *testing.T) registry {
	t.Helper()
	if testDB == nil {
		dbtest.Skip(t)
	}
	require.NoError(t, testDB.Exec("TRUNCATE catalog_external_ref CASCADE").Error)
	for _, table := range []string{"catalog_work_screenshot", "catalog_work", "src_vndb.vn_screenshots", "src_vndb.images"} {
		require.NoError(t, testDB.Exec("TRUNCATE "+table+" CASCADE").Error)
	}
	reg, err := resolveRegistry(context.Background(), testDB)
	require.NoError(t, err)
	return reg
}

func mkWork(t *testing.T, reg registry, name string, status int16) int64 {
	t.Helper()
	w := model.CatalogWork{MediumID: reg.galgameMedium, OLang: "ja", DisplayName: name, Status: status}
	require.NoError(t, testDB.Create(&w).Error)
	return w.ID
}

func mkAnchor(t *testing.T, reg registry, workID int64, vid string, linkKind int16, dead bool) {
	t.Helper()
	require.NoError(t, testDB.Exec(`
		INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by, dead_at)
		VALUES (?,?,?,?,?,'test', CASE WHEN ? THEN now() END)`,
		model.EntityTypeWork, workID, reg.vndbSource, vid, linkKind, dead).Error)
}

func mkShot(t *testing.T, vid, scr string, votes, sexual, violence int) {
	t.Helper()
	require.NoError(t, testDB.Create(&srcv.VNScreenshot{ID: vid, Scr: scr}).Error)
	require.NoError(t, testDB.Create(&srcv.Image{ID: scr, Width: 1280, Height: 720,
		VoteCount: votes, SexualAvg: int16(sexual), ViolenceAvg: int16(violence)}).Error)
}

func candidateIDs(t *testing.T, reg registry) []int64 {
	t.Helper()
	cands, err := loadCandidates(context.Background(), testDB, reg, nil)
	require.NoError(t, err)
	var out []int64
	for _, c := range cands {
		out = append(out, c.WorkID)
	}
	return out
}

func TestCandidatesAreLiveAnchoredWorksWithoutAnyScreenshotNewestFirst(t *testing.T) {
	reg := setup(t)
	older := mkWork(t, reg, "older", model.WorkStatusLive)
	mkAnchor(t, reg, older, "v1", model.LinkKindExact, false)
	newer := mkWork(t, reg, "newer", model.WorkStatusLive)
	mkAnchor(t, reg, newer, "v2", model.LinkKindExact, false)

	dlsiteShot := mkWork(t, reg, "has a dlsite screenshot", model.WorkStatusLive)
	mkAnchor(t, reg, dlsiteShot, "v3", model.LinkKindExact, false)
	var dlsite int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_source WHERE key = 'dlsite'`).Scan(&dlsite).Error)
	require.NoError(t, testDB.Create(&model.CatalogWorkScreenshot{WorkID: dlsiteShot, ImageHash: "h", SourceID: dlsite}).Error)

	probable := mkWork(t, reg, "probable anchor", model.WorkStatusLive)
	mkAnchor(t, reg, probable, "v4", model.LinkKindProbable, false)
	dead := mkWork(t, reg, "dead anchor", model.WorkStatusLive)
	mkAnchor(t, reg, dead, "v5", model.LinkKindExact, true)
	merged := mkWork(t, reg, "merged", model.WorkStatusMerged)
	mkAnchor(t, reg, merged, "v6", model.LinkKindExact, false)

	assert.Equal(t, []int64{newer, older}, candidateIDs(t, reg))
}

func TestPlanCapsOrdersAndGradesEachWorksScreenshots(t *testing.T) {
	reg := setup(t)
	a := mkWork(t, reg, "a", model.WorkStatusLive)
	mkAnchor(t, reg, a, "v1", model.LinkKindExact, false)
	for _, scr := range []string{"sf10", "sf9", "sf11", "sf100"} {
		mkShot(t, "v1", scr, 3, 0, 0)
	}
	b := mkWork(t, reg, "b", model.WorkStatusLive)
	mkAnchor(t, reg, b, "v2", model.LinkKindExact, false)
	mkShot(t, "v2", "sf20", 2, 150, 50)
	mkShot(t, "v2", "sf21", 0, 0, 0)
	c := mkWork(t, reg, "c", model.WorkStatusLive)
	mkAnchor(t, reg, c, "v3", model.LinkKindExact, false)

	cands, err := loadCandidates(context.Background(), testDB, reg, nil)
	require.NoError(t, err)
	stats := &Stats{}
	plan, err := buildPlan(context.Background(), testDB, cands, 3, 0, stats)
	require.NoError(t, err)

	require.Len(t, plan, 2)
	assert.Equal(t, b, plan[0].WorkID)
	assert.Equal(t, []shot{{Scr: "sf20", Rel: "sf/20/20.jpg", Sexual: 2, Violence: 1}}, plan[0].Shots)
	assert.Equal(t, a, plan[1].WorkID)
	var scrs []string
	for _, s := range plan[1].Shots {
		scrs = append(scrs, s.Scr)
	}
	assert.Equal(t, []string{"sf9", "sf10", "sf11"}, scrs, "VNDB's numeric order, capped per work")
	assert.Equal(t, 1, stats.Unrated)
	assert.Equal(t, 1, stats.NoShots)
	assert.Equal(t, 4, stats.Planned)

	limited := &Stats{}
	plan, err = buildPlan(context.Background(), testDB, cands, 3, 1, limited)
	require.NoError(t, err)
	require.Len(t, plan, 1, "--limit counts works that have something to upload")
	assert.Equal(t, b, plan[0].WorkID)
}

func TestFilesOutListsOnlyWhatTheMirrorLacks(t *testing.T) {
	dir := t.TempDir()
	writeJPEG(t, dir, "sf/09/9.jpg")
	plan := []workPlan{{Shots: []shot{{Rel: "sf/09/9.jpg"}, {Rel: "sf/10/10.jpg"}}}, {Shots: []shot{{Rel: "sf/10/10.jpg"}}}}
	out := filepath.Join(t.TempDir(), "files")
	n, err := writeFilesOut(out, dir, plan)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	got, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Equal(t, "sf/10/10.jpg\n", string(got))
}

type stubUploader struct {
	mu    sync.Mutex
	calls int
	fail  map[string]error
}

func (s *stubUploader) UploadWithSub(_ context.Context, r io.Reader, filename, _, _ string) (*imageclient.UploadResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if err := s.fail[filename]; err != nil {
		return nil, err
	}
	return &imageclient.UploadResult{Hash: "hash-" + filename}, nil
}

func (s *stubUploader) ReferencePing(context.Context, []string) (*imageclient.ReferencePingResult, error) {
	return &imageclient.ReferencePingResult{}, nil
}

func (s *stubUploader) Health(context.Context) error { return nil }

func writeJPEG(t *testing.T, dir, rel string) {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 16, 9)), nil))
	p := filepath.Join(dir, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, buf.Bytes(), 0o644))
}

func TestApplyWritesWholeWorksOnly(t *testing.T) {
	reg := setup(t)
	dir := t.TempDir()
	whole := mkWork(t, reg, "whole", model.WorkStatusLive)
	partial := mkWork(t, reg, "partial mirror", model.WorkStatusLive)
	flaky := mkWork(t, reg, "flaky upload", model.WorkStatusLive)
	writeJPEG(t, dir, "sf/01/1.jpg")
	writeJPEG(t, dir, "sf/02/2.jpg")
	writeJPEG(t, dir, "sf/03/3.jpg")
	writeJPEG(t, dir, "sf/05/5.jpg")
	plan := []workPlan{
		{WorkID: whole, Shots: []shot{{Scr: "sf1", Rel: "sf/01/1.jpg", Sexual: 2}, {Scr: "sf2", Rel: "sf/02/2.jpg", Violence: 1}}},
		{WorkID: partial, Shots: []shot{{Scr: "sf3", Rel: "sf/03/3.jpg"}, {Scr: "sf4", Rel: "sf/04/4.jpg"}}},
		{WorkID: flaky, Shots: []shot{{Scr: "sf5", Rel: "sf/05/5.jpg"}}},
	}
	defaultWait := retryWait
	retryWait = func(int) time.Duration { return 0 }
	t.Cleanup(func() { retryWait = defaultWait })
	cli := &stubUploader{fail: map[string]error{"5.jpg": errors.New("connection reset")}}
	stats := &Stats{}
	r := &runner{db: testDB, cli: cli, sourceID: reg.vndbSource, imageDir: dir, stats: stats}
	require.NoError(t, r.apply(context.Background(), plan, 2))

	var rows []model.CatalogWorkScreenshot
	require.NoError(t, testDB.Order("work_id, sort_order").Find(&rows).Error)
	require.Len(t, rows, 2)
	for i, row := range rows {
		assert.Equal(t, whole, row.WorkID)
		assert.Equal(t, i, row.SortOrder)
		assert.Equal(t, reg.vndbSource, row.SourceID)
	}
	assert.EqualValues(t, 2, rows[0].Sexual)
	assert.EqualValues(t, 1, rows[1].Violence)
	assert.Equal(t, 2, stats.Uploaded)
	assert.Equal(t, 1, stats.Incomplete, "a work the mirror holds only part of waits for next week")
	assert.Equal(t, 1, stats.Errors, "a failed upload leaves its work a candidate")
	assert.ElementsMatch(t, []int64{whole}, r.touched)

	assert.NotContains(t, candidateIDsFor(t, reg, whole, partial, flaky), whole)
}

func TestQuotaStopsTheRun(t *testing.T) {
	reg := setup(t)
	dir := t.TempDir()
	writeJPEG(t, dir, "sf/01/1.jpg")
	var plan []workPlan
	for i := range 5 {
		plan = append(plan, workPlan{WorkID: mkWork(t, reg, fmt.Sprint(i), model.WorkStatusLive),
			Shots: []shot{{Scr: "sf1", Rel: "sf/01/1.jpg"}}})
	}
	cli := &stubUploader{fail: map[string]error{"1.jpg": imageclient.ErrQuotaExceeded}}
	stats := &Stats{}
	r := &runner{db: testDB, cli: cli, sourceID: reg.vndbSource, imageDir: dir, stats: stats}
	require.NoError(t, r.apply(context.Background(), plan, 1))
	assert.True(t, stats.Quota)
	assert.Equal(t, 1, cli.calls, "nothing is uploaded after the quota answer")
}

func candidateIDsFor(t *testing.T, reg registry, ids ...int64) []int64 {
	t.Helper()
	for _, id := range ids {
		mkAnchor(t, reg, id, fmt.Sprintf("v%d", id), model.LinkKindExact, false)
	}
	return candidateIDs(t, reg)
}
