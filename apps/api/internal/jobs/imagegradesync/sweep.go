package imagegradesync

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/repository"

	"gorm.io/gorm"
)

// The grade ladder tops out at an explicit sexual act (3), which the catalog
// axis does not distinguish from nudity — both are "adult", the strongest value
// the wire carries.
func mapLevel(level int) (int16, bool) {
	switch level {
	case 0, 1, 2:
		return int16(level), true
	case 3:
		return 2, true
	default:
		return 0, false
	}
}

type mediaRow struct {
	ID       int64  `gorm:"column:id"`
	WorkID   int64  `gorm:"column:work_id"`
	Hash     string `gorm:"column:image_hash"`
	Sexual   int16  `gorm:"column:sexual"`
	SourceID int16  `gorm:"column:source_id"`
}

type gradeRow struct {
	Hash  string `gorm:"column:hash"`
	Level *int   `gorm:"column:level"`
}

func sweepAll(ctx context.Context, db, idb *gorm.DB, sc scope, opts Opts) (*Stats, error) {
	st := &Stats{}
	for _, table := range mediaTables {
		if err := sweep(ctx, db, idb, table, sc, opts, st); err != nil {
			return st, fmt.Errorf("%s: %w", table, err)
		}
	}
	return st, nil
}

func sweep(ctx context.Context, db, idb *gorm.DB, table string, sc scope, opts Opts, st *Stats) error {
	var cursor int64
	oddLevels := 0
	for {
		page := opts.Batch
		if opts.Limit > 0 {
			left := opts.Limit - st.Scanned
			if left <= 0 {
				break
			}
			if left < page {
				page = left
			}
		}
		var rows []mediaRow
		if err := db.WithContext(ctx).Raw(`
			SELECT id, work_id, image_hash, sexual, source_id
			  FROM `+table+`
			 WHERE source_id IN ? AND id > ?
			   AND (`+editspec.HumanFieldProvenanceSQL("field_provenance", "sexual")+`) IS NOT TRUE
			 ORDER BY id
			 LIMIT ?`, sc.ids, cursor, page).Scan(&rows).Error; err != nil {
			return fmt.Errorf("page rows: %w", err)
		}
		if len(rows) == 0 {
			break
		}
		cursor = rows[len(rows)-1].ID
		st.Scanned += len(rows)

		grades, err := loadGrades(ctx, idb, rows)
		if err != nil {
			return err
		}
		plan := make(map[int16][]int64, 3)
		works := make(map[int16][]int64, 3)
		for _, r := range rows {
			level, known := grades[r.Hash]
			switch {
			case !known:
				st.Missing++
				continue
			case level == nil:
				st.Ungraded++
				continue
			}
			want, ok := mapLevel(*level)
			if !ok {
				oddLevels++
				st.Ungraded++
				continue
			}
			if want == r.Sexual {
				st.Unchanged++
				continue
			}
			st.Planned++
			st.note(sc.names[r.SourceID], table, r.Sexual, want)
			plan[want] = append(plan[want], r.ID)
			works[want] = append(works[want], r.WorkID)
		}
		if opts.Apply {
			applyPlan(ctx, db, table, plan, works, st)
		}
		if len(rows) < page {
			break
		}
	}
	if oddLevels > 0 {
		slog.Warn("grade level outside the 0-3 ladder", "table", table, "rows", oddLevels)
	}
	return nil
}

func loadGrades(ctx context.Context, idb *gorm.DB, rows []mediaRow) (map[string]*int, error) {
	hashes := make([]string, 0, len(rows))
	seen := make(map[string]bool, len(rows))
	for _, r := range rows {
		if r.Hash == "" || seen[r.Hash] {
			continue
		}
		seen[r.Hash] = true
		hashes = append(hashes, r.Hash)
	}
	out := make(map[string]*int, len(hashes))
	if len(hashes) == 0 {
		return out, nil
	}
	// Deliberately not filtered on `review_labels -> 'grade' IS NOT NULL`: an
	// image that is present but ungraded and one that is not in the image
	// service at all are different findings (nightly grader vs dangling ref),
	// and only presence in this result set tells them apart.
	var got []gradeRow
	if err := idb.WithContext(ctx).Raw(`
		SELECT hash, (review_labels->'grade'->>'level')::int AS level
		  FROM images
		 WHERE hash IN ?`, hashes).Scan(&got).Error; err != nil {
		return nil, fmt.Errorf("read image grades: %w", err)
	}
	for _, g := range got {
		out[g.Hash] = g.Level
	}
	return out, nil
}

func applyPlan(ctx context.Context, db *gorm.DB, table string, plan, works map[int16][]int64, st *Stats) {
	wants := make([]int, 0, len(plan))
	for want := range plan {
		wants = append(wants, int(want))
	}
	sort.Ints(wants)
	for _, w := range wants {
		ids := plan[int16(w)]
		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			res := tx.Exec(`UPDATE `+table+` SET sexual = ?, updated_at = now() WHERE id IN ?`, int16(w), ids)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return nil
			}
			if err := repository.TouchWorks(ctx, tx, works[int16(w)]); err != nil {
				return err
			}
			st.Updated += int(res.RowsAffected)
			return nil
		})
		if err != nil {
			st.Errors += len(ids)
			slog.Warn("update sexual", "table", table, "sexual", w, "rows", len(ids), "err", err)
		}
	}
}
