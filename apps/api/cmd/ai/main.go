package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"api/internal/app"
	"api/internal/infrastructure/cache"
	"api/internal/infrastructure/database"
	"api/internal/middleware"
	aiHandler "api/internal/platform/ai/handler"
	aiPerm "api/internal/platform/ai/perm"
	"api/internal/platform/ai/service"
	"api/internal/platform/ai/upstream"
	"api/internal/platform/llm"
	"api/internal/platform/llm/proxy"
	"api/internal/platform/permissions"
	"api/internal/platform/settings"
	"api/internal/platform/settings/keys"
	siteRepo "api/internal/platform/site/repository"
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

	health.MaybeProbe(cfg.AIService.Port, "/healthz")

	logger.Init(cfg.Server.Env)

	application, err := app.New(cfg, app.Options{Name: "kun-ai"})
	if err != nil {
		slog.Error("app init", "error", err)
		os.Exit(1)
	}

	permCtx, cancelPerm := context.WithCancel(context.Background())
	defer cancelPerm()
	settings.NewDistributor(application.DB.DB(), keys.Live(), nil).Start(permCtx)

	aiDB, err := database.NewPostgresDB(cfg.AIDatabase)
	if err != nil {
		slog.Error("ai db connect", "error", err)
		os.Exit(1)
	}

	kek, err := llm.ParseKEK(cfg.LLM.KEK)
	if err != nil {
		slog.Error("llm kek", "error", err)
		os.Exit(1)
	}
	if kek == nil {
		slog.Warn("KUN_LLM_KEK is not set — provider API keys cannot be saved from the admin console")
	}
	if err := llm.SeedDefaults(permCtx, aiDB.DB(), kek, llm.BootstrapFromConfig(cfg)); err != nil {
		slog.Error("llm seed", "error", err)
		os.Exit(1)
	}
	gw := llm.New(aiDB.DB(), kek)
	if redisCache, err := cache.NewRedisCache(cfg.Redis); err == nil && redisCache.Enabled() {
		gw.SetCoordinator(llm.NewRedisCoord(redisCache.Storage().Conn()))
		defer redisCache.Close()
	} else {
		slog.Warn("llm gateway: no redis, so route cooldowns and limits stay inside this process", "error", err)
	}
	if err := gw.Refresh(permCtx); err != nil {
		slog.Error("llm gateway snapshot", "error", err)
		os.Exit(1)
	}
	gw.Start(permCtx)

	up := upstream.NewClient(gw, llm.SceneModerationLLM)
	if up.Configured() {
		slog.Info("ai llm (tier2) configured", "scene", llm.SceneModerationLLM, "model", up.Model())
	} else {
		slog.Warn("ai llm (tier2) NOT configured — moderate-text runs Tier1 alone / degraded", "scene", llm.SceneModerationLLM)
	}

	omni := upstream.NewOmniClient(gw, llm.SceneModerationOmni)
	if omni.Configured() {
		slog.Info("ai omni (tier1) configured", "scene", llm.SceneModerationOmni, "model", omni.Model(),
			"escalate_threshold", keys.AIEscalateThreshold.Get(), "negative_sample_rate", keys.AINegativeSampleRate.Get())
	} else {
		slog.Warn("ai omni (tier1) NOT configured — moderate-text runs the Tier2 LLM path only", "scene", llm.SceneModerationOmni)
	}

	moderationSvc := service.NewModerationService(aiDB.DB(), omni, up, service.ModerationOptions{})
	if pairs := keys.AIForceEscalate.Get(); len(pairs) > 0 {
		slog.Info("ai cascade forced escalation", "pairs", pairs)
	}
	statsSvc := service.NewStatsService(aiDB.DB())
	budgetSvc := service.NewBudgetService(aiDB.DB())

	application.Fiber.Use(middleware.RequestID())
	application.Fiber.Use(middleware.Logger())
	application.Fiber.Get("/healthz", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})
	application.Fiber.Use(middleware.CORS(cfg.Server.CORSOrigin))

	clientRepo := siteRepo.NewOAuthClientRepository(application.DB.DB())
	application.Fiber.Use("/api/v1/ai", aiHandler.S2SAuth(clientRepo))

	tokenVerifier := oidctoken.NewVerifierWithJWKS(cfg.JWT.Secret, cfg.OIDC.JWKSURL)
	application.Fiber.Use("/api/v1/admin/ai",
		middleware.JWTAuth(tokenVerifier), middleware.RequirePermission(aiPerm.Resolver, aiPerm.UsageView))
	application.Fiber.Use("/api/v1/admin/ai/llm", middleware.RequirePermission(aiPerm.Resolver, aiPerm.LLMManage))

	s2sAPI := aiHandler.Setup(application.Fiber, moderationSvc)
	llmAdmin := llm.NewAdmin(aiDB.DB(), kek, gw)
	aiHandler.SetupAdmin(application.Fiber, statsSvc, budgetSvc, llmAdmin)
	go llmAdmin.RunUsageJobs(permCtx)
	go func() {
		time.Sleep(5 * time.Minute)
		for {
			llmAdmin.SyncAllProviders(permCtx)
			time.Sleep(24 * time.Hour)
		}
	}()
	proxy.Mount(application.Fiber, &proxy.Server{GW: gw})

	application.Fiber.Get("/openapi.json", func(c fiber.Ctx) error {
		b, err := json.Marshal(s2sAPI.OpenAPI())
		if err != nil {
			return err
		}
		c.Set("Content-Type", "application/json")
		return c.Send(b)
	})

	permissions.NewDistributor(application.DB.DB(), permissions.Live(), nil).Start(permCtx)

	slog.Info("ai service starting",
		"addr", fmt.Sprintf("%s:%d", cfg.AIService.Host, cfg.AIService.Port),
		"dbname", cfg.AIDatabase.DBName,
	)

	defer func() {
		if err := aiDB.Close(); err != nil {
			slog.Error("close ai db", "error", err)
		}
	}()

	if err := application.Run(cfg.AIService.Host, cfg.AIService.Port); err != nil {
		slog.Error("run", "error", err)
		os.Exit(1)
	}
}
