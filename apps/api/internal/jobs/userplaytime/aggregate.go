package userplaytime

import (
	"context"
	"fmt"
	"log/slog"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

type Stats struct {
	Eligible  int
	Written   int
	Unchanged int
	Deleted   int
	Errors    int
}

type Opts struct {
	DB             *gorm.DB
	DSN            string
	Apply          bool
	MinReporters   int
	ExcludeClients []string
}

type candidate struct {
	WorkID    int64
	Median    int
	Reporters int
}

func Run(ctx context.Context, o Opts) (*Stats, error) {
	st := &Stats{}
	min := o.MinReporters
	if min <= 0 {
		min = model.PlaytimeMinReporters
	}
	if o.DB == nil {
		db, err := database.OpenJob(o.DSN)
		if err != nil {
			return nil, fmt.Errorf("open catalog: %w", err)
		}
		defer func() {
			if sqlDB, err := db.DB(); err == nil {
				sqlDB.Close()
			}
		}()
		o.DB = db
	}

	sourceID, err := nextmoeSourceID(ctx, o.DB)
	if err != nil {
		return nil, err
	}

	cands, err := aggregate(ctx, o.DB, min, o.ExcludeClients)
	if err != nil {
		return nil, err
	}
	st.Eligible = len(cands)

	keep := make(map[int64]bool, len(cands))
	for _, c := range cands {
		keep[c.WorkID] = true
		if !o.Apply {
			continue
		}
		changed, err := upsert(ctx, o.DB, c, sourceID)
		switch {
		case err != nil:
			slog.Warn("playtime aggregate upsert", "work", c.WorkID, "err", err)
			st.Errors++
		case changed:
			st.Written++
		default:
			st.Unchanged++
		}
	}

	stale, err := staleRows(ctx, o.DB, sourceID, keep)
	if err != nil {
		return nil, err
	}
	st.Deleted = len(stale)
	if o.Apply && len(stale) > 0 {
		if err := o.DB.WithContext(ctx).Exec(
			`DELETE FROM catalog_work_playtime WHERE source_id = ? AND work_id IN ?`,
			sourceID, stale).Error; err != nil {
			return nil, fmt.Errorf("delete stale playtime rows: %w", err)
		}
	}
	return st, nil
}

func nextmoeSourceID(ctx context.Context, db *gorm.DB) (int16, error) {
	var id int16
	if err := db.WithContext(ctx).Raw(
		`SELECT id FROM catalog_source WHERE key = 'nextmoe'`).Scan(&id).Error; err != nil {
		return 0, err
	}
	if id == 0 {
		return 0, fmt.Errorf("catalog_source has no 'nextmoe' row — run the seed first")
	}
	return id, nil
}

func aggregate(ctx context.Context, db *gorm.DB, min int, excludeClients []string) ([]candidate, error) {
	if len(excludeClients) == 0 {
		excludeClients = []string{""}
	}
	var out []candidate
	err := db.WithContext(ctx).Raw(`
		WITH per_user AS (
		    SELECT p.work_id, p.actor_uid, MAX(p.minutes) AS minutes
		      FROM catalog_user_playtime p
		      JOIN catalog_user_work_state s
		        ON s.actor_uid = p.actor_uid AND s.work_id = p.work_id AND s.state = ?
		     WHERE p.minutes >= ? AND p.minutes <= ?
		       AND p.client_id NOT IN ?
		     GROUP BY p.work_id, p.actor_uid
		)
		SELECT work_id,
		       percentile_disc(0.5) WITHIN GROUP (ORDER BY minutes)::int AS median,
		       COUNT(*)::int AS reporters
		  FROM per_user
		 GROUP BY work_id
		HAVING COUNT(*) >= ?
		 ORDER BY work_id`,
		model.WorkStateDone,
		model.PlaytimeMinutesMin, model.PlaytimeMinutesMax,
		excludeClients, min).Scan(&out).Error
	if err != nil {
		return nil, fmt.Errorf("aggregate playtime: %w", err)
	}
	return out, nil
}

func upsert(ctx context.Context, db *gorm.DB, c candidate, sourceID int16) (bool, error) {
	res := db.WithContext(ctx).Exec(`
		INSERT INTO catalog_work_playtime (work_id, source_id, minutes, vote_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, now(), now())
		ON CONFLICT (work_id, source_id) DO UPDATE
		   SET minutes = EXCLUDED.minutes, vote_count = EXCLUDED.vote_count, updated_at = now()
		 WHERE catalog_work_playtime.minutes IS DISTINCT FROM EXCLUDED.minutes
		    OR catalog_work_playtime.vote_count IS DISTINCT FROM EXCLUDED.vote_count`,
		c.WorkID, sourceID, c.Median, c.Reporters)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func staleRows(ctx context.Context, db *gorm.DB, sourceID int16, keep map[int64]bool) ([]int64, error) {
	var published []int64
	if err := db.WithContext(ctx).Raw(
		`SELECT work_id FROM catalog_work_playtime WHERE source_id = ?`, sourceID).
		Scan(&published).Error; err != nil {
		return nil, err
	}
	var stale []int64
	for _, id := range published {
		if !keep[id] {
			stale = append(stale, id)
		}
	}
	return stale, nil
}
