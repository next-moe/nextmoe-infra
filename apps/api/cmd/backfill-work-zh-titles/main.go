// backfill-work-zh-titles — wave 210: give works a Chinese title.
//
// Lanes, in the order they must run (refs/proj/210):
//
//  1. --mode source --source bgm|vndb: the human supply that was never
//     imported. Fill-missing, provenance=source, no model involved.
//  2. --mode reclassify-forum-mt: re-labels the three forum accounts' imported
//     zh titles as machine, except the ones a source publishes verbatim.
//  3. --mode census / kanji: the deterministic split of what is left. Titles
//     that are Han with no kana need glyph conversion, not translation, and
//     latin-only titles need nothing at all.
//  4. --mode auto: the kana residue. Proposes, then gates the proposal three
//     ways, and writes only a CSV. --mode apply-csv installs the accepted rows
//     as provenance=machine.
//
// A machine title never displaces a source one, on any lane.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"api/internal/infrastructure/database"
	"api/pkg/config"
	"api/pkg/logger"
)

func main() {
	dsn := flag.String("dsn", "", "catalog DSN (REQUIRED)")
	mode := flag.String("mode", "census", "census | source | kanji | auto | export-batches | apply-csv | reclassify-forum-mt | series-audit")
	source := flag.String("source", "", "--mode source: bgm | vndb")
	apply := flag.Bool("apply", false, "write (default: dry — counts + samples)")
	idsFile := flag.String("ids-file", "", "--mode reclassify-forum-mt: file of forum galgame ids, one per line")
	in := flag.String("in", "", "--mode apply-csv: the reviewed CSV to install")
	out := flag.String("out", "", "--mode auto/kanji: output CSV path")
	limit := flag.Int("limit", 0, "cap the candidates this run; 0 = all")
	samples := flag.Int("samples", 10, "how many sample rows to print")
	batchSize := flag.Int("batch-size", defaultBatchSize, "--mode auto: max works per series batch")
	shard := flag.String("shard", "", "--mode auto: i/n — process only works with id %% n == i")
	excludeIDs := flag.String("exclude-ids", "", "--mode auto: file of work ids to skip, for cheap resume")
	openccBin := flag.String("opencc", "", "--mode kanji: path to the opencc binary; without it the worklist carries no conversion")
	openccCfg := flag.String("opencc-config", "jp2s.json", "--mode kanji: opencc config name")
	modelName := flag.String("model", envOr("KUN_INTRO_MT_LLM_MODEL", envOr("KUN_AI_UPSTREAM_MODEL", config.DefaultAIUpstreamModel)), "served model id")
	llmBase := flag.String("llm-base", envOr("KUN_INTRO_MT_LLM_BASE", os.Getenv("KUN_AI_UPSTREAM_BASE_URL")), "OpenAI-compatible gateway base URL (…/v1)")
	llmToken := flag.String("llm-token", envOr("KUN_INTRO_MT_LLM_TOKEN", os.Getenv("KUN_AI_UPSTREAM_TOKEN")), "gateway bearer token")
	maxTokens := flag.Int("max-tokens", 4096, "--mode auto: max_tokens (thinking models spend most of it before the title)")
	delayMS := flag.Int("delay-ms", 0, "--mode auto: delay between gateway calls (ms)")
	flag.Parse()

	logger.Init("development")

	if *dsn == "" {
		slog.Error("--dsn is required")
		os.Exit(2)
	}
	db, err := database.OpenJob(*dsn)
	if err != nil {
		slog.Error("open database", "error", err)
		os.Exit(1)
	}
	ctx := context.Background()

	switch *mode {
	case "census":
		err = runCensus(ctx, db, *samples)
	case "source":
		if *source == "" {
			slog.Error("--mode source requires --source bgm|vndb")
			os.Exit(2)
		}
		err = runSource(ctx, db, *source, *apply, *limit, *samples)
	case "kanji":
		err = runKanji(ctx, db, openccFrom(*openccBin, *openccCfg), *out, *limit, *samples)
	case "series-audit":
		err = runSeriesAudit(ctx, db, *samples)
	case "reclassify-forum-mt":
		if *idsFile == "" {
			slog.Error("--mode reclassify-forum-mt requires --ids-file")
			os.Exit(2)
		}
		err = runReclassify(ctx, db, *idsFile, *apply, *samples)
	case "apply-csv":
		if *in == "" {
			slog.Error("--mode apply-csv requires --in")
			os.Exit(2)
		}
		err = runApplyCSV(ctx, db, *in, *apply, *limit, *samples)
	case "export-batches":
		if *out == "" {
			slog.Error("--mode export-batches requires --out (the JSONL path)")
			os.Exit(2)
		}
		var opts autoOpts
		opts, err = autoOptsFrom(*out, *limit, *batchSize, *shard, *excludeIDs, *delayMS, *modelName)
		if err == nil {
			err = runExportBatches(ctx, db, opts)
		}
	case "auto":
		if *out == "" {
			slog.Error("--mode auto requires --out (the review CSV path)")
			os.Exit(2)
		}
		tr := newHTTPTranslator(*llmBase, *llmToken, *modelName, *maxTokens)
		if !tr.Configured() {
			fmt.Println("BLOCKED: LLM gateway not configured (need --llm-base + --llm-token, or KUN_INTRO_MT_LLM_* / KUN_AI_UPSTREAM_*).\n" +
				"This is a designed precondition for --mode auto, not a failure.")
			os.Exit(3)
		}
		var opts autoOpts
		opts, err = autoOptsFrom(*out, *limit, *batchSize, *shard, *excludeIDs, *delayMS, *modelName)
		if err == nil {
			err = runAuto(ctx, db, tr, opts)
		}
	default:
		slog.Error("unknown --mode", "mode", *mode)
		os.Exit(2)
	}
	if err != nil {
		slog.Error("run failed", "error", err)
		os.Exit(1)
	}
}

func autoOptsFrom(out string, limit, batchSize int, shard, excludePath string, delayMS int, modelName string) (autoOpts, error) {
	shardI, shardN, err := parseShard(shard)
	if err != nil {
		return autoOpts{}, err
	}
	exclude, err := loadExcludeIDs(excludePath)
	if err != nil {
		return autoOpts{}, err
	}
	return autoOpts{
		Out: out, Limit: limit, BatchSize: batchSize,
		ShardI: shardI, ShardN: shardN, Exclude: exclude,
		Delay: time.Duration(delayMS) * time.Millisecond, Model: modelName,
	}, nil
}

func openccFrom(bin, config string) converter {
	if bin == "" {
		return nil
	}
	return openccConverter{bin: bin, config: config}
}

func parseShard(s string) (int, int, error) {
	if s == "" {
		return 0, 0, nil
	}
	var i, n int
	if _, err := fmt.Sscanf(s, "%d/%d", &i, &n); err != nil || n < 1 || i < 0 || i >= n {
		return 0, 0, fmt.Errorf("bad --shard %q (want i/n with 0 <= i < n)", s)
	}
	return i, n, nil
}

func loadExcludeIDs(path string) (map[int64]bool, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := make(map[int64]bool)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		id, err := strconv.ParseInt(line, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%s: bad id %q", path, line)
		}
		out[id] = true
	}
	return out, sc.Err()
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
