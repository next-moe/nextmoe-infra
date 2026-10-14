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

const (
	waveTag49  = "rule:catalog-dedup step-49"
	waveTag50  = "rule:catalog-dedup step-50"
	waveTag98  = "rule:catalog-dedup step-98"
	waveTag106 = "rule:catalog-dedup step-106"
	waveTag154 = "rule:catalog-dedup step-154"
)

func noteTagFor(worklist, override string) string {
	if override != "" {
		return override
	}
	if worklist != "" {
		return waveTag154
	}
	return waveTag106
}

func main() {
	actor := flag.Int64("actor", 0, "operator user id recorded as proposer/approver/executor (required)")
	mode := flag.String("mode", "detect", "detect | propose | execute | cleanup")
	class := flag.String("class", "both", "propose/execute scope: character | credit_name | both (step 49) | orphan-creditname (all roles since step 98) | mixed-creditname (step 98)")
	run := flag.Bool("run", false, "write (default: dry-run preview)")
	limit := flag.Int("limit", 0, "propose: max GROUPS this run; execute: max proposals this run (0 = all)")
	worklist := flag.String("worklist", "", "propose/execute: drive the merges from this JSONL worklist instead of the SQL detectors (see worklist.go)")
	note := flag.String("note", "", "override the wave note tag stamped on proposals and matched by -mode execute (a later worklist wave must not share step-154's tag)")
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
	merge, resolve := mergeService(db)

	switch *mode {
	case "detect":
		err = runDetect(db, os.Stdout)
	case "propose":
		err = runPropose(ctx, db, os.Stdout, merge, *actor, *class, *worklist, *note, *limit, *run)
	case "execute":
		err = runExecute(ctx, db, os.Stdout, merge, resolve, *actor, noteTagFor(*worklist, *note), *limit, *run)
	case "cleanup":
		err = runCleanup(db, os.Stdout, *run)
	default:
		fmt.Fprintf(os.Stderr, "unknown mode %q\n", *mode)
		os.Exit(2)
	}
	if err != nil {
		slog.Error("catalog-dedup-batch failed", "mode", *mode, "error", err)
		os.Exit(1)
	}
}

func mergeService(db *gorm.DB) (*service.MergeService, *service.ResolveService) {
	resolve := service.NewResolveService(repository.NewRedirectRepository(db))
	return service.NewMergeService(db, resolve), resolve
}
