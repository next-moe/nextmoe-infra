package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"api/internal/jobs/getchuattach"
	"api/pkg/logger"
)

func main() {
	dsn := flag.String("dsn", "", "catalog DSN (REQUIRED)")
	getchuDSN := flag.String("getchu-dsn", "", "Getchu staging DSN (REQUIRED)")
	apply := flag.Bool("apply", false, "write changes (default dry)")
	receipts := flag.String("receipts", "", "JSONL path, one line per planned action (dry and apply)")
	holdout := flag.Bool("holdout-report", false, "print the holdout measurement instead of planning; never writes")
	flag.Parse()

	logger.Init("development")
	st, err := getchuattach.Run(context.Background(), getchuattach.Opts{
		Apply: *apply, DSN: *dsn, GetchuDSN: *getchuDSN, Receipts: *receipts, HoldoutReport: *holdout,
	})
	if err != nil {
		slog.Error("reconcile-getchu failed", "error", err)
		os.Exit(1)
	}
	if *holdout {
		fmt.Printf("\n=== reconcile-getchu HOLDOUT ===\n")
	} else {
		fmt.Printf("\n=== reconcile-getchu %s ===\n", mode(*apply))
	}
	fmt.Printf("population=%d attached=%d uncorroborated=%d multi_hit=%d no_hit=%d rejected_skips=%d written=%d errors=%d\n",
		st.Population, st.Attached, st.Uncorroborated, st.MultiHit, st.NoHit, st.RejectedSkips, st.Written, st.Errors)
	if st.Errors > 0 {
		os.Exit(1)
	}
}

func mode(apply bool) string {
	if apply {
		return "APPLY"
	}
	return "DRY"
}
