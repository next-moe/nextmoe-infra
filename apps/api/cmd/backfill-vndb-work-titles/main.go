package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"api/internal/jobs/vndbtitles"
	"api/pkg/config"
	"api/pkg/logger"

	"github.com/joho/godotenv"
)

func main() {
	apply := flag.Bool("apply", false, "write changes (default: dry run, counters only)")
	dsn := flag.String("dsn", "", "catalog DSN — REQUIRED; refusing to guess")
	limit := flag.Int("limit", 0, "max population works (0 = all), ascending work_id")
	flag.Parse()

	_ = godotenv.Load("apps/api/.env")

	if cfg, err := config.Load(); err == nil {
		logger.Init(cfg.Server.Env)
	}

	st, err := vndbtitles.Run(context.Background(), vndbtitles.Opts{
		Apply: *apply,
		DSN:   *dsn,
		Limit: *limit,
	})
	if err != nil {
		slog.Error("backfill-vndb-work-titles", "error", err)
		os.Exit(1)
	}
	summary := []any{"apply", *apply}
	summary = append(summary, st.LogArgs()...)
	slog.Info("backfill-vndb-work-titles summary", summary...)
	if !*apply {
		slog.Info("DRY RUN — nothing written; re-run with --apply")
	}
	if st.Errors > 0 {
		slog.Error("backfill-vndb-work-titles write failures", "errors", st.Errors, "first_error", st.FirstError)
		os.Exit(1)
	}
}
