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
	egDSN := flag.String("eg-dsn", "", "EG staging DSN (REQUIRED)")
	apply := flag.Bool("apply", false, "write changes (default dry)")
	receipts := flag.String("receipts", "", "JSONL path, one line per planned action (dry and apply)")
	holdout := flag.Bool("holdout-report", false, "print the holdout measurement instead of planning; never writes")
	flag.Parse()

	logger.Init("development")
	st, err := getchuattach.Run(context.Background(), getchuattach.Opts{
		Apply: *apply, DSN: *dsn, GetchuDSN: *getchuDSN, EGDSN: *egDSN,
		Receipts: *receipts, HoldoutReport: *holdout,
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
	fmt.Printf("population=%d attached=%d jan_vndb=%d jan_eg=%d title_date=%d title_cut=%d eg_brand=%d eg_near=%d jan_conflict=%d bundles=%d goods=%d all_ages=%d extras=%d general=%d addons=%d reissues=%d cancelled=%d undated=%d brand_unknown=%d unmapped_relations=%d eg_editions=%d rejected_skips=%d mint_groups=%d minted_live=%d minted_quarantined=%d candidates=%d written=%d errors=%d\n",
		st.Population, st.Attached, st.JanVNDB, st.JanEG, st.TitleDate, st.TitleCut, st.EGBrand, st.EGBrandNear, st.JanConflict,
		st.Bundles, st.Goods, st.AllAges, st.Extras, st.General, st.Addons, st.Reissues, st.Cancelled, st.Undated,
		st.BrandUnknown, st.UnmappedRelations, st.EGEditions, st.RejectedSkips, st.MintGroups,
		st.MintedLive, st.MintedQuarantined, st.Candidates, st.Written, st.Errors)
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
