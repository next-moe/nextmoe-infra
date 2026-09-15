package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/llmsuggest"
	"api/internal/platform/catalog/repository"
	"api/internal/platform/catalog/service"
	"api/pkg/config"
	"api/pkg/logger"

	"github.com/joho/godotenv"
	"gorm.io/gorm"
)

const defaultGoldSet = "internal/platform/catalog/llmsuggest/testdata/goldset.jsonl"

func main() {
	task := flag.String("task", "", "goldset | residue | sanity | queue-creditname | queue-workpair | queue-refs")
	mode := flag.String("mode", "task", "task | apply | calibrate")
	queue := flag.String("queue", "", "apply/calibrate: creditname | workpair | ref")
	actor := flag.Int64("actor", 0, "apply: operator user id stamped on live writes")
	minConf := flag.Float64("min-confidence", 0.9, "apply: minimum confidence to accept a merge or confirm a ref")
	minConfReject := flag.Float64("min-confidence-reject", 0.7, "apply: minimum confidence to reject a candidate")
	families := flag.String("families", "all", "queue-refs: chain | llm | all")
	apply := flag.Bool("apply", false, "write suggestions (default: dry run — sample + print)")
	limit := flag.Int("limit", 0, "cap items processed (0 = all); dry-run sample size")
	conc := flag.Int("concurrency", 4, "parallel LLM calls (<=4)")
	llmBase := flag.String("llm-base", "http://127.0.0.1:8002/v1", "vLLM OpenAI base URL")
	model := flag.String("model", "qwen3-14b", "served model id")
	goldPath := flag.String("goldset", defaultGoldSet, "gold set JSONL path")
	egDSN := flag.String("eg-dsn", "", "erogamescape staging DSN (default: erogamescape on the catalog server)")
	hltbDSN := flag.String("hltb-dsn", "", "howlongtobeat mirror DSN (default: howlongtobeat on the catalog server)")
	buildGold := flag.Bool("build-goldset", false, "regenerate the gold set JSONL from local dumps, then exit")
	calibrate := flag.Bool("calibrate", false, "print calibration metrics from persisted goldset verdicts, then exit")
	batch := flag.Bool("batch", false, "goldset: judge in batches (throughput comparison; prompt_version v1-batch)")
	flag.Parse()

	promptVersion := llmsuggest.PromptNamePairV1
	if *batch {
		promptVersion = llmsuggest.PromptNamePairV1B
	}

	_ = godotenv.Load("apps/api/.env")
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}
	logger.Init(cfg.Server.Env)

	catalogDB, err := database.NewPostgresDB(cfg.CatalogDatabase)
	if err != nil {
		slog.Error("catalog db connect", "error", err)
		os.Exit(1)
	}
	defer catalogDB.Close()
	if err := llmsuggest.EnsureSchema(catalogDB.DB()); err != nil {
		slog.Error("ensure src_llm schema", "error", err)
		os.Exit(1)
	}

	if *conc > 4 {
		*conc = 4
	}
	ctx := context.Background()

	if *buildGold {
		egDB := openEG(cfg, *egDSN)
		stats, err := llmsuggest.BuildGoldSet(catalogDB.DB(), egDB, *goldPath)
		if err != nil {
			slog.Error("build gold set", "error", err)
			os.Exit(1)
		}
		fmt.Printf("gold set written: %s\n", *goldPath)
		fmt.Printf("total=%d positive=%d negative=%d\n", stats.Total, stats.Positive, stats.Negative)
		fmt.Printf("layers: %v\n", stats.Layers)
		fmt.Printf("paren filtered: role=%d circle=%d persona=%d\n",
			stats.ParenFilteredRole, stats.ParenFilteredCircle, stats.ParenFilteredPersona)
		return
	}

	opts := llmsuggest.Options{
		Model: *model, Concurrency: *conc, Limit: *limit, DryRun: !*apply, GoldSetPath: *goldPath, Batch: *batch,
		Actor: *actor, MinConfidence: *minConf, MinConfidenceReject: *minConfReject,
		Families: *families, Queue: *queue,
	}

	switch *families {
	case "", llmsuggest.FamiliesAll, llmsuggest.FamiliesChain, llmsuggest.FamiliesLLM:
	default:
		fmt.Fprintf(os.Stderr, "unknown --families %q (want chain|llm|all)\n", *families)
		os.Exit(2)
	}

	if *mode == "apply" {
		if *actor <= 0 {
			fmt.Fprintln(os.Stderr, "--actor <user-id> is required for --mode apply")
			os.Exit(2)
		}
		up := llmsuggest.StagingDBs{EG: tryEG(cfg, *egDSN)}
		st, err := llmsuggest.RunApply(ctx, catalogDB.DB(), up, adminQueue(catalogDB.DB()), opts)
		fail(err)
		fmt.Printf("apply queue=%s applied=%d counts=%v dry=%v\n", *queue, st.Applied, st.Counts, opts.DryRun)
		if opts.DryRun {
			fmt.Println("[dry run] nothing written — re-run with --apply")
		}
		return
	}

	if *mode == "calibrate" {
		if *queue != llmsuggest.QueueWorkPair && *queue != llmsuggest.QueueRef {
			fmt.Fprintln(os.Stderr, "--mode calibrate requires --queue workpair|ref")
			os.Exit(2)
		}
		client := mustLLM(ctx, *llmBase, *model)
		var up llmsuggest.StagingDBs
		if *queue == llmsuggest.QueueRef {
			up = llmsuggest.StagingDBs{
				EG:   tryEG(cfg, *egDSN),
				HLTB: tryStaging(cfg, *hltbDSN, "howlongtobeat"),
			}
		}
		metrics, err := llmsuggest.RunCalibrateQueue(ctx, catalogDB.DB(), up, client, opts)
		fail(err)
		printCalibration(metrics)
		if opts.DryRun {
			fmt.Println("[dry run] nothing written — re-run with --apply")
		}
		return
	}

	if *calibrate {
		metrics, err := llmsuggest.Calibrate(catalogDB.DB(), *model, promptVersion)
		if err != nil {
			slog.Error("calibrate", "error", err)
			os.Exit(1)
		}
		printCalibration(metrics)
		return
	}

	if *task == "" {
		slog.Error("no --task given (goldset | residue | sanity | queue-creditname | queue-workpair | queue-refs), or use --build-goldset / --calibrate / --mode apply|calibrate")
		os.Exit(2)
	}

	var client *llmsuggest.Client
	if needsLLM(*task, *families) {
		client = mustLLM(ctx, *llmBase, *model)
	}

	failures := 0
	switch *task {
	case "goldset":
		judged, errs, err := llmsuggest.RunGoldset(ctx, catalogDB.DB(), client, opts)
		fail(err)
		failures = errs
		if *apply {
			slog.Info("goldset done", "batch", *batch, "judged", judged, "errors", errs)
			metrics, err := llmsuggest.Calibrate(catalogDB.DB(), *model, promptVersion)
			fail(err)
			printCalibration(metrics)
		}
	case "residue":
		done, errs, err := llmsuggest.RunResidue(ctx, catalogDB.DB(), client, opts)
		fail(err)
		failures = errs
		if *apply {
			slog.Info("residue done", "extracted", done, "errors", errs)
		}
	case "sanity":
		mean, sampled, err := llmsuggest.RunSanity(ctx, catalogDB.DB(), client, *limit)
		fail(err)
		fmt.Printf("sanity: extraction↔parser key overlap = %.3f over %d parse-OK infoboxes\n", mean, sampled)
		return
	case "queue-creditname":
		judged, errs, err := llmsuggest.RunQueueCreditName(ctx, catalogDB.DB(), client, opts)
		fail(err)
		failures = errs
		slog.Info("queue-creditname done", "judged", judged, "errors", errs, "dry", opts.DryRun)
	case "queue-workpair":
		judged, errs, err := llmsuggest.RunQueueWorkPair(ctx, catalogDB.DB(), client, opts)
		fail(err)
		failures = errs
		slog.Info("queue-workpair done", "judged", judged, "errors", errs, "dry", opts.DryRun)
	case "queue-refs":
		up := llmsuggest.StagingDBs{EG: tryEG(cfg, *egDSN), HLTB: tryStaging(cfg, *hltbDSN, "howlongtobeat")}
		judged, errs, err := llmsuggest.RunQueueRefs(ctx, catalogDB.DB(), up, client, opts)
		fail(err)
		failures = errs
		slog.Info("queue-refs done", "judged", judged, "errors", errs, "families", *families, "dry", opts.DryRun)
	default:
		slog.Error("unknown task", "task", *task)
		os.Exit(2)
	}
	if !*apply {
		fmt.Println("[dry run] nothing written — re-run with --apply")
	}
	if failures > 0 {
		os.Exit(1)
	}
}

func needsLLM(task, families string) bool {
	switch task {
	case "queue-refs":
		return families != llmsuggest.FamiliesChain
	case "goldset", "residue", "sanity", "queue-creditname", "queue-workpair":
		return true
	default:
		return false
	}
}

func mustLLM(ctx context.Context, base, model string) *llmsuggest.Client {
	client := llmsuggest.NewClient(base, model)
	models, err := client.Ping(ctx)
	if err != nil {
		fmt.Printf("BLOCKED: local vLLM unreachable at %s (%v).\n"+
			"Start kungal-llm-infra and retry — this is a designed precondition, not a failure.\n", base, err)
		os.Exit(3)
	}
	slog.Info("vLLM reachable", "base", base, "models", models)
	return client
}

func openEG(cfg *config.Config, dsn string) *gorm.DB {
	db := tryEG(cfg, dsn)
	if db == nil {
		os.Exit(1)
	}
	return db
}

func tryEG(cfg *config.Config, dsn string) *gorm.DB {
	return tryStaging(cfg, dsn, "erogamescape")
}

// Every upstream mirror is a database on the catalog's own postgres server, so
// the default is the catalog credentials with the dbname swapped. A mirror that
// will not open is not fatal: the lane that needs it reports its rows as
// unavailable and the others still run.
func tryStaging(cfg *config.Config, dsn, dbName string) *gorm.DB {
	if dsn == "" {
		c := cfg.CatalogDatabase
		c.DBName = dbName
		dsn = c.DSN()
	}
	db, err := database.OpenJob(dsn)
	if err != nil {
		slog.Warn("staging mirror connect", "database", dbName, "error", err)
		return nil
	}
	return db
}

func adminQueue(db *gorm.DB) *service.AdminQueueService {
	resolve := service.NewResolveService(repository.NewRedirectRepository(db))
	merge := service.NewMergeService(db, resolve,
		repository.NewProposalRepository(db), repository.NewRevisionRepository(db))
	return service.NewAdminQueueService(db, merge)
}

func printCalibration(metrics []llmsuggest.LayerMetrics) {
	fmt.Printf("\n%-26s %5s %4s %4s %4s %4s %6s %6s %6s %8s\n",
		"layer", "n", "TP", "FP", "FN", "TN", "unsure", "prec", "recall", "acc(-uns)")
	for _, m := range metrics {
		fmt.Printf("%-26s %5d %4d %4d %4d %4d %6d %6.3f %6.3f %8.3f\n",
			m.Layer, m.N, m.TP, m.FP, m.FN, m.TN, m.Unsure, m.Precision, m.Recall, m.AccuracyExclUnsure)
	}
}

func fail(err error) {
	if err != nil {
		slog.Error("task failed", "error", err)
		os.Exit(1)
	}
}
