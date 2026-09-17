package bangumicovers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	"api/internal/platform/catalog/service"
	"api/internal/testsupport/dbtest"
	"api/pkg/config"
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
		fmt.Fprintln(os.Stderr, "SKIP: no TEST_DATABASE_DSN — DB-backed bangumicovers tests will skip individually")
		os.Exit(m.Run())
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("jobs/bangumicovers", "cannot connect to test database: %v", err)
	}
	if err := migrate.Run(db); err != nil {
		dbtest.SkipMainf("jobs/bangumicovers", "catalog migrate failed: %v", err)
	}
	if err := seed.Run(db); err != nil {
		dbtest.SkipMainf("jobs/bangumicovers", "catalog seed failed: %v", err)
	}
	testDB = db
	os.Exit(m.Run())
}

type fakeUploader struct {
	fail    error
	uploads int
	pinged  []string
}

func (f *fakeUploader) UploadWithSub(_ context.Context, r io.Reader, _, _, _ string) (*imageclient.UploadResult, error) {
	if f.fail != nil {
		return nil, f.fail
	}
	b, _ := io.ReadAll(r)
	sum := sha256.Sum256(b)
	f.uploads++
	return &imageclient.UploadResult{Hash: hex.EncodeToString(sum[:])}, nil
}

func (f *fakeUploader) ReferencePing(_ context.Context, hashes []string) (*imageclient.ReferencePingResult, error) {
	f.pinged = append(f.pinged, hashes...)
	return &imageclient.ReferencePingResult{Updated: int64(len(hashes))}, nil
}

func (f *fakeUploader) Health(context.Context) error { return nil }

func truncate(t *testing.T) {
	t.Helper()
	if testDB == nil {
		dbtest.Skip(t)
	}
	for _, tbl := range []string{"catalog_work_cover", "catalog_external_ref", "catalog_work"} {
		require.NoError(t, testDB.Exec("TRUNCATE "+tbl+" RESTART IDENTITY CASCADE").Error)
	}
}

func mkWork(t *testing.T, medium int16, name string, site *string) int64 {
	t.Helper()
	w := model.CatalogWork{MediumID: medium, OLang: "ja", DisplayName: name, Site: site}
	if site != nil {
		pid := int64(700000 + len(name))
		w.ProductWorkID = &pid
	}
	require.NoError(t, testDB.Create(&w).Error)
	return w.ID
}

func mkRef(t *testing.T, workID int64, source int16, subject string, kind int16, matchedBy string) {
	t.Helper()
	require.NoError(t, testDB.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeWork, EntityID: workID, SourceID: source,
		ExternalID: subject, LinkKind: kind, MatchedBy: matchedBy,
	}).Error)
}

func coversOf(t *testing.T, workID int64) []model.CatalogWorkCover {
	t.Helper()
	var rows []model.CatalogWorkCover
	require.NoError(t, testDB.Where("work_id = ?", workID).Order("portrait_pinned").Find(&rows).Error)
	return rows
}

func writeMirror(t *testing.T, entries []dimsEntry, withFile map[string]bool) string {
	t.Helper()
	root := t.TempDir()
	var manifest string
	for _, e := range entries {
		manifest += fmt.Sprintf(`{"subject_id":%d,"w":%d,"h":%d,"file":%q}`+"\n", e.SubjectID, e.W, e.H, e.File)
		sid := fmt.Sprintf("%d", e.SubjectID)
		if withFile[sid] {
			dir := filepath.Join(root, sid)
			require.NoError(t, os.MkdirAll(dir, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "cover.jpg"), []byte("bytes-of-"+sid), 0o644))
		}
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, dimsFileName), []byte(manifest), 0o644))
	return root
}

func resolveTestRegistry(t *testing.T) registry {
	t.Helper()
	reg, err := resolveRegistry(context.Background(), testDB)
	require.NoError(t, err)
	require.EqualValues(t, 3, reg.bangumiSource, "bangumi source id must resolve to 3")
	return reg
}

func TestLoadCandidates(t *testing.T) {
	truncate(t)
	reg := resolveTestRegistry(t)
	claimed := "galgame_wiki"

	want := mkWork(t, reg.galgameMedium, "bodyless-exact", nil)
	mkRef(t, want, reg.bangumiSource, "111", model.LinkKindExact, ruleBgmTitleYear)

	wantType4 := mkWork(t, reg.galgameMedium, "bodyless-type4", nil)
	mkRef(t, wantType4, reg.bangumiSource, "666", model.LinkKindExact, ruleBgmType4Gated)

	wProbable := mkWork(t, reg.galgameMedium, "bodyless-probable", nil)
	mkRef(t, wProbable, reg.bangumiSource, "222", model.LinkKindProbable, "rule:bgm-title-only")

	wClaimed := mkWork(t, reg.galgameMedium, "claimed-exact", &claimed)
	mkRef(t, wClaimed, reg.bangumiSource, "333", model.LinkKindExact, ruleBgmTitleYear)

	wManga := mkWork(t, 2, "manga-exact", nil)
	mkRef(t, wManga, reg.bangumiSource, "444", model.LinkKindExact, ruleBgmType4Gated)

	wOtherRule := mkWork(t, reg.galgameMedium, "bodyless-otherrule", nil)
	mkRef(t, wOtherRule, reg.bangumiSource, "555", model.LinkKindExact, "rule:title-year-strict")

	cands, err := loadCandidates(context.Background(), testDB, reg, 0, 0)
	require.NoError(t, err)
	require.Len(t, cands, 2, "only bodyless galgame exact anchors of the two trusted rules are candidates")
	byWork := map[int64]string{}
	for _, c := range cands {
		byWork[c.WorkID] = c.SubjectID
	}
	assert.Equal(t, "111", byWork[want])
	assert.Equal(t, "666", byWork[wantType4])
}

func TestWritePath(t *testing.T) {
	truncate(t)
	reg := resolveTestRegistry(t)
	claimed := "galgame_wiki"

	wPortrait := mkWork(t, reg.galgameMedium, "portrait", nil)
	wLandscape := mkWork(t, reg.galgameMedium, "landscape", nil)
	wNoDims := mkWork(t, reg.galgameMedium, "nodims", nil)
	wMissing := mkWork(t, reg.galgameMedium, "missing", nil)

	dimsRows := []dimsEntry{
		{SubjectID: 9001, W: 800, H: 1200, File: "9001/cover.jpg"},
		{SubjectID: 9002, W: 1200, H: 800, File: "9002/cover.jpg"},
		{SubjectID: 9004, W: 800, H: 1200, File: "9004/cover.jpg"},
		{SubjectID: 9006, W: 800, H: 1200, File: "9006/cover.jpg"},
	}
	mirror := writeMirror(t, dimsRows, map[string]bool{"9001": true, "9002": true, "9004": true})
	d, err := loadDims(mirror)
	require.NoError(t, err)

	cands := []candidate{
		{WorkID: wPortrait, SubjectID: "9001", Site: nil},
		{WorkID: wLandscape, SubjectID: "9002", Site: nil},
		{WorkID: 424242, SubjectID: "9004", Site: &claimed},
		{WorkID: wNoDims, SubjectID: "9005", Site: nil},
		{WorkID: wMissing, SubjectID: "9006", Site: nil},
	}
	ctx := context.Background()
	opts := Opts{Apply: true, BangumiMirror: mirror}

	fake := &fakeUploader{}
	exist, err := preloadExistingCovers(ctx, testDB, []int64{wPortrait, wLandscape, wNoDims, wMissing}, reg.bangumiSource)
	require.NoError(t, err)
	r := &runner{db: testDB, cli: fake, sourceID: reg.bangumiSource, exist: exist}
	require.False(t, r.process(ctx, opts, cands, d))

	assert.Equal(t, 1, r.c.coverUploaded, "only the portrait is written")
	assert.Equal(t, 1, r.c.coverLandscape, "landscape skipped (DLsite supplies it)")
	assert.Equal(t, 1, r.c.coverNoDims, "subject absent from dims skipped")
	assert.Equal(t, 1, r.c.coverMissing, "portrait with no mirror file skipped")
	assert.Equal(t, 1, r.c.coverRefused, "claimed work refused by XOR guard")
	assert.Equal(t, 1, fake.uploads, "exactly one upload")
	require.Len(t, r.pingHashes, 1, "the uploaded hash is queued for refping")

	rows := coversOf(t, wPortrait)
	require.Len(t, rows, 1)
	assert.True(t, rows[0].PortraitPinned, "portrait_pinned MUST be true — the whole point")
	assert.EqualValues(t, reg.bangumiSource, rows[0].SourceID, "source_id = bangumi")
	assert.Equal(t, "main", rows[0].Kind)
	assert.EqualValues(t, 0, rows[0].SortOrder)
	assert.EqualValues(t, 0, rows[0].Sexual, "Bangumi covers default SFW (sexual 0)")
	assert.EqualValues(t, 0, rows[0].Violence)
	assert.Empty(t, coversOf(t, 424242))

	fake2 := &fakeUploader{}
	exist2, err := preloadExistingCovers(ctx, testDB, []int64{wPortrait}, reg.bangumiSource)
	require.NoError(t, err)
	r2 := &runner{db: testDB, cli: fake2, sourceID: reg.bangumiSource, exist: exist2}
	require.False(t, r2.process(ctx, opts, cands[:1], d))
	assert.Equal(t, 0, r2.c.coverUploaded)
	assert.Equal(t, 1, r2.c.coverExists, "preloaded Bangumi cover → skip before upload")
	assert.Equal(t, 0, fake2.uploads, "no upload on the idempotent second pass")

	fake3 := &fakeUploader{}
	r3 := &runner{db: testDB, cli: fake3, sourceID: reg.bangumiSource, exist: map[int64]bool{}}
	require.False(t, r3.writeCover(ctx, mirror, cands[0], d.entry["9001"], true))
	assert.Equal(t, 0, r3.c.coverUploaded)
	assert.Equal(t, 1, r3.c.coverDedup, "ON CONFLICT refuses the duplicate under a stale preload")
	require.Len(t, coversOf(t, wPortrait), 1, "still exactly one cover row")
}

func TestAllowLandscape(t *testing.T) {
	truncate(t)
	reg := resolveTestRegistry(t)

	wLandscape := mkWork(t, reg.galgameMedium, "landscape-ok", nil)
	wPortrait := mkWork(t, reg.galgameMedium, "portrait-ok", nil)

	mirror := writeMirror(t, []dimsEntry{
		{SubjectID: 9401, W: 1200, H: 800, File: "9401/cover.jpg"},
		{SubjectID: 9402, W: 800, H: 1200, File: "9402/cover.jpg"},
	}, map[string]bool{"9401": true, "9402": true})
	d, err := loadDims(mirror)
	require.NoError(t, err)

	ctx := context.Background()
	r := &runner{db: testDB, cli: &fakeUploader{}, sourceID: reg.bangumiSource, exist: map[int64]bool{}}
	require.False(t, r.process(ctx, Opts{Apply: true, BangumiMirror: mirror, AllowLandscape: true}, []candidate{
		{WorkID: wLandscape, SubjectID: "9401", Site: nil},
		{WorkID: wPortrait, SubjectID: "9402", Site: nil},
	}, d))

	assert.Equal(t, 2, r.c.coverUploaded)
	assert.Equal(t, 1, r.c.coverLandscapeOK)
	assert.Equal(t, 0, r.c.coverLandscape)

	landRows := coversOf(t, wLandscape)
	require.Len(t, landRows, 1)
	assert.False(t, landRows[0].PortraitPinned, "a landscape cover must never take the portrait pin")

	portRows := coversOf(t, wPortrait)
	require.Len(t, portRows, 1)
	assert.True(t, portRows[0].PortraitPinned, "the portrait in the same run still pins")
}

func TestTwoCoversReadFace(t *testing.T) {
	truncate(t)
	reg := resolveTestRegistry(t)
	var dlsiteSource int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_source WHERE key = 'dlsite'`).Scan(&dlsiteSource).Error)
	require.NotZero(t, dlsiteSource)

	work := mkWork(t, reg.galgameMedium, "two-covers", nil)
	mkRef(t, work, reg.bangumiSource, "9100", model.LinkKindExact, ruleBgmTitleYear)

	require.NoError(t, testDB.Create(&model.CatalogWorkCover{
		WorkID: work, ImageHash: sha256hex("dlsite-landscape-9100"), SortOrder: 0, Kind: "main",
		PortraitPinned: false, SourceID: dlsiteSource,
	}).Error)

	dimsRows := []dimsEntry{{SubjectID: 9100, W: 900, H: 1350, File: "9100/cover.jpg"}}
	mirror := writeMirror(t, dimsRows, map[string]bool{"9100": true})
	d, err := loadDims(mirror)
	require.NoError(t, err)

	ctx := context.Background()
	exist, err := preloadExistingCovers(ctx, testDB, []int64{work}, reg.bangumiSource)
	require.NoError(t, err)
	r := &runner{db: testDB, cli: &fakeUploader{}, sourceID: reg.bangumiSource, exist: exist}
	require.False(t, r.process(ctx, Opts{Apply: true, BangumiMirror: mirror},
		[]candidate{{WorkID: work, SubjectID: "9100", Site: nil}}, d))
	require.Equal(t, 1, r.c.coverUploaded)

	detail, err := service.NewReadService(testDB).WorkByID(ctx, work, 0)
	require.NoError(t, err)
	require.Len(t, detail.Covers, 2, "read face must carry both the landscape and the portrait")

	var landscape, portrait *service.WorkCoverRow
	for i := range detail.Covers {
		if detail.Covers[i].PortraitPinned {
			portrait = &detail.Covers[i]
		} else {
			landscape = &detail.Covers[i]
		}
	}
	require.NotNil(t, landscape, "the step-55 DLsite landscape cover is present")
	require.NotNil(t, portrait, "the Bangumi portrait cover is present")
	assert.EqualValues(t, dlsiteSource, landscape.SourceID, "landscape attributed to DLsite")
	assert.EqualValues(t, reg.bangumiSource, portrait.SourceID, "portrait attributed to Bangumi")
}

func TestWriteCoverDoesNotStealExistingPin(t *testing.T) {
	truncate(t)
	reg := resolveTestRegistry(t)
	var curated int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_source WHERE key = 'curated'`).Scan(&curated).Error)
	require.NotZero(t, curated)

	alreadyPinned := mkWork(t, reg.galgameMedium, "already-pinned", nil)
	fresh := mkWork(t, reg.galgameMedium, "no-pin", nil)
	require.NoError(t, testDB.Create(&model.CatalogWorkCover{
		WorkID: alreadyPinned, ImageHash: sha256hex("human-pin"), SortOrder: 0, Kind: "main",
		PortraitPinned: true, SourceID: curated,
	}).Error)

	mirror := writeMirror(t, []dimsEntry{
		{SubjectID: 9301, W: 800, H: 1200, File: "9301/cover.jpg"},
		{SubjectID: 9302, W: 800, H: 1200, File: "9302/cover.jpg"},
	}, map[string]bool{"9301": true, "9302": true})
	d, err := loadDims(mirror)
	require.NoError(t, err)

	ctx := context.Background()
	workIDs := []int64{alreadyPinned, fresh}
	exist, err := preloadExistingCovers(ctx, testDB, workIDs, reg.bangumiSource)
	require.NoError(t, err)
	pinned, err := preloadPinnedCovers(ctx, testDB, workIDs)
	require.NoError(t, err)
	assert.True(t, pinned[alreadyPinned])
	assert.False(t, pinned[fresh])

	r := &runner{db: testDB, cli: &fakeUploader{}, sourceID: reg.bangumiSource, exist: exist, pinned: pinned}
	require.False(t, r.process(ctx, Opts{Apply: true, BangumiMirror: mirror}, []candidate{
		{WorkID: alreadyPinned, SubjectID: "9301", Site: nil},
		{WorkID: fresh, SubjectID: "9302", Site: nil},
	}, d))
	assert.Equal(t, 2, r.c.coverUploaded)

	held := coversOf(t, alreadyPinned)
	require.Len(t, held, 2)
	var humanPin, bangumiOnHeld *model.CatalogWorkCover
	for i := range held {
		switch held[i].SourceID {
		case curated:
			humanPin = &held[i]
		case reg.bangumiSource:
			bangumiOnHeld = &held[i]
		}
	}
	require.NotNil(t, humanPin)
	require.NotNil(t, bangumiOnHeld)
	assert.True(t, humanPin.PortraitPinned, "the human pin stays")
	assert.False(t, bangumiOnHeld.PortraitPinned, "bangumi lands unpinned when a pin already exists")

	freshRows := coversOf(t, fresh)
	require.Len(t, freshRows, 1)
	assert.True(t, freshRows[0].PortraitPinned, "a work with no pin still gets the bangumi pin")
	assert.EqualValues(t, reg.bangumiSource, freshRows[0].SourceID)
}

func TestWriteCoverDecodeFailedIsRejectedWithoutRetry(t *testing.T) {
	uploads := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uploads++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":80010,"message":"图片解码失败"}`))
	}))
	t.Cleanup(srv.Close)

	mirror := writeMirror(t, []dimsEntry{{SubjectID: 9001, W: 800, H: 1200, File: "9001/cover.jpg"}}, map[string]bool{"9001": true})
	d, err := loadDims(mirror)
	require.NoError(t, err)

	r := &runner{
		cli:   imageclient.New(imageclient.Config{BaseURL: srv.URL, ClientID: "t", ClientSecret: "t"}),
		exist: map[int64]bool{},
	}
	quota := r.writeCover(context.Background(), mirror, candidate{WorkID: 1, SubjectID: "9001"}, d.entry["9001"], true)
	assert.False(t, quota)
	assert.Equal(t, 1, r.c.coverRejected)
	assert.Equal(t, 0, r.c.errors)
	assert.Equal(t, 1, uploads)
}

func TestQuotaAbort(t *testing.T) {
	truncate(t)
	reg := resolveTestRegistry(t)
	work := mkWork(t, reg.galgameMedium, "quota", nil)
	mirror := writeMirror(t, []dimsEntry{{SubjectID: 9200, W: 800, H: 1200, File: "9200/cover.jpg"}}, map[string]bool{"9200": true})
	d, err := loadDims(mirror)
	require.NoError(t, err)

	ctx := context.Background()
	r := &runner{db: testDB, cli: &fakeUploader{fail: imageclient.ErrQuotaExceeded},
		sourceID: reg.bangumiSource, exist: map[int64]bool{}}
	quota := r.process(ctx, Opts{Apply: true, BangumiMirror: mirror},
		[]candidate{{WorkID: work, SubjectID: "9200", Site: nil}}, d)
	assert.True(t, quota, "quota exhaustion aborts the run")
	assert.Empty(t, coversOf(t, work), "no row written on quota abort")
}

func TestRunGuards(t *testing.T) {
	cfg := minimalConfig()
	_, err := Run(context.Background(), cfg, Opts{DSN: "", BangumiMirror: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--dsn")

	_, err = Run(context.Background(), cfg, Opts{DSN: "x", BangumiMirror: ""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--bangumi-mirror")

	_, err = Run(context.Background(), cfg, Opts{Apply: true, DSN: "x", BangumiMirror: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "catalog image client not configured")
}

func sha256hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func minimalConfig() *config.Config { return &config.Config{} }
