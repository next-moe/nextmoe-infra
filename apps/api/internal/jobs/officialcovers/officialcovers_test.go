package officialcovers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	"api/internal/testsupport/dbtest"
	"api/pkg/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	testDB  *gorm.DB
	testDSN string
)

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		os.Exit(m.Run())
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err == nil {
		err = migrate.Run(db)
	}
	if err == nil {
		err = seed.Run(db)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: test database: %v\n", err)
		os.Exit(1)
	}
	testDB, testDSN = db, dsn
	os.Exit(m.Run())
}

func str(s string) *string { return &s }

func TestBestDigitalPrefersPortraitThenPixelsAndSkipsOtherVNs(t *testing.T) {
	rels := []release{
		{ID: "r1", VNs: []struct {
			ID string `json:"id"`
		}{{"v1"}, {"v2"}}, Images: []relImage{
			{Type: "pkgfront", URL: "box", Dims: []int{2000, 3000}},
			{Type: "dig", URL: "wide", Dims: []int{1920, 1080}},
			{Type: "dig", URL: "other-vn", Dims: []int{1500, 2100}, VN: str("v2")},
			{Type: "dig", URL: "small", Dims: []int{600, 850}},
		}},
		{ID: "r2", VNs: []struct {
			ID string `json:"id"`
		}{{"v1"}}, Images: []relImage{{Type: "dig", URL: "big", Dims: []int{1000, 1414}, VN: str("v1")}}},
		{ID: "r3", VNs: []struct {
			ID string `json:"id"`
		}{{"v3"}}, Images: []relImage{{Type: "dig", URL: "unrelated", Dims: []int{3000, 4000}}}},
	}
	got := bestDigital(rels, "v1")
	require.NotNil(t, got)
	assert.Equal(t, "big", got.URL, "a portrait digital cover with the most pixels, from a release of this VN only")
	assert.Nil(t, bestDigital(rels, "v9"))
}

func clean(t *testing.T) {
	t.Helper()
	if testDB == nil {
		dbtest.Skip(t)
	}
	require.NoError(t, testDB.Exec("TRUNCATE catalog_external_ref, catalog_work_cover, catalog_work RESTART IDENTITY CASCADE").Error)
}

func work(t *testing.T, vndb, bgm string) int64 {
	t.Helper()
	w := model.CatalogWork{MediumID: 1, OLang: "ja", DisplayName: "w" + vndb + bgm, Status: model.WorkStatusLive}
	require.NoError(t, testDB.Create(&w).Error)
	for src, id := range map[int16]string{model.SourceVNDB: vndb, model.SourceBangumi: bgm} {
		if id == "" {
			continue
		}
		require.NoError(t, testDB.Exec(`INSERT INTO catalog_external_ref
			(entity_type, entity_id, source_id, external_id, link_kind, matched_by, created_at)
			VALUES (?, ?, ?, ?, ?, 'test', now())`, model.EntityTypeWork, w.ID, src, id, model.LinkKindExact).Error)
	}
	return w.ID
}

func cover(t *testing.T, workID int64, source int16, kind, seed string, pinned bool) {
	t.Helper()
	require.NoError(t, testDB.Create(&model.CatalogWorkCover{WorkID: workID, ImageHash: strings.Repeat(seed, 64),
		Kind: kind, SourceID: source, PortraitPinned: pinned, FieldProvenance: datatypes.JSON(`{}`)}).Error)
}

func TestCandidatesAreWorksHoldingUploadedCovers(t *testing.T) {
	clean(t)
	both := work(t, "v1", "11")
	cover(t, both, model.SourceCurated, "", "a", false)
	hasDig := work(t, "v2", "")
	cover(t, hasDig, model.SourceUpscale, "", "b", false)
	cover(t, hasDig, model.SourceVNDB, "dig", "c", false)
	hasBgm := work(t, "", "13")
	cover(t, hasBgm, model.SourceCurated, "", "d", false)
	cover(t, hasBgm, model.SourceBangumi, "main", "e", false)
	officialOnly := work(t, "v4", "14")
	cover(t, officialOnly, model.SourceVNDB, "main", "f", false)
	noAnchor := work(t, "", "")
	cover(t, noAnchor, model.SourceCurated, "", "0", false)

	got, err := loadCandidates(context.Background(), testDB, nil)
	require.NoError(t, err)
	assert.Equal(t, []candidate{{WorkID: both, VNDBID: "v1", BgmID: "11"}}, got,
		"a work that already holds a dig or a bangumi cover needs nothing from that lane")
}

func TestApplyAddsTheCoversAndLiftsOnlyMachinePins(t *testing.T) {
	clean(t)
	w := work(t, "v7", "77")
	cover(t, w, model.SourceCurated, "", "1", false)
	cover(t, w, model.SourceVNDB, "main", "2", true)

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz", r.URL.Path == "/image/reference-ping":
			_, _ = rw.Write([]byte(`{"code":0}`))
		case r.URL.Path == "/release":
			_ = json.NewEncoder(rw).Encode(map[string]any{"more": false, "results": []any{map[string]any{
				"id": "r7", "vns": []any{map[string]any{"id": "v7"}},
				"images": []any{map[string]any{"type": "dig", "url": srv.URL + "/dig.jpg", "dims": []int{1000, 1414}, "sexual": 2.0}},
			}}})
		case r.URL.Path == "/v0/subjects/77":
			_ = json.NewEncoder(rw).Encode(map[string]any{"images": map[string]any{"large": srv.URL + "/bgm.jpg"}})
		case r.URL.Path == "/image/upload":
			require.NoError(t, r.ParseMultipartForm(1<<20))
			_, fh, err := r.FormFile("file")
			require.NoError(t, err)
			seed, w, h := "d", 1000, 1414
			if fh.Filename == "bgm.jpg" {
				seed, w, h = "9", 1280, 720
			}
			_ = json.NewEncoder(rw).Encode(map[string]any{"code": 0, "data": map[string]any{
				"hash": strings.Repeat(seed, 64), "width": w, "height": h}})
		default:
			_, _ = rw.Write([]byte("jpeg-bytes"))
		}
	}))
	defer srv.Close()

	cfg := &config.Config{}
	cfg.CatalogImageClient.ClientID, cfg.CatalogImageClient.ClientSecret = "id", "secret"
	stats, err := Run(context.Background(), cfg, Opts{DSN: testDSN, Apply: true,
		ImageBaseURL: srv.URL, VNDBBase: srv.URL, BgmBase: srv.URL})
	require.NoError(t, err)
	assert.Equal(t, 2, stats.Uploaded, stats.String())
	assert.Equal(t, 1, stats.Unpinned, "only the portrait upload lifts the importer's pin")

	var rows []model.CatalogWorkCover
	require.NoError(t, testDB.Where("work_id = ?", w).Order("image_hash").Find(&rows).Error)
	byHash := map[string]model.CatalogWorkCover{}
	for _, r := range rows {
		byHash[r.ImageHash[:1]] = r
	}
	require.Len(t, rows, 4)
	assert.Equal(t, "dig", byHash["d"].Kind)
	assert.Equal(t, model.SourceVNDB, byHash["d"].SourceID)
	assert.Equal(t, model.SexualExplicit, byHash["d"].Sexual)
	assert.Equal(t, model.SourceBangumi, byHash["9"].SourceID)
	assert.False(t, byHash["2"].PortraitPinned, "the importer's pin on the thumbnail is lifted")

	again, err := Run(context.Background(), cfg, Opts{DSN: testDSN, Apply: true,
		ImageBaseURL: srv.URL, VNDBBase: srv.URL, BgmBase: srv.URL})
	require.NoError(t, err)
	assert.Zero(t, again.Candidates, "a rerun finds both lanes filled")
}
