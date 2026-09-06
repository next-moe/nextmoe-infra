package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"api/internal/jobs/vndbcovers"
	"api/pkg/config"
	"api/pkg/logger"

	"github.com/joho/godotenv"
)

func main() {
	apply := flag.Bool("apply", false, "write changes (default: dry-run forecast only)")
	dsn := flag.String("dsn", "", "catalog DSN — REQUIRED; the rehearsal copy locally (kun_catalog_rehearsal), the live catalog only in the production run")
	ids := flag.String("ids", "", "restrict to these catalog_work ids (comma separated); the anchor / no-official-cover predicates still apply")
	limit := flag.Int("limit", 0, "max covers to upload in --apply (0 = all); the dry-run forecast always covers the whole population")
	offset := flag.Int("offset", 0, "skip this many candidate works (for chunking)")
	imageBaseURL := flag.String("image-base-url", "", "image_service base override (point at the LOCAL dev service, e.g. http://127.0.0.1:9278)")
	uploadGap := flag.Duration("upload-gap", 0, "min delay between uploads (0 = none; raise for a gentle production sweep)")
	apiBase := flag.String("vndb-api-base", "", "VNDB API base override (default https://api.vndb.org/kana)")
	flag.Parse()

	_ = godotenv.Load("apps/api/.env")

	workIDs, err := vndbcovers.ParseIDs(*ids)
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

	stats, err := vndbcovers.Run(context.Background(), cfg, vndbcovers.Opts{
		DSN:          *dsn,
		Apply:        *apply,
		Limit:        *limit,
		Offset:       *offset,
		IDs:          workIDs,
		ImageBaseURL: *imageBaseURL,
		UploadGap:    *uploadGap,
		APIBase:      *apiBase,
	})
	if stats != nil {
		slog.Info("backfill-vndb-covers summary", "result", stats.String())
	}
	if err != nil {
		slog.Error("backfill-vndb-covers", "error", err)
		os.Exit(1)
	}
	if stats != nil && stats.Errors > 0 {
		os.Exit(1)
	}
}
