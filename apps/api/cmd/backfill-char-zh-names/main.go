// backfill-char-zh-names — wave 178 P1: give live characters a zh-Hans name row.
//
// Three lanes, run in this order (refs/proj/178):
//
//  1. dict (NOT here): the bangumi dictionary lane is backfill-bgm-zh-names
//     -lane character — run it first so a real translated name claims the
//     locale primary before the passthrough considers the character at all.
//  2. passthrough (default mode): a character whose display name is pure Han
//     needs no translation — the same glyphs ARE its Simplified-Chinese
//     rendering. Writes a zh-Hans alias row equal to the display name
//     (provenance=source, primary when the character has no zh name at all).
//     The read face already treats the identity row exactly right: it feeds
//     localized{} and is dropped from the flat aliases[] (is_display).
//  3. --mt: the kana / romaji residue. NEVER writes the DB — emits a review
//     CSV for human review; --apply-csv writes the reviewed rows back as
//     provenance=machine, which the public localized{} face refuses (only
//     aliases[], search and the MT glossaries see them).
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"api/internal/infrastructure/database"
	"api/pkg/config"
	"api/pkg/logger"

	"gorm.io/gorm"
)

func main() {
	dsn := flag.String("dsn", "", "catalog DSN (REQUIRED)")
	apply := flag.Bool("apply", false, "passthrough: write the identity rows (default: dry — counts + samples)")
	mtMode := flag.Bool("mt", false, "residue lane: LLM-propose zh names for the kana/romaji characters and emit a review CSV (never writes the DB)")
	autoMode := flag.Bool("auto", false, "machine-review lane: propose + three-framing unanimous machine gate, emit an apply-ready CSV (never writes the DB)")
	shard := flag.String("shard", "", "--auto: i/n — process only characters with id % n == i (disjoint under concurrent shards)")
	excludeIDs := flag.String("exclude-ids", "", "--auto: file of character ids (one per line) to skip, for cheap resume")
	out := flag.String("out", "", "--mt/--auto: review CSV output path (required)")
	applyCSV := flag.String("apply-csv", "", "write back a REVIEWED --mt CSV as machine provenance")
	limit := flag.Int("limit", 0, "cap the candidates (passthrough/--mt/--auto) or accepted rows (--apply-csv) this run; 0 = all. --mt/--auto order by work-appearance count so a small batch covers the most-visible names first")
	model := flag.String("model", envOr("KUN_INTRO_MT_LLM_MODEL", envOr("KUN_AI_UPSTREAM_MODEL", config.DefaultAIUpstreamModel)), "served model id")
	llmBase := flag.String("llm-base", envOr("KUN_INTRO_MT_LLM_BASE", config.LLMBaseURL()), "OpenAI-compatible gateway base URL (…/v1)")
	llmToken := flag.String("llm-token", envOr("KUN_INTRO_MT_LLM_TOKEN", config.LLMToken()), "gateway bearer token")
	maxTokens := flag.Int("max-tokens", 4096, "--mt: max_tokens (thinking models spend most of it before the name)")
	delayMS := flag.Int("delay-ms", 0, "--mt: delay between gateway calls (ms)")
	samples := flag.Int("samples", 10, "how many sample rows to print")
	flag.Parse()

	logger.Init("development")

	if *dsn == "" {
		slog.Error("--dsn is required")
		os.Exit(2)
	}
	db, err := open(*dsn)
	if err != nil {
		slog.Error("open database", "error", err)
		os.Exit(1)
	}
	ctx := context.Background()

	switch {
	case *mtMode || *autoMode:
		if *out == "" {
			slog.Error("--mt/--auto require --out (the review CSV path)")
			os.Exit(2)
		}
		tr := newHTTPTranslator(*llmBase, *llmToken, *model, *maxTokens)
		if !tr.Configured() {
			fmt.Println("BLOCKED: LLM gateway not configured (need --llm-base + --llm-token, or KUN_INTRO_MT_LLM_* / KUN_LLM_GATEWAY_KEY / KUN_AI_UPSTREAM_*).\n" +
				"This is a designed precondition for --mt/--auto, not a failure.")
			os.Exit(3)
		}
		if *autoMode {
			shardI, shardN, perr := parseShard(*shard)
			if perr != nil {
				slog.Error("bad --shard", "error", perr)
				os.Exit(2)
			}
			exclude, xerr := loadExcludeIDs(*excludeIDs)
			if xerr != nil {
				slog.Error("read --exclude-ids", "error", xerr)
				os.Exit(2)
			}
			err = runAuto(ctx, db, tr, *model, *out, *limit, shardI, shardN, exclude, time.Duration(*delayMS)*time.Millisecond)
		} else {
			err = runMT(ctx, db, tr, *out, *limit, time.Duration(*delayMS)*time.Millisecond)
		}
	case *applyCSV != "":
		err = runApplyCSV(ctx, db, *applyCSV, *samples, *limit)
	default:
		err = runPassthrough(ctx, db, *apply, *limit, *samples)
	}
	if err != nil {
		slog.Error("run failed", "error", err)
		os.Exit(1)
	}
}

func open(dsn string) (*gorm.DB, error) {
	return database.OpenJob(dsn)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
