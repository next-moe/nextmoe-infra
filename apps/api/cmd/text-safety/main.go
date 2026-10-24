package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"api/internal/infrastructure/database"
	"api/internal/jobs/textsafety"
	"api/pkg/logger"
)

func main() {
	mode := flag.String("mode", "", "titles | release-titles | title-review | intros | char-intros | apply | report")
	dsn := flag.String("dsn", os.Getenv("KUN_TEXT_SAFETY_DSN"), "catalog DSN (required; refuses to guess)")
	llmBase := flag.String("llm-base", os.Getenv("KUN_TEXT_SAFETY_LLM_BASE"), "OpenAI-compatible base URL (…/v1)")
	llmToken := flag.String("llm-token", os.Getenv("KUN_TEXT_SAFETY_LLM_TOKEN"), "bearer token for -llm-base")
	model := flag.String("model", envOr("KUN_TEXT_SAFETY_MODEL", "grok-4.7:low:fast"), "model id served by -llm-base")
	conc := flag.Int("concurrency", 16, "LLM calls in flight")
	limit := flag.Int("limit", 0, "titles/intros: cap items judged this run (0 = all)")
	sample := flag.Int("sample", 0, "titles/intros: judge a fixed pseudo-random sample of N and write -sample-out instead of the database")
	sampleOut := flag.String("sample-out", "", "JSONL path for -sample")
	run := flag.Bool("run", false, "apply: write (default dry run)")
	prune := flag.Bool("prune", false, "apply: also delete verdicts no current title or intro hashes to")
	reportOut := flag.String("out", "", "report: directory for the summary and review samples")
	reportN := flag.Int("n", 50, "report: rows per review sample")
	flag.Parse()

	logger.Init("development")
	if *dsn == "" {
		fail(fmt.Errorf("-dsn is required"))
	}
	db, err := database.OpenJob(*dsn)
	if err != nil {
		fail(fmt.Errorf("connect: %w", err))
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	newLLM := func() *textsafety.LLM {
		if *llmBase == "" || *llmToken == "" {
			fail(fmt.Errorf("-llm-base and -llm-token are required for -mode %s", *mode))
		}
		return textsafety.NewLLM(*llmBase, *llmToken, *model)
	}
	opts := textsafety.JudgeOpts{Limit: *limit, Concurrency: *conc}
	if *sample > 0 {
		if *sampleOut == "" {
			fail(fmt.Errorf("-sample needs -sample-out"))
		}
		f, err := os.Create(*sampleOut)
		if err != nil {
			fail(err)
		}
		defer f.Close()
		var mu sync.Mutex
		enc := json.NewEncoder(f)
		opts.Limit = *sample
		opts.Sink = func(v any) { mu.Lock(); defer mu.Unlock(); _ = enc.Encode(v) }
	}

	switch *mode {
	case "titles", "release-titles", "title-review":
		llm := newLLM()
		judge := textsafety.JudgeTitles
		switch *mode {
		case "release-titles":
			judge = textsafety.JudgeReleaseTitles
		case "title-review":
			judge = textsafety.ReviewTitles
		}
		st, err := judge(ctx, db, llm, opts)
		printUsage(llm)
		if st != nil {
			fmt.Printf("%s done pending=%d judged=%d failed=%d classes=%v\n", *mode, st.Pending, st.Judged, st.Failed, st.ByClass)
		}
		if err != nil {
			fail(err)
		}
		if st.Failed > 0 && st.Judged == 0 {
			fail(fmt.Errorf("every title judgement failed (%d); the model upstream is down", st.Failed))
		}
	case "intros", "char-intros":
		llm := newLLM()
		kind, loadSample, loadPending := textsafety.WorkIntros, textsafety.LoadIntroSample, textsafety.LoadPendingIntros
		if *mode == "char-intros" {
			kind, loadSample, loadPending = textsafety.CharacterIntros, textsafety.LoadCharacterIntroSample, textsafety.LoadPendingCharacterIntros
		}
		var pending []textsafety.IntroInput
		if *sample > 0 {
			pending, err = loadSample(ctx, db, *sample)
		} else {
			pending, err = loadPending(ctx, db, *limit)
		}
		if err != nil {
			fail(fmt.Errorf("load intros: %w", err))
		}
		st, err := textsafety.JudgeIntros(ctx, db, llm, opts, pending, kind)
		printUsage(llm)
		if st != nil {
			fmt.Printf("%s done pending=%d failed=%d modes=%v\n", *mode, st.Pending, st.Failed, st.ByMode)
		}
		if err != nil {
			fail(err)
		}
		if st.Failed > 0 && st.Failed == st.Pending {
			fail(fmt.Errorf("every intro judgement failed (%d); the model upstream is down", st.Failed))
		}
	case "apply":
		st, err := textsafety.Apply(ctx, db, textsafety.ApplyOpts{Run: *run, Prune: *prune})
		if err != nil {
			fail(err)
		}
		fmt.Println(st)
	case "report":
		if *reportOut == "" {
			fail(fmt.Errorf("-out is required for -mode report"))
		}
		if err := textsafety.Report(ctx, db, *reportOut, *reportN); err != nil {
			fail(err)
		}
	default:
		fail(fmt.Errorf("unknown -mode %q", *mode))
	}
}

func printUsage(llm *textsafety.LLM) {
	calls, in, out := llm.Usage()
	fmt.Printf("llm model=%s calls=%d prompt_tokens=%d completion_tokens=%d\n", llm.Model(), calls, in, out)
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func fail(err error) {
	slog.Error("text-safety", "error", err)
	os.Exit(1)
}
