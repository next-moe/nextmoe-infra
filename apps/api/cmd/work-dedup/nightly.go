package main

import (
	"context"
	"fmt"
	"io"

	"api/internal/platform/catalog/service"

	"gorm.io/gorm"
)

// runNightly is seed -> propose -> execute over ONE census. Running the three
// modes back to back from cron would build it three times, and the census is an
// ACCESS SHARE self-join over every live work title on prod — the same
// query whose second concurrent copy is what the wrapper's yield guard exists
// to cancel.
//
// execute reads proposals from the DB, not from the census, so the proposals
// this run just approved are still inside their 48h cooling window and are not
// executed until a later night.
func runNightly(ctx context.Context, db *gorm.DB, w io.Writer, merge *service.MergeService,
	resolve *service.ResolveService, actor int64, note string, limit int, run bool) (int, error) {
	c, err := buildCensus(ctx, db)
	if err != nil {
		return 0, err
	}
	needsManualNew, err := seedFromCensus(ctx, db, w, c, actor, run)
	if err != nil {
		return needsManualNew, err
	}
	if err := proposeFromCensus(ctx, db, w, c, merge, actor, note, limit, run); err != nil {
		return needsManualNew, err
	}
	if err := runExecute(ctx, db, w, merge, resolve, actor, note, limit, run); err != nil {
		return needsManualNew, err
	}

	mode := "DRY-RUN (pass -run to seed+propose+execute)"
	if run {
		mode = "APPLIED"
	}
	fmt.Fprintf(w, "%s [nightly] actor=%d note=%s pairs=%d needs_manual_new=%d\n",
		mode, actor, note, len(c.rows), needsManualNew)
	return needsManualNew, nil
}
