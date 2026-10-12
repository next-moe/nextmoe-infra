// Before merges went survivor-first, ExecuteMerge moved the loser's official
// (kind 0) titles onto the survivor still as kind 0, and the read face elects a
// locale's title by ORDER BY provenance, kind, id, so the older loser row won:
// 2,720 olang=ja works served a localized.ja that was not their display_name
// (9632 showed 「咎狗の血 True Blood NITRO ARCHIVE」). This demotes those rows
// to lang-less aliases where the survivor has a title of its own in that
// language.
package main

import (
	"context"
	"flag"
	"io"
	"log/slog"
	"os"

	"api/internal/infrastructure/database"
)

func main() {
	dsn := flag.String("dsn", "", "catalog DSN — REQUIRED, never inferred from the environment")
	apply := flag.Bool("apply", false, "write (default: dry run, plan and counts only)")
	receipts := flag.String("receipts", "", "write one JSON line per planned (dry run) or committed (apply) title change; "+
		"action demote = kind 1 with lang '', delete = an identical lang-less alias already existed, "+
		"promote = the survivor's own alias in that language becomes kind 0")
	leftovers := flag.String("leftovers", "", "write one JSON line per survivor whose original-language title would still differ from display_name")
	batch := flag.Int("batch", 500, "survivors per transaction")
	flag.Parse()

	if *dsn == "" {
		slog.Error("repair-merge-survivor-titles", "error", "--dsn is required")
		os.Exit(2)
	}
	if *batch <= 0 {
		slog.Error("repair-merge-survivor-titles", "error", "--batch must be positive")
		os.Exit(2)
	}

	db, err := database.OpenJob(*dsn)
	if err != nil {
		slog.Error("repair-merge-survivor-titles: connect", "error", err)
		os.Exit(1)
	}
	if sqlDB, e := db.DB(); e == nil {
		defer sqlDB.Close()
	}

	o := opts{Apply: *apply, Batch: *batch, Receipts: io.Discard, Leftovers: io.Discard}
	for _, out := range []struct {
		path string
		dst  *io.Writer
	}{{*receipts, &o.Receipts}, {*leftovers, &o.Leftovers}} {
		if out.path == "" {
			continue
		}
		f, err := os.Create(out.path)
		if err != nil {
			slog.Error("repair-merge-survivor-titles: open output", "path", out.path, "error", err)
			os.Exit(1)
		}
		defer f.Close()
		*out.dst = f
	}

	st, err := run(context.Background(), db, o)
	summary := append([]any{"apply", *apply}, st.logArgs()...)
	slog.Info("repair-merge-survivor-titles summary", summary...)
	if err != nil {
		slog.Error("repair-merge-survivor-titles", "error", err)
		os.Exit(1)
	}
	if !*apply {
		slog.Info("DRY RUN — nothing written; re-run with --apply")
	}
}
