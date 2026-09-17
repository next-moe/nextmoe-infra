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

func openStaging(base config.DatabaseConfig, override, dbName string) (*gorm.DB, error) {
	dsn := override
	if dsn == "" {
		cfg := base
		cfg.DBName = dbName
		dsn = cfg.DSN()
	}
	return database.OpenJob(dsn)
}

func main() {
	apply := flag.Bool("run", false, "write (default: dry run — plan counts only)")
	limit := flag.Int("limit", 0, "cap works processed (0 = all)")
	egDSN := flag.String("eg-dsn", "", "erogamescape staging DSN (default: erogamescape db on the catalog server)")
	dlsiteDSN := flag.String("dlsite-dsn", "", "DLsite staging DSN (default: dlsite db on the catalog server)")
	resolveAmbiguous := flag.Bool("resolve-ambiguous", false, "step-29 three-layer handling of dlsite_ids claimed by several EG games (default: skip them)")
	conflicts := flag.String("export-conflicts", "", "write the B3 conflict worklist (SKU → several wiki works) to this TSV path")
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

	egDB, err := openStaging(cfg.CatalogDatabase, *egDSN, "erogamescape")
	if err != nil {
		slog.Error("erogamescape db connect", "error", err)
		os.Exit(1)
	}
	if s, err := egDB.DB(); err == nil {
		defer s.Close()
	}
	dlsiteDB, err := openStaging(cfg.CatalogDatabase, *dlsiteDSN, "dlsite")
	if err != nil {
		slog.Error("dlsite db connect", "error", err)
		os.Exit(1)
	}
	if s, err := dlsiteDB.DB(); err == nil {
		defer s.Close()
	}

	im := importer.New(catalogDB.DB(), egDB, importer.Options{
		DryRun: !*apply, Limit: *limit, ResolveAmbiguous: *resolveAmbiguous, ConflictsOut: *conflicts,
	})
	st, err := im.RunEGDLsite(dlsiteDB)
	if err != nil {
		slog.Error("eg-dlsite wave failed", "error", err)
		os.Exit(1)
	}
	slog.Info("eg-dlsite wave summary",
		"attached", st.Attached, "minted", st.Minted, "already", st.Already,
		"ambiguous", st.Ambiguous, "missing", st.Missing,
		"amb_b1_attach", st.AmbB1, "amb_b2_mint", st.AmbB2, "amb_b3_conflict", st.AmbConflicts,
		"releases", st.ReleasesCreated, "titles", st.TitlesCreated, "labels", st.LabelsCreated,
		"names", st.NamesCreated, "credits", st.CreditsWritten, "edges", st.EdgesWritten,
		"eg_refs", st.EGRefsWritten, "stubs", st.Stubs, "skipped_unmapped_role", st.SkippedUnmappedRole,
		"title_collisions", st.TitleCollisions, "quarantined", st.Quarantined,
		"skipped_intra_collision", st.SkippedIntraCollision, "eg_aliases", st.EGAliases,
		"errors", st.Errors,
	)
	if !*apply {
		slog.Info("DRY RUN — nothing written; re-run with --run")
	}
	if st.Errors > 0 {
		os.Exit(1)
	}
}
