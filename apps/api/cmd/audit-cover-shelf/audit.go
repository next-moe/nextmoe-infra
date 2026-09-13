package main

import (
	"context"
	"fmt"
	"io"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"

	"gorm.io/gorm"
)

const censoredSourceKey = "censored"

const defaultMaxFix = 2000

// exitFindings is distinct from 1 (the audit broke) so the cron wrapper can
// tell "the audit found work" from "the audit is broken".
const exitFindings = 3

// A claimed work sits on the shelf its site chose (display_nsfw), and the
// cover election only offers a viewer explicit art once that shelf says nsfw.
// So a claimed work on the SFW shelf whose every cover-art row is graded
// explicit has an unsatisfiable contract: there is nothing safe to elect, and
// every viewer — in both modes — gets the blurred 'censored' stand-in instead
// of the real cover. Production carried 961 of these on 2026-09-13; they had
// accumulated because the forum's submit form defaults content_limit to 'sfw'.
//
// This has to be an audit and cannot be a write-path guard: `sexual` is set
// asynchronously by the nightly image grader, so a re-grade can turn a work's
// last safe cover explicit with nobody touching display_nsfw. There is no
// write to hang the check on.
const unrenderableSFW = `w.deleted_at IS NULL AND w.status = ?
	AND w.site IS NOT NULL AND w.site <> '' AND w.product_work_id IS NOT NULL
	AND NOT w.display_nsfw
	AND EXISTS (SELECT 1 FROM catalog_work_cover c JOIN catalog_source s ON s.id = c.source_id
	             WHERE c.work_id = w.id AND s.key <> ? AND c.image_hash <> '' AND c.kind NOT IN (?))
	AND NOT EXISTS (SELECT 1 FROM catalog_work_cover c JOIN catalog_source s ON s.id = c.source_id
	                 WHERE c.work_id = w.id AND s.key <> ? AND c.image_hash <> '' AND c.kind NOT IN (?)
	                   AND c.sexual < ?)`

func unrenderableArgs() []any {
	return []any{
		model.WorkStatusLive,
		censoredSourceKey, model.PackagingCoverKinds,
		censoredSourceKey, model.PackagingCoverKinds, model.SexualExplicit,
	}
}

type finding struct {
	ID            int64
	DisplayName   string
	ContentRating int16
}

type stats struct {
	// Mislabelled is r18: the rating already says adult, so only the shelf is
	// wrong and --fix can move it.
	Mislabelled []finding
	// Misrated is everything else: an all-ages work whose only cover art is
	// explicit is more likely mis-rated than mis-shelved, and moving it would
	// hide the wrong thing. Reported for a human, never written.
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
		return len(st.Misrated), nil
	}
	return len(st.Mislabelled) + len(st.Misrated), nil
}

func audit(ctx context.Context, db *gorm.DB, fix bool, maxFix int) (stats, error) {
	var st stats
	db = db.WithContext(ctx)

	var rows []finding
	if err := db.Raw(`SELECT w.id, w.display_name, w.content_rating
		FROM catalog_work w WHERE `+unrenderableSFW+` ORDER BY w.id`, unrenderableArgs()...).
		Scan(&rows).Error; err != nil {
		return st, fmt.Errorf("scan works with no electable safe cover: %w", err)
	}
	for _, f := range rows {
		if f.ContentRating == model.ContentRatingR18 {
			st.Mislabelled = append(st.Mislabelled, f)
		} else {
			st.Misrated = append(st.Misrated, f)
		}
	}
	if !fix || len(st.Mislabelled) == 0 {
		return st, nil
	}
	if len(st.Mislabelled) > maxFix {
		return st, fmt.Errorf(
			"refusing to fix: %d works are over the --max-fix ceiling of %d — a jump this size is a "+
				"mass re-grade or a broken grader, not editorial drift, and moving them would empty the SFW shelf",
			len(st.Mislabelled), maxFix)
	}

	ids := make([]int64, len(st.Mislabelled))
	for i, f := range st.Mislabelled {
		ids[i] = f.ID
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		res := tx.Exec(`UPDATE catalog_work SET display_nsfw = true WHERE id IN (?)`, ids)
		if res.Error != nil {
			return fmt.Errorf("move works off the sfw shelf: %w", res.Error)
		}
		st.Fixed = res.RowsAffected
		return repository.TouchWorks(ctx, tx, ids)
	})
	return st, err
}

// content_limit reaches the search index only through cmd/reindex-catalog, so
// a fix is live on the SQL list faces at once and invisible to search until
// that runs — the daily 22:10 UTC cron, or by hand.
func report(w io.Writer, st stats, fix bool) {
	for _, f := range st.Mislabelled {
		fmt.Fprintf(w, "[shelf] work=%d rating=r18 %q — sfw shelf, no safe cover art to elect\n", f.ID, f.DisplayName)
	}
	for _, f := range st.Misrated {
		fmt.Fprintf(w, "[rating] work=%d rating=%d %q — every cover is explicit but the work is not rated r18; needs a human\n",
			f.ID, f.ContentRating, f.DisplayName)
	}
	if fix {
		fmt.Fprintf(w, "[audit-cover-shelf] moved off the sfw shelf=%d left for a human=%d "+
			"(search face still stale until reindex-catalog runs)\n", st.Fixed, len(st.Misrated))
		return
	}
	fmt.Fprintf(w, "[audit-cover-shelf] fixable=%d needs a human=%d (report-only — pass --fix to write)\n",
		len(st.Mislabelled), len(st.Misrated))
}
