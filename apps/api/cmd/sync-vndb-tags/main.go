package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"api/internal/jobs/vndbtags"
)

func main() {
	apply := flag.Bool("apply", false, "write changes (default: dry run)")
	dsn := flag.String("dsn", "", "catalog DSN — REQUIRED")
	receipts := flag.String("receipts", "", "apply: append one JSON line per write (created only on the first write)")
	minMirrorRows := flag.Int64("min-mirror-rows", 1_000_000,
		"refuse to run unless src_vndb.tags_vn holds at least this many rows")
	limit := flag.Int("limit", 0, "max population works, in ascending work_id order (0 = all)")
	flag.Parse()

	st, err := vndbtags.Run(context.Background(), vndbtags.Opts{
		Apply:         *apply,
		DSN:           *dsn,
		Receipts:      *receipts,
		MinMirrorRows: *minMirrorRows,
		Limit:         *limit,
	})
	if err != nil {
		slog.Error("sync-vndb-tags", "error", err)
		os.Exit(1)
	}
	summary := []any{"apply", *apply}
	summary = append(summary, st.LogArgs()...)
	slog.Info("sync-vndb-tags summary", summary...)
	if !*apply {
		slog.Info("DRY RUN — nothing written; re-run with --apply")
	}
	if st.Errors > 0 {
		slog.Error("sync-vndb-tags write failures", "errors", st.Errors, "first_error", st.FirstError)
		os.Exit(1)
	}
}
