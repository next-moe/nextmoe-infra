package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"api/internal/jobs/asmrclaims"
	"api/pkg/config"
	"api/pkg/logger"
)

func main() {
	apply := flag.Bool("apply", false, "write changes (default: dry run — report candidates only)")
	limit := flag.Int("limit", 0, "max candidate rows to process (0 = all)")
	dsn := flag.String("dsn", "", "catalog DSN — REQUIRED; the rehearsal copy locally (kun_catalog_rehearsal), the live catalog only in the production run")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}
	logger.Init(cfg.Server.Env)

	sum, err := asmrclaims.Run(context.Background(), cfg, asmrclaims.Opts{
		Apply: *apply,
		Limit: *limit,
		DSN:   *dsn,
	})
	if sum != nil {
		slog.Info("clear-asmr-self-claims summary", "summary", sum)
	}
	if err != nil {
		slog.Error("clear-asmr-self-claims", "error", err)
		os.Exit(1)
	}
}
