package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"strings"
	"time"

	"api/internal/infrastructure/database"
	catalogSearch "api/internal/platform/catalog/search"
	"api/internal/platform/catalog/search/docsync"
	"api/pkg/config"
	"api/pkg/logger"
)

func main() {
	indexFlag := flag.String("index", "catalog_credit_names,catalog_characters,catalog_labels,catalog_works,catalog_tags,catalog_series,catalog_engines,catalog_traits", "comma-separated index uids")
	batch := flag.Int("batch", 5000, "batch size per search upsert")
	recreate := flag.Bool("recreate", false, "drop and recreate each named index (opensearch only)")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}
	logger.Init(cfg.Server.Env)

	db, err := database.NewPostgresDB(cfg.CatalogDatabase)
	if err != nil {
		slog.Error("catalog db connect", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	idx, engineName, err := catalogSearch.NewIndexerFromConfig(cfg)
	if err != nil {
		slog.Error("search indexer", "error", err)
		os.Exit(1)
	}
	slog.Info("reindex-catalog engine", "engine", engineName)
	if *recreate && engineName != catalogSearch.EngineOpenSearch {
		slog.Error("-recreate is unsupported for this search engine", "engine", engineName)
		os.Exit(1)
	}
	ctx := context.Background()
	if err := idx.Health(ctx); err != nil {
		slog.Error("search engine unreachable", "engine", engineName, "error", err)
		os.Exit(1)
	}
	indexes := strings.Split(*indexFlag, ",")
	if *recreate {
		for _, t := range indexes {
			t = strings.TrimSpace(t)
			switch t {
			case catalogSearch.IndexCreditNames, catalogSearch.IndexCharacters, catalogSearch.IndexLabels, catalogSearch.IndexWorks, catalogSearch.IndexTags, catalogSearch.IndexSeries, catalogSearch.IndexEngines, catalogSearch.IndexTraits:
				if err := idx.RecreateIndex(ctx, t); err != nil {
					slog.Error("recreate failed", "index", t, "error", err)
					os.Exit(1)
				}
			case "":
				continue
			default:
				slog.Warn("unknown index, skipping", "index", t)
			}
		}
	}
	if err := idx.EnsureIndexes(ctx); err != nil {
		slog.Error("ensure indexes", "error", err)
		os.Exit(1)
	}

	start := time.Now()
	for _, t := range indexes {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if !docsync.Known(t) {
			slog.Warn("unknown index, skipping", "index", t)
			continue
		}
		if err := docsync.Reindex(ctx, db.DB(), idx, t, *batch); err != nil {
			slog.Error("reindex failed", "index", t, "error", err)
			os.Exit(1)
		}
		if err := idx.Refresh(ctx, t); err != nil {
			slog.Error("refresh failed", "index", t, "error", err)
			os.Exit(1)
		}
	}
	slog.Info("reindex-catalog complete", "duration", time.Since(start))
}
