package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"api/internal/jobs/tagsafety"
	"api/pkg/config"
	"api/pkg/logger"
)

func main() {
	mode := flag.String("mode", "", "classify | report | apply")
	dsn := flag.String("dsn", "", "catalog DSN (REQUIRED for classify/apply; rehearsal locally, live only in the acceptance run)")

	out := flag.String("out", "", "classify: verdict JSONL output path (appended — resume-safe)")
	in := flag.String("in", "", "report/apply: verdict JSONL input path (classify --out)")
	sources := flag.String("sources", "bangumi,dlsite", "classify: comma-separated catalog_source keys")
	vocab := flag.Bool("vocab", false, "classify: also judge every catalog_tag canonical name")
	minUses := flag.Int("min-uses", 1, "classify: skip names used on fewer than N distinct works (1 = all)")
	batch := flag.Int("batch", 0, "classify: names per LLM call (0 = pinned default)")

	reviewOut := flag.String("review-out", "", "apply: JSONL of lines needing a human ruling")
	reviewed := flag.String("reviewed", "", "apply: OPTIONAL hand-ruled JSONL ({source,name,class}) applied at full trust")
	minConfidence := flag.Float64("min-confidence", 0.90, "report/apply: auto-apply confidence floor")
	run := flag.Bool("run", false, "apply: write changes (default DRY)")

	limit := flag.Int("limit", 0, "rehearsal cap — classify: names judged; apply: write operations (0 = no cap)")

	model := flag.String("model", envOr("KUN_TAG_PAIR_LLM_MODEL", envOr("KUN_AI_UPSTREAM_MODEL", config.DefaultAIUpstreamModel)), "served model id")
	llmBase := flag.String("llm-base", envOr("KUN_TAG_PAIR_LLM_BASE", config.LLMBaseURL()), "OpenAI-compatible gateway base URL (…/v1)")
	llmToken := flag.String("llm-token", envOr("KUN_TAG_PAIR_LLM_TOKEN", envOr("KUN_AI_UPSTREAM_KEY", config.LLMToken())), "gateway bearer token")
	maxTokens := flag.Int("max-tokens", 4096, "LLM max_tokens per batch")
	mock := flag.Bool("mock", false, "classify: REHEARSAL ONLY offline deterministic classifier (no network)")
	flag.Parse()

	logger.Init("development")
	ctx := context.Background()

	switch *mode {
	case "classify":
		cl := selectClassifier(*mock, *llmBase, *llmToken, *model, *maxTokens)
		st, err := tagsafety.Classify(ctx, cl, tagsafety.ClassifyOpts{
			DSN: *dsn, Sources: splitCSV(*sources), Vocab: *vocab,
			Out: *out, MinUses: *minUses, Limit: *limit, BatchSize: *batch,
		})
		must(err)
		fmt.Printf("\n=== classify ===\npool=%d skipped_done=%d judged=%d errors=%d batches=%d\n",
			st.Pool, st.Skipped, st.Judged, st.Errors, st.Batches)
		fmt.Printf("classes: sexual=%d junk=%d normal=%d\nout=%s\n",
			st.ClassCounts[tagsafety.ClassSexual], st.ClassCounts[tagsafety.ClassJunk],
			st.ClassCounts[tagsafety.ClassNormal], *out)
		if st.Errors > 0 {
			os.Exit(1)
		}

	case "report":
		st, err := tagsafety.Report(*in, *minConfidence)
		must(err)
		fmt.Printf("\n=== report (min-confidence %.2f) ===\n%s", *minConfidence, st)

	case "apply":
		st, err := tagsafety.Apply(ctx, tagsafety.ApplyOpts{
			DSN: *dsn, In: *in, Reviewed: *reviewed, ReviewOut: *reviewOut,
			MinConfidence: *minConfidence, Limit: *limit, Run: *run,
		})
		must(err)
		p := st.Plan
		fmt.Printf("\n=== apply %s (min-confidence %.2f) ===\n", modeLabel(*run), *minConfidence)
		fmt.Printf("verdicts=%d below_threshold=%d hand_ruled=%d\n", p.Counts.Total, p.Counts.BelowThreshold, p.Counts.Reviewed)
		fmt.Printf("planned: work_tag_sexual=%d vocab_sexual=%d vocab_hidden=%d (truncated=%v)\n",
			len(p.WorkTagSexual), len(p.VocabSexual), len(p.VocabHidden), p.Truncated)
		fmt.Printf("no-ops: already_sexual=%d already_hidden=%d unmapped_junk=%d\n",
			p.Counts.AlreadySexual, p.Counts.AlreadyHidden, p.Counts.UnmappedJunk)
		fmt.Printf("review=%d (of which deflag_candidates=%d)%s\n", len(p.Review), p.Counts.DeflagGuard, reviewNote(*reviewOut))
		printSamples(p)
		if *run {
			fmt.Printf("wrote: work_tag_rows=%d vocab_sexual_rows=%d vocab_hidden_rows=%d errors=%d\n",
				st.WorkTagRows, st.VocabSexualRows, st.VocabHiddenRows, st.Errors)
		} else {
			fmt.Printf("DRY RUN — nothing written; re-run with --run\n")
		}
		if st.Errors > 0 {
			os.Exit(1)
		}

	default:
		fmt.Fprintf(os.Stderr, "usage: -mode classify|report|apply (see cmd/classify-tag-safety doc)\n")
		os.Exit(2)
	}
}

func printSamples(p tagsafety.Plan) {
	sample := func(label string, items []string) {
		if len(items) == 0 {
			return
		}
		n := min(len(items), 10)
		fmt.Printf("  %s sample: %s\n", label, strings.Join(items[:n], ", "))
	}
	work := make([]string, 0, len(p.WorkTagSexual))
	for _, t := range p.WorkTagSexual {
		work = append(work, t.Source+":"+t.Name)
	}
	sample("work_tag_sexual", work)
	sample("vocab_sexual", p.VocabSexual)
	sample("vocab_hidden", p.VocabHidden)
}

func selectClassifier(mock bool, base, token, model string, maxTokens int) tagsafety.Classifier {
	if mock {
		slog.Warn("MOCK classifier active — rehearsal only; verdicts are NOT real judgments")
		return tagsafety.MockClassifier{Model: model}
	}
	ht := tagsafety.NewHTTPClassifier(base, token, model, maxTokens)
	if !ht.Configured() {
		fmt.Printf("BLOCKED: LLM gateway not configured (need --llm-base + --llm-token, or KUN_TAG_PAIR_LLM_* / KUN_LLM_GATEWAY_KEY / KUN_AI_UPSTREAM_*).\n" +
			"This is a designed precondition for a real classify, not a failure. Use --mock for the offline rehearsal.\n")
		os.Exit(3)
	}
	slog.Info("live gateway classifier", "base", base, "model", model)
	return ht
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func reviewNote(path string) string {
	if path == "" {
		return " [--review-out unset: NOT persisted]"
	}
	return " → " + path
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func modeLabel(run bool) string {
	if run {
		return "RUN"
	}
	return "DRY"
}

func must(err error) {
	if err != nil {
		slog.Error("classify-tag-safety failed", "error", err)
		os.Exit(1)
	}
}
