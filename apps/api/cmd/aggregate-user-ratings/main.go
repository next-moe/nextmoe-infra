package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/service"
	"api/pkg/config"
	"api/pkg/logger"

	"github.com/joho/godotenv"
)

func main() {
	dsn := flag.String("dsn", "", "catalog DSN (REQUIRED)")
	apply := flag.Bool("apply", false, "write changes (default dry)")
	flag.Parse()

	_ = godotenv.Load("apps/api/.env")
	if cfg, err := config.Load(); err == nil {
		logger.Init(cfg.Server.Env)
	}
	if *dsn == "" {
		slog.Error("aggregate-user-ratings: --dsn is required; refusing to guess")
		os.Exit(2)
	}
	db, err := database.OpenJob(*dsn)
	if err != nil {
		slog.Error("aggregate-user-ratings: open catalog", "error", err)
		os.Exit(1)
	}
	st, err := service.RefreshRatingAggregates(context.Background(), db, *apply)
	if err != nil {
		slog.Error("aggregate-user-ratings failed", "error", err)
		os.Exit(1)
	}
	mode := "DRY RUN"
	if *apply {
		mode = "APPLY"
	}
	fmt.Printf("\n=== aggregate-user-ratings %s ===\n", mode)
	fmt.Printf("rated=%d changed=%d deleted=%d\n", st.Rated, st.Written, st.Deleted)
	slog.Info("aggregate-user-ratings summary", "apply", *apply, "rated", st.Rated, "changed", st.Written, "deleted", st.Deleted)
}
