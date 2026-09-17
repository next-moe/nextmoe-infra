package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"api/internal/jobs/bangumicovers"
	"api/pkg/config"
	"api/pkg/logger"

	"github.com/joho/godotenv"
)

func main() {
	apply := flag.Bool("apply", false, "write changes (default: dry-run forecast only)")
	dsn := flag.String("dsn", "", "catalog DSN — REQUIRED; the rehearsal copy locally (kun_catalog_rehearsal), the live catalog only in the production run")
	mirror := flag.String("bangumi-mirror", "", "local mirror root — REQUIRED (<dir>/<subject_id>/cover.jpg + <dir>/dims.jsonl)")
	subjectsOut := flag.String("subjects-out", "", "dry run: write the subject ids with no bangumi cover yet whose image the mirror lacks, one per line (fetch-bangumi-images --ids-file)")
	limit := flag.Int("limit", 0, "max candidate works to process (0 = all)")
	offset := flag.Int("offset", 0, "skip this many candidate works (for chunking)")
	imageBaseURL := flag.String("image-base-url", "", "image_service base override (point at the LOCAL dev service, e.g. http://127.0.0.1:9278)")
	uploadGap := flag.Duration("upload-gap", 0, "min delay between uploads (0 = none; raise for a gentle production sweep)")
	allowLandscape := flag.Bool("allow-landscape", false, "also write landscape covers (never portrait-pinned); default keeps the portrait-only behavior")
	flag.Parse()

	_ = godotenv.Load("apps/api/.env")

	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}
	logger.Init(cfg.Server.Env)

	sum, err := bangumicovers.Run(context.Background(), cfg, bangumicovers.Opts{
		Apply:          *apply,
		Limit:          *limit,
		Offset:         *offset,
		DSN:            *dsn,
		BangumiMirror:  *mirror,
		SubjectsOut:    *subjectsOut,
		ImageBaseURL:   *imageBaseURL,
		UploadGap:      *uploadGap,
		AllowLandscape: *allowLandscape,
	})
	if sum != nil {
		slog.Info("backfill-bangumi-covers summary", "summary", sum)
	}
	if err != nil {
		slog.Error("backfill-bangumi-covers", "error", err)
		os.Exit(1)
	}
	if n, _ := sum["errors"].(int); n > 0 {
		os.Exit(1)
	}
}
