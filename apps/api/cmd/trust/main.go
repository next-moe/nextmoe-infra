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
	"api/internal/platform/permissions"
	"api/internal/platform/settings"
	"api/internal/platform/settings/keys"
	siteRepo "api/internal/platform/site/repository"
	trustHandler "api/internal/platform/trust/handler"
	trustPerm "api/internal/platform/trust/perm"
	"api/internal/platform/trust/service"
	"api/pkg/config"
	"api/pkg/health"
	"api/pkg/logger"
	"api/pkg/oidctoken"

	"github.com/gofiber/fiber/v3"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	health.MaybeProbe(cfg.TrustService.Port, "/healthz")

	logger.Init(cfg.Server.Env)

	application, err := app.New(cfg, app.Options{Name: "kun-trust"})
	if err != nil {
		slog.Error("app init", "error", err)
		os.Exit(1)
	}

	trustDB, err := database.NewPostgresDB(cfg.TrustDatabase)
	if err != nil {
		slog.Error("trust db connect", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	settings.NewDistributor(application.DB.DB(), keys.Live(), nil).Start(ctx)

	weigher := service.NewDBWeigher(application.DB.DB())

	policySvc := service.NewPolicyServiceFrom(trustDB.DB(), func() service.PlatformDefaults {
		return service.PlatformDefaults{
			ScanMode:           service.ScanModeFromName(keys.TrustScanMode.Get()),
			SampleRate:         keys.TrustScanSampleRate.Get(),
			AggregateThreshold: service.DefaultAggregateThreshold(),
			AutoHideEnabled:    true,
		}
	})

	reviewSvc := service.NewReviewService(trustDB.DB())
	registrySvc := service.NewRegistryService(trustDB.DB())
	dispositionSvc := service.NewDispositionService(trustDB.DB())
	worker := service.NewCallbackWorker(trustDB.DB())

	forwarders := make(map[string]bool, len(cfg.TrustForwarderClientIDs))
	for _, id := range cfg.TrustForwarderClientIDs {
		forwarders[id] = true
	}
	reportSvc := service.NewReportService(trustDB.DB(), weigher,
		service.WithReportPolicy(policySvc), service.WithReportForwarders(forwarders))
	forwardSvc := service.NewForwardService(trustDB.DB(), forwarders)
	scanSvc := service.NewScanService(trustDB.DB(), forwarders)
	slog.Info("trust forward face", "allowed_forwarders", len(forwarders))

	termSvc := service.NewTermService(trustDB.DB(), forwarders)

	aiGateway := service.NewAIGatewayClient(cfg.AIClient.BaseURL, cfg.AIClient.ClientID, cfg.AIClient.ClientSecret)
	scanWorker := service.NewScanWorker(trustDB.DB(), aiGateway, termSvc,
		service.WithPolicy(policySvc))
	slog.Info("trust scan worker", "gateway_configured", aiGateway.Configured(), "default_mode", keys.TrustScanMode.Get())

	application.Fiber.Use(middleware.RequestID())
	application.Fiber.Use(middleware.Logger())
	application.Fiber.Get("/healthz", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})
	application.Fiber.Use(middleware.CORS(cfg.Server.CORSOrigin))

	clientRepo := siteRepo.NewOAuthClientRepository(application.DB.DB())
	application.Fiber.Use("/api/v1/trust", trustHandler.S2SAuth(clientRepo))

	tokenVerifier := oidctoken.NewVerifierWithJWKS(cfg.JWT.Secret, cfg.OIDC.JWKSURL)
	application.Fiber.Use("/api/v1/admin/trust",
		middleware.JWTAuth(tokenVerifier), middleware.RequirePermission(trustPerm.Resolver, trustPerm.QueueAccess))

	s2sAPI := trustHandler.Setup(application.Fiber, reportSvc, registrySvc, forwardSvc, scanSvc, termSvc)
	trustHandler.SetupAdmin(application.Fiber, reviewSvc, registrySvc, dispositionSvc, termSvc, policySvc, clientRepo)

	application.Fiber.Get("/openapi.json", func(c fiber.Ctx) error {
		b, err := json.Marshal(s2sAPI.OpenAPI())
		if err != nil {
			return err
		}
		c.Set("Content-Type", "application/json")
		return c.Send(b)
	})

	permissions.NewDistributor(application.DB.DB(), permissions.Live(), nil).Start(ctx)

	go worker.Run(ctx)
	go scanWorker.Run(ctx)

	slog.Info("trust service starting",
		"addr", fmt.Sprintf("%s:%d", cfg.TrustService.Host, cfg.TrustService.Port),
		"dbname", cfg.TrustDatabase.DBName,
	)

	defer func() {
		if err := trustDB.Close(); err != nil {
			slog.Error("close trust db", "error", err)
		}
	}()

	if err := application.Run(cfg.TrustService.Host, cfg.TrustService.Port); err != nil {
		slog.Error("run", "error", err)
		os.Exit(1)
	}
}
