package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"api/internal/jobs/releasemeta"
	"api/pkg/config"
	"api/pkg/logger"

	"github.com/joho/godotenv"
)

func main() {
	apply := flag.Bool("apply", false, "write changes (default: dry run, counters + samples only)")
	dsn := flag.String("dsn", "", "catalog DSN (also hosts src_bangumi) — REQUIRED; the rehearsal copy locally, the live catalog only in the acceptance run")
	dlsiteDSN := flag.String("dlsite-dsn", "", "DLsite mirror DSN (the dlsite database) — REQUIRED")
	egDSN := flag.String("eg-dsn", "", "EG mirror DSN (the erogamescape database) — REQUIRED")
	getchuDSN := flag.String("getchu-dsn", "", "Getchu mirror DSN (the getchu database) — REQUIRED")
	receipts := flag.String("receipts", "", "apply: append one JSON line per written date (created only on the first write)")
	limit := flag.Int("limit", 0, "max candidates on the ordered date-sync list and the rating lane (0 = all)")
	offset := flag.Int("offset", 0, "skip this many candidates on the ordered date-sync list and the rating lane (for chunking)")
	flag.Parse()

	_ = godotenv.Load("apps/api/.env")

	if cfg, err := config.Load(); err == nil {
		logger.Init(cfg.Server.Env)
	}

	st, err := releasemeta.Run(context.Background(), releasemeta.Opts{
		Apply:     *apply,
		DSN:       *dsn,
		DlsiteDSN: *dlsiteDSN,
		EGDSN:     *egDSN,
		GetchuDSN: *getchuDSN,
		Limit:     *limit,
		Offset:    *offset,
		Receipts:  *receipts,
	})
	if err != nil {
		slog.Error("backfill-release-meta", "error", err)
		os.Exit(1)
	}
	summary := []any{"apply", *apply}
	summary = append(summary, st.DateLogArgs()...)
	summary = append(summary,
		"rating_candidates", st.RatingCandidates,
		"rating_vndb_r18", st.RatingVndbR18,
		"rating_dl_r18", st.RatingDlR18, "rating_dl_sensitive", st.RatingDlSensitive,
		"rating_dl_all_ages", st.RatingDlAllAges,
		"rating_eg_r18", st.RatingEgR18,
		"rating_bgm_r18", st.RatingBgmR18,
		"rating_no_verdict", st.RatingNoVerdict, "rating_planned", st.RatingPlanned,
		"rating_filled", st.RatingFilled, "rating_skipped_non_empty", st.RatingSkippedNonEmpty,
		"rating_curated_override", st.RatingCuratedOverride,
		"rating_all_ages_verdicts", st.RatingDlAllAges,
		"errors", st.Errors,
	)
	slog.Info("backfill-release-meta summary", summary...)
	if !*apply {
		slog.Info("DRY RUN — nothing written; re-run with --apply")
	}
	if st.Errors > 0 {
		os.Exit(1)
	}
}
