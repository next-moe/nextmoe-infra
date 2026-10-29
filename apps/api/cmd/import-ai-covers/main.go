package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"api/internal/jobs/aicovers"
	"api/pkg/config"
	"api/pkg/logger"

	"github.com/joho/godotenv"
)

func main() {
	apply := flag.Bool("apply", false, "upload and write (default: report only)")
	dsn := flag.String("dsn", "", "catalog DSN — REQUIRED")
	dir := flag.String("dir", "", "directory of <work_id>__<origin_hash>.<ext> products — REQUIRED")
	limit := flag.Int("limit", 0, "max works to import (0 = all)")
	imageBaseURL := flag.String("image-base-url", "", "image_service base override (e.g. http://127.0.0.1:9278)")
	uploadGap := flag.Duration("upload-gap", 0, "min delay between uploads (0 = none)")
	flag.Parse()

	_ = godotenv.Load("apps/api/.env")

	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}
	logger.Init(cfg.Server.Env)

	stats, err := aicovers.Run(context.Background(), cfg, aicovers.Opts{
		DSN: *dsn, Dir: *dir, Apply: *apply, Limit: *limit,
		ImageBaseURL: *imageBaseURL, UploadGap: *uploadGap,
	})
	if stats != nil {
		slog.Info("import-ai-covers summary", "apply", *apply, "result", stats.String())
	}
	if err != nil {
		slog.Error("import-ai-covers", "error", err)
		os.Exit(1)
	}
	if stats != nil && stats.Errors > 0 {
		os.Exit(1)
	}
}
