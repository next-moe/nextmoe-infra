package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"api/internal/infrastructure/database"
	catalogSearch "api/internal/platform/catalog/search"
	"api/internal/platform/catalog/search/docsync"
	"api/pkg/config"
	"api/pkg/logger"
)

func main() {
	entity := flag.String("entity", "works", "works | characters")
	batch := flag.Int("batch", 2000, "entities loaded per page")
	embedBatch := flag.Int("embed-batch", 16, "texts per embeddings request")
	concurrency := flag.Int("concurrency", 32, "embeddings requests in flight")
	model := flag.String("model", catalogSearch.DefaultEmbedModel, "embedding model id")
	dims := flag.Int("dims", 1024, "vector dimension of -model")
	idsFlag := flag.String("ids", "", "comma-separated entity ids (default: every live one)")
	flag.Parse()

	embed, uid := docsync.EmbedWorks, catalogSearch.IndexWorkVectors
	switch *entity {
	case "works":
	case "characters":
		embed, uid = docsync.EmbedCharacters, catalogSearch.IndexCharacterVectors
	default:
		slog.Error("unknown -entity", "value", *entity)
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}
	logger.Init(cfg.Server.Env)
	if cfg.AIUpstream.BaseURL == "" || cfg.AIUpstream.Token == "" {
		slog.Error("KUN_AI_UPSTREAM_BASE_URL and KUN_AI_UPSTREAM_TOKEN are required")
		os.Exit(1)
	}
	var ids []int64
	if *idsFlag != "" {
		for _, raw := range strings.Split(*idsFlag, ",") {
			id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
			if err != nil {
				slog.Error("bad -ids entry", "value", raw)
				os.Exit(1)
			}
			ids = append(ids, id)
		}
	}

	db, err := database.NewPostgresDB(cfg.CatalogDatabase)
	if err != nil {
		slog.Error("catalog db connect", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	idx, _, err := catalogSearch.NewIndexerFromConfig(cfg)
	if err != nil {
		slog.Error("search indexer", "error", err)
		os.Exit(1)
	}
	ctx := context.Background()
	if err := idx.EnsureVectorIndex(ctx, uid, *dims); err != nil {
		slog.Error("vector index", "error", err)
		os.Exit(1)
	}
	emb := catalogSearch.NewOpenAIEmbedder(cfg.AIUpstream.BaseURL, cfg.AIUpstream.Token, *model)
	start := time.Now()
	stats, err := embed(ctx, db.DB(), idx, emb, docsync.EmbedOptions{
		Batch: *batch, EmbedBatch: *embedBatch, Concurrency: *concurrency, IDs: ids,
	})
	slog.Info("embed-catalog", "entity", *entity, "scanned", stats.Scanned, "embedded", stats.Embedded,
		"filter_only", stats.FilterOnly, "elapsed", time.Since(start).Round(time.Second))
	if err != nil {
		slog.Error("embed-catalog failed", "error", err)
		os.Exit(1)
	}
}
