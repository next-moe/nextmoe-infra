// import-walkthroughs — turns the crawled walkthrough pages (walkthrough_save,
// sites seiya and sagaoz_foolmaker) into structured catalog walkthrough charts.
//
// The source prose is never republished: the model only indexes the page into
// a graph of entries, saves, choices and endings, and every choice text must
// reappear verbatim in the page or the chart is refused.
//
// Stages, each resumable and run in order:
//
//	extract    LLM → wt_stage.extraction (in the source database)
//	regate     re-run repair + gate over the stored raw extractions, no LLM calls
//	match      entry → catalog work, chart characters → catalog characters
//	translate  choice/ending strings → zh-Hans, names only from source-provenance aliases
//	apply      gated charts → catalog_walkthrough (dry unless --apply)
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/llmsuggest"
	"api/pkg/config"
	"api/pkg/logger"

	"gorm.io/gorm"
)

func main() {
	stage := flag.String("stage", "", "extract | regate | match | translate | apply (REQUIRED)")
	sourceDSN := flag.String("source-dsn", "", "walkthrough_save DSN (REQUIRED)")
	dsn := flag.String("dsn", "", "catalog DSN (required by match, translate and apply)")
	apply := flag.Bool("apply", false, "apply stage: write catalog rows (default: dry)")
	entryIDs := flag.String("entry-ids", "", "comma-separated source entry ids instead of every chart page")
	limit := flag.Int("limit", 0, "max source entries this run (0 = all)")
	redo := flag.Bool("redo", false, "re-run entries whose stored result is already settled")
	model := flag.String("model", envOr("KUN_AI_UPSTREAM_MODEL", config.DefaultAIUpstreamModel), "served model id")
	llmBase := flag.String("llm-base", os.Getenv("KUN_AI_UPSTREAM_BASE_URL"), "OpenAI-compatible gateway base URL (…/v1); the token is KUN_AI_UPSTREAM_TOKEN")
	workers := flag.Int("workers", 6, "concurrent gateway calls")
	maxTokens := flag.Int("max-tokens", 24000, "max_tokens per call")
	flag.Parse()

	logger.Init("development")

	if *sourceDSN == "" {
		slog.Error("--source-dsn is required")
		os.Exit(2)
	}
	ids, err := parseIDs(*entryIDs)
	if err != nil {
		slog.Error("--entry-ids", "error", err)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	src, err := database.OpenJob(*sourceDSN)
	if err != nil {
		slog.Error("open source database", "error", err)
		os.Exit(1)
	}
	if err := ensureStage(ctx, src); err != nil {
		slog.Error("ensure staging schema", "error", err)
		os.Exit(1)
	}
	entries, err := loadEntries(ctx, src, entryFilter{IDs: ids, Limit: *limit})
	if err != nil {
		slog.Error("load entries", "error", err)
		os.Exit(1)
	}

	newLLM := func() *llm {
		if *llmBase == "" || os.Getenv("KUN_AI_UPSTREAM_TOKEN") == "" {
			fmt.Println("BLOCKED: LLM gateway not configured (need --llm-base and KUN_AI_UPSTREAM_TOKEN).")
			os.Exit(3)
		}
		return &llm{client: llmsuggest.NewClient(*llmBase, *model), model: *model}
	}

	switch *stage {
	case "extract":
		err = runExtract(ctx, src, newLLM(), entries, extractOpts{Workers: *workers, MaxTokens: *maxTokens, Redo: *redo})
	case "regate":
		err = runRegate(ctx, src, entries)
	case "match":
		err = runMatch(ctx, src, openCatalog(*dsn), entries)
	case "apply":
		err = runApply(ctx, src, openCatalog(*dsn), entries, *apply)
	case "translate":
		err = runTranslate(ctx, src, openCatalog(*dsn), newLLM(), entries, extractOpts{Workers: *workers, MaxTokens: *maxTokens, Redo: *redo})
	default:
		slog.Error("--stage must be extract, match, translate or apply", "value", *stage)
		os.Exit(2)
	}
	if err != nil {
		slog.Error("run failed", "stage", *stage, "error", err)
		os.Exit(1)
	}
}

func openCatalog(dsn string) *gorm.DB {
	if dsn == "" {
		slog.Error("--dsn is required by this stage")
		os.Exit(2)
	}
	db, err := database.OpenJob(dsn)
	if err != nil {
		slog.Error("open catalog database", "error", err)
		os.Exit(1)
	}
	return db
}

func parseIDs(s string) ([]int64, error) {
	var out []int64
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", part, err)
		}
		out = append(out, id)
	}
	return out, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
