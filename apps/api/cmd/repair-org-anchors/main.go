package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"api/internal/jobs/orglabels"
	"api/pkg/logger"
)

func main() {
	dsn := flag.String("dsn", "", "catalog DSN — REQUIRED (also hosts src_vndb / src_bangumi)")
	egDSN := flag.String("eg-dsn", "", "erogamescape DSN (default: catalog DSN with dbname=erogamescape)")
	dlsiteDSN := flag.String("dlsite-dsn", "", "dlsite DSN (default: catalog DSN with dbname=dlsite)")
	decisions := flag.String("decisions", "", "JSONL of {label_id, src, ext, reason, target_label_id?} — REQUIRED")
	report := flag.String("report", "", "write one JSON result per decision here (default stdout)")
	apply := flag.Bool("apply", false, "write (default: dry run, every decision rolled back)")
	flag.Parse()
	logger.Init("development")

	f, err := os.Open(*decisions)
	if err != nil {
		slog.Error("open decisions", "error", err)
		os.Exit(1)
	}
	ds, err := orglabels.ReadRepairDecisions(f)
	_ = f.Close()
	if err != nil {
		slog.Error("read decisions", "error", err)
		os.Exit(1)
	}

	results, err := orglabels.RunRepair(context.Background(), orglabels.Opts{
		Apply: *apply, DSN: *dsn, EGDSN: *egDSN, DlsiteDSN: *dlsiteDSN,
	}, ds)
	if err != nil {
		slog.Error("repair-org-anchors failed", "error", err)
		os.Exit(1)
	}

	out := os.Stdout
	if *report != "" {
		if out, err = os.Create(*report); err != nil {
			slog.Error("create report", "error", err)
			os.Exit(1)
		}
		defer out.Close()
	}
	enc := json.NewEncoder(out)
	status := map[string]int{}
	how := map[string]int{}
	var rel, work, cred, intro, alias, link int64
	for _, r := range results {
		_ = enc.Encode(r)
		status[r.Status]++
		how[r.TargetHow]++
		rel, work, cred = rel+r.ReleaseRows, work+r.WorkRows, cred+r.Credits
		intro, alias, link = intro+r.Intros, alias+r.Aliases, link+r.Links
	}
	mode := "DRY"
	if *apply {
		mode = "APPLY"
	}
	fmt.Fprintf(os.Stderr, "\n=== repair-org-anchors %s ===\ndecisions=%d status=%v target=%v\n", mode, len(results), status, how)
	fmt.Fprintf(os.Stderr, "moved: release_rows=%d work_rows=%d credits=%d intros=%d aliases=%d links=%d\n",
		rel, work, cred, intro, alias, link)
	if status["error"] > 0 {
		os.Exit(1)
	}
}
