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
)

func main() {
	apply := flag.Bool("run", false, "write (default: dry run — plan counts only)")
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

	im := importer.New(catalogDB.DB(), nil, importer.Options{DryRun: !*apply})
	st, err := im.RunBangumiXmedia()
	if err != nil {
		slog.Error("bangumi cross-media wave failed", "error", err)
		os.Exit(1)
	}
	slog.Info("bangumi cross-media wave summary",
		"registered_anime", st.RegisteredAnime, "registered_manga", st.RegisteredManga,
		"registered_novel", st.RegisteredNovel, "edges", st.Edges, "edges_written", st.EdgesWritten,
		"already_edge", st.AlreadyEdge, "already_work", st.AlreadyWork,
		"skipped_platform", st.SkippedPlatform, "skipped_no_title", st.SkippedNoTitle,
		"skipped_self", st.SkippedSelf,
		"errors", st.Errors,
	)
	if !*apply {
		slog.Info("DRY RUN — nothing written; re-run with --run")
	}
	if st.Errors > 0 {
		os.Exit(1)
	}
}
