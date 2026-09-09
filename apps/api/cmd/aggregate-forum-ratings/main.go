package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"api/internal/jobs/forumratings"
	"api/pkg/config"
	"api/pkg/logger"

	"github.com/joho/godotenv"
)

func main() {
	dsn := flag.String("dsn", "", "catalog DSN (REQUIRED)")
	forumDSN := flag.String("forum-dsn", "", "forum DSN (REQUIRED)")
	minVoters := flag.Int("min-voters", 3, "raters required to publish a work's mean")
	apply := flag.Bool("apply", false, "write changes (default dry)")
	flag.Parse()

	_ = godotenv.Load("apps/api/.env")
	if cfg, err := config.Load(); err == nil {
		logger.Init(cfg.Server.Env)
	}

	st, err := forumratings.Run(context.Background(), forumratings.Opts{
		DSN: *dsn, ForumDSN: *forumDSN, Apply: *apply, MinVoters: *minVoters,
	})
	if err != nil {
		slog.Error("aggregate-forum-ratings failed", "error", err)
		os.Exit(1)
	}
	fmt.Printf("\n=== aggregate-forum-ratings %s ===\n", mode(*apply))
	fmt.Printf("eligible=%d candidates=%d unmapped=%d multi_claim=%d written=%d unchanged=%d deleted=%d errors=%d\n",
		st.Eligible, st.Candidates, st.Unmapped, st.MultiClaim, st.Written, st.Unchanged, st.Deleted, st.Errors)
	slog.Info("aggregate-forum-ratings summary",
		"apply", *apply,
		"eligible", st.Eligible,
		"candidates", st.Candidates,
		"unmapped", st.Unmapped,
		"multi_claim", st.MultiClaim,
		"written", st.Written,
		"unchanged", st.Unchanged,
		"deleted", st.Deleted,
		"errors", st.Errors,
	)
	if !*apply {
		slog.Info("DRY RUN — nothing written; re-run with --apply")
	}
	if st.Errors > 0 {
		os.Exit(1)
	}
}

func mode(apply bool) string {
	if apply {
		return "APPLY"
	}
	return "DRY RUN"
}
