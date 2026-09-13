package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"api/internal/infrastructure/database"
	"api/internal/jobs/wikizh"
	"api/pkg/config"
	"api/pkg/logger"

	"github.com/joho/godotenv"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	var (
		bucket      = fs.String("bucket", string(wikizh.BucketUsable), "judge: usable (no zh at all) | compare (a machine row holds the slot)")
		out         = fs.String("out", "", "judge: JSONL verdict file (appended; already-judged keys are skipped)")
		in          = fs.String("in", "", "apply: comma-separated JSONL verdict files — one per INDEPENDENT judging round. Auto-apply requires every round to agree.")
		apply       = fs.Bool("apply", false, "apply: write (default is a dry forecast)")
		tiebreak    = fs.String("tiebreak", "", "apply: verdicts that decide the works the rounds CONTESTED, and only those (the adversarial round)")
		receipts    = fs.String("receipts", "", "apply: where to write the row-id receipt (default: beside the first --in file, which must be writable)")
		onlyFile    = fs.String("only", "", "judge: restrict to the work ids in this file, one per line")
		adversarial = fs.Bool("adversarial", false, "judge: re-framed prompts for contested works — compare swaps A/B positions, usable must cite a disqualifying defect")
		limit       = fs.Int("limit", 0, "max candidates (0 = all)")
		chunk       = fs.Int("chunk", 5, "judge: candidates per gateway request")
		rpm         = fs.Int("rpm", 60, "judge: gateway requests per minute (even pacing, shared across workers)")
		workers     = fs.Int("workers", 6, "judge: concurrent chunks; the reasoning model's per-request latency dominates, so this is the throughput lever, while --rpm stays the politeness ceiling")
		maxTokens   = fs.Int("max-tokens", 24576, "judge: output budget; a reasoning model needs headroom or finish_reason trips")
		mock        = fs.Bool("mock", false, "judge: offline deterministic stand-in")
		llmBase     = fs.String("llm-base", os.Getenv("KUN_AI_UPSTREAM_BASE_URL"), "OpenAI-compatible base URL")
		llmToken    = fs.String("llm-token", os.Getenv("KUN_AI_UPSTREAM_TOKEN"), "bearer token")
		model       = fs.String("model", envOr("KUN_AI_UPSTREAM_MODEL", "glm-5.2"), "model id")
	)
	_ = fs.Parse(os.Args[2:])

	_ = godotenv.Load("apps/api/.env")
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
	ctx := context.Background()

	switch cmd {
	case "judge":
		if *out == "" {
			slog.Error("--out is required")
			os.Exit(2)
		}
		b := wikizh.Bucket(*bucket)
		cands, err := wikizh.LoadCandidates(ctx, db.DB(), b, *limit)
		if err != nil {
			slog.Error("load candidates", "error", err)
			os.Exit(1)
		}
		done, err := wikizh.LoadVerdictKeys(*out)
		if err != nil {
			slog.Error("read existing verdicts", "error", err)
			os.Exit(1)
		}
		only, err := readWorkIDs(*onlyFile)
		if err != nil {
			slog.Error("read --only list", "error", err)
			os.Exit(1)
		}
		pending := cands[:0]
		for _, c := range cands {
			if only != nil && !only[c.WorkID] {
				continue
			}
			if !done[c.Key()] {
				pending = append(pending, c)
			}
		}
		slog.Info("wiki-zh judge", "bucket", b, "candidates", len(cands),
			"restricted_to", len(only), "already_judged", len(done), "pending", len(pending))
		if only != nil && len(pending)+len(done) < len(only) {
			slog.Warn("some --only ids are not in this bucket's candidate set",
				"requested", len(only), "found", len(pending)+len(done))
		}

		var judge wikizh.Judge
		if *mock {
			judge = wikizh.MockJudge{}
			slog.Warn("MOCK judge — verdicts are NOT real judgements")
		} else {
			hj := wikizh.NewHTTPJudge(*llmBase, *llmToken, *model, *maxTokens, *rpm)
			if *adversarial {
				hj = hj.Adversarial()
				slog.Info("ADVERSARIAL pass — compare positions are swapped, usable must cite a defect")
			}
			if !hj.Configured() {
				fmt.Println("BLOCKED: LLM gateway not configured (need --llm-base + --llm-token or KUN_AI_UPSTREAM_*).")
				os.Exit(3)
			}
			judge = hj
			slog.Info("live gateway judge", "base", *llmBase, "model", *model)
		}

		f, err := os.OpenFile(*out, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			slog.Error("open verdict file", "error", err)
			os.Exit(1)
		}
		defer f.Close()
		enc := json.NewEncoder(f)

		type chunkJob struct{ from, to int }
		jobs := make(chan chunkJob)
		var wg sync.WaitGroup
		var mu sync.Mutex
		var judged, failed int

		for w := 0; w < max(*workers, 1); w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := range jobs {
					vs, err := judge.JudgeBatch(ctx, b, pending[j.from:j.to])
					mu.Lock()
					if err != nil {
						failed += j.to - j.from
						slog.Warn("chunk failed", "from", j.from, "to", j.to, "err", err)
					} else {
						for _, v := range vs {
							if e := enc.Encode(v); e != nil {
								slog.Error("write verdict", "error", e)
								mu.Unlock()
								os.Exit(1)
							}
							judged++
						}
						if judged%100 < *chunk {
							slog.Info("progress", "judged", judged, "of", len(pending), "failed", failed)
						}
					}
					mu.Unlock()
				}
			}()
		}
		for i := 0; i < len(pending); i += *chunk {
			jobs <- chunkJob{i, min(i+*chunk, len(pending))}
		}
		close(jobs)
		wg.Wait()
		slog.Info("wiki-zh judge done", "judged", judged, "failed", failed, "file", *out)
		if failed > 0 {
			os.Exit(1)
		}

	case "apply":
		if *in == "" {
			slog.Error("--in is required")
			os.Exit(2)
		}
		files := strings.Split(*in, ",")
		rounds := make([][]wikizh.Verdict, 0, len(files))
		for _, f := range files {
			f = strings.TrimSpace(f)
			if f == "" {
				continue
			}
			r, err := wikizh.LoadVerdicts(f)
			if err != nil {
				slog.Error("read verdicts", "file", f, "error", err)
				os.Exit(1)
			}
			slog.Info("round loaded", "file", f, "verdicts", len(r))
			rounds = append(rounds, r)
		}
		if len(rounds) < 2 {
			slog.Warn("SINGLE ROUND — a lone judging round cannot tell a borderline case from a confident one; " +
				"the wave-168 calibration saw 6 of 15 verdicts move between identical runs")
		}
		vs, cs := wikizh.Consensus(rounds)
		slog.Info("consensus", "result", cs.String())
		if *tiebreak != "" {
			tb, err := wikizh.LoadVerdicts(*tiebreak)
			if err != nil {
				slog.Error("read tiebreak verdicts", "file", *tiebreak, "error", err)
				os.Exit(1)
			}
			var ts wikizh.TiebreakStats
			vs, ts = wikizh.Tiebreak(vs, tb)
			slog.Info("tiebreak", "file", *tiebreak, "verdicts", len(tb), "result", ts.String())
		}
		if *limit > 0 && len(vs) > *limit {
			vs = vs[:*limit]
			slog.Warn("limited apply — a rehearsal, not the pass", "verdicts", len(vs))
		}

		st, err := wikizh.Apply(ctx, db.DB(), vs, *apply)
		if st != nil {
			slog.Info("wiki-zh apply done", "apply", *apply, "result", st.String())
			if *apply && len(st.ReceiptIDs) > 0 {
				name := fmt.Sprintf("%s.receipts.%d.json", strings.TrimSpace(files[0]), time.Now().Unix())
				if *receipts != "" {
					name = *receipts
				}
				b, _ := json.Marshal(st.ReceiptIDs)
				if err := os.WriteFile(name, b, 0o644); err != nil {
					slog.Warn("write receipts", "error", err)
				} else {
					slog.Info("receipts written", "file", name, "rows", len(st.ReceiptIDs))
				}
			}
		}
		if err != nil {
			slog.Error("apply", "error", err)
			os.Exit(1)
		}
		if st != nil && st.Errors > 0 {
			os.Exit(1)
		}

	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `catalog-wiki-zh <judge|apply> [flags]

  judge --bucket usable|compare --out FILE [--limit N] [--chunk 5] [--mock]
        run it N times to N different files — independent rounds are what
        distinguishes a borderline case from a confident one
  apply --in r1.jsonl,r2.jsonl,r3.jsonl [--apply]
        auto-applies only where EVERY round agreed and cleared the gate
`)
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func readWorkIDs(path string) (map[int64]bool, error) {
	if path == "" {
		return nil, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	ids := map[int64]bool{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		id, err := strconv.ParseInt(line, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse work id %q: %w", line, err)
		}
		ids[id] = true
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("%s contains no work ids", path)
	}
	return ids, nil
}
