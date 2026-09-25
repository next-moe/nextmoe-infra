package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"api/internal/infrastructure/database"
	"api/pkg/logger"

	"gorm.io/gorm"
)

func main() {
	dsn := flag.String("dsn", "", "catalog DSN (REQUIRED)")
	script := flag.String("script", "", "path to the VNDBTranslatorLib userscript (required unless --mt / --mt-desc / --apply-csv / --apply-desc-csv)")
	apply := flag.Bool("apply", false, "write the curated renderings (default: dry — counts + samples, no writes)")
	includeTags := flag.Bool("include-tag-vocab", false, "ALSO read the script's VN-TAG sections (a different vocabulary that shares ~365 names with the trait table) — review the diff before using")
	mtMode := flag.Bool("mt", false, "residue lane: LLM-propose the names still empty and emit a review CSV (never writes to the DB)")
	mtDesc := flag.Bool("mt-desc", false, "description lane: LLM-propose Simplified Chinese descriptions and emit a review CSV (never writes to the DB)")
	out := flag.String("out", "", "--mt / --mt-desc: review CSV output path (required)")
	applyCSV := flag.String("apply-csv", "", "write back a REVIEWED review CSV as machine provenance")
	applyDescCSV := flag.String("apply-desc-csv", "", "write back a REVIEWED description CSV")
	limit := flag.Int("limit", 0, "--mt / --mt-desc: propose at most N candidates (0 = all)")
	model := flag.String("model", envOr("KUN_INTRO_MT_LLM_MODEL", envOr("KUN_AI_UPSTREAM_MODEL", "glm-5.2")), "served model id")
	llmBase := flag.String("llm-base", envOr("KUN_INTRO_MT_LLM_BASE", os.Getenv("KUN_AI_UPSTREAM_BASE_URL")), "OpenAI-compatible gateway base URL (…/v1)")
	llmToken := flag.String("llm-token", envOr("KUN_INTRO_MT_LLM_TOKEN", os.Getenv("KUN_AI_UPSTREAM_TOKEN")), "gateway bearer token")
	maxTokens := flag.Int("max-tokens", 256, "--mt: max_tokens (a trait name is a handful of characters)")
	descMaxTokens := flag.Int("desc-max-tokens", 4096, "--mt-desc: max_tokens")
	delayMS := flag.Int("delay-ms", 0, "--mt / --mt-desc: delay between gateway calls (ms)")
	idsFlag := flag.String("ids", "", "--mt-desc: comma-separated trait ids (forced re-translation)")
	samples := flag.Int("samples", 10, "how many sample pairs to print")
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
	case *mtDesc:
		if *out == "" {
			slog.Error("--mt-desc requires --out (the review CSV path)")
			os.Exit(2)
		}
		ids, perr := parseIDList(*idsFlag)
		if perr != nil {
			slog.Error("--ids", "error", perr)
			os.Exit(2)
		}
		tr := newDescHTTPTranslator(*llmBase, *llmToken, *model, *descMaxTokens)
		if !tr.Configured() {
			fmt.Println("BLOCKED: LLM gateway not configured (need --llm-base + --llm-token, or KUN_INTRO_MT_LLM_* / KUN_AI_UPSTREAM_*).\n" +
				"This is a designed precondition for --mt-desc, not a failure.")
			os.Exit(3)
		}
		err = runMTDesc(ctx, db, tr, *out, *limit, time.Duration(*delayMS)*time.Millisecond, ids)
	case *mtMode:
		if *out == "" {
			slog.Error("--mt requires --out (the review CSV path)")
			os.Exit(2)
		}
		tr := newHTTPTranslator(*llmBase, *llmToken, *model, *maxTokens)
		if !tr.Configured() {
			fmt.Println("BLOCKED: LLM gateway not configured (need --llm-base + --llm-token, or KUN_INTRO_MT_LLM_* / KUN_AI_UPSTREAM_*).\n" +
				"This is a designed precondition for --mt, not a failure.")
			os.Exit(3)
		}
		err = runMT(ctx, db, tr, *out, *limit, time.Duration(*delayMS)*time.Millisecond)
	case *applyDescCSV != "":
		err = runApplyDescCSV(ctx, db, *applyDescCSV, *apply)
	case *applyCSV != "":
		err = runIngest(ctx, db, csvSource(*applyCSV), provenanceMachine, true, *samples)
	default:
		if *script == "" {
			slog.Error("--script is required")
			os.Exit(2)
		}
		err = runIngest(ctx, db, scriptSource(*script, parseOpts{IncludeTagVocab: *includeTags}), provenanceCurated, *apply, *samples)
	}
	if err != nil {
		slog.Error("run failed", "error", err)
		os.Exit(1)
	}
}

type source struct {
	label string
	load  func() ([]pair, error)
}

func scriptSource(path string, opts parseOpts) source {
	return source{label: "userscript " + path, load: func() ([]pair, error) { return parseScript(path, opts) }}
}

func csvSource(path string) source {
	return source{label: "reviewed CSV " + path, load: func() ([]pair, error) { return readReviewCSV(path) }}
}

func runIngest(ctx context.Context, db *gorm.DB, src source, prov int16, apply bool, samples int) error {
	pairs, err := src.load()
	if err != nil {
		return err
	}
	rows, err := loadTraits(ctx, db)
	if err != nil {
		return err
	}
	writes := plan(pairs, rows, prov)
	c := summarise(pairs, writes)

	fmt.Printf("\n=== ingest-trait-zh %s (%s, provenance=%d) ===\n", mode(apply), src.label, prov)
	fmt.Printf("proposals=%d  vocabulary_rows=%d  matched_rows=%d  write=%d  already_same=%d  conflict=%d\n",
		c.Proposals, len(rows), c.Matched, c.Write, c.Same, c.Conflict)
	fmt.Printf("still_without_zh_after_run=%d\n", withoutZhAfter(rows, writes))
	printSamples(writes, samples)
	printConflicts(writes)

	if !apply {
		fmt.Println("\n[dry run] nothing written — re-run with --apply")
		return nil
	}
	n, err := applyWrites(ctx, db, writes, prov)
	if err != nil {
		return err
	}
	fmt.Printf("\nwritten=%d\n", n)
	return nil
}

func runMT(ctx context.Context, db *gorm.DB, tr translator, out string, limit int, delay time.Duration) error {
	cands, err := loadMTCandidates(ctx, db, limit)
	if err != nil {
		return err
	}
	fmt.Printf("\n=== ingest-trait-zh MT (candidates without name_zh: %d) ===\n", len(cands))
	rows := make([]csvRow, 0, len(cands))
	var errs int
	for i, c := range cands {
		if i > 0 && delay > 0 {
			time.Sleep(delay)
		}
		zh, _, err := tr.Translate(ctx, c)
		if err != nil {
			errs++
			slog.Warn("translate failed", "trait", c.Name, "error", err)
			zh = ""
		}
		rows = append(rows, csvRow{
			TraitID: c.ID, VndbTID: c.VndbTID, Name: c.Name, Group: c.GroupName,
			Description: truncateRunes(plainDescription(c.Description), 120), ProposedZh: zh,
		})
		if (i+1)%50 == 0 {
			slog.Info("mt progress", "done", i+1, "of", len(cands), "errors", errs)
		}
	}
	if err := writeReviewCSV(out, rows); err != nil {
		return err
	}
	fmt.Printf("proposed=%d errors=%d → %s\nreview it, then: --apply-csv %s\n", len(rows)-errs, errs, out, out)
	if errs > 0 {
		os.Exit(1)
	}
	return nil
}

func withoutZhAfter(rows []traitRow, writes []plannedWrite) int {
	filled := map[int64]bool{}
	for _, w := range writes {
		if w.Decision == decWrite {
			filled[w.Trait.ID] = true
		}
	}
	n := 0
	for _, r := range rows {
		if r.NameZh == "" && !filled[r.ID] {
			n++
		}
	}
	return n
}

func printSamples(writes []plannedWrite, n int) {
	shown := 0
	for _, w := range writes {
		if w.Decision != decWrite || shown >= n {
			continue
		}
		fmt.Printf("  sample: %-40s → %s\n", w.Trait.Name, w.Zh)
		shown++
	}
}

func printConflicts(writes []plannedWrite) {
	for _, w := range writes {
		if w.Decision == decConflict {
			fmt.Printf("  CONFLICT trait %d (%s) %q: curated %q vs proposed %q — left untouched\n",
				w.Trait.ID, w.Trait.VndbTID, w.Trait.Name, w.Trait.NameZh, w.Zh)
		}
	}
}

func mode(apply bool) string {
	if apply {
		return "APPLY"
	}
	return "DRY"
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
