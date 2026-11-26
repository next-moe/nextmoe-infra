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
	source := flag.String("source", "all", "bangumi | eg | eg-music | vndb | all")
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
	if *source == "eg" || *source == "eg-music" || *source == "all" {
		egDB = openEG(cfg, *egDSN)
	}

	im := importer.New(catalogDB.DB(), egDB, importer.Options{Source: *source, DryRun: !*apply, Limit: *limit})
	stats, err := im.Run(*source)
	if err != nil {
		slog.Error("import failed", "error", err)
		os.Exit(1)
	}
	slog.Info("import summary",
		"source", *source,
		"names_created", stats.NamesCreated,
		"labels_created", stats.LabelsCreated,
		"characters_created", stats.CharactersCreated,
		"characters_attached", stats.CharactersAttached,
		"credits_written", stats.CreditsWritten,
		"skipped_unmapped_role", stats.SkippedUnmappedRole,
		"skipped_gate", stats.SkippedGate,
		"skipped_claimed_probable_name", stats.SkippedClaimedProbableName,
		"skipped_claimed_probable_char", stats.SkippedClaimedProbableChar,
		"skipped_retired_exact_char", stats.SkippedRetiredExactChar,
		"already", stats.Already,
		"errors", stats.Errors,
	)

	if *source == "all" && egDB != nil {
		cs, err := im.RunCandidates()
		if err != nil {
			slog.Error("candidates failed", "error", err)
			os.Exit(1)
		}
		slog.Info("shared-handle candidates",
			"twitter_handles", cs.Twitter, "pixiv_handles", cs.Pixiv,
			"written", cs.Written, "already", cs.Already, "ambiguous_skipped", cs.AmbiguousSkipped)
	}

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
