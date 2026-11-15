package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/service"
	"api/pkg/logger"
)

func main() {
	dsn := flag.String("dsn", "", "catalog DSN (REQUIRED)")
	apply := flag.Bool("apply", false, "write the items (default: count them)")
	flag.Parse()
	logger.Init("development")
	if *dsn == "" {
		slog.Error("backfill-activity-states: -dsn is required")
		os.Exit(2)
	}
	db, err := database.OpenJob(*dsn)
	if err != nil {
		slog.Error("open catalog", "error", err)
		os.Exit(1)
	}
	n, err := service.BackfillPlayStateFeed(context.Background(), db, *apply)
	if err != nil {
		slog.Error("backfill-activity-states failed", "error", err)
		os.Exit(1)
	}
	if *apply {
		fmt.Printf("queued %d play-state items; the catalog pusher sends them at 4 batches of 100 a second\n", n)
		return
	}
	fmt.Printf("DRY RUN: %d play-state items would be queued (re-run with -apply)\n", n)
}
