package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"time"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/ratingsummary"
	"api/pkg/config"
	"api/pkg/logger"

	"github.com/joho/godotenv"
)

func main() {
	dsn := flag.String("dsn", "", "catalog DSN (REQUIRED)")
	apply := flag.Bool("apply", false, "write changes (default dry)")
	bump := flag.Bool("bump", false, "after re-cooking, stamp changed_at on every rating with a summary so site mirrors pull it again; only once forum, moyu and letmoe read the cooked fields")
	batch := flag.Int("batch", 200, "ratings per changed_at batch")
	pause := flag.Duration("pause", time.Second, "pause between changed_at batches")
	flag.Parse()

	_ = godotenv.Load("apps/api/.env")
	if cfg, err := config.Load(); err == nil {
		logger.Init(cfg.Server.Env)
	}
	if *dsn == "" {
		slog.Error("recook-rating-summaries: --dsn is required; refusing to guess")
		os.Exit(2)
	}
	if *batch <= 0 {
		slog.Error("recook-rating-summaries: --batch must be positive")
		os.Exit(2)
	}
	db, err := database.OpenJob(*dsn)
	if err != nil {
		slog.Error("recook-rating-summaries: open catalog", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	mode := "DRY RUN"
	if *apply {
		mode = "APPLY"
	}
	st, err := ratingsummary.Cook(ctx, db, ratingsummary.Scope{EveryRow: true, DryRun: !*apply})
	if err != nil {
		slog.Error("recook-rating-summaries: cook failed", "error", err)
		os.Exit(1)
	}
	fmt.Printf("\n=== recook-rating-summaries %s ===\n", mode)
	fmt.Printf("scanned=%d recooked=%d\n", st.Scanned, st.Cooked)
	if !*bump {
		return
	}
	bs, err := ratingsummary.Bump(ctx, db, *batch, *pause, !*apply)
	if err != nil {
		slog.Error("recook-rating-summaries: bump failed", "error", err, "bumped", bs.Bumped)
		os.Exit(1)
	}
	fmt.Printf("bumped=%d batches=%d\n", bs.Bumped, bs.Batches)
	slog.Info("recook-rating-summaries summary", "apply", *apply, "scanned", st.Scanned, "recooked", st.Cooked, "bumped", bs.Bumped)
}
