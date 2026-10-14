package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/repository"
	"api/internal/platform/catalog/service"
	"api/pkg/config"
	"api/pkg/logger"

	"github.com/joho/godotenv"
	"gorm.io/gorm"
)

const noteTag = "rule:label-official-collision step-44"

func main() {
	actor := flag.Int64("actor", 0, "operator user id recorded as decider/approver/executor (required)")
	mode := flag.String("mode", "mechanical", "mechanical | export | receipts | execute")
	run := flag.Bool("run", false, "write (default: dry-run preview)")
	out := flag.String("out", "", "export mode: dossier JSONL path (required for export)")
	receipts := flag.String("receipts", "", "receipts mode: adjudicated TSV path (required for receipts)")
	limit := flag.Int("limit", 0, "execute mode: max proposals to execute this run (0 = all cooled)")
	flag.Parse()

	if *actor <= 0 {
		fmt.Fprintln(os.Stderr, "-actor <user-id> is required")
		os.Exit(2)
	}

	_ = godotenv.Load("apps/api/.env")

	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}
	logger.Init(cfg.Server.Env)

	catalogDB, err := database.NewPostgresDB(cfg.CatalogDatabase)
	if err != nil {
		slog.Error("catalog db connect", "error", err)
		os.Exit(1)
	}
	defer catalogDB.Close()
	db := catalogDB.DB()
	ctx := context.Background()

	switch *mode {
	case "mechanical":
		err = runMechanical(ctx, db, os.Stdout, *actor, *run)
	case "export":
		if *out == "" {
			fmt.Fprintln(os.Stderr, "-out <jsonl> is required for export")
			os.Exit(2)
		}
		err = runExport(db, *out)
	case "receipts":
		if *receipts == "" {
			fmt.Fprintln(os.Stderr, "-receipts <tsv> is required for receipts")
			os.Exit(2)
		}
		err = runReceipts(ctx, db, os.Stdout, *receipts, *actor, *run)
	case "execute":
		err = runExecute(ctx, db, os.Stdout, *actor, *limit, *run)
	default:
		fmt.Fprintf(os.Stderr, "unknown mode %q\n", *mode)
		os.Exit(2)
	}
	if err != nil {
		slog.Error("label-candidate-batch failed", "mode", *mode, "error", err)
		os.Exit(1)
	}
}

func services(db *gorm.DB) (*service.AdminQueueService, *service.MergeService) {
	resolve := service.NewResolveService(repository.NewRedirectRepository(db))
	merge := service.NewMergeService(db, resolve)
	return service.NewAdminQueueService(db, merge), merge
}
