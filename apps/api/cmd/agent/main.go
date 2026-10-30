package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"time"

	"api/internal/app"
	"api/internal/infrastructure/cache"
	"api/internal/infrastructure/database"
	"api/internal/middleware"
	agentHandler "api/internal/platform/agent/handler"
	"api/internal/platform/agent/service"
	"api/internal/platform/agent/stream"
	"api/internal/platform/agent/tools"
	"api/internal/platform/llm"
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
	health.MaybeProbe(cfg.AgentService.Port, "/healthz")
	logger.Init(cfg.Server.Env)

	application, err := app.New(cfg, app.Options{Name: "kun-agent", BodyLimit: 32 << 20})
	if err != nil {
		slog.Error("app init", "error", err)
		os.Exit(1)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	settings.NewDistributor(application.DB.DB(), keys.Live(), nil).Start(ctx)

	agentDB := mustDB("agent", cfg.AgentDatabase)
	aiDB := mustDB("ai", cfg.AIDatabase)
	redisCache, err := cache.NewRedisCache(cfg.Redis)
	if err != nil || !redisCache.Enabled() {
		slog.Error("agent needs redis for quotas and rate limits", "error", err)
		os.Exit(1)
	}
	rdb := redisCache.Storage().Conn()

	kek, err := llm.ParseKEK(cfg.LLM.KEK)
	if err != nil {
		slog.Error("llm kek", "error", err)
		os.Exit(1)
	}
	if err := llm.SeedDefaults(ctx, aiDB.DB(), kek, llm.BootstrapFromConfig(cfg)); err != nil {
		slog.Error("llm seed", "error", err)
		os.Exit(1)
	}
	gw := llm.New(aiDB.DB(), kek)
	gw.SetCoordinator(llm.NewRedisCoord(rdb))
	if err := gw.Refresh(ctx); err != nil {
		slog.Error("llm gateway snapshot", "error", err)
		os.Exit(1)
	}
	gw.Start(ctx)

	ledger := service.NewLedgerClient(cfg.LLM.OAuthBase, cfg.LLM.ClientID, cfg.LLM.ClientSecret)
	if ledger == nil {
		slog.Warn("agent: KUN_AGENT_CLIENT_ID/SECRET not set — users get the daily free units only")
	}
	quota := service.NewQuota(rdb, ledger)

	store := service.NewStore(agentDB.DB())
	orphans, err := store.InterruptOrphans(ctx)
	if err != nil {
		slog.Error("interrupt orphaned runs", "error", err)
		os.Exit(1)
	}
	for _, run := range orphans {
		quota.ReleaseInterrupted(ctx, run)
	}
	if len(orphans) > 0 {
		slog.Warn("marked runs left by the previous process as interrupted", "runs", len(orphans))
	}
	go func() {
		for range time.Tick(time.Hour) {
			if n, err := store.SweepUploads(context.Background(), time.Now().Add(-24*time.Hour)); err != nil {
				slog.Warn("agent: sweep unused uploads", "err", err)
			} else if n > 0 {
				slog.Info("agent: swept unused uploads", "n", n)
			}
		}
	}()

	catalogHTTP := &http.Client{Timeout: 15 * time.Second}
	catalogFor := func(siteID int64) *tools.CatalogClient {
		return &tools.CatalogClient{Base: cfg.LLM.CatalogBase, NSFW: keys.AgentCatalogNSFW.ForSite(uint(siteID)), HTTP: catalogHTTP}
	}
	public := tools.NewPublicHTTPClient()
	webReady := func() bool {
		_, ok := gw.Ready(llm.SceneWebSearch)
		return ok
	}
	webSearch := &tools.WebSearch{Search: func(ctx context.Context, query, instructions string) (llm.SearchResult, error) {
		return gw.WebSearch(ctx, llm.SceneWebSearch, query, instructions, "agent", 0)
	}}
	readURL := &tools.ReadURL{HTTP: public}
	siteTools := service.NewSiteTools(agentDB.DB(), &http.Client{Timeout: 30 * time.Second})
	toolsFor := func(siteID int64, userToken string) *tools.Registry {
		cat := catalogFor(siteID)
		set := []tools.Tool{&tools.SearchCatalog{Client: cat}, &tools.GetWork{Client: cat}}
		if webReady() {
			set = append(set, webSearch, readURL)
		}
		set = append(set, siteTools.For(context.Background(), siteID, userToken)...)
		return tools.NewRegistry(set...)
	}
	broker := stream.NewBroker()
	runner := service.NewRunner(store, gw, broker, quota, toolsFor)

	powVerifier, err := agentHandler.NewPowVerifier(cfg.LLM.PowKeys, rdb)
	if err != nil {
		slog.Error("pow keys", "error", err)
		os.Exit(1)
	}
	server := &agentHandler.Server{
		Store: store, Runner: runner, Broker: broker, Quota: quota,
		Limits: agentHandler.NewLimiter(rdb), Search: &agentHandler.Searcher{Catalog: catalogFor}, WebSearch: webReady,
		Vision:    func() bool { _, ok := gw.Ready(llm.SceneVision); return ok },
		SiteTools: siteTools,
	}
	if powVerifier != nil {
		server.Verify = powVerifier
	} else {
		slog.Warn("agent: KUN_AGENT_POW_KEYS not set — sites cannot require proof of work")
	}

	application.Fiber.Use(middleware.RequestID())
	application.Fiber.Use(middleware.Logger())
	application.Fiber.Get("/healthz", func(c fiber.Ctx) error { return c.JSON(fiber.Map{"status": "ok"}) })
	verifier := oidctoken.NewVerifierWithJWKS(cfg.JWT.Secret, cfg.OIDC.JWKSURL).RequiringIssuer(cfg.OIDC.Issuer)
	api := agentHandler.Setup(application.Fiber, server, verifier,
		siteRepo.NewOAuthClientRepository(application.DB.DB()), agentHandler.NewSiteResolver(application.DB.DB()))
	application.Fiber.Get("/openapi.json", func(c fiber.Ctx) error {
		b, err := json.Marshal(api.OpenAPI())
		if err != nil {
			return err
		}
		c.Set("Content-Type", "application/json")
		return c.Send(b)
	})

	slog.Info("agent service starting", "port", cfg.AgentService.Port, "dbname", cfg.AgentDatabase.DBName)
	if err := application.Run(cfg.AgentService.Host, cfg.AgentService.Port); err != nil {
		slog.Error("run", "error", err)
	}
	runner.Wait()
	_ = redisCache.Close()
	_ = agentDB.Close()
	_ = aiDB.Close()
}

func mustDB(name string, c config.DatabaseConfig) *database.PostgresDB {
	db, err := database.NewPostgresDB(c)
	if err != nil {
		slog.Error("db connect", "db", name, "error", err)
		os.Exit(1)
	}
	return db
}
