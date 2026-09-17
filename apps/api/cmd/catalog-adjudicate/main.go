package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"api/internal/jobs/personadj"
	"api/pkg/logger"
)

func main() {
	packets := flag.String("packets", "", "packets JSONL produced by the wave's builder — REQUIRED")
	verdicts := flag.String("verdicts", "", "verdicts JSONL, appended to and used for resume — REQUIRED")
	errorsPath := flag.String("errors", "", "optional JSONL of refused/failed packets")
	bucket := flag.String("bucket", "", "judge only this bucket (default: every bucket in the packets file)")
	workers := flag.Int("workers", 16, "concurrent requests")
	limit := flag.Int("limit", 0, "judge at most this many unjudged packets (0 = all) — the canary knob")
	maxTokens := flag.Int("max-tokens", 24576, "completion budget; glm is a thinking model, a small cap truncates and the answer is refused")
	model := flag.String("model", "", "model id (default: $KUN_AI_UPSTREAM_MODEL)")
	chunk := flag.Int("chunk", 1, "pack this many cases into one request; the gateway ceiling is requests/min, so >1 is the throughput lever (a misaligned chunk is re-judged by a later -chunk 1 run)")
	rpm := flag.Int("rpm", 24, "requests per minute ceiling; the gateway quota is per-minute inference requests, so pacing beats worker count (0 = unpaced)")
	mock := flag.Bool("mock", false, "use the offline deterministic judge instead of the gateway")
	requestTimeout := flag.Duration("request-timeout", 0, "bound on one gateway call (0 = the judge default, 900s)")
	deadline := flag.Duration("deadline", 0, "stop starting new packets after this long and cancel the ones in flight (0 = none)")
	flag.Parse()

	logger.Init("development")
	if *packets == "" || *verdicts == "" {
		fmt.Fprintln(os.Stderr, "-packets and -verdicts are required")
		os.Exit(2)
	}
	if b := personadj.Bucket(*bucket); *bucket != "" && !personadj.ValidBucket(b) {
		fmt.Fprintf(os.Stderr, "unknown bucket %q\n", *bucket)
		os.Exit(2)
	}

	var judge personadj.Judge
	if *mock {
		judge = personadj.MockJudge{}
	} else {
		m := *model
		if m == "" {
			m = os.Getenv("KUN_AI_UPSTREAM_MODEL")
		}
		h := personadj.NewHTTPJudge(os.Getenv("KUN_AI_UPSTREAM_BASE_URL"), os.Getenv("KUN_AI_UPSTREAM_TOKEN"), m, *maxTokens, *rpm)
		if !h.Configured() {
			fmt.Fprintln(os.Stderr, "gateway not configured: set KUN_AI_UPSTREAM_BASE_URL and KUN_AI_UPSTREAM_TOKEN (or pass -mock)")
			os.Exit(2)
		}
		judge = h.WithRequestTimeout(*requestTimeout)
	}

	ctx := context.Background()
	if *deadline > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *deadline)
		defer cancel()
	}
	st, err := personadj.RunBatch(ctx, judge, personadj.BatchOpts{
		PacketsPath:  *packets,
		VerdictsPath: *verdicts,
		ErrorsPath:   *errorsPath,
		Bucket:       personadj.Bucket(*bucket),
		Workers:      *workers,
		Limit:        *limit,
		Chunk:        *chunk,
	})
	if st != nil {
		slog.Info("adjudicate done", "prompt_version", personadj.PromptVersion(),
			"packets", st.Packets, "skipped_already_judged", st.Skipped, "judged", st.Judged,
			"merge", st.Merge, "distinct", st.Distinct, "unsure", st.Unsure,
			"errors", st.Errors, "elapsed", st.Elapsed.Round(1e9))
	}
	if err != nil {
		slog.Error("adjudicate", "error", err)
		os.Exit(1)
	}
	if st != nil && st.Errors > 0 {
		os.Exit(1)
	}
}
