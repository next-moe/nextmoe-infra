package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"api/internal/jobs/labelrelations"
	"api/pkg/config"
	"api/pkg/logger"

	"github.com/joho/godotenv"
)

func main() {
	apply := flag.Bool("apply", false, "write changes (default: dry run, counters only)")
	dsn := flag.String("dsn", "", "catalog DSN — REQUIRED (also hosts src_vndb); falls back to $CATALOG_DSN")
	flag.Parse()

	_ = godotenv.Load("apps/api/.env")

	if cfg, err := config.Load(); err == nil {
		logger.Init(cfg.Server.Env)
	}
	if *dsn == "" {
		*dsn = os.Getenv("CATALOG_DSN")
	}

	st, err := labelrelations.Run(context.Background(), labelrelations.Opts{Apply: *apply, DSN: *dsn})
	if err != nil {
		slog.Error("import-label-relations", "error", err)
		os.Exit(1)
	}
	slog.Info("import-label-relations done", "apply", *apply,
		"edges_total", st.EdgesTotal, "both_anchored", st.BothAnchored,
		"written", st.Written, "skipped_unanchored", st.SkippedUnanchored,
		"skipped_unknown_relation", st.SkippedUnknownRelation,
		"skipped_self", st.SkippedSelf, "skipped_suppressed", st.SkippedSuppressed, "deleted", st.Deleted)
	if st.SkippedUnknownRelation > 0 {
		slog.Warn("unmapped relation codes seen — VNDB extended its vocabulary; update relationmap.go",
			"rows", st.SkippedUnknownRelation)
	}
	if !*apply {
		slog.Info("DRY RUN — nothing written; re-run with --apply")
	}
}
