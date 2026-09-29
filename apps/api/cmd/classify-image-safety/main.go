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
	mode := flag.String("mode", "", "scan | report | backfill | grade")
	dsn := flag.String("dsn", "", "images DSN (REQUIRED for scan)")
	out := flag.String("out", "", "scan: JSONL output path (appended — resume-safe)")
	in := flag.String("in", "", "report: JSONL input path")

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
	apply := flag.Bool("apply", false, "backfill: write review_labels->auto; grade: write review_labels->grade (dry run without it)")

	cfAccount := flag.String("cf-account", os.Getenv("CLOUDFLARE_ACCOUNT_ID"), "grade: Cloudflare account id")
	cfToken := flag.String("cf-token", os.Getenv("CLOUDFLARE_API_TOKEN"), "grade: Workers AI token")
	cfModel := flag.String("cf-model", "@cf/moondream/moondream3.1-9B-A2B", "grade: Workers AI vision model")
	maxNeurons := flag.Float64("max-neurons", 0, "grade: stop once this many neurons are spent (0 = no cap)")
	guardDSN := flag.String("guard-dsn", "", "grade: kun_ai DSN watched for live fail-open (empty disables the guard)")
	guardShare := flag.Float64("guard-share", 0.106, "grade: abort when the live ai_usage failure share exceeds this")
	quotaWait := flag.Duration("quota-wait", time.Hour, "grade: how long to wait for a used-up Workers AI daily allocation to free before carrying the rest to the next run")
	quotaProbe := flag.Duration("quota-probe", 2*time.Minute, "grade: how often a held worker re-asks while the daily allocation is used up")
	carryMax := flag.Int64("carry-max", 0, "grade: fail when a run carries more than this many ungraded images to the next one (0 = never)")
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
	case "grade":
		err = runGrade(ctx, gradeOptions{
			DSN:         *dsn,
			BaseURL:     *baseURL,
			Variant:     *variant,
			Limit:       *limit,
			Batch:       *batch,
			Concurrency: *concurrency,
			Apply:       *apply,
			MaxNeurons:  *maxNeurons,
			GuardDSN:    *guardDSN,
			GuardShare:  *guardShare,
			QuotaWait:   *quotaWait,
			QuotaProbe:  *quotaProbe,
			CarryMax:    *carryMax,
			Client:      newMoondreamClient(*cfAccount, *cfToken, *cfModel),
		}, os.Stdout)
	case "report":
		err = runReport(*in, os.Stdout)
	default:
		err = fmt.Errorf("--mode must be scan, report, backfill or grade")
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
