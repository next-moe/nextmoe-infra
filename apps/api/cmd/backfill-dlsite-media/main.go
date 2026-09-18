package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"api/internal/jobs/dlsitemedia"
	"api/pkg/config"
	"api/pkg/logger"
)

func main() {
	apply := flag.Bool("apply", false, "write changes (default: dry-run forecast only)")
	kind := flag.String("kind", "all", "which media to backfill: all | intro | cover | screenshot (comma-separated)")
	limit := flag.Int("limit", 0, "max candidate works to process (0 = all)")
	offset := flag.Int("offset", 0, "skip this many BODYLESS candidate works (for chunking); the claimed screenshot lane is always taken from its head — it self-resumes, so offsetting into it would skip works")
	dsn := flag.String("dsn", "", "catalog DSN — REQUIRED; the rehearsal copy locally (kun_catalog_rehearsal), the live catalog only in the production run")
	dlsiteDSN := flag.String("dlsite-dsn", "", "dlsite staging DSN — REQUIRED; reads product_json/page_json")
	mirrorDir := flag.String("mirror-dir", "", "local mirror root <root>/<workno>/<filename> (required for cover/screenshot)")
	worknosOut := flag.String("worknos-out", "", "write the worknos with a planned cover or screenshot the mirror lacks, one per line (kun-dlsite-api mirror --worknos-file)")
	cdnMissing := flag.String("cdn-missing", "", "file of <workno>/<filename> lines the DLsite CDN answered 404 for: such a file the mirror lacks is counted as cdn_missing and does not list its work in --worknos-out")
	imageBaseURL := flag.String("image-base-url", "", "image_service base override (point at the LOCAL dev service, e.g. http://127.0.0.1:9278)")
	uploadGap := flag.Duration("upload-gap", 0, "min delay between uploads (0 = none; raise for a gentle production sweep)")
	flag.Parse()

	kinds, err := dlsitemedia.ParseKinds(*kind)
	if err != nil {
		slog.Error("bad --kind", "error", err)
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}
	logger.Init(cfg.Server.Env)

	sum, err := dlsitemedia.Run(context.Background(), cfg, dlsitemedia.Opts{
		Apply:        *apply,
		Kinds:        kinds,
		Limit:        *limit,
		Offset:       *offset,
		DSN:          *dsn,
		DlsiteDSN:    *dlsiteDSN,
		MirrorDir:    *mirrorDir,
		WorknosOut:   *worknosOut,
		CDNMissing:   *cdnMissing,
		ImageBaseURL: *imageBaseURL,
		UploadGap:    *uploadGap,
	})
	if sum != nil {
		slog.Info("backfill-dlsite-media summary", "summary", sum)
	}
	if err != nil {
		slog.Error("backfill-dlsite-media", "error", err)
		os.Exit(1)
	}
	if n, _ := sum["errors"].(int); n > 0 {
		os.Exit(1)
	}
}
