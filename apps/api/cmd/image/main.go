package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"api/internal/app"
	"api/internal/infrastructure/database"
	"api/internal/middleware"
	imgHandler "api/internal/platform/image/handler"
	imgMW "api/internal/platform/image/middleware"
	imgModel "api/internal/platform/image/model"
	"api/internal/platform/image/preset"
	"api/internal/platform/image/quota"
	"api/internal/platform/image/repository"
	"api/internal/platform/image/service"
	"api/internal/platform/image/storage"
	"api/internal/platform/settings"
	"api/internal/platform/settings/keys"
	siteRepo "api/internal/platform/site/repository"
	"api/pkg/config"
	"api/pkg/errors"
	"api/pkg/health"
	"api/pkg/logger"
	"api/pkg/response"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	health.MaybeProbe(cfg.ImageService.Port, "/healthz")

	logger.Init(cfg.Server.Env)

	application, err := app.New(cfg, app.Options{
		Name:      "kun-image",
		NeedCache: true,
	})
	if err != nil {
		slog.Error("app init", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	settings.NewDistributor(application.DB.DB(), keys.Live(), application.Cache).Start(ctx)

	imagesDB, err := database.NewPostgresDB(cfg.ImagesDatabase)
	if err != nil {
		slog.Error("images db connect", "error", err,
			"hint", "若是新环境，请先跑 `go run ./cmd/image-setup`")
		os.Exit(1)
	}
	if err := imagesDB.AutoMigrate(&imgModel.Image{}, &imgModel.ImageSiteUsage{}, &imgModel.ModerationQueue{}); err != nil {
		slog.Error("images automigrate", "error", err)
		os.Exit(1)
	}
	go func() {
		if err := repository.EnsureIndexes(ctx, imagesDB.DB()); err != nil {
			slog.Error("images ensure indexes", "error", err)
		}
	}()

	s3Client, err := storage.NewClient(cfg.ImageS3)
	if err != nil {
		slog.Error("s3 init", "error", err)
		os.Exit(1)
	}
	if err := s3Client.EnsureBucket(context.Background()); err != nil {
		slog.Warn("ensure bucket (skip if pre-provisioned)", "error", err)
	}

	presets, err := preset.Load(cfg.ImageService.PresetsPath)
	if err != nil {
		slog.Error("presets load", "error", err, "path", cfg.ImageService.PresetsPath)
		os.Exit(1)
	}

	imgRepo := repository.NewImageRepository(imagesDB.DB())
	usageRepo := repository.NewSiteUsageRepository(imagesDB.DB())
	statsRepo := repository.NewStatsRepository(imagesDB.DB())
	clientRepo := siteRepo.NewOAuthClientRepository(application.DB.DB())

	svc := service.New(presets, s3Client, imgRepo, usageRepo, cfg.ImageService.CDNBase,
		service.Options{DB: imagesDB.DB()})
	q := quota.New(application.Cache)
	h := imgHandler.New(svc, q, statsRepo)

	application.Fiber.Use(middleware.RequestID())
	application.Fiber.Use(middleware.Logger())

	application.Fiber.Get("/healthz", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})
	application.Fiber.Get("/metrics", adaptor.HTTPHandler(promhttp.Handler()))

	application.Fiber.Use(middleware.CORS(cfg.Server.CORSOrigin))

	application.Fiber.Post("/image/upload", imgMW.ClientAuth(clientRepo, cfg), uploadGate, h.Upload)
	if !keys.ImageUploadEnabled.Get() {
		slog.Warn("image upload disabled at boot (image.upload_enabled=false); other endpoints still serve")
	}

	img := application.Fiber.Group("/image", imgMW.ClientAuth(clientRepo, cfg))
	img.Get("/stats", h.Stats)
	img.Get("/:hash", h.Meta)
	img.Post("/meta-batch", h.MetaBatch)
	img.Post("/reference-ping", h.Ping)
	img.Delete("/:hash", h.SoftDelete)

	addr := fmt.Sprintf("%s:%d", cfg.ImageService.Host, cfg.ImageService.Port)
	slog.Info("image service starting",
		"addr", addr,
		"cdn_base", cfg.ImageService.CDNBase,
		"bucket", s3Client.Bucket(),
	)

	defer func() {
		if err := imagesDB.Close(); err != nil {
			slog.Error("close images db", "error", err)
		}
	}()

	if err := application.Run(cfg.ImageService.Host, cfg.ImageService.Port); err != nil {
		slog.Error("run", "error", err)
		os.Exit(1)
	}
}

func uploadGate(c fiber.Ctx) error {
	if client := imgMW.ClientFromCtx(c); client != nil && client.SiteID != nil {
		if !keys.ImageUploadEnabled.ForSite(*client.SiteID) {
			return uploadDisabled(c)
		}
		return c.Next()
	}
	if !keys.ImageUploadEnabled.Get() {
		return uploadDisabled(c)
	}
	return c.Next()
}

func uploadDisabled(c fiber.Ctx) error {
	return response.Error(c,
		fiber.StatusServiceUnavailable,
		errors.ErrImageUploadDisabled,
		errors.GetMessage(errors.ErrImageUploadDisabled),
	)
}
