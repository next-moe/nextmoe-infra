package aicovers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	gormlogger "gorm.io/gorm/logger"
)

var testDB *gorm.DB

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		fmt.Fprintln(os.Stderr, "SKIP: no TEST_DATABASE_DSN — DB-backed aicovers tests will skip individually")
		os.Exit(m.Run())
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
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

func reset(t *testing.T) {
	t.Helper()
	if testDB == nil {
		dbtest.Skip(t)
	}
	require.NoError(t, testDB.Exec(`TRUNCATE catalog_work_cover, catalog_work RESTART IDENTITY CASCADE`).Error)
}

func mkWork(t *testing.T, status int16) int64 {
	t.Helper()
	w := model.CatalogWork{MediumID: 1, OLang: "ja", DisplayName: "w", Status: status}
	require.NoError(t, testDB.Create(&w).Error)
	return w.ID
}

func mkCover(t *testing.T, workID int64, hash string, source int16, pinned bool, provenance string) {
	t.Helper()
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_work_cover
		(work_id, image_hash, kind, sexual, source_id, portrait_pinned, field_provenance, created_at, updated_at)
		VALUES (?, ?, 'main', 1, ?, ?, ?::jsonb, now(), now())`, workID, hash, source, pinned, provenance).Error)
}

func fakeImageService(t *testing.T) *imageclient.Client {
	t.Helper()
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/image/upload" {
			n++
			_, _ = fmt.Fprintf(w, `{"code":0,"data":{"hash":"ai-%d","width":832,"height":1248}}`, n)
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{}}`))
	}))
	t.Cleanup(srv.Close)
	return imageclient.New(imageclient.Config{BaseURL: srv.URL, ClientID: "x", ClientSecret: "y"})
}

func writeProduct(t *testing.T, dir string, workID int64, origin string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("%d__%s.jpg", workID, origin)), []byte("jpeg"), 0o644))
}

func TestImportPinsTheRedrawAndSkipsWhatItMust(t *testing.T) {
	reset(t)
	ctx := context.Background()
	dir := t.TempDir()

	plain := mkWork(t, model.WorkStatusLive)
	mkCover(t, plain, "origin-plain", model.SourceVNDB, true, `{}`)
	writeProduct(t, dir, plain, "origin-plain")

	edited := mkWork(t, model.WorkStatusLive)
	mkCover(t, edited, "origin-edited", model.SourceVNDB, false, `{}`)
	mkCover(t, edited, "editor-choice", model.SourceCurated, true, `{}`)
	writeProduct(t, dir, edited, "origin-edited")

	done := mkWork(t, model.WorkStatusLive)
	mkCover(t, done, "origin-done", model.SourceVNDB, false, `{}`)
	mkCover(t, done, "earlier-redraw", model.SourceAIRedraw, true, `{}`)
	writeProduct(t, dir, done, "origin-done")

	merged := mkWork(t, model.WorkStatusMerged)
	mkCover(t, merged, "origin-merged", model.SourceVNDB, false, `{}`)
	writeProduct(t, dir, merged, "origin-merged")

	writeProduct(t, dir, plain+1000, "origin-missing")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644))

	r := &runner{db: testDB, cli: fakeImageService(t), stats: &Stats{}}
	products, bad, err := listProducts(dir)
	require.NoError(t, err)
	r.stats.Files, r.stats.BadName = len(products), bad
	for _, p := range products {
		r.handle(ctx, p, true)
	}

	assert.Equal(t, Stats{Files: 5, BadName: 1, Imported: 1, NoOrigin: 1, WorkGone: 1, Candidates: 2, Uploaded: 2, Inserted: 2, Pinned: 1, EditorPin: 1}, *r.stats)

	type row struct {
		ImageHash      string
		SourceID       int16
		Sexual         int16
		PortraitPinned bool
	}
	var got []row
	require.NoError(t, testDB.Raw(`SELECT image_hash, source_id, sexual, portrait_pinned FROM catalog_work_cover
		WHERE work_id = ? ORDER BY sort_order, image_hash`, plain).Scan(&got).Error)
	assert.Equal(t, []row{{"origin-plain", model.SourceVNDB, 1, false}, {"ai-1", model.SourceAIRedraw, 1, true}}, got,
		"the redraw takes the origin's grade and the machine pin")

	var editorPinned bool
	require.NoError(t, testDB.Raw(`SELECT portrait_pinned FROM catalog_work_cover WHERE work_id = ? AND image_hash = 'editor-choice'`,
		edited).Scan(&editorPinned).Error)
	assert.True(t, editorPinned, "an editor's pin is never lifted")
}
