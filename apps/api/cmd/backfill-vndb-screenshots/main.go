package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"api/internal/jobs/vndbcovers"
	"api/internal/jobs/vndbshots"
	"api/pkg/config"
	"api/pkg/logger"

	"github.com/joho/godotenv"
)

func main() {
	apply := flag.Bool("apply", false, "write changes (default: dry-run forecast only)")
	dsn := flag.String("dsn", "", "catalog DSN — REQUIRED; the rehearsal copy locally (kun_catalog_rehearsal), the live catalog only in the production run")
	ids := flag.String("ids", "", "restrict to these catalog_work ids (comma separated); the no-screenshot predicate still applies")
	limit := flag.Int("limit", 0, "plan at most this many works, newest first (0 = all); pass the same value to the dry run and the apply")
	perWork := flag.Int("per-work", vndbshots.DefaultPerWork, "screenshots per work, in VNDB's order")
	imageDir := flag.String("image-dir", "", "local mirror of rsync://dl.vndb.org/vndb-img — REQUIRED; a work whose files it lacks is skipped whole")
	filesOut := flag.String("files-out", "", "dry run: write the mirror-relative paths the plan needs and --image-dir lacks, one per line (rsync --files-from)")
	imageBaseURL := flag.String("image-base-url", "", "image_service base override (point at the LOCAL dev service, e.g. http://127.0.0.1:9278)")
	uploadGap := flag.Duration("upload-gap", 0, "min delay between uploads (0 = none; raise for a gentle production sweep)")
	workers := flag.Int("workers", 1, "works filled concurrently; --upload-gap stays a single shared pace across all of them")
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

	stats, err := vndbshots.Run(context.Background(), cfg, vndbshots.Opts{
		DSN:          *dsn,
		Apply:        *apply,
		IDs:          workIDs,
		Limit:        *limit,
		PerWork:      *perWork,
		ImageDir:     *imageDir,
		FilesOut:     *filesOut,
		ImageBaseURL: *imageBaseURL,
		UploadGap:    *uploadGap,
		Workers:      *workers,
	})
	if stats != nil {
		slog.Info("backfill-vndb-screenshots summary", "result", stats.String())
	}
	if err != nil {
		slog.Error("backfill-vndb-screenshots", "error", err)
		os.Exit(1)
	}
	if stats != nil && stats.Errors > 0 {
		os.Exit(1)
	}
}
