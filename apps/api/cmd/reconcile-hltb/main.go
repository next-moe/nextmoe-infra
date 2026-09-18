package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"api/internal/jobs/hltbattach"
	"api/pkg/logger"
)

func main() {
	dsn := flag.String("dsn", "", "catalog DSN (REQUIRED)")
	hltbDSN := flag.String("hltb-dsn", "", "HLTB staging DSN (REQUIRED)")
	apply := flag.Bool("apply", false, "write changes (default dry)")
	receipts := flag.String("receipts", "", "JSONL path, one line per planned action (dry and apply)")
	holdout := flag.Bool("holdout-report", false, "print the holdout measurement instead of planning; never writes")
	flag.Parse()

	logger.Init("development")
	st, err := hltbattach.Run(context.Background(), hltbattach.Opts{
		Apply: *apply, DSN: *dsn, HltbDSN: *hltbDSN, Receipts: *receipts, HoldoutReport: *holdout,
	})
	if err != nil {
		slog.Error("reconcile-hltb failed", "error", err)
		os.Exit(1)
	}
	if *holdout {
		fmt.Printf("\n=== reconcile-hltb HOLDOUT ===\n")
	} else {
		fmt.Printf("\n=== reconcile-hltb %s ===\n", mode(*apply))
	}
	fmt.Printf("population=%d attached=%d uncorroborated=%d multi_hit=%d no_hit=%d\n",
		st.Population, st.Attached, st.Uncorroborated, st.MultiHit, st.NoHit)
	fmt.Printf("rejected_skips=%d written=%d errors=%d\n",
		st.RejectedSkips, st.Written, st.Errors)
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
