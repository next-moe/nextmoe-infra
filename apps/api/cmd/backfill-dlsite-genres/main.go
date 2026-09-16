package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"api/internal/jobs/dlsitegenres"
	"api/pkg/config"
	"api/pkg/logger"

	"github.com/joho/godotenv"
)

func main() {
	apply := flag.Bool("apply", false, "write changes (default: dry run, counters + samples only)")
	dsn := flag.String("dsn", "", "catalog DSN — REQUIRED; the rehearsal copy locally, the live catalog only in the acceptance run")
	dlsiteDSN := flag.String("dlsite-dsn", "", "DLsite mirror DSN (the dlsite database, also hosts genre_taxonomy) — REQUIRED")
	limit := flag.Int("limit", 0, "max candidate works (0 = all)")
	offset := flag.Int("offset", 0, "skip this many candidate works (for chunking)")
	flag.Parse()

	_ = godotenv.Load("apps/api/.env")

	if cfg, err := config.Load(); err == nil {
		logger.Init(cfg.Server.Env)
	}

	st, err := dlsitegenres.Run(context.Background(), dlsitegenres.Opts{
		Apply:     *apply,
		DSN:       *dsn,
		DlsiteDSN: *dlsiteDSN,
		Limit:     *limit,
		Offset:    *offset,
	})
	if err != nil {
		slog.Error("backfill-dlsite-genres", "error", err)
		os.Exit(1)
	}
	hitRate := 0.0
	if st.ZhHit+st.JaFallback > 0 {
		hitRate = float64(st.ZhHit) / float64(st.ZhHit+st.JaFallback)
	}
	slog.Info("backfill-dlsite-genres summary",
		"apply", *apply,
		"taxonomy_rows", st.TaxonomyRows,
		"candidates", st.Candidates,
		"skipped_bundle", st.SkippedBundle,
		"missing_mirror", st.MissingMirror,
		"no_genres", st.NoGenres,
		"not_array", st.NotArray,
		"zh_hit", st.ZhHit,
		"ja_fallback", st.JaFallback,
		"taxonomy_hit_rate", hitRate,
		"name_blank", st.NameBlank,
		"dup_collapsed", st.DupCollapsed,
		"planned", st.Planned,
		"distinct_names", st.DistinctNames,
		"written", st.Written,
		"sexual_inherited", st.SexualInherited,
		"conflict", st.Conflict,
		"errors", st.Errors,
	)
	if !*apply {
		slog.Info("DRY RUN — nothing written; re-run with --apply")
	}
	if st.Errors > 0 {
		os.Exit(1)
	}
}
