package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"api/internal/app"
	"api/internal/infrastructure/database"
	"api/internal/middleware"
	artHandler "api/internal/platform/artifact/handler"
	artMW "api/internal/platform/artifact/middleware"
	artModel "api/internal/platform/artifact/model"
	"api/internal/platform/artifact/quota"
	"api/internal/platform/artifact/repository"
	"api/internal/platform/artifact/service"
	"api/internal/platform/artifact/storage"
	"api/internal/platform/settings"
	"api/internal/platform/settings/keys"
	siteRepo "api/internal/platform/site/repository"
	"api/pkg/config"
	"api/pkg/health"
	"api/pkg/logger"

	"github.com/gofiber/fiber/v3"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	health.MaybeProbe(cfg.ArtifactService.Port, "/healthz")

	logger.Init(cfg.Server.Env)

	application, err := app.New(cfg, app.Options{
		Name:      "kun-artifact",
		NeedCache: true,
		BodyLimit: artHandler.CompositeBodyLimit,
	})
	if err != nil {
		slog.Error("app init", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	settings.NewDistributor(application.DB.DB(), keys.Live(), application.Cache).Start(ctx)

	artifactsDB, err := database.NewPostgresDB(cfg.ArtifactsDatabase)
	if err != nil {
		slog.Error("artifacts db connect", "error", err)
		os.Exit(1)
	}
	if err := artifactsDB.AutoMigrate(artModel.All()...); err != nil {
		slog.Error("artifacts automigrate", "error", err)
		os.Exit(1)
	}

	s3Client, err := storage.NewClient(cfg.ArtifactS3)
	if err != nil {
		slog.Error("s3 init", "error", err)
		os.Exit(1)
	}
	if err := s3Client.EnsureBucket(context.Background()); err != nil {
		slog.Warn("ensure bucket (skip if pre-provisioned)", "error", err)
	}
	cleanupClient, err := storage.NewClient(cfg.ArtifactCleanupS3())
	if err != nil {
		slog.Error("s3 cleanup init", "error", err)
		os.Exit(1)
	}

	repo := repository.NewArtifactRepository(artifactsDB.DB())
	clientRepo := siteRepo.NewOAuthClientRepository(application.DB.DB())
	q := quota.New(application.Cache)
	svc := service.New(repo, s3Client, q)
	composites := service.NewCompositeService(repository.NewCompositeRepository(artifactsDB.DB()), s3Client, cleanupClient)

	application.Fiber.Use(middleware.RequestID())
	application.Fiber.Use(middleware.Logger())
	application.Fiber.Get("/healthz", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})
	application.Fiber.Use(middleware.CORS(cfg.Server.CORSOrigin))

	application.Fiber.Use("/api/v1/artifacts", artMW.ClientAuth(clientRepo, cfg))

	humaAPI := artHandler.Setup(application.Fiber, svc, composites)

	application.Fiber.Get("/openapi.json", func(c fiber.Ctx) error {
		b, err := json.Marshal(humaAPI.OpenAPI())
		if err != nil {
			return err
		}
		c.Set("Content-Type", "application/json")
		return c.Send(b)
	})

	slog.Info("artifact service starting",
		"addr", fmt.Sprintf("%s:%d", cfg.ArtifactService.Host, cfg.ArtifactService.Port),
		"bucket", s3Client.Bucket(),
		"upload_enabled", keys.ArtifactUploadEnabled.Get(),
	)

	defer func() {
		if err := artifactsDB.Close(); err != nil {
			slog.Error("close artifacts db", "error", err)
		}
	}()

	if err := application.Run(cfg.ArtifactService.Host, cfg.ArtifactService.Port); err != nil {
		slog.Error("run", "error", err)
		os.Exit(1)
	}
}
