package main

import (
	"flag"
	"fmt"
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
	limit := flag.Int("limit", 0, "cap groups minted (0 = all); attaches are uncapped")
	dlsiteDSN := flag.String("dlsite-dsn", "", "DLsite staging DSN (default: dlsite db on the catalog server)")
	receipts := flag.String("receipts", "", "JSONL path, one line per planned action (dry and apply)")
	holdout := flag.Bool("holdout-report", false, "read-only hold-out of VNDB-anchored DLsite products")
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

	dlsiteDB, err := openStaging(cfg.CatalogDatabase, *dlsiteDSN, "dlsite")
	if err != nil {
		slog.Error("dlsite db connect", "error", err)
		os.Exit(1)
	}
	if s, err := dlsiteDB.DB(); err == nil {
		defer s.Close()
	}

	im := importer.New(catalogDB.DB(), nil, importer.Options{DryRun: !*apply, Limit: *limit})
	if *holdout {
		rep, err := im.ReportDLsiteGamesHoldout(dlsiteDB)
		if err != nil {
			slog.Error("dlsite-games holdout failed", "error", err)
			os.Exit(1)
		}
		for _, line := range rep.Lines() {
			fmt.Println(line)
		}
		if err := importer.WriteDLsiteGamesReceipts(*receipts, rep.Wrong); err != nil {
			slog.Error("holdout receipts", "error", err)
			os.Exit(1)
		}
		return
	}

	st, err := im.RunDLsiteGamesWith(dlsiteDB, importer.DLsiteGamesRun{Receipts: *receipts})
	if err != nil {
		slog.Error("dlsite-games wave failed", "error", err)
		os.Exit(1)
	}
	slog.Info("dlsite-games wave summary",
		"population", st.Population, "total_groups", st.TotalGroups, "pack_products", st.PackProducts,
		"edition_groups", st.EditionGroups, "declared_groups", st.DeclaredGroups, "split_groups", st.SplitGroups,
		"title_attached_groups", st.TitleAttachedGroups, "rejected_skips", st.RejectedSkips,
		"quarantined_groups", st.QuarantinedGroups, "minted_groups", st.MintedGroups, "folded_groups", st.FoldedGroups,
		"releases_planned", st.ReleasesPlanned, "refs_planned", st.RefsPlanned, "candidates_planned", st.CandidatesPlanned,
		"limited_groups", st.LimitedGroups, "written", st.Written, "errors", st.Errors,
	)
	if !*apply {
		slog.Info("DRY RUN — nothing written; re-run with --run")
	}
	if st.Errors > 0 {
		os.Exit(1)
	}
}
