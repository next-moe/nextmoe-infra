package image_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"api/internal/middleware"
	imgHandler "api/internal/platform/image/handler"
	imgMW "api/internal/platform/image/middleware"
	"api/internal/platform/image/model"
	"api/internal/platform/image/preset"
	"api/internal/platform/image/repository"
	"api/internal/platform/image/service"
	"api/internal/platform/image/storage"
	siteModel "api/internal/platform/site/model"
	siteRepo "api/internal/platform/site/repository"
	"api/pkg/config"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var (
	testSvc      *service.Service
	testImgRepo  *repository.ImageRepository
	testS3Client *storage.Client

	testApp        *fiber.App
	testCfg        *config.Config
	testDB         *gorm.DB
	testClientRepo *siteRepo.OAuthClientRepository
)

const (
	testClientID         = "test-client"
	testClientSecret     = "test-secret"
	testClientSiteKey    = "testsite"
	testDisabledClientID = "disabled-client"
	testRestrictedClient = "restricted-client"
	testTinyClient       = "tiny-client"
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	presetPath := presetsPath()
	presets, err := preset.Load(presetPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: cannot load presets at %s: %v\n", presetPath, err)
		os.Exit(0)
	}

	s3Cfg := config.S3Config{
		Endpoint:        envOr("KUN_IMAGE_TEST_S3_ENDPOINT", "http://127.0.0.1:9000"),
		Region:          envOr("KUN_IMAGE_TEST_S3_REGION", "us-east-1"),
		AccessKeyID:     envOr("KUN_IMAGE_TEST_S3_ACCESS_KEY", "minioadmin"),
		SecretAccessKey: envOr("KUN_IMAGE_TEST_S3_SECRET_KEY", "minioadmin"),
		Bucket:          envOr("KUN_IMAGE_TEST_S3_BUCKET", "kun-images-test"),
		UsePathStyle:    true,
	}
	s3Client, err := storage.NewClient(s3Cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: s3 client init failed: %v\n", err)
		os.Exit(0)
	}
	if err := s3Client.EnsureBucket(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: minio unreachable at %s: %v\n", s3Cfg.Endpoint, err)
		os.Exit(0)
	}
	testS3Client = s3Client

	dsn := os.Getenv("KUN_IMAGE_TEST_PG_DSN")
	if dsn == "" {
		fmt.Fprintf(os.Stderr, "SKIP: KUN_IMAGE_TEST_PG_DSN not set; DB-backed tests require a clean Postgres DB\n")
		os.Exit(0)
	}
	gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: postgres unreachable: %v\n", err)
		os.Exit(0)
	}
	_ = gdb.Migrator().DropTable(
		&model.Image{},
		&model.ImageSiteUsage{},
		&model.ModerationQueue{},
		&siteModel.OAuthClient{},
	)
	if err := gdb.AutoMigrate(
		&model.Image{},
		&model.ImageSiteUsage{},
		&model.ModerationQueue{},
		&siteModel.OAuthClient{},
	); err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: automigrate failed: %v\n", err)
		os.Exit(0)
	}

	testDB = gdb
	testImgRepo = repository.NewImageRepository(gdb)
	usageRepo := repository.NewSiteUsageRepository(gdb)
	statsRepo := repository.NewStatsRepository(gdb)
	testClientRepo = siteRepo.NewOAuthClientRepository(gdb)

	cdnBase := "http://127.0.0.1:9000/" + s3Cfg.Bucket
	testSvc = service.New(presets, s3Client, testImgRepo, usageRepo, cdnBase,
		service.Options{DB: gdb})

	if err := seedHTTPTestClients(gdb); err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: seed test clients failed: %v\n", err)
		os.Exit(0)
	}

	testCfg = &config.Config{
		JWT: config.JWTConfig{Secret: "test-jwt-secret-not-used-by-basic-auth"},
	}
	testApp = buildTestApp(testSvc, statsRepo, testClientRepo, testCfg)

	code := m.Run()
	os.Exit(code)
}

func seedHTTPTestClients(db *gorm.DB) error {
	emptyJSON := datatypes.JSON(`[]`)
	allPresets := datatypes.JSON(`["avatar","topic","galgame_banner"]`)
	onlyTopic := datatypes.JSON(`["topic"]`)
	allowAvatar := datatypes.JSON(`["avatar"]`)

	rows := []*siteModel.OAuthClient{
		{
			ID:                   testClientID,
			Name:                 "test",
			Secret:               testClientSecret,
			RedirectURIs:         emptyJSON,
			Grants:               emptyJSON,
			ImageEnabled:         true,
			ImageSiteKey:         testClientSiteKey,
			ImageQuotaDaily:      10000,
			ImageQuotaBytesDaily: 10737418240,
			ImageMaxFileSize:     10485760,
			ImageAllowedPresets:  allPresets,
		},
		{
			ID:               testDisabledClientID,
			Name:             "disabled",
			Secret:           "secret",
			RedirectURIs:     emptyJSON,
			Grants:           emptyJSON,
			ImageEnabled:     false,
			ImageSiteKey:     "x",
			ImageMaxFileSize: 10485760,
		},
		{
			ID:                   testRestrictedClient,
			Name:                 "restricted",
			Secret:               "secret",
			RedirectURIs:         emptyJSON,
			Grants:               emptyJSON,
			ImageEnabled:         true,
			ImageSiteKey:         "restrictedsite",
			ImageQuotaDaily:      100,
			ImageQuotaBytesDaily: 1073741824,
			ImageMaxFileSize:     10485760,
			ImageAllowedPresets:  onlyTopic,
		},
		{
			ID:                   testTinyClient,
			Name:                 "tiny",
			Secret:               "secret",
			RedirectURIs:         emptyJSON,
			Grants:               emptyJSON,
			ImageEnabled:         true,
			ImageSiteKey:         "tinysite",
			ImageQuotaDaily:      100,
			ImageQuotaBytesDaily: 1073741824,
			ImageMaxFileSize:     128,
			ImageAllowedPresets:  allowAvatar,
		},
	}
	for _, r := range rows {
		if err := db.Create(r).Error; err != nil {
			return err
		}
	}
	return nil
}

func buildTestApp(svc *service.Service, statsRepo *repository.StatsRepository, clientRepo *siteRepo.OAuthClientRepository, cfg *config.Config) *fiber.App {
	h := imgHandler.New(svc, nil, statsRepo)
	app := fiber.New()
	app.Use(middleware.RequestID())

	app.Get("/healthz", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})
	app.Get("/metrics", adaptor.HTTPHandler(promhttp.Handler()))

	img := app.Group("/image", imgMW.ClientAuth(clientRepo, cfg))
	img.Post("/upload", h.Upload)
	img.Get("/stats", h.Stats)
	img.Get("/:hash", h.Meta)
	img.Post("/meta-batch", h.MetaBatch)
	img.Post("/reference-ping", h.Ping)
	img.Delete("/:hash", h.SoftDelete)
	return app
}

func presetsPath() string {
	_, thisFile, _, _ := runtime.Caller(0)
	apiDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	return filepath.Join(apiDir, "configs", "image_presets.yaml")
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func fixturePNG(w, h int, r, g, b uint8) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.NRGBA{r, g, b, 255})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func TestUpload_NewAvatar_CreatesMainAndVariants(t *testing.T) {
	ctx := context.Background()
	body := fixturePNG(600, 600, 128, 64, 200)

	result, err := testSvc.Upload(ctx, service.UploadRequest{
		Body:        body,
		Preset:      "avatar",
		Site:        "testsite-a",
		UploaderSub: "u1",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Deduplicated)
	assert.Len(t, result.Hash, 64)
	assert.NotEmpty(t, result.URL)

	require.Len(t, result.VariantURLs, 2)
	assert.NotEmpty(t, result.VariantURLs["256"])
	assert.NotEmpty(t, result.VariantURLs["100"])

	img, err := testImgRepo.FindByHash(ctx, result.Hash)
	require.NoError(t, err)
	require.NotNil(t, img)
	assert.Equal(t, "image/webp", img.MIME)
	assert.Equal(t, "webp", img.Ext)

	variants := img.VariantList()
	assert.ElementsMatch(t, []string{"256", "100"}, variants)

	exists, err := testS3Client.Exists(ctx, storageKey(img.Hash, "", "webp"))
	require.NoError(t, err)
	assert.True(t, exists, "main image should exist in S3")

	for _, v := range variants {
		key := storageKey(img.Hash, v, "webp")
		exists, err := testS3Client.Exists(ctx, key)
		require.NoError(t, err)
		assert.True(t, exists, "variant %s should exist at %s", v, key)
	}
}

func TestUpload_SameHashAgain_IsDeduplicated(t *testing.T) {
	ctx := context.Background()
	body := fixturePNG(500, 500, 200, 200, 50)

	r1, err := testSvc.Upload(ctx, service.UploadRequest{
		Body: body, Preset: "avatar", Site: "siteB", UploaderSub: "u2",
	})
	require.NoError(t, err)
	assert.False(t, r1.Deduplicated)

	r2, err := testSvc.Upload(ctx, service.UploadRequest{
		Body: body, Preset: "avatar", Site: "siteC", UploaderSub: "u3",
	})
	require.NoError(t, err)
	assert.True(t, r2.Deduplicated, "second upload of same hash should dedup")
	assert.Equal(t, r1.Hash, r2.Hash)
	assert.Equal(t, r1.URL, r2.URL)
}

func TestUpload_TopicPreset_NoVariants(t *testing.T) {
	ctx := context.Background()
	body := fixturePNG(400, 300, 10, 200, 30)

	r, err := testSvc.Upload(ctx, service.UploadRequest{
		Body: body, Preset: "topic", Site: "siteD", UploaderSub: "u4",
	})
	require.NoError(t, err)
	assert.Empty(t, r.VariantURLs)
}

func TestUpload_CrossPresetBackfill(t *testing.T) {
	ctx := context.Background()
	body := fixturePNG(700, 700, 20, 20, 20)

	r1, err := testSvc.Upload(ctx, service.UploadRequest{
		Body: body, Preset: "topic", Site: "siteE", UploaderSub: "u5",
	})
	require.NoError(t, err)
	assert.Empty(t, r1.VariantURLs)

	r2, err := testSvc.Upload(ctx, service.UploadRequest{
		Body: body, Preset: "avatar", Site: "siteE", UploaderSub: "u5",
	})
	require.NoError(t, err)
	assert.True(t, r2.Deduplicated)
	assert.Len(t, r2.VariantURLs, 2)

	img, err := testImgRepo.FindByHash(ctx, r1.Hash)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"256", "100"}, img.VariantList())
}

func TestUpload_RejectsNonImage(t *testing.T) {
	ctx := context.Background()
	_, err := testSvc.Upload(ctx, service.UploadRequest{
		Body:   []byte("this is not an image, just ASCII text to break mime sniffer"),
		Preset: "avatar", Site: "siteF",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, service.ErrMIMENotAllowed)
}

func TestUpload_UnknownPreset(t *testing.T) {
	ctx := context.Background()
	body := fixturePNG(100, 100, 0, 0, 0)
	_, err := testSvc.Upload(ctx, service.UploadRequest{
		Body: body, Preset: "totally-fake", Site: "siteG",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, service.ErrPresetNotFound)
}

func TestReferencePing_TouchesLastReferencedAt(t *testing.T) {
	ctx := context.Background()
	body := fixturePNG(200, 200, 7, 7, 7)

	r, err := testSvc.Upload(ctx, service.UploadRequest{
		Body: body, Preset: "avatar", Site: "siteH",
	})
	require.NoError(t, err)

	before, err := testImgRepo.FindByHash(ctx, r.Hash)
	require.NoError(t, err)
	time.Sleep(1100 * time.Millisecond)

	updated, notFound, err := testSvc.ReferencePing(ctx, "siteH", []string{r.Hash, "0000000000000000000000000000000000000000000000000000000000000000"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), updated)
	assert.Len(t, notFound, 1)

	after, err := testImgRepo.FindByHash(ctx, r.Hash)
	require.NoError(t, err)
	assert.True(t, after.LastReferencedAt.After(before.LastReferencedAt))
}

func TestSoftDelete_SharedHashDetachesPerSite(t *testing.T) {
	ctx := context.Background()
	body := fixturePNG(64, 64, 9, 9, 9)

	up, err := testSvc.Upload(ctx, service.UploadRequest{
		Body: body, Preset: "topic", Site: "siteDelA",
	})
	require.NoError(t, err)
	_, err = testSvc.Upload(ctx, service.UploadRequest{
		Body: body, Preset: "topic", Site: "siteDelB",
	})
	require.NoError(t, err)

	ok, err := testSvc.SoftDelete(ctx, up.Hash, "stranger")
	require.NoError(t, err)
	assert.False(t, ok)

	ok, err = testSvc.SoftDelete(ctx, up.Hash, "siteDelA")
	require.NoError(t, err)
	assert.True(t, ok)

	img, err := testImgRepo.FindByHash(ctx, up.Hash)
	require.NoError(t, err)
	require.NotNil(t, img, "image must survive while another site references it")
	assert.Nil(t, img.DeletedAt)

	var usages []model.ImageSiteUsage
	require.NoError(t, testDB.Where("hash = ?", up.Hash).Find(&usages).Error)
	require.Len(t, usages, 1)
	assert.Equal(t, "siteDelB", usages[0].Site)

	ok, err = testSvc.SoftDelete(ctx, up.Hash, "siteDelA")
	require.NoError(t, err)
	assert.False(t, ok)

	ok, err = testSvc.SoftDelete(ctx, up.Hash, "siteDelB")
	require.NoError(t, err)
	assert.True(t, ok)

	var gone model.Image
	require.NoError(t, testDB.Where("hash = ?", up.Hash).First(&gone).Error)
	assert.NotNil(t, gone.DeletedAt)

	var remaining int64
	require.NoError(t, testDB.Model(&model.ImageSiteUsage{}).
		Where("hash = ?", up.Hash).Count(&remaining).Error)
	assert.Zero(t, remaining)

	ok, err = testSvc.SoftDelete(ctx,
		"0000000000000000000000000000000000000000000000000000000000000000", "siteDelA")
	require.NoError(t, err)
	assert.False(t, ok)
}

func storageKey(hash, variant, ext string) string {
	if variant == "" {
		return fmt.Sprintf("%s/%s/%s.%s", hash[:2], hash[2:4], hash, ext)
	}
	return fmt.Sprintf("%s/%s/%s_%s.%s", hash[:2], hash[2:4], hash, variant, ext)
}

func TestFixturePNGIsStable(t *testing.T) {
	a := fixturePNG(32, 32, 1, 2, 3)
	b := fixturePNG(32, 32, 1, 2, 3)
	aHash := sha256.Sum256(a)
	bHash := sha256.Sum256(b)
	assert.Equal(t, hex.EncodeToString(aHash[:]), hex.EncodeToString(bHash[:]))
}
