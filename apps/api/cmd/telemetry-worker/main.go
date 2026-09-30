package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"api/internal/infrastructure/database"
	"api/internal/platform/telemetry/store"
	"api/internal/platform/telemetry/symbolicate"
	"api/internal/platform/telemetry/symbols"
	"api/internal/platform/telemetry/worker"
	"api/pkg/config"
	"api/pkg/logger"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}
	logger.Init(cfg.Server.Env)

	telDB, err := database.NewPostgresDB(cfg.TelemetryDatabase)
	if err != nil {
		slog.Error("telemetry db connect", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := telDB.Close(); err != nil {
			slog.Error("close telemetry db", "error", err)
		}
	}()

	st := store.New(telDB.DB())
	blobs, err := newSymbolBlobs(cfg)
	if err != nil {
		slog.Error("telemetry symbol store", "error", err)
		os.Exit(1)
	}
	st.SetBlobStore(blobs)

	cache := &worker.SymbolCache{Dir: cfg.TelemetryWorker.SymbolCacheDir}
	tools := symbolicate.ExecTools{
		DecodeBin:      cfg.TelemetryWorker.DecodeBin,
		JavaBin:        cfg.TelemetryWorker.JavaBin,
		R8Jar:          cfg.TelemetryWorker.R8Jar,
		LLVMSymbolizer: cfg.TelemetryWorker.LLVMSymbolizer,
	}
	w := &worker.Worker{
		Store:  st,
		Tools:  tools,
		Lookup: worker.StoreLookup{Store: st, Cache: cache},
		Log:    slog.Default(),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slog.Info("telemetry worker starting", "dbname", cfg.TelemetryDatabase.DBName)
	if err := w.Run(ctx); err != nil && err != context.Canceled {
		slog.Error("telemetry worker exit", "error", err)
		os.Exit(1)
	}
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
