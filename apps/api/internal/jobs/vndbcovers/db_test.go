package vndbcovers

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
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
		fmt.Fprintln(os.Stderr, "SKIP: no TEST_DATABASE_DSN — DB-backed vndbcovers tests will skip individually")
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
	testDB = db
	os.Exit(m.Run())
}

func clean(t *testing.T) {
	t.Helper()
	if testDB == nil {
		dbtest.Skip(t)
	}
	require.NoError(t, testDB.Exec("TRUNCATE catalog_external_ref CASCADE").Error)
	for _, table := range []string{"catalog_work_cover", "catalog_work"} {
		require.NoError(t, testDB.Exec("TRUNCATE "+table+" RESTART IDENTITY CASCADE").Error)
	}
}

func sourceID(t *testing.T, key string) int16 {
	t.Helper()
	var id int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_source WHERE key = ?`, key).Scan(&id).Error)
	require.NotZero(t, id, "source %q not seeded", key)
	return id
}

func mkWork(t *testing.T, reg registry, name string) int64 {
	t.Helper()
	w := model.CatalogWork{MediumID: reg.galgameMedium, OLang: "ja", DisplayName: name}
	require.NoError(t, testDB.Create(&w).Error)
	return w.ID
}

func mkAnchor(t *testing.T, reg registry, workID int64, vndbID string, linkKind int16) {
	t.Helper()
	require.NoError(t, testDB.Exec(`
		INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by)
		VALUES (?,?,?,?,?,?)`,
		model.EntityTypeWork, workID, reg.vndbSource, vndbID, linkKind, "test").Error)
}

func mkCover(t *testing.T, workID int64, source int16, kind, hash string) {
	t.Helper()
	require.NoError(t, testDB.Create(&model.CatalogWorkCover{
		WorkID: workID, ImageHash: hash, Kind: kind, SourceID: source,
	}).Error)
}

func candidateIDs(t *testing.T, reg registry, ids []int64) []int64 {
	t.Helper()
	cands, err := loadCandidates(context.Background(), testDB, reg, ids)
	require.NoError(t, err)
	out := make([]int64, 0, len(cands))
	for _, c := range cands {
		out = append(out, c.WorkID)
	}
	return out
}

func TestLoadCandidatesOfficialGap(t *testing.T) {
	clean(t)
	reg, err := resolveRegistry(context.Background(), testDB)
	require.NoError(t, err)
	curated, upscale, bangumi := sourceID(t, "curated"), sourceID(t, "upscale"), sourceID(t, "bangumi")

	coverless := mkWork(t, reg, "coverless")
	mkAnchor(t, reg, coverless, "v1", model.LinkKindExact)

	curatedOnly := mkWork(t, reg, "curated-only")
	mkAnchor(t, reg, curatedOnly, "v2", model.LinkKindExact)
	mkCover(t, curatedOnly, curated, "main", "hash-cur")

	wikiLineage := mkWork(t, reg, "curated+upscale")
	mkAnchor(t, reg, wikiLineage, "v3", model.LinkKindExact)
	mkCover(t, wikiLineage, curated, "", "hash-cur2")
	mkCover(t, wikiLineage, upscale, "main", "hash-ups")

	pkgOnly := mkWork(t, reg, "official-pkg-only")
	mkAnchor(t, reg, pkgOnly, "v4", model.LinkKindExact)
	mkCover(t, pkgOnly, reg.vndbSource, "pkgfront", "hash-pkg")

	hasVNDB := mkWork(t, reg, "vndb-covered")
	mkAnchor(t, reg, hasVNDB, "v5", model.LinkKindExact)
	mkCover(t, hasVNDB, reg.vndbSource, "main", "hash-vndb")

	hasBangumi := mkWork(t, reg, "bangumi-covered")
	mkAnchor(t, reg, hasBangumi, "v6", model.LinkKindExact)
	mkCover(t, hasBangumi, bangumi, "main", "hash-bgm")
	mkCover(t, hasBangumi, curated, "main", "hash-cur3")

	probable := mkWork(t, reg, "probable-anchor")
	mkAnchor(t, reg, probable, "v7", model.LinkKindProbable)

	assert.ElementsMatch(t,
		[]int64{coverless, curatedOnly, wikiLineage, pkgOnly},
		candidateIDs(t, reg, nil))

	assert.ElementsMatch(t, []int64{curatedOnly},
		candidateIDs(t, reg, []int64{curatedOnly, hasVNDB}),
		"--ids must not bypass the official-cover gate")
}

type stubUploader struct {
	mu    sync.Mutex
	calls int
	limit int
	fixed string
}

func (s *stubUploader) UploadWithSub(_ context.Context, _ io.Reader, filename, _, _ string) (*imageclient.UploadResult, error) {
	s.mu.Lock()
	s.calls++
	n := s.calls
	s.mu.Unlock()
	if s.limit > 0 && n > s.limit {
		return nil, imageclient.ErrQuotaExceeded
	}
	if s.fixed != "" {
		return &imageclient.UploadResult{Hash: s.fixed}, nil
	}
	return &imageclient.UploadResult{Hash: fmt.Sprintf("stub-%03d-%s", n, filename)}, nil
}

func (s *stubUploader) ReferencePing(context.Context, []string) (*imageclient.ReferencePingResult, error) {
	return &imageclient.ReferencePingResult{}, nil
}

func (s *stubUploader) Health(context.Context) error { return nil }

func TestDrainPoolWritesRowsAndStopsOnQuota(t *testing.T) {
	clean(t)
	reg, err := resolveRegistry(context.Background(), testDB)
	require.NoError(t, err)

	var jpg bytes.Buffer
	require.NoError(t, jpeg.Encode(&jpg, image.NewRGBA(image.Rect(0, 0, 12, 16)), nil))
	mirror := t.TempDir()

	rows := make([]planRow, 0, 12)
	for i := 0; i < 12; i++ {
		id := mkWork(t, reg, fmt.Sprintf("pool-%02d", i))
		rel := filepath.Join("cv", fmt.Sprintf("%02d", i), fmt.Sprintf("%d.jpg", 1000+i))
		require.NoError(t, os.MkdirAll(filepath.Join(mirror, filepath.Dir(rel)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(mirror, rel), jpg.Bytes(), 0o644))
		rows = append(rows, planRow{
			WorkID: id,
			VNDBID: fmt.Sprintf("v%d", 1000+i),
			Img:    &vnImage{URL: "https://t.vndb.org/" + filepath.ToSlash(rel), Dims: []int{12, 16}},
		})
	}

	stats := &Stats{}
	r := &runner{db: testDB, cli: &stubUploader{limit: 6}, sourceID: reg.vndbSource,
		imageDir: mirror, stats: stats}
	r.drain(context.Background(), rows, 4)

	assert.Equal(t, 6, stats.Uploaded)
	assert.True(t, stats.Quota, "quota abort must be recorded")
	assert.Zero(t, stats.Errors)
	assert.GreaterOrEqual(t, stats.Local, 6, "every processed row must come from the mirror")

	var n int64
	require.NoError(t, testDB.Model(&model.CatalogWorkCover{}).Count(&n).Error)
	assert.EqualValues(t, 6, n)
	assert.Len(t, r.touched, 6)
}

func TestFillRelabelsTheByteIdenticalLegacyRow(t *testing.T) {
	clean(t)
	reg, err := resolveRegistry(context.Background(), testDB)
	require.NoError(t, err)
	curated := sourceID(t, "curated")

	var jpg bytes.Buffer
	require.NoError(t, jpeg.Encode(&jpg, image.NewRGBA(image.Rect(0, 0, 12, 16)), nil))
	mirror := t.TempDir()
	rel := filepath.Join("cv", "06", "12306.jpg")
	require.NoError(t, os.MkdirAll(filepath.Join(mirror, filepath.Dir(rel)), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(mirror, rel), jpg.Bytes(), 0o644))

	id := mkWork(t, reg, "wiki-clone")
	mkCover(t, id, curated, "", "official-hash")

	row := planRow{
		WorkID: id, VNDBID: "v12306",
		Img: &vnImage{URL: "https://t.vndb.org/cv/06/12306.jpg", Dims: []int{12, 16}, Sexual: 2, Violence: 0},
	}
	stats := &Stats{}
	r := &runner{db: testDB, cli: &stubUploader{fixed: "official-hash"}, sourceID: reg.vndbSource,
		imageDir: mirror, stats: stats}
	r.fill(context.Background(), row)

	var n int64
	require.NoError(t, testDB.Model(&model.CatalogWorkCover{}).Where("work_id = ?", id).Count(&n).Error)
	assert.EqualValues(t, 1, n, "relabel must not add a second row")
	var got model.CatalogWorkCover
	require.NoError(t, testDB.Where("work_id = ?", id).First(&got).Error)
	assert.Equal(t, reg.vndbSource, got.SourceID, "byte-identical legacy row is re-sourced to vndb")
	assert.Equal(t, "main", got.Kind, "legacy '' kind is normalized")
	assert.Equal(t, ratingLevel(2), got.Sexual)
	assert.True(t, got.PortraitPinned)
	assert.Equal(t, 1, stats.Uploaded)
	assert.Zero(t, stats.Dedup)
	assert.Equal(t, []int64{id}, r.touched, "relabeled work must reach TouchWorks")

	rerun := &runner{db: testDB, cli: &stubUploader{fixed: "official-hash"}, sourceID: reg.vndbSource,
		imageDir: mirror, stats: &Stats{}}
	rerun.fill(context.Background(), row)
	assert.Equal(t, 1, rerun.stats.Dedup, "an already-vndb row must count as dedup on re-run")
	assert.Zero(t, rerun.stats.Uploaded)
	assert.Empty(t, rerun.touched)
}
