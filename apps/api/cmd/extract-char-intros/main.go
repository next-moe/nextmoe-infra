// extract-char-intros — wave 178 P3, extended by wave 214 with the refresh
// bucket and a batched lane.
//
// A work's zh-Hans intro often embeds per-character introductions. For the
// roster characters that have NO zh-Hans intro row at all, this job asks the
// model to EXTRACT (never write) those passages and files them as
// source=derived, provenance=machine rows (adjudication refs/proj/178 §3:
// a character without an intro adopts the extracted passage directly).
//
// The panel bucket (--panel; adjudicated 2026-08-13, narrow (c)): a character
// whose zh-Hans face is a TRANSLATED machine row gets the extracted passage
// only after a three-vote judge panel prefers it with a unanimous direction.
// The row lands as source=derived, which the read faces elect above translated
// machine rows — never above source rows. When the incumbent is judged better
// nothing is written, so the verdict is re-checkable on any later run.
//
// The refresh bucket (--refresh; wave 214): a derived row is an excerpt, so a
// retranslated work intro leaves it quoting text that no longer exists — with
// the character names the retranslation fixed. src_hash names the intro the
// excerpt came from, and a row matching none of its works' current intros is
// re-extracted and rewritten in place.
//
// Anti-hallucination gate: an extracted passage must reappear verbatim
// (modulo whitespace and leading list markers) inside the work intro, or it
// is refused. The model is an index into the text, never an author.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"api/internal/infrastructure/database"
	"api/pkg/config"
	"api/pkg/logger"

	"gorm.io/gorm"
)

func main() {
	dsn := flag.String("dsn", "", "catalog DSN (REQUIRED)")
	apply := flag.Bool("apply", false, "write the extracted rows (default: dry — extraction runs, nothing written)")
	panel := flag.Bool("panel", false, "panel bucket: characters whose zh-Hans face is a translated machine row — extract, 3-vote compare, adopt only on a unanimous direction")
	refresh := flag.Bool("refresh", false, "refresh bucket: derived rows whose src_hash matches none of their works' current zh intros — re-extract from the fresh intro and rewrite in place")
	since := flag.String("since", "", "only works whose elected zh intro changed at or after this RFC3339 time (a re-scan trigger, not a filter on the rows)")
	limit := flag.Int("limit", 0, "max WORKS this run (0 = all) — ramp through a small limit first (the rehearsal rule)")
	offset := flag.Int("offset", 0, "skip the first N candidate works (stable id order)")
	workIDs := flag.String("work-ids", "", "comma-separated work ids to run instead of the whole bucket — the retry lane for works a batch failed on")
	gateway := flag.String("gateway", "openai", "openai (chat/completions) or cursor (agent runs, batched)")
	model := flag.String("model", envOr("KUN_INTRO_MT_LLM_MODEL", envOr("KUN_AI_UPSTREAM_MODEL", config.DefaultAIUpstreamModel)), "served model id")
	llmBase := flag.String("llm-base", envOr("KUN_INTRO_MT_LLM_BASE", os.Getenv("KUN_AI_UPSTREAM_BASE_URL")), "OpenAI-compatible gateway base URL (…/v1)")
	llmToken := flag.String("llm-token", envOr("KUN_INTRO_MT_LLM_TOKEN", os.Getenv("KUN_AI_UPSTREAM_TOKEN")), "gateway bearer token")
	cursorKeyFile := flag.String("cursor-key-file", "", "file holding the Cursor API key (or set CURSOR_API_KEY) — never pass a key on the command line")
	effort := flag.String("effort", "", "reasoning effort: low | medium | high (empty = the gateway's own default; the cursor lane needs one and falls back to low)")
	runTimeout := flag.Duration("run-timeout", 20*time.Minute, "how long one Cursor agent run may take")
	batch := flag.Int("batch", 0, "works per extraction call (0 = 20)")
	judgeBatch := flag.Int("judge-batch", 0, "comparisons per judge call (0 = 40)")
	workers := flag.Int("workers", 0, "concurrent calls in flight (0 = 1 for openai, 6 for cursor)")
	maxTokens := flag.Int("max-tokens", 16384, "max_tokens per call (openai gateway only)")
	delayMS := flag.Int("delay-ms", 0, "delay before each gateway call (ms)")
	samples := flag.Int("samples", 5, "how many extracted samples to print")
	flag.Parse()

	logger.Init("development")

	if *dsn == "" {
		slog.Error("--dsn is required")
		os.Exit(2)
	}
	if *panel && *refresh {
		slog.Error("--panel and --refresh are different buckets; pick one")
		os.Exit(2)
	}
	if *since != "" {
		if _, err := time.Parse(time.RFC3339, *since); err != nil {
			slog.Error("--since must be RFC3339", "value", *since, "error", err)
			os.Exit(2)
		}
	}
	ids, err := parseWorkIDs(*workIDs)
	if err != nil {
		slog.Error("--work-ids", "error", err)
		os.Exit(2)
	}
	db, err := database.OpenJob(*dsn)
	if err != nil {
		slog.Error("open database", "error", err)
		os.Exit(1)
	}

	var ex extractor
	var judge panelJudge
	switch *gateway {
	case "openai":
		http := newHTTPExtractor(*llmBase, *llmToken, *model, *effort, *maxTokens)
		if !http.Configured() {
			fmt.Println("BLOCKED: LLM gateway not configured (need --llm-base + --llm-token, or KUN_INTRO_MT_LLM_* / KUN_AI_UPSTREAM_*).")
			os.Exit(3)
		}
		ex, judge = http, http
		defaultTo(batch, 20)
		defaultTo(judgeBatch, 40)
		// The gateway serialises per account, so extra workers only buy 429s.
		defaultTo(workers, 1)
	case "cursor":
		key, err := readCursorKey(*cursorKeyFile)
		if err != nil {
			fmt.Println("BLOCKED: " + err.Error())
			os.Exit(3)
		}
		cur := newCursorClient(key, cursorModel(*model), orDefault(*effort, "low"), *runTimeout)
		ex, judge = cur, cur
		defaultTo(batch, 20)
		defaultTo(judgeBatch, 40)
		defaultTo(workers, 6)
	default:
		slog.Error("--gateway must be openai or cursor", "value", *gateway)
		os.Exit(2)
	}
	if !*panel {
		judge = nil
	}

	if err := run(context.Background(), db, ex, judge, opts{
		Apply: *apply, Panel: *panel, Refresh: *refresh,
		Cand:  candidateOpts{Limit: *limit, Offset: *offset, Since: *since, WorkIDs: ids},
		Batch: *batch, JudgeBatch: *judgeBatch, Workers: *workers,
		Delay: time.Duration(*delayMS) * time.Millisecond, Samples: *samples,
	}); err != nil {
		slog.Error("run failed", "error", err)
		os.Exit(1)
	}
}

type opts struct {
	Apply      bool
	Panel      bool
	Refresh    bool
	Cand       candidateOpts
	Batch      int
	JudgeBatch int
	Workers    int
	Delay      time.Duration
	Samples    int
}

func run(ctx context.Context, db *gorm.DB, ex extractor, judge panelJudge, o opts) error {
	load := loadCandidateWorks
	switch {
	case o.Panel:
		load = loadPanelCandidateWorks
	case o.Refresh:
		load = loadRefreshCandidateWorks
	}
	works, err := load(ctx, db, o.Cand)
	if err != nil {
		return err
	}
	modeStr := mode(o.Apply)
	switch {
	case o.Panel:
		modeStr = "PANEL " + modeStr
	case o.Refresh:
		modeStr = "REFRESH " + modeStr
	}
	fmt.Printf("\n=== extract-char-intros (%s) candidate_works=%d ===\n", modeStr, len(works))
	st := &stats{}
	w := &writer{db: db, apply: o.Apply, refresh: o.Refresh, st: st, samples: o.Samples, judge: judge}
	slots := newCallSlots(at(o.Workers, 1))
	dispatch := voteDispatch(judge, at(o.JudgeBatch, 1), slots, o.Delay)

	done := 0
	for res := range extractBatches(ctx, ex, chunkWorks(works, at(o.Batch, 1)), slots, o.Delay) {
		var contested []gated
		for i, cand := range res.works {
			e := res.out[i]
			if e.Err != nil {
				st.CallErrors++
				slog.Warn("extract failed", "work", cand.WorkID, "err", e.Err)
				continue
			}
			ready, c := w.gate(cand, e.Found, e.Model)
			for _, g := range ready {
				w.commit(ctx, g)
			}
			contested = append(contested, c...)
		}
		for _, g := range w.resolvePanel(ctx, contested, dispatch) {
			w.commit(ctx, g)
		}
		done += len(res.works)
		slog.Info("progress", "works", done, "of", len(works), "inserted", st.Inserted, "updated", st.Updated,
			"refused_not_verbatim", st.RefusedNotVerbatim, "call_errors", st.CallErrors)
	}
	if o.Apply {
		if err := w.flushTouch(ctx); err != nil {
			return err
		}
	}
	fmt.Printf("\nworks=%d extracted=%d inserted=%d updated=%d conflict=%d refused_not_verbatim=%d refused_not_chinese=%d refused_short=%d refused_name_absent=%d unmatched_name=%d call_errors=%d touched_works=%d\n",
		len(works), st.Extracted, st.Inserted, st.Updated, st.Conflict, st.RefusedNotVerbatim, st.RefusedNotChinese, st.RefusedShort, st.RefusedNameAbsent, st.UnmatchedName, st.CallErrors, st.Touched)
	if o.Panel {
		fmt.Printf("panel: adopted=%d kept_incumbent=%d panel_errors=%d\n", st.PanelAdopted, st.PanelKept, st.PanelErrors)
	}
	if !o.Apply {
		fmt.Println("[dry run] nothing written — re-run with --apply")
	}
	if st.CallErrors+st.PanelErrors > 0 {
		os.Exit(1)
	}
	return nil
}

type batchResult struct {
	works []candidateWork
	out   []extraction
}

func chunkWorks(works []candidateWork, size int) [][]candidateWork {
	var out [][]candidateWork
	for start := 0; start < len(works); start += size {
		out = append(out, works[start:min(start+size, len(works))])
	}
	return out
}

// callSlots caps how many gateway calls are in flight across BOTH stages. One
// semaphore per stage is not enough: extractBatches starts the next extraction
// the moment the previous one returns, so it overlaps the judge round the
// consumer is still working through — two calls in flight even at --workers 1.
// A gateway that serialises per account answers the second with 429, and after
// the backoff ladder that costs a whole batch (2026-08-22: 40 works).
type callSlots chan struct{}

func newCallSlots(n int) callSlots { return make(callSlots, max(n, 1)) }

func (s callSlots) acquire() { s <- struct{}{} }
func (s callSlots) release() { <-s }

// extractBatches runs the batches concurrently but delivers them in order, so
// a rerun with the same window walks the same works in the same sequence and
// the progress line means what it says.
func extractBatches(ctx context.Context, ex extractor, batches [][]candidateWork, slotsIn callSlots, delay time.Duration) <-chan batchResult {
	slots := make([]chan batchResult, len(batches))
	for i := range slots {
		slots[i] = make(chan batchResult, 1)
	}
	go func() {
		for i, b := range batches {
			slotsIn.acquire()
			go func(i int, b []candidateWork) {
				defer slotsIn.release()
				if delay > 0 {
					time.Sleep(delay)
				}
				slots[i] <- batchResult{works: b, out: ex.ExtractBatch(ctx, b)}
			}(i, b)
		}
	}()
	out := make(chan batchResult)
	go func() {
		defer close(out)
		for i := range slots {
			select {
			case r := <-slots[i]:
				out <- r
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

func voteDispatch(judge panelJudge, size int, slots callSlots, delay time.Duration) voteDispatcher {
	return func(ctx context.Context, cmps []comparison) []comparisonResult {
		out := make([]comparisonResult, len(cmps))
		var wg sync.WaitGroup
		for start := 0; start < len(cmps); start += size {
			end := min(start+size, len(cmps))
			wg.Add(1)
			slots.acquire()
			go func(start, end int) {
				defer wg.Done()
				defer slots.release()
				if delay > 0 {
					time.Sleep(delay)
				}
				res := judge.CompareBatch(ctx, cmps[start:end])
				if len(res) != end-start {
					err := fmt.Errorf("judge answered %d of %d votes", len(res), end-start)
					for i := start; i < end; i++ {
						out[i] = comparisonResult{Err: err}
					}
					return
				}
				copy(out[start:end], res)
			}(start, end)
		}
		wg.Wait()
		return out
	}
}

// readCursorKey keeps the key off the command line: a process list is world
// readable, an env file and a mode-600 file are not.
func readCursorKey(path string) (string, error) {
	if key := strings.TrimSpace(os.Getenv("CURSOR_API_KEY")); key != "" {
		return key, nil
	}
	if path == "" {
		return "", fmt.Errorf("cursor gateway needs CURSOR_API_KEY or --cursor-key-file")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read cursor key: %w", err)
	}
	key := strings.TrimSpace(string(raw))
	if key == "" {
		return "", fmt.Errorf("cursor key file %s is empty", path)
	}
	return key, nil
}

// cursorModel keeps --model meaningful for both gateways: the default names a
// model the OpenAI-compatible gateway serves, which Cursor does not.
func cursorModel(model string) string {
	if model == "" || strings.Contains(model, "glm") {
		return "grok-4.6"
	}
	return model
}

func defaultTo(v *int, n int) {
	if *v == 0 {
		*v = n
	}
}

func at(v, fallback int) int {
	if v < 1 {
		return fallback
	}
	return v
}

func mode(apply bool) string {
	if apply {
		return "APPLY"
	}
	return "DRY"
}

func parseWorkIDs(csv string) ([]int64, error) {
	csv = strings.TrimSpace(csv)
	if csv == "" {
		return nil, nil
	}
	var out []int64
	for field := range strings.SplitSeq(csv, ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		id, err := strconv.ParseInt(field, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a work id", field)
		}
		out = append(out, id)
	}
	return out, nil
}

// orDefault fills in for a flag the cursor lane cannot leave empty: its agent
// API requires an effort param, while the chat face is happy to omit one.
func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
