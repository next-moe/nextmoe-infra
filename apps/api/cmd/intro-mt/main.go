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

	"api/internal/jobs/intromt"
	"api/pkg/logger"
)

func main() {
	dsn := flag.String("dsn", "", "catalog DSN (REQUIRED; rehearsal locally, live catalog only in the acceptance run)")
	apply := flag.Bool("apply", false, "translate + write (default: dry — counts + samples, no LLM, no writes)")
	population := flag.String("population", string(intromt.PopulationBodyless), "candidate lane: bodyless | claimed | published (claimed narrowed to the public face)")
	sourceLang := flag.String("source-lang", string(intromt.SourceJa), "translate FROM: ja (default) or en (last resort — excludes anything with ja, and anything Getchu anchors)")
	top := flag.Int("top", 5000, "popularity-ranked candidate ceiling (0 = unlimited, the full-sweep posture)")
	limit := flag.Int("limit", 0, "process only the most-popular N candidates (0 = all within --top)")
	model := flag.String("model", envOr("KUN_INTRO_MT_LLM_MODEL", envOr("KUN_AI_UPSTREAM_MODEL", "deepseek-chat")), "served model id (recorded in mt_model)")
	llmBase := flag.String("llm-base", envOr("KUN_INTRO_MT_LLM_BASE", os.Getenv("KUN_AI_UPSTREAM_BASE_URL")), "OpenAI-compatible gateway base URL (…/v1)")
	llmToken := flag.String("llm-token", envOr("KUN_INTRO_MT_LLM_TOKEN", os.Getenv("KUN_AI_UPSTREAM_TOKEN")), "gateway bearer token")
	maxTokens := flag.Int("max-tokens", 4096, "translation max_tokens (ja intros run up to ~4.5k chars)")
	delayMS := flag.Int("delay-ms", 0, "rate-limit delay between real gateway calls (ms)")
	mock := flag.Bool("mock", false, "REHEARSAL ONLY: offline deterministic mock translator (no network; obvious marker output)")
	workers := flag.Int("workers", 1, "apply-mode concurrency (per-request latency dominates; 8 ≈ 10 req/min, far under gateway rate limits)")
	workIDs := flag.String("work-ids", "", "comma-separated work ids — the named list IS the population, so --top stops applying")
	force := flag.Bool("force", false, "retranslate even when src_hash is unchanged (the prompt is not in the hash, so a prompt rewrite needs this)")
	effort := flag.String("effort", "", "reasoning effort: low | medium | high (empty = the gateway's own default; a reasoning model needs low or it thinks for minutes about a translation)")
	flag.Parse()

	ids, err := parseWorkIDs(*workIDs)
	if err != nil {
		slog.Error("--work-ids", "error", err)
		os.Exit(2)
	}

	logger.Init("development")

	if *dsn == "" {
		slog.Error("--dsn is required")
		os.Exit(2)
	}

	var tr intromt.Translator
	if *apply {
		if *mock {
			tr = intromt.MockTranslator{Model: *model}
			slog.Warn("MOCK translator active — rehearsal write-path proof only; rows are NOT real translations")
		} else {
			ht := intromt.NewHTTPTranslator(*llmBase, *llmToken, *model, *maxTokens)
			ht.SetSourceLang(intromt.SourceLang(*sourceLang))
			ht.SetEffort(*effort)
			if !ht.Configured() {
				fmt.Printf("BLOCKED: LLM gateway not configured (need --llm-base + --llm-token, or KUN_INTRO_MT_LLM_* / KUN_AI_UPSTREAM_*).\n" +
					"This is a designed precondition for a real --apply, not a failure. Use --mock for the offline write-path rehearsal.\n")
				os.Exit(3)
			}
			tr = ht
			slog.Info("live gateway translator", "base", *llmBase, "model", *model)
		}
	}

	st, err := intromt.Run(context.Background(), tr, intromt.Opts{
		DSN: *dsn, Apply: *apply, Population: intromt.Population(*population),
		SourceLang: intromt.SourceLang(*sourceLang), Top: *top, Limit: *limit,
		WorkIDs: ids, Force: *force,
		Delay:   time.Duration(*delayMS) * time.Millisecond,
		Workers: *workers,
	})
	if err != nil {
		slog.Error("run failed", "error", err)
		os.Exit(1)
	}

	printReport(st, *apply)
	// Healthy nightly runs carry transient LLM errors every single night
	// (fill-missing retries them); errors>0 alone would be red daily. Dead
	// upstream looks like: attempts made, nothing translated.
	if st.Errors > 0 && st.Inserted+st.Retranslated == 0 {
		os.Exit(1)
	}
}

func printReport(st *intromt.Stats, apply bool) {
	fmt.Printf("\n=== intro-mt %s ===\n", modeLabel(apply))
	fmt.Printf("candidates=%d with_glossary=%d would_insert=%d would_retranslate=%d skip_unchanged=%d would_prune=%d\n",
		st.Candidates, st.WithGlossary, st.WouldInsert, st.WouldRetranslate, st.SkipUnchanged, st.WouldPrune)
	if apply {
		fmt.Printf("inserted=%d retranslated=%d refused=%d errors=%d pruned=%d\n",
			st.Inserted, st.Retranslated, st.Refused, st.Errors, st.Pruned)
	}
	for i, s := range st.Samples {
		fmt.Printf("\n--- sample %d (work %d, %s%s) ---\n", i+1, s.WorkID, s.Decision, modelSuffix(s.MTModel))
		printGlossary(s.Gloss)
		fmt.Printf("JA: %s\n", s.Ja)
		if s.Zh != "" {
			fmt.Printf("ZH: %s\n", s.Zh)
		}
	}
	if !apply {
		fmt.Println("\n[dry run] no LLM called, nothing written — re-run with --apply")
	}
}

func printGlossary(g intromt.Glossary) {
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

func parseWorkIDs(csv string) ([]int64, error) {
	csv = strings.TrimSpace(csv)
	if csv == "" {
		return nil, nil
	}
	var out []int64
	for _, f := range strings.Split(csv, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		id, err := strconv.ParseInt(f, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a work id", f)
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
