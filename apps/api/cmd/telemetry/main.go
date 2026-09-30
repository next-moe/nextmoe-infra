package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"api/internal/app"
	"api/internal/infrastructure/database"
	"api/internal/middleware"
	"api/internal/platform/permissions"
	"api/internal/platform/settings"
	"api/internal/platform/settings/keys"
	telemetryHandler "api/internal/platform/telemetry/handler"
	"api/internal/platform/telemetry/ingest"
	telemetryPerm "api/internal/platform/telemetry/perm"
	"api/internal/platform/telemetry/store"
	"api/internal/platform/telemetry/symbols"
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

	health.MaybeProbe(cfg.TelemetryService.Port, "/healthz")

	logger.Init(cfg.Server.Env)

	application, err := app.New(cfg, app.Options{Name: "kun-telemetry", StreamRequestBody: true})
	if err != nil {
		slog.Error("app init", "error", err)
		os.Exit(1)
	}

	permCtx, cancelPerm := context.WithCancel(context.Background())
	defer cancelPerm()
	settings.NewDistributor(application.DB.DB(), keys.Live(), nil).Start(permCtx)

	telDB, err := database.NewPostgresDB(cfg.TelemetryDatabase)
	if err != nil {
		slog.Error("telemetry db connect", "error", err)
		os.Exit(1)
	}

	st := store.New(telDB.DB())
	blobs, err := newSymbolBlobs(cfg)
	if err != nil {
		slog.Error("telemetry symbol store", "error", err)
		os.Exit(1)
	}
	st.SetBlobStore(blobs)
	if err := st.EnsurePartitions(permCtx, time.Now().UTC()); err != nil {
		slog.Error("telemetry ensure partitions", "error", err)
		os.Exit(1)
	}

	cache := ingest.NewKeyCache(st, nil, slog.Default())
	loadCtx, cancelLoad := context.WithTimeout(permCtx, 3*time.Second)
	if err := cache.Reload(loadCtx); err != nil {
		slog.Error("telemetry key cache initial load", "error", err)
	}
	cancelLoad()
	go cache.Run(permCtx)
	lim := ingest.NewLimiter(nil)
	ing := ingest.NewHandler(cache, lim, st, nil, slog.Default())

	application.Fiber.Get("/healthz", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})
	application.Fiber.Post("/v1/logs", ing.Logs)
	sym := symbols.NewHandler(st, st, slog.Default())
	application.Fiber.Post("/v1/symbols", sym.Symbols)

	tokenVerifier := oidctoken.NewVerifierWithJWKS(cfg.JWT.Secret, cfg.OIDC.JWKSURL)
	application.Fiber.Use("/api/v1/admin/telemetry",
		middleware.Logger(),
		middleware.JWTAuth(tokenVerifier),
		middleware.RequirePermission(telemetryPerm.Resolver, telemetryPerm.View),
	)
	telemetryHandler.SetupAdmin(application.Fiber, st, cache)

	permissions.NewDistributor(application.DB.DB(), permissions.Live(), nil).Start(permCtx)

	go runLoops(permCtx, st)
	fetcher := symbols.NewFetcher(st, blobs, cfg.TelemetrySymbols.EngineSymbolsBaseURL, slog.Default())
	go runEngineFetch(permCtx, st, fetcher)

	slog.Info("telemetry service starting",
		"addr", fmt.Sprintf("%s:%d", cfg.TelemetryService.Host, cfg.TelemetryService.Port),
		"dbname", cfg.TelemetryDatabase.DBName,
	)

	defer func() {
		if err := telDB.Close(); err != nil {
			slog.Error("close telemetry db", "error", err)
		}
	}()

	if err := application.Run(cfg.TelemetryService.Host, cfg.TelemetryService.Port); err != nil {
		slog.Error("run", "error", err)
		os.Exit(1)
	}
}

func runLoops(ctx context.Context, st *store.Store) {
	runHourly(ctx, st)
	five := time.NewTicker(5 * time.Minute)
	hour := time.NewTicker(time.Hour)
	defer five.Stop()
	defer hour.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-five.C:
			runFive(ctx, st)
		case <-hour.C:
			runHourly(ctx, st)
		}
	}
}

func runFive(ctx context.Context, st *store.Store) {
	if err := st.TryMaintenance(ctx, func(ctx context.Context) error {
		today := dateUTC(time.Now())
		return st.Rollup(ctx, today.AddDate(0, 0, -2), today)
	}); err != nil {
		slog.Error("telemetry 5m rollup", "err", err)
	}
}

func runHourly(ctx context.Context, st *store.Store) {
	if err := st.TryMaintenance(ctx, func(ctx context.Context) error {
		now := time.Now().UTC()
		today := dateUTC(now)
		if err := st.EnsurePartitions(ctx, now); err != nil {
			return err
		}
		if err := st.DropExpiredPartitions(ctx, now); err != nil {
			return err
		}
		if err := st.PurgeExpired(ctx, now); err != nil {
			return err
		}
		if err := st.PurgeExpiredSymbols(ctx, now); err != nil {
			return err
		}
		return st.Rollup(ctx, today.AddDate(0, 0, -29), today)
	}); err != nil {
		slog.Error("telemetry hourly maintenance", "err", err)
	}
}

func dateUTC(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func newSymbolBlobs(cfg *config.Config) (symbols.BlobStore, error) {
	if cfg.TelemetrySymbols.S3.Bucket != "" {
		s3store, err := symbols.NewS3Store(cfg.TelemetrySymbols.S3)
		if err != nil {
			return nil, err
		}
		slog.Info("telemetry symbol blobs", "backend", "s3", "bucket", cfg.TelemetrySymbols.S3.Bucket)
		return s3store, nil
	}
	slog.Info("telemetry symbol blobs", "backend", "filesystem", "dir", cfg.TelemetrySymbols.Dir)
	return symbols.NewFSStore(cfg.TelemetrySymbols.Dir), nil
}

func runEngineFetch(ctx context.Context, st *store.Store, f *symbols.Fetcher) {
	run := func() {
		if err := st.TryEngineFetch(ctx, f.Cycle); err != nil {
			slog.Error("telemetry engine fetch", "err", err)
		}
	}
	run()
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			run()
		}
	}
}
