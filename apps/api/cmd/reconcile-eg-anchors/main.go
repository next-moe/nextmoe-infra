package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"api/internal/jobs/eganchors"
	"api/pkg/logger"
)

func main() {
	dsn := flag.String("dsn", "", "catalog DSN (REQUIRED)")
	egDSN := flag.String("eg-dsn", "", "EG staging DSN (REQUIRED)")
	apply := flag.Bool("apply", false, "write changes (default dry)")
	receipts := flag.String("receipts", "", "JSONL path, one line per planned write (dry and apply)")
	flag.Parse()

	logger.Init("development")
	st, err := eganchors.Run(context.Background(), eganchors.Opts{
		Apply: *apply, DSN: *dsn, EGDSN: *egDSN, Receipts: *receipts,
	})
	if err != nil {
		slog.Error("reconcile-eg-anchors failed", "error", err)
		os.Exit(1)
	}
	fmt.Printf("\n=== reconcile-eg-anchors %s ===\n", mode(*apply))
	fmt.Printf("games=%d anchored_games=%d no_evidence_games=%d multi_games=%d twin_games=%d candidate_games=%d\n",
		st.Games, st.AnchoredGames, st.NoEvidenceGames, st.MultiGames, st.TwinGames, st.CandidateGames)
	fmt.Printf("rejected_skips=%d exact_planned=%d probable_planned=%d related_planned=%d corroborated=%d written=%d exists=%d errors=%d\n",
		st.RejectedSkips, st.ExactPlanned, st.ProbablePlanned, st.RelatedPlanned, st.Corroborated, st.Written, st.Exists, st.Errors)
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
