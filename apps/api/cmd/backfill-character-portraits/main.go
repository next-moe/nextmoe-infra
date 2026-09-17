package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"api/internal/jobs/charportraits"
	"api/pkg/config"
	"api/pkg/logger"
)

func main() {
	apply := flag.Bool("apply", false, "write changes (default: dry run — resolve candidates + local-file forecast only)")
	limit := flag.Int("limit", 0, "max portrait_backfill rows to process (0 = all)")
	offset := flag.Int("offset", 0, "skip this many rows (for chunking)")
	dsn := flag.String("dsn", "", "catalog DSN — REQUIRED; the rehearsal copy locally (kun_catalog_rehearsal), the live catalog only in the production run")
	vndbImageDir := flag.String("vndb-image-dir", "", "local rsync mirror root containing ch/ (bytes are read from here) [required]")
	filesOut := flag.String("files-out", "", "dry run: write the mirror-relative paths of unfilled portraits --vndb-image-dir lacks, one per line (rsync --files-from)")
	imageBaseURL := flag.String("image-base-url", "", "image_service base override (point at the LOCAL compose/dev service, e.g. http://127.0.0.1:9278)")
	uploadGap := flag.Duration("upload-gap", 0, "min delay between uploads (0 = none; raise for a gentle production sweep)")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}
	logger.Init(cfg.Server.Env)

	sum, err := charportraits.Run(context.Background(), cfg, charportraits.Opts{
		Apply:        *apply,
		Limit:        *limit,
		Offset:       *offset,
		DSN:          *dsn,
		VNDBImageDir: *vndbImageDir,
		FilesOut:     *filesOut,
		ImageBaseURL: *imageBaseURL,
		UploadGap:    *uploadGap,
	})
	if sum != nil {
		slog.Info("backfill-character-portraits summary", "summary", sum)
	}
	if err != nil {
		slog.Error("backfill-character-portraits", "error", err)
		os.Exit(1)
	}
	if n, _ := sum["errors"].(int); n > 0 {
		os.Exit(1)
	}
}
