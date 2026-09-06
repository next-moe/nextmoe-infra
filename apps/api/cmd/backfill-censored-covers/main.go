package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"api/internal/jobs/censoredcovers"
	"api/pkg/config"
	"api/pkg/logger"

	"github.com/joho/godotenv"
)

func main() {
	apply := flag.Bool("apply", false, "write changes (default: dry-run forecast only)")
	dsn := flag.String("dsn", "", "catalog DSN — REQUIRED; the rehearsal copy locally (kun_catalog_rehearsal), the live catalog only in the production run")
	ids := flag.String("ids", "", "restrict to these catalog_work ids (comma separated); the explicit-only predicate still applies")
	limit := flag.Int("limit", 0, "max ghosts to stage in --apply (0 = all)")
	offset := flag.Int("offset", 0, "skip this many candidate works (for chunking)")
	imageBaseURL := flag.String("image-base-url", "", "image_service base override (point at the LOCAL dev service, e.g. http://127.0.0.1:9278)")
	uploadGap := flag.Duration("upload-gap", 0, "min delay between uploads (0 = none; raise for a gentle production sweep)")
	flag.Parse()

	_ = godotenv.Load("apps/api/.env")

	workIDs, err := censoredcovers.ParseIDs(*ids)
	if err != nil {
		slog.Error("parse --ids", "error", err)
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}
	logger.Init(cfg.Server.Env)

	stats, err := censoredcovers.Run(context.Background(), cfg, censoredcovers.Opts{
		DSN:          *dsn,
		Apply:        *apply,
		Limit:        *limit,
		Offset:       *offset,
		IDs:          workIDs,
		ImageBaseURL: *imageBaseURL,
		UploadGap:    *uploadGap,
	})
	if stats != nil {
		slog.Info("backfill-censored-covers summary", "result", stats.String())
	}
	if err != nil {
		slog.Error("backfill-censored-covers", "error", err)
		os.Exit(1)
	}
	if stats != nil && stats.Errors > 0 {
		os.Exit(1)
	}
}
