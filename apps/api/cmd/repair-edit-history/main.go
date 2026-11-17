package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"api/internal/infrastructure/database"
	"api/internal/jobs/historyrepair"
	"api/internal/platform/catalog/editspec"
	"api/internal/platform/editing"
)

func main() {
	mode := flag.String("mode", "", "prior | stamp-uploads | restore-covers")
	dsn := flag.String("dsn", "", "catalog DSN — REQUIRED")
	apply := flag.Bool("apply", false, "write (default: dry-run report only)")
	actorUID := flag.Int64("actor-uid", 0, "restore-covers: the uid the restoring revisions are attributed to")
	site := flag.String("site", "editor", "restore-covers: site recorded on the restoring revisions")
	flag.Parse()

	if err := run(context.Background(), *mode, *dsn, *apply, *actorUID, *site); err != nil {
		slog.Error("repair-edit-history", "mode", *mode, "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, mode, dsn string, apply bool, actorUID int64, site string) error {
	if dsn == "" {
		return fmt.Errorf("--dsn is required")
	}
	db, err := database.OpenJob(dsn)
	if err != nil {
		return err
	}
	reg := editing.NewRegistry()
	if err := editspec.RegisterAll(reg, db); err != nil {
		return err
	}
	var st *historyrepair.Stats
	switch mode {
	case "prior":
		st, err = historyrepair.BackfillPrior(ctx, db, reg, apply)
	case "stamp-uploads":
		st, err = historyrepair.StampUploads(ctx, db, apply)
	case "restore-covers":
		if actorUID <= 0 {
			return fmt.Errorf("--actor-uid is required for restore-covers")
		}
		actor := editing.PolicyContext{
			UserID: actorUID, Site: site, TrustTier: editing.TrustedTier,
			HasPerm: func(string) bool { return true },
		}
		st, err = historyrepair.RestoreLostCovers(ctx, db, editing.NewEngine(db, reg), actor, apply)
	default:
		return fmt.Errorf("--mode must be prior, stamp-uploads or restore-covers")
	}
	if st != nil {
		fmt.Println(st.String())
	}
	if err != nil {
		return err
	}
	if len(st.Problems) > 0 {
		return fmt.Errorf("%d problem(s)", len(st.Problems))
	}
	return nil
}
