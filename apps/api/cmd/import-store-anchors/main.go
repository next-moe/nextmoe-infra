package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"api/internal/jobs/storeanchors"
	"api/pkg/logger"
)

func main() {
	dsn := flag.String("dsn", "", "catalog DSN (REQUIRED; also hosts src_vndb)")
	only := flag.String("only", "", "run a single lane: steam | dmm | dlsite | dlsite-en (default: all)")
	limit := flag.Int("limit", 0, "max candidates per lane (0 = all)")
	apply := flag.Bool("apply", false, "write changes (default: dry-run forecast only)")
	flag.Parse()

	logger.Init("development")
	st, err := storeanchors.Run(context.Background(), storeanchors.Opts{
		Apply: *apply, DSN: *dsn, Only: *only, Limit: *limit,
	})
	if err != nil {
		slog.Error("import-store-anchors failed", "error", err)
		os.Exit(1)
	}
	report(st, *apply)
}

func report(st *storeanchors.Stats, apply bool) {
	mode := "DRY"
	if apply {
		mode = "APPLY"
	}
	fmt.Printf("\n=== import-store-anchors %s ===\n", mode)
	var totalPlanned, totalWritten, totalErrors int
	for _, name := range st.Order {
		ls := st.Lanes[name]
		totalPlanned += ls.Planned
		totalWritten += ls.Written
		totalErrors += ls.Errors
		fmt.Printf("%-10s candidates=%d planned=%d (work_grain=%d) written=%d conflict=%d errors=%d\n",
			name, ls.Candidates, ls.Planned, ls.PlannedWorkGrain, ls.Written, ls.Conflict, ls.Errors)
		fmt.Printf("%-10s skipped: malformed=%d rejection=%d value_taken=%d ambiguous=%d dedup=%d sibling=%d work_held=%d\n",
			"", ls.SkippedMalformed, ls.SkippedRejection,
			ls.SkippedValueTaken, ls.SkippedAmbiguous, ls.SkippedDedup, ls.SkippedSibling, ls.SkippedWorkHeld)
		if len(ls.TakenSamples) > 0 {
			fmt.Printf("%-10s value_taken e.g. %s\n", "", strings.Join(ls.TakenSamples, ", "))
		}
		if len(ls.AmbiguousSamples) > 0 {
			fmt.Printf("%-10s ambiguous e.g. %s\n", "", strings.Join(ls.AmbiguousSamples, ", "))
		}
	}
	fmt.Printf("TOTAL      planned=%d written=%d errors=%d\n", totalPlanned, totalWritten, totalErrors)
	if totalErrors > 0 {
		os.Exit(1)
	}
}
