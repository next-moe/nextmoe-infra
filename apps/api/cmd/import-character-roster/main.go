package main

import (
	"flag"
	"log/slog"
	"os"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/importer"
	"api/pkg/config"
	"api/pkg/logger"

	"github.com/joho/godotenv"
	"gorm.io/gorm"
)

func main() {
	source := flag.String("source", "all", "bangumi | eg | vndb | all")
	apply := flag.Bool("apply", false, "write (default: dry run — plan counts only)")
	limit := flag.Int("limit", 0, "cap works processed per wave (0 = all)")
	egDSN := flag.String("eg-dsn", "", "erogamescape staging DSN (default: erogamescape on the catalog server)")
	flag.Parse()

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

	var egDB *gorm.DB
	if *source == "eg" || *source == "all" {
		egDB = openEG(cfg, *egDSN)
	}

	im := importer.New(catalogDB.DB(), egDB, importer.Options{Source: *source, DryRun: !*apply, Limit: *limit})
	stats, err := im.RunRoster(*source)
	if err != nil {
		slog.Error("roster import failed", "error", err)
		os.Exit(1)
	}
	slog.Info("roster import summary",
		"source", *source,
		"characters_created", stats.CharactersCreated,
		"attached_existing", stats.AttachedExisting,
		"aliases_created", stats.AliasesCreated,
		"edges_written", stats.EdgesWritten,
		"already", stats.Already,
		"skipped_no_work_anchor", stats.SkippedNoWorkAnchor,
		"skipped_no_name", stats.SkippedNoName,
		"skipped_foreign_roster", stats.SkippedForeignRoster,
		"skipped_claimed_probable", stats.SkippedClaimedProbable,
		"skipped_retired_exact_squat", stats.SkippedRetiredExactSquat,
		"portrait_candidates", stats.PortraitCandidates,
		"errors", stats.Errors,
	)

	if !*apply {
		slog.Info("DRY RUN — nothing written; re-run with --apply")
	}
	if stats.Errors > 0 {
		os.Exit(1)
	}
}

func openEG(cfg *config.Config, dsn string) *gorm.DB {
	if dsn == "" {
		egCfg := cfg.CatalogDatabase
		egCfg.DBName = "erogamescape"
		dsn = egCfg.DSN()
	}
	db, err := database.OpenJob(dsn)
	if err != nil {
		slog.Error("erogamescape connect", "error", err)
		os.Exit(1)
	}
	return db
}
