package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"api/internal/jobs/officialcovers"
	"api/pkg/config"
	"api/pkg/logger"

	"github.com/joho/godotenv"
)

func main() {
	apply := flag.Bool("apply", false, "upload and write (default: dry-run plan only)")
	dsn := flag.String("dsn", "", "catalog DSN — REQUIRED; a local copy for rehearsal, the live catalog only in the production run")
	ids := flag.String("ids", "", "restrict to these catalog_work ids (comma separated)")
	limit := flag.Int("limit", 0, "max covers to upload in --apply (0 = all)")
	imageBaseURL := flag.String("image-base-url", "", "image_service base override")
	uploadGap := flag.Duration("upload-gap", 0, "min delay between uploads")
	vndbBase := flag.String("vndb-api-base", "", "VNDB API base override (default https://api.vndb.org/kana)")
	bgmBase := flag.String("bangumi-api-base", "", "Bangumi API base override (default https://api.bgm.tv)")
	flag.Parse()

	_ = godotenv.Load("apps/api/.env")

	workIDs, err := parseIDs(*ids)
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

	stats, err := officialcovers.Run(context.Background(), cfg, officialcovers.Opts{
		DSN: *dsn, Apply: *apply, IDs: workIDs, Limit: *limit,
		ImageBaseURL: *imageBaseURL, UploadGap: *uploadGap,
		VNDBBase: *vndbBase, BgmBase: *bgmBase, BgmToken: os.Getenv("KUN_BANGUMI_TOKEN"),
	})
	if stats != nil {
		slog.Info("backfill-official-covers summary", "result", stats.String())
	}
	if err != nil {
		slog.Error("backfill-official-covers", "error", err)
		os.Exit(1)
	}
	if stats != nil && stats.Errors > 0 {
		os.Exit(1)
	}
}

func parseIDs(raw string) ([]int64, error) {
	var out []int64
	for _, part := range strings.Split(raw, ",") {
		if part = strings.TrimSpace(part); part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("--ids: %q is not a work id", part)
		}
		out = append(out, id)
	}
	return out, nil
}
