package dlsitemedia

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"
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
		fmt.Fprintln(os.Stderr, "SKIP: no TEST_DATABASE_DSN — DB-backed dlsitemedia tests will skip individually")
		os.Exit(m.Run())
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("jobs/dlsitemedia", "cannot connect to test database: %v", err)
	}
	if err := migrate.Run(db); err != nil {
		dbtest.SkipMainf("jobs/dlsitemedia", "catalog migrate failed: %v", err)
	}
	if err := seed.Run(db); err != nil {
		dbtest.SkipMainf("jobs/dlsitemedia", "catalog seed failed: %v", err)
	}
	testDB = db
	os.Exit(m.Run())
}

func requireDB(t *testing.T) *gorm.DB {
	t.Helper()
	if testDB == nil {
		dbtest.Skip(t)
	}
	return testDB
}

func TestIntroWritePath(t *testing.T) {
	db := requireDB(t)
	for _, tbl := range []string{"catalog_work_intro", "catalog_work"} {
		require.NoError(t, db.Exec("TRUNCATE "+tbl+" RESTART IDENTITY CASCADE").Error)
	}

	reg, err := resolveRegistry(context.Background(), db)
	require.NoError(t, err)
	require.NotZero(t, reg.dlsiteSource)

	mkWork := func(name string, site *string) int64 {
		w := model.CatalogWork{MediumID: reg.galgameMedium, OLang: "ja", DisplayName: name, Site: site}
		require.NoError(t, db.Create(&w).Error)
		return w.ID
	}
	claimed := "kungal"
	wBody := mkWork("bodyless-with-intro", nil)
	wEmpty := mkWork("bodyless-no-text", nil)
	wClaimed := mkWork("claimed", &claimed)

	cands := []candidate{
		{WorkID: wBody, Workno: "RJ000001", Site: nil},
		{WorkID: wEmpty, Workno: "RJ000002", Site: nil},
		{WorkID: wClaimed, Workno: "RJ000003", Site: &claimed},
	}
	metas := map[string]dlsiteMeta{
		"RJ000001": {Age: "3", Intro: "A bodyless doujin blurb."},
		"RJ000002": {Age: "1", Intro: ""},
		"RJ000003": {Age: "3", Intro: "A claimed work's store blurb (wave 166)."},
	}

	ctx := context.Background()
	run := func(apply bool) *runner {
		exist, err := preloadExisting(ctx, db, []int64{wBody, wEmpty, wClaimed}, reg.dlsiteSource, langJa)
		require.NoError(t, err)
		r := &runner{db: db, sourceID: reg.dlsiteSource, exist: exist}
		for _, c := range cands {
			r.writeIntro(ctx, c, metas[c.Workno], apply)
		}
		return r
	}

	r := run(false)
	assert.Equal(t, 2, r.c.introWould, "wBody and wClaimed both would write (wave 166)")
	assert.Equal(t, 1, r.c.introNoText, "wEmpty no text")
	assert.Equal(t, 0, r.c.introWritten)
	var n int64
	require.NoError(t, db.Raw("SELECT count(*) FROM catalog_work_intro").Scan(&n).Error)
	assert.EqualValues(t, 0, n, "dry run writes nothing")

	r = run(true)
	assert.Equal(t, 2, r.c.introWritten)
	var row model.CatalogWorkIntro
	require.NoError(t, db.Where("work_id = ?", wBody).First(&row).Error)
	assert.Equal(t, "ja", row.Lang)
	assert.EqualValues(t, reg.dlsiteSource, row.SourceID)
	assert.Equal(t, "A bodyless doujin blurb.", row.Intro)
	var claimedRow model.CatalogWorkIntro
	require.NoError(t, db.Where("work_id = ?", wClaimed).First(&claimedRow).Error)
	assert.Equal(t, "ja", claimedRow.Lang)
	assert.Equal(t, "A claimed work's store blurb (wave 166).", claimedRow.Intro)
	assert.EqualValues(t, 0, claimedRow.Provenance, "an ingested store blurb is a source row, never machine")

	r = run(true)
	assert.Equal(t, 0, r.c.introWritten)
	assert.Equal(t, 2, r.c.introExists, "preloaded exists → skip before write")

	rStale := &runner{db: db, sourceID: reg.dlsiteSource,
		exist: &existing{intro: map[int64]bool{}, cover: map[int64]bool{}, shot: map[int64]map[int]bool{}}}
	rStale.writeIntro(ctx, cands[0], metas["RJ000001"], true)
	assert.Equal(t, 0, rStale.c.introWritten, "ON CONFLICT refuses the duplicate")
	assert.Equal(t, 1, rStale.c.introExists)
	require.NoError(t, db.Raw("SELECT count(*) FROM catalog_work_intro WHERE work_id = ?", wBody).Scan(&n).Error)
	assert.EqualValues(t, 1, n, "still exactly one row for the retried work")
	require.NoError(t, db.Raw("SELECT count(*) FROM catalog_work_intro").Scan(&n).Error)
	assert.EqualValues(t, 2, n, "bodyless + claimed, nothing more")
}

var claimedLaneClaimIDs = []int64{9101, 9102, 9103, 9104, 9105}

const vndbSourceID = int16(2)

func stubImageService(t *testing.T) *imageclient.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/image/upload", r.URL.Path)
		require.NoError(t, r.ParseMultipartForm(1<<20))
		f, hdr, err := r.FormFile("file")
		require.NoError(t, err)
		defer f.Close()
		sum := sha256.New()
		buf := make([]byte, hdr.Size)
		n, _ := f.Read(buf)
		sum.Write(buf[:n])
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200,
			"data": map[string]any{"hash": hex.EncodeToString(sum.Sum(nil)), "size_bytes": hdr.Size},
		})
	}))
	t.Cleanup(srv.Close)
	return imageclient.New(imageclient.Config{BaseURL: srv.URL, ClientID: "test", ClientSecret: "test"})
}

func TestClaimedScreenshotLane(t *testing.T) {
	db := requireDB(t)
	ctx := context.Background()
	for _, tbl := range []string{"catalog_external_ref", "catalog_release", "catalog_work_screenshot", "catalog_work"} {
		require.NoError(t, db.Exec("TRUNCATE "+tbl+" CASCADE").Error)
	}

	reg, err := resolveRegistry(ctx, db)
	require.NoError(t, err)

	anchored := func(name, workno string, site *string, galgameID *int64) int64 {
		w := model.CatalogWork{MediumID: reg.galgameMedium, OLang: "ja", DisplayName: name, Site: site, ProductWorkID: galgameID}
		require.NoError(t, db.Create(&w).Error)
		rel := model.CatalogRelease{WorkID: w.ID, Kind: 0}
		require.NoError(t, db.Create(&rel).Error)
		require.NoError(t, db.Create(&model.CatalogExternalRef{
			EntityType: model.EntityTypeRelease, EntityID: rel.ID, SourceID: reg.dlsiteSource,
			ExternalID: workno, LinkKind: model.LinkKindExact, MatchedBy: "import:test"}).Error)
		return w.ID
	}
	claimed := "kungal"
	wBodyless := anchored("bodyless", "RJ100001", nil, nil)
	wClaimedBare := anchored("claimed-no-screenshot", "RJ100002", &claimed, &claimedLaneClaimIDs[0])
	wClaimedNative := anchored("claimed-with-native-screenshot", "RJ100004", &claimed, &claimedLaneClaimIDs[2])
	wClaimedOther := anchored("claimed-with-vndb-screenshot", "RJ100006", &claimed, &claimedLaneClaimIDs[4])

	wDraft := anchored("claimed-but-draft", "RJ100005", &claimed, &claimedLaneClaimIDs[3])
	require.NoError(t, db.Model(&model.CatalogWork{}).Where("id = ?", wDraft).
		Update("claim_state", model.ClaimStateDraft).Error)

	require.NoError(t, db.Create(&model.CatalogWorkScreenshot{
		WorkID: wClaimedNative, ImageHash: "already_here", SortOrder: 0, SourceID: reg.dlsiteSource}).Error)
	require.NoError(t, db.Create(&model.CatalogWorkScreenshot{
		WorkID: wClaimedOther, ImageHash: "vndb_shot", SortOrder: 0, SourceID: vndbSourceID}).Error)
	require.NoError(t, db.Create(&model.CatalogWorkScreenshot{
		WorkID: wDraft, ImageHash: "draft_shot", SortOrder: 0, SourceID: reg.dlsiteSource}).Error)

	shotCands, err := loadCandidates(ctx, db, reg, Kinds{Screenshot: true}, 0, 0)
	require.NoError(t, err)
	ids := map[int64]string{}
	for _, c := range shotCands {
		ids[c.WorkID] = c.Workno
	}
	assert.Contains(t, ids, wBodyless, "bodyless lane unchanged")
	assert.Contains(t, ids, wClaimedBare, "claimed with no screenshot at all → admitted")
	assert.Contains(t, ids, wClaimedOther, "claimed whose only rows come from another source → admitted (wave 188)")
	assert.NotContains(t, ids, wClaimedNative, "claimed that already has a DLSITE row → excluded")
	bodyless, claimedCount := laneSplit(shotCands)
	assert.Equal(t, 1, bodyless)
	assert.Equal(t, 2, claimedCount)

	coverCands, err := loadCandidates(ctx, db, reg, Kinds{Cover: true}, 0, 0)
	require.NoError(t, err)
	require.Len(t, coverCands, 1, "a cover-only run resolves the bodyless lane alone")
	assert.Equal(t, wBodyless, coverCands[0].WorkID)

	introCands, err := loadCandidates(ctx, db, reg, Kinds{Intro: true}, 0, 0)
	require.NoError(t, err)
	introIDs := map[int64]bool{}
	for _, c := range introCands {
		introIDs[c.WorkID] = true
	}
	assert.Contains(t, introIDs, wBodyless, "bodyless lane unchanged")
	assert.Contains(t, introIDs, wClaimedBare, "claimed work with no ja intro → admitted")
	assert.NotContains(t, introIDs, wDraft, "a DRAFT claim is not on the public face → excluded")
	introBodyless, introClaimed := laneSplit(introCands)
	assert.Equal(t, 1, introBodyless)
	assert.Equal(t, 3, introClaimed, "the three live claims; the draft is out")

	for _, off := range []int{1, 99} {
		windowed, err := loadCandidates(ctx, db, reg, Kinds{Screenshot: true}, 0, off)
		require.NoError(t, err)
		require.Len(t, windowed, 2, "offset %d skips the bodyless lane, never the claimed one", off)
		windowedIDs := []int64{windowed[0].WorkID, windowed[1].WorkID}
		assert.ElementsMatch(t, []int64{wClaimedBare, wClaimedOther}, windowedIDs)
	}
	capped, err := loadCandidates(ctx, db, reg, Kinds{Screenshot: true}, 1, 0)
	require.NoError(t, err)
	require.Len(t, capped, 1, "--limit caps the concatenation")
	assert.Equal(t, wBodyless, capped[0].WorkID)

	var cand candidate
	for _, c := range shotCands {
		if c.WorkID == wClaimedBare {
			cand = c
		}
	}
	require.Equal(t, wClaimedBare, cand.WorkID)

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, cand.Workno), 0o755))
	for _, f := range []string{"a.jpg", "b.jpg"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, cand.Workno, f), []byte("bytes-of-"+f), 0o644))
	}
	meta := dlsiteMeta{Age: "3", Intro: "claimed prose", CoverFile: "cover.jpg",
		SampleFiles: []string{"a.jpg", "b.jpg", "never_mirrored.jpg"}}

	newRunner := func() *runner {
		exist, err := preloadExisting(ctx, db, []int64{cand.WorkID}, reg.dlsiteSource, langJa)
		require.NoError(t, err)
		return &runner{db: db, sourceID: reg.dlsiteSource, exist: exist, cli: stubImageService(t)}
	}

	r := newRunner()
	r.writeIntro(ctx, cand, meta, true)
	r.writeCover(ctx, dir, cand, meta, true)
	assert.Equal(t, 1, r.c.introWritten, "intro now writes claimed (wave 166)")
	assert.Equal(t, 1, r.c.coverRefused, "cover still refuses claimed")
	assert.Zero(t, r.c.coverUploaded)
	var n int64
	require.NoError(t, db.Raw("SELECT count(*) FROM catalog_work_intro WHERE work_id = ?", cand.WorkID).Scan(&n).Error)
	assert.EqualValues(t, 1, n, "claimed intro materialised")
	require.NoError(t, db.Raw("SELECT count(*) FROM catalog_work_cover WHERE work_id = ?", cand.WorkID).Scan(&n).Error)
	assert.EqualValues(t, 0, n, "claimed cover never materialised")

	r = newRunner()
	assert.False(t, r.writeScreenshots(ctx, dir, cand, meta, false))
	assert.Equal(t, 2, r.c.shotWould)
	assert.Equal(t, 1, r.c.shotMissing, "unmirrored sample is a forecast miss, not an error")
	assert.Equal(t, map[string]bool{cand.Workno: true}, r.unmirrored, "the work is listed for the mirror")
	assert.Empty(t, r.touched, "a dry run touches nothing")
	require.NoError(t, db.Raw("SELECT count(*) FROM catalog_work_screenshot WHERE work_id = ?", cand.WorkID).Scan(&n).Error)
	assert.EqualValues(t, 0, n)

	r = newRunner()
	assert.False(t, r.writeScreenshots(ctx, dir, cand, meta, true))
	assert.Equal(t, 2, r.c.shotUploaded)
	assert.Equal(t, 1, r.c.shotMissing)
	assert.Equal(t, []int64{cand.WorkID, cand.WorkID}, r.touched, "every real write feeds the touch list")
	require.NoError(t, repository.TouchWorks(ctx, r.db, r.touched))
	var rows []model.CatalogWorkScreenshot
	require.NoError(t, db.Where("work_id = ?", cand.WorkID).Order("sort_order").Find(&rows).Error)
	require.Len(t, rows, 2)
	assert.EqualValues(t, 0, rows[0].SortOrder)
	assert.EqualValues(t, 1, rows[1].SortOrder)
	for _, row := range rows {
		assert.EqualValues(t, reg.dlsiteSource, row.SourceID, "claimed native rows are dlsite-sourced only")
		assert.EqualValues(t, 2, row.Sexual, "age_category 3 (adult) → sexual 2")
		assert.EqualValues(t, 0, row.Violence)
		assert.Equal(t, "", row.Caption)
	}
	assert.Len(t, r.pingHashes, 2, "fresh uploads are reference-pinged immediately")

	r = newRunner()
	assert.False(t, r.writeScreenshots(ctx, dir, cand, meta, true))
	assert.Zero(t, r.c.shotUploaded)
	assert.Equal(t, 2, r.c.shotExists)
	assert.Empty(t, r.touched, "idempotent re-run moves no watermark")

	shotCands, err = loadCandidates(ctx, db, reg, Kinds{Screenshot: true}, 0, 0)
	require.NoError(t, err)
	for _, c := range shotCands {
		assert.NotEqual(t, cand.WorkID, c.WorkID, "a filled work is no longer a candidate")
	}

	r = newRunner()
	assert.False(t, r.writeScreenshots(ctx, dir, cand, dlsiteMeta{Age: "1"}, true))
	assert.Equal(t, 1, r.c.shotNoSamples)
}

func TestCrossSourceSameBytesAreNotWrittenTwice(t *testing.T) {
	if testDB == nil {
		t.Skip("no test database")
	}
	db := testDB
	ctx := context.Background()
	for _, tbl := range []string{"catalog_external_ref", "catalog_release", "catalog_work_screenshot", "catalog_work"} {
		require.NoError(t, db.Exec("TRUNCATE "+tbl+" CASCADE").Error)
	}
	reg, err := resolveRegistry(ctx, db)
	require.NoError(t, err)

	w := model.CatalogWork{MediumID: reg.galgameMedium, OLang: "ja", DisplayName: "shared bytes"}
	require.NoError(t, db.Create(&w).Error)

	dir := t.TempDir()
	workno := "RJ200001"
	require.NoError(t, os.MkdirAll(filepath.Join(dir, workno), 0o755))
	shared, fresh := []byte("shared-picture"), []byte("dlsite-only-picture")
	require.NoError(t, os.WriteFile(filepath.Join(dir, workno, "a.jpg"), shared, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, workno, "b.jpg"), fresh, 0o644))
	sharedHash := sha256.Sum256(shared)

	require.NoError(t, db.Create(&model.CatalogWorkScreenshot{
		WorkID: w.ID, ImageHash: hex.EncodeToString(sharedHash[:]), SortOrder: 0,
		SourceID: vndbSourceID}).Error)

	exist, err := preloadExisting(ctx, db, []int64{w.ID}, reg.dlsiteSource, langJa)
	require.NoError(t, err)
	assert.Empty(t, exist.shot[w.ID], "the preload is per-source: the vndb row is invisible to it")

	r := &runner{db: db, sourceID: reg.dlsiteSource, exist: exist, cli: stubImageService(t)}
	cand := candidate{WorkID: w.ID, Workno: workno}
	assert.False(t, r.writeScreenshots(ctx, dir, cand, dlsiteMeta{Age: "3",
		SampleFiles: []string{"a.jpg", "b.jpg"}}, true))
	assert.Equal(t, 1, r.c.shotDedup, "the byte another source already holds is not written again")
	assert.Equal(t, 1, r.c.shotUploaded, "the genuinely new byte still lands")

	var rows []model.CatalogWorkScreenshot
	require.NoError(t, db.Where("work_id = ?", w.ID).Order("image_hash").Find(&rows).Error)
	require.Len(t, rows, 2, "one row per distinct image, never two rows for one picture")
	for _, row := range rows {
		if row.ImageHash == hex.EncodeToString(sharedHash[:]) {
			assert.EqualValues(t, vndbSourceID, row.SourceID, "the first writer keeps its source attribution")
		}
	}
}

func TestWorknosOutListsEachWorkOnceInOrder(t *testing.T) {
	r := &runner{}
	for _, w := range []string{"RJ300", "VJ100", "RJ300", "BJ200"} {
		r.markUnmirrored(w)
	}
	path := filepath.Join(t.TempDir(), "worknos")
	require.NoError(t, writeWorknos(path, r.unmirrored))
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "BJ200\nRJ300\nVJ100\n", string(got))

	empty := filepath.Join(t.TempDir(), "empty")
	require.NoError(t, writeWorknos(empty, nil))
	got, err = os.ReadFile(empty)
	require.NoError(t, err)
	assert.Empty(t, got, "nothing to fetch still writes the file the job reads")
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

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "RJ1"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "RJ1", "cover.jpg"), []byte("bytes"), 0o644))

	r := &runner{
		cli:   imageclient.New(imageclient.Config{BaseURL: srv.URL, ClientID: "t", ClientSecret: "t"}),
		exist: &existing{cover: map[int64]bool{}},
	}
	quota := r.writeCover(context.Background(), dir, candidate{WorkID: 1, Workno: "RJ1"}, dlsiteMeta{CoverFile: "cover.jpg"}, true)
	assert.False(t, quota)
	assert.Equal(t, 1, r.c.coverRejected)
	assert.Equal(t, 0, r.c.errors)
	assert.Equal(t, 1, uploads)
}

func TestAnUnmirroredCoverListsItsWork(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "RJ2"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "RJ2", "RJ2_img_main.jpg"), []byte("x"), 0o644))
	r := &runner{exist: &existing{cover: map[int64]bool{3: true}}}
	ctx := context.Background()

	r.writeCover(ctx, dir, candidate{WorkID: 1, Workno: "RJ1"}, dlsiteMeta{CoverFile: "RJ1_img_main.jpg"}, false)
	r.writeCover(ctx, dir, candidate{WorkID: 2, Workno: "RJ2"}, dlsiteMeta{CoverFile: "RJ2_img_main.jpg"}, false)
	r.writeCover(ctx, dir, candidate{WorkID: 3, Workno: "RJ3"}, dlsiteMeta{CoverFile: "RJ3_img_main.jpg"}, false)
	r.writeCover(ctx, dir, candidate{WorkID: 4, Workno: "RJ4"}, dlsiteMeta{}, false)

	assert.Equal(t, map[string]bool{"RJ1": true}, r.unmirrored,
		"only the cover it would upload and cannot read: not the mirrored one, the covered work, or a placeholder")
	assert.Equal(t, 1, r.c.coverMissing)
	assert.Equal(t, 1, r.c.coverWould)
}
