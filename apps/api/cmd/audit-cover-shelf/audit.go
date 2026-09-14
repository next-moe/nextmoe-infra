package main

import (
	"context"
	"fmt"
	"io"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"

	"gorm.io/gorm"
)

const defaultMaxFix = 2000

// exitFindings is distinct from 1 (the audit broke) so the cron wrapper can
// tell "the audit found work" from "the audit is broken".
const exitFindings = 3

// Until 2026-09-14 this job was the only thing standing between a reader and a
// work on the SFW shelf with no safe cover to elect: the slot election fell
// through to the blurred 'censored' stand-in, so every viewer — in BOTH modes —
// got the ghost instead of the real cover, and production had accumulated 961
// of them. That state is now unreachable by construction. catalog_work
// .cover_art_all_explicit is maintained by a database trigger and the display
// axis reads it (model.WorkShelf.NSFW), so a work whose cover art is entirely
// explicit is on the nsfw shelf the moment the last safe row goes.
//
// Do not restore the old query. What is left for a daily run is the pair of
// things a derived column cannot answer for itself:
//
//	drift    the column disagrees with the cover rows — the trigger did not
//	         fire. Mechanical, and --fix repairs it.
//	misrated every cover the work owns is explicit, yet it is not rated r18.
//	         The work is either mis-rated or its art is mis-graded; both are
//	         editorial, so this is reported and never written.
type finding struct {
	ID            int64
	DisplayName   string
	ContentRating int16
	Stored        bool
	Recomputed    bool
}

type stats struct {
	Drift    []finding
	Misrated []finding
	Fixed    int64
}

func run(ctx context.Context, dsn string, fix bool, maxFix int, w io.Writer) (outstanding int, err error) {
	db, err := database.OpenJob(dsn)
	if err != nil {
		return 0, fmt.Errorf("connect catalog db: %w", err)
	}
	if sqlDB, e := db.DB(); e == nil {
		defer sqlDB.Close()
	}
	st, err := audit(ctx, db, fix, maxFix)
	if err != nil {
		return 0, err
	}
	report(w, st, fix)
	if fix {
		return len(st.Drift) - int(st.Fixed) + len(st.Misrated), nil
	}
	return len(st.Drift) + len(st.Misrated), nil
}

func audit(ctx context.Context, db *gorm.DB, fix bool, maxFix int) (stats, error) {
	var st stats
	db = db.WithContext(ctx)

	truth, err := migrate.CoverArtAllExplicitSQL()
	if err != nil {
		return st, err
	}
	if err := db.Raw(`
		WITH truth AS (` + truth + `)
		SELECT w.id, w.display_name, w.content_rating, w.cover_art_all_explicit AS stored,
		       (t.work_id IS NOT NULL) AS recomputed
		  FROM catalog_work w LEFT JOIN truth t ON t.work_id = w.id
		 WHERE w.deleted_at IS NULL AND w.cover_art_all_explicit <> (t.work_id IS NOT NULL)
		 ORDER BY w.id`).Scan(&st.Drift).Error; err != nil {
		return st, fmt.Errorf("recompute cover_art_all_explicit: %w", err)
	}
	if err := db.Raw(`SELECT w.id, w.display_name, w.content_rating
		FROM catalog_work w
		WHERE w.deleted_at IS NULL AND w.status = ?
		  AND w.cover_art_all_explicit AND w.content_rating <> ?
		ORDER BY w.id`, model.WorkStatusLive, model.ContentRatingR18).Scan(&st.Misrated).Error; err != nil {
		return st, fmt.Errorf("scan works whose art outruns their rating: %w", err)
	}
	if !fix || len(st.Drift) == 0 {
		return st, nil
	}
	if len(st.Drift) > maxFix {
		return st, fmt.Errorf(
			"refusing to fix: %d works have drifted past the --max-fix ceiling of %d — drift this wide is a "+
				"missing trigger, not a missed statement, and repairing the column would hide that",
			len(st.Drift), maxFix)
	}

	ids := make([]int64, len(st.Drift))
	for i, f := range st.Drift {
		ids[i] = f.ID
	}
	// Both statements are guarded on a real change, so together they touch
	// exactly the drifted rows and need no id list of their own.
	err = db.Transaction(func(tx *gorm.DB) error {
		for _, stmt := range []string{
			`UPDATE catalog_work SET cover_art_all_explicit = true
			  WHERE NOT cover_art_all_explicit AND id IN (` + truth + `)`,
			`UPDATE catalog_work SET cover_art_all_explicit = false
			  WHERE cover_art_all_explicit AND id NOT IN (` + truth + `)`,
		} {
			res := tx.Exec(stmt)
			if res.Error != nil {
				return fmt.Errorf("repair cover_art_all_explicit: %w", res.Error)
			}
			st.Fixed += res.RowsAffected
		}
		return repository.TouchWorks(ctx, tx, ids)
	})
	return st, err
}

// content_limit reaches the search index only through cmd/reindex-catalog, so
// a repair is live on the SQL list faces at once and invisible to search until
// that runs — the daily 22:10 UTC cron, or by hand.
func report(w io.Writer, st stats, fix bool) {
	for _, f := range st.Drift {
		fmt.Fprintf(w, "[drift] work=%d %q — cover_art_all_explicit is %v, the cover rows say %v; the trigger did not fire\n",
			f.ID, f.DisplayName, f.Stored, f.Recomputed)
	}
	for _, f := range st.Misrated {
		fmt.Fprintf(w, "[rating] work=%d rating=%d %q — every cover is explicit but the work is not rated r18; needs a human\n",
			f.ID, f.ContentRating, f.DisplayName)
	}
	if fix {
		fmt.Fprintf(w, "[audit-cover-shelf] repaired=%d left for a human=%d "+
			"(search face still stale until reindex-catalog runs)\n", st.Fixed, len(st.Misrated))
		return
	}
	fmt.Fprintf(w, "[audit-cover-shelf] drifted=%d needs a human=%d (report-only — pass --fix to write)\n",
		len(st.Drift), len(st.Misrated))
}
