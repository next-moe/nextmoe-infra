package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
)

func main() {
	dsn := flag.String("dsn", "", "catalog DSN — REQUIRED, never inferred from the environment")
	fix := flag.Bool("fix", false, "repair works whose derived cover grade has drifted (default: report-only)")
	maxFix := flag.Int("max-fix", defaultMaxFix, "refuse to --fix more works than this in one run")
	failOnFindings := flag.Bool("fail-on-findings", false,
		fmt.Sprintf("exit %d when findings remain outstanding after the run", exitFindings))
	flag.Parse()

	if *dsn == "" {
		slog.Error("audit-cover-shelf", "error", "--dsn is required")
		os.Exit(1)
	}
	outstanding, err := run(context.Background(), *dsn, *fix, *maxFix, os.Stdout)
	if err != nil {
		slog.Error("audit-cover-shelf", "error", err)
		os.Exit(1)
	}
	if *failOnFindings && outstanding > 0 {
		os.Exit(exitFindings)
	}
}
