package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"api/internal/jobs/entityintromt"
	"api/pkg/logger"
)

func main() {
	dsn := flag.String("dsn", "", "catalog DSN (REQUIRED; rehearsal locally, live catalog only in the acceptance run)")
	apply := flag.Bool("apply", false, "translate + write (default: dry — counts + samples, no LLM, no writes)")
	lane := flag.String("lane", "", "restrict to one lane: character | person | label (empty = all three)")
	limit := flag.Int("limit", 0, "process only N candidates per lane in entity-id order (0 = all)")
	offset := flag.Int("offset", 0, "skip the first N candidates per lane — with --limit, the resumable batch window")
	model := flag.String("model", envOr("KUN_INTRO_MT_LLM_MODEL", envOr("KUN_AI_UPSTREAM_MODEL", "deepseek-chat")), "served model id (recorded in mt_model)")
	llmBase := flag.String("llm-base", envOr("KUN_INTRO_MT_LLM_BASE", os.Getenv("KUN_AI_UPSTREAM_BASE_URL")), "OpenAI-compatible gateway base URL (…/v1)")
	llmToken := flag.String("llm-token", envOr("KUN_INTRO_MT_LLM_TOKEN", os.Getenv("KUN_AI_UPSTREAM_TOKEN")), "gateway bearer token")
	maxTokens := flag.Int("max-tokens", 4096, "translation max_tokens")
	delayMS := flag.Int("delay-ms", 0, "rate-limit delay between real gateway calls (ms)")
	mock := flag.Bool("mock", false, "REHEARSAL ONLY: offline deterministic mock translator (no network; obvious marker output)")
	workers := flag.Int("workers", 1, "apply-mode concurrency (per-request latency dominates)")
	force := flag.Bool("force", false, "retranslate rows whose src_hash already matches — the prompt is not in the hash, so a prompt change needs this")
	entityIDs := flag.String("entity-ids", "", "comma-separated entity ids for --lane; the named list IS the population")
	flag.Parse()

	ids, err := parseEntityIDs(*entityIDs)
	if err != nil {
		slog.Error("--entity-ids", "error", err)
		os.Exit(2)
	}

	logger.Init("development")

	if *dsn == "" {
		slog.Error("--dsn is required")
		os.Exit(2)
	}

	var tr entityintromt.Translator
	if *apply {
		if *mock {
			tr = entityintromt.MockTranslator{Model: *model}
			slog.Warn("MOCK translator active — rehearsal write-path proof only; rows are NOT real translations")
		} else {
			ht := entityintromt.NewHTTPTranslator(*llmBase, *llmToken, *model, *maxTokens)
			if !ht.Configured() {
				fmt.Printf("BLOCKED: LLM gateway not configured (need --llm-base + --llm-token, or KUN_INTRO_MT_LLM_* / KUN_AI_UPSTREAM_*).\n" +
					"This is a designed precondition for a real --apply, not a failure. Use --mock for the offline write-path rehearsal.\n")
				os.Exit(3)
			}
			tr = ht
			slog.Info("live gateway translator", "base", *llmBase, "model", *model)
		}
	}

	st, err := entityintromt.Run(context.Background(), tr, entityintromt.Opts{
		DSN: *dsn, Apply: *apply, Lane: *lane, Limit: *limit, Offset: *offset,
		Delay:     time.Duration(*delayMS) * time.Millisecond,
		Workers:   *workers,
		Force:     *force,
		EntityIDs: ids,
	})
	if err != nil {
		slog.Error("run failed", "error", err)
		os.Exit(1)
	}

	printReport(st, *apply)

	// Healthy nightly runs carry transient LLM errors every single night
	// (fill-missing retries them); errors>0 alone would be red daily. Dead
	// upstream looks like: attempts made, nothing translated.
	errs, wrote := 0, 0
	for _, lane := range st {
		errs += lane.Errors
		wrote += lane.Inserted + lane.Retranslated
	}
	if errs > 0 && wrote == 0 {
		os.Exit(1)
	}
}

func printReport(lanes []*entityintromt.LaneStats, apply bool) {
	fmt.Printf("\n=== entity-intro-mt %s ===\n", modeLabel(apply))
	for _, st := range lanes {
		fmt.Printf("\n[%s] candidates=%d from_ja=%d from_en=%d with_glossary=%d would_insert=%d would_retranslate=%d skip_unchanged=%d would_prune=%d\n",
			st.Lane, st.Candidates, st.FromJa, st.FromEn, st.WithGlossary,
			st.WouldInsert, st.WouldRetranslate, st.SkipUnchanged, st.WouldPrune)
		if apply {
			fmt.Printf("[%s] inserted=%d retranslated=%d refused=%d errors=%d pruned=%d\n",
				st.Lane, st.Inserted, st.Retranslated, st.Refused, st.Errors, st.Pruned)
		}
		for i, s := range st.Samples {
			fmt.Printf("\n--- %s sample %d (entity %d, %s%s) ---\n", s.Lane, i+1, s.EntityID, s.Decision, modelSuffix(s.MTModel))
			printGlossary(s.Gloss)
			fmt.Printf("SRC(%s): %s\n", s.SrcLang, s.Src)
			if s.Zh != "" {
				fmt.Printf("ZH: %s\n", s.Zh)
			}
		}
	}
	if !apply {
		fmt.Println("\n[dry run] no LLM called, nothing written — re-run with --apply")
	}
}

func printGlossary(g entityintromt.Glossary) {
	if len(g) == 0 {
		return
	}
	fmt.Printf("GLOSSARY(%d):", len(g))
	for _, e := range g {
		fmt.Printf(" %s→%s", e.Src, e.Zh)
	}
	fmt.Println()
}

func modeLabel(apply bool) string {
	if apply {
		return "APPLY"
	}
	return "DRY"
}

func modelSuffix(m string) string {
	if m == "" {
		return ""
	}
	return " · " + m
}

func parseEntityIDs(csv string) ([]int64, error) {
	var out []int64
	for _, f := range strings.Split(csv, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		id, err := strconv.ParseInt(f, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not an entity id", f)
		}
		out = append(out, id)
	}
	return out, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
