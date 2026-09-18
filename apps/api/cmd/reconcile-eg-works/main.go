package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"api/internal/jobs/egworks"
	"api/pkg/logger"
)

func main() {
	dsn := flag.String("dsn", "", "catalog DSN (REQUIRED)")
	egDSN := flag.String("eg-dsn", "", "EG staging DSN (REQUIRED)")
	apply := flag.Bool("apply", false, "write changes (default dry)")
	limit := flag.Int("limit", 0, "cap on games minted per run (0 = no cap); attaches are not capped")
	receipts := flag.String("receipts", "", "JSONL path, one line per planned action (dry and apply)")
	holdout := flag.Bool("holdout-report", false, "print the holdout measurement instead of planning; never writes")
	flag.Parse()

	logger.Init("development")
	st, err := egworks.Run(context.Background(), egworks.Opts{
		Apply: *apply, DSN: *dsn, EGDSN: *egDSN, Limit: *limit, Receipts: *receipts, HoldoutReport: *holdout,
	})
	if err != nil {
		slog.Error("reconcile-eg-works failed", "error", err)
		os.Exit(1)
	}
	if *holdout {
		fmt.Printf("\n=== reconcile-eg-works HOLDOUT ===\n")
	} else {
		fmt.Printf("\n=== reconcile-eg-works %s ===\n", mode(*apply))
	}
	fmt.Printf("population=%d pack_games=%d port_games=%d attached=%d quarantined=%d minted_live=%d edition_folded=%d\n",
		st.Population, st.PackGames, st.PortGames, st.Attached, st.Quarantined, st.MintedLive, st.EditionFolded)
	fmt.Printf("rejected_skips=%d limited=%d refs_planned=%d candidates_planned=%d written=%d errors=%d\n",
		st.RejectedSkips, st.Limited, st.RefsPlanned, st.CandidatesPlanned, st.Written, st.Errors)
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
