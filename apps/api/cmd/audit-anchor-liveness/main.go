package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"api/internal/jobs/anchorliveness"
)

func main() {
	dsn := flag.String("dsn", "", "catalog DSN — REQUIRED, never inferred from the environment")
	source := flag.String("source", "", "vndb | bangumi | erogamescape (one source per run)")
	egDSN := flag.String("eg-dsn", "", "EG mirror DSN (required with --source erogamescape, rejected otherwise)")
	apply := flag.Bool("apply", false, "write dead_at (default: report-only dry run)")
	receipts := flag.String("receipts", "", "apply only: JSONL path, one line per mark/clear, created on first write")
	flag.Parse()

	opts := anchorliveness.Opts{
		DSN: *dsn, EGDSN: *egDSN, Source: *source, Apply: *apply, Receipts: *receipts,
	}
	if err := anchorliveness.ValidateOpts(opts); err != nil {
		slog.Error("audit-anchor-liveness", "error", err)
		os.Exit(1)
	}
	_, err := anchorliveness.Run(context.Background(), opts)
	if err != nil {
		slog.Error("audit-anchor-liveness", "error", err)
		os.Exit(1)
	}
}
