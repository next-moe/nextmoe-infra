package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"api/pkg/logger"
)

func main() {
	mode := flag.String("mode", "", "scan | report | backfill | tag | apply | regrade")
	dsn := flag.String("dsn", "", "images DSN (REQUIRED for scan)")
	out := flag.String("out", "", "scan, tag: JSONL output path (appended — resume-safe)")
	in := flag.String("in", "", "report: JSONL input path; tag: CSV of hash,ext; apply: tag's JSONL; regrade: JSONL of {hash, level}")

	limit := flag.Int("limit", 1000, "scan: images sampled; backfill: images processed (0 = all)")
	salt := flag.String("salt", "kungal-safety-v1", "scan: sample ordering salt")
	baseURL := flag.String("base-url", os.Getenv("KUN_IMAGE_PUBLIC_BASE_URL"), "scan: public image base URL")
	variant := flag.String("variant", "", "scan: variant name to score instead of the main image")

	omniBase := flag.String("omni-base", os.Getenv("KUN_AI_OMNI_BASE_URL"), "scan: moderations API root")
	omniToken := flag.String("omni-token", os.Getenv("KUN_AI_OMNI_TOKEN"), "scan: bearer token")
	omniModel := flag.String("omni-model", envOr("KUN_AI_OMNI_MODEL", "omni-moderation-latest"), "scan: model id")

	concurrency := flag.Int("concurrency", 8, "scan: parallel requests")
	qps := flag.Float64("qps", 8, "scan: request rate ceiling")

	batch := flag.Int("batch", 2000, "backfill: rows fetched per page")
	apply := flag.Bool("apply", false, "backfill: write review_labels->auto; apply, regrade: write review_labels->grade (dry run without it)")

	taggerURL := flag.String("tagger-url", os.Getenv("KUN_WDTAGGER_URL"), "tag: base URL of the wdtagger service (docker/wdtagger.Dockerfile)")
	readyWait := flag.Duration("tagger-ready-wait", 3*time.Minute, "tag: how long to wait for the tagger's /healthz before giving up")
	replaceMachine := flag.Bool("replace-machine", false, "apply: also replace moondream grades, keeping each under review_labels->grade_moondream")
	flag.Parse()

	logger.Init("production")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch *mode {
	case "scan":
		err = runScan(ctx, scanOptions{
			DSN:         *dsn,
			Out:         *out,
			Limit:       *limit,
			Salt:        *salt,
			BaseURL:     *baseURL,
			Variant:     *variant,
			Concurrency: *concurrency,
			QPS:         *qps,
			Client:      newOmniImageClient(*omniBase, *omniToken, *omniModel),
		}, os.Stdout)
	case "backfill":
		err = runBackfill(ctx, backfillOptions{
			DSN:         *dsn,
			BaseURL:     *baseURL,
			Variant:     *variant,
			Limit:       *limit,
			Batch:       *batch,
			Concurrency: *concurrency,
			QPS:         *qps,
			Apply:       *apply,
			Client:      newOmniImageClient(*omniBase, *omniToken, *omniModel),
		}, os.Stdout)
	case "tag":
		err = runTag(ctx, tagOptions{
			In:          *in,
			Out:         *out,
			BaseURL:     *baseURL,
			Concurrency: *concurrency,
			ReadyWait:   *readyWait,
			Tagger:      newWDClient(*taggerURL),
		}, os.Stdout)
	case "apply":
		err = runApply(ctx, applyOptions{DSN: *dsn, In: *in, ReplaceMachine: *replaceMachine, Apply: *apply}, os.Stdout)
	case "regrade":
		err = runRegrade(ctx, regradeOptions{DSN: *dsn, In: *in, Apply: *apply}, os.Stdout)
	case "report":
		err = runReport(*in, os.Stdout)
	default:
		err = fmt.Errorf("--mode must be scan, report, backfill, tag, apply or regrade")
	}
	if err != nil {
		slog.Error("classify-image-safety", "mode", *mode, "error", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
