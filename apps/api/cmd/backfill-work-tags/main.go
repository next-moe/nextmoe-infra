package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"api/internal/jobs/worktags"
	"api/pkg/config"
	"api/pkg/logger"

	"github.com/joho/godotenv"
)

func main() {
	apply := flag.Bool("apply", false, "write changes (default: dry run, counters + samples only)")
	dsn := flag.String("dsn", "", "catalog DSN (also hosts src_bangumi) — REQUIRED; the rehearsal copy locally, the live catalog only in the acceptance run")
	limit := flag.Int("limit", 0, "max candidate works (0 = all)")
	offset := flag.Int("offset", 0, "skip this many candidate works (for chunking)")
	flag.Parse()

	_ = godotenv.Load("apps/api/.env")

	if cfg, err := config.Load(); err == nil {
		logger.Init(cfg.Server.Env)
	}

	st, err := worktags.Run(context.Background(), worktags.Opts{
		Apply:  *apply,
		DSN:    *dsn,
		Limit:  *limit,
		Offset: *offset,
	})
	if err != nil {
		slog.Error("backfill-work-tags", "error", err)
		os.Exit(1)
	}
	slog.Info("backfill-work-tags summary",
		"apply", *apply,
		"candidates", st.Candidates,
		"no_tags", st.NoTags,
		"not_array", st.NotArray,
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
	// 2026-08-05: 120,646 failed writes exited 0, so neither the cron failure trap nor the watchdog fired.
	// The cause is also logged per row, but at WARN and under a message that does not name this tool
	// ("write tag"), so grepping the weekly log for "backfill-work-tags" found 15 days of errors=N and
	// not one line saying why — it was a NOT NULL column the staged binary predated. Restate it here.
	if st.Errors > 0 {
		slog.Error("backfill-work-tags write failures", "errors", st.Errors, "first_error", st.FirstError)
		os.Exit(1)
	}
}
