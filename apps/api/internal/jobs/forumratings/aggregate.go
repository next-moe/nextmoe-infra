package forumratings

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Stats struct {
	Candidates int
	Unmapped   int
	MultiClaim int
	Eligible   int
	Written    int
	Unchanged  int
	Deleted    int
	Errors     int
}

type Opts struct {
	DB        *gorm.DB
	DSN       string
	ForumDB   *gorm.DB
	ForumDSN  string
	Apply     bool
	MinVoters int
}

type candidate struct {
	GalgameID int64
	VoteCount int
	Score     float64
	Stdev     float64
	Buckets   map[string]int
}

const defaultMinVoters = 3

func Run(ctx context.Context, o Opts) (*Stats, error) {
	st := &Stats{}
	min := o.MinVoters
	if min <= 0 {
		min = defaultMinVoters
	}
	if o.DB == nil {
		if o.DSN == "" {
			return nil, fmt.Errorf("catalog DSN is required (--dsn); refusing to guess")
		}
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
	if o.ForumDB == nil {
		if o.ForumDSN == "" {
			return nil, fmt.Errorf("forum DSN is required (--forum-dsn); refusing to guess")
		}
		db, err := database.OpenJob(o.ForumDSN)
		if err != nil {
			return nil, fmt.Errorf("open forum: %w", err)
		}
		defer func() {
			if sqlDB, err := db.DB(); err == nil {
				sqlDB.Close()
			}
		}()
		o.ForumDB = db
	}

	sourceID, err := nextmoeSourceID(ctx, o.DB)
	if err != nil {
		return nil, err
	}

	cands, err := forumAggregates(ctx, o.ForumDB, min)
	if err != nil {
		return nil, err
	}
	st.Candidates = len(cands)

	gids := make([]int64, len(cands))
	for i, c := range cands {
		gids[i] = c.GalgameID
	}
	if err := attachBuckets(ctx, o.ForumDB, cands, gids); err != nil {
		return nil, err
	}

	byGid, err := liveClaims(ctx, o.DB, gids)
	if err != nil {
		return nil, err
	}

	keep := make(map[int64]bool, len(cands))
	var touched []int64
	for _, c := range cands {
		works := byGid[c.GalgameID]
		switch len(works) {
		case 0:
			st.Unmapped++
			continue
		case 1:
		default:
			st.MultiClaim++
			continue
		}
		workID := works[0]
		st.Eligible++
		keep[workID] = true
		if !o.Apply {
			continue
		}
		changed, err := upsert(ctx, o.DB, workID, sourceID, c)
		switch {
		case err != nil:
			slog.Warn("forum rating upsert", "work", workID, "err", err)
			st.Errors++
		case changed:
			st.Written++
			touched = append(touched, workID)
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
			`DELETE FROM catalog_work_rating WHERE source_id = ? AND work_id IN ?`,
			sourceID, stale).Error; err != nil {
			return nil, fmt.Errorf("delete stale nextmoe ratings: %w", err)
		}
		touched = append(touched, stale...)
	}
	if o.Apply {
		if err := repository.TouchWorks(ctx, o.DB, touched); err != nil {
			return nil, fmt.Errorf("touch works: %w", err)
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

func forumAggregates(ctx context.Context, db *gorm.DB, min int) ([]candidate, error) {
	var rows []struct {
		GalgameID int64   `gorm:"column:galgame_id"`
		VoteCount int     `gorm:"column:vote_count"`
		Score     float64 `gorm:"column:score"`
		Stdev     float64 `gorm:"column:stdev"`
	}
	err := db.WithContext(ctx).Raw(`
		SELECT galgame_id,
		       COUNT(*)::int AS vote_count,
		       ROUND(AVG(overall)::numeric, 2)::float8 AS score,
		       ROUND(STDDEV_SAMP(overall)::numeric, 2)::float8 AS stdev
		  FROM galgame_rating
		 GROUP BY galgame_id
		HAVING COUNT(*) >= ?
		 ORDER BY galgame_id`, min).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("aggregate forum ratings: %w", err)
	}
	out := make([]candidate, len(rows))
	for i, r := range rows {
		out[i] = candidate{
			GalgameID: r.GalgameID, VoteCount: r.VoteCount,
			Score: r.Score, Stdev: r.Stdev,
		}
	}
	return out, nil
}

func attachBuckets(ctx context.Context, db *gorm.DB, cands []candidate, gids []int64) error {
	if len(gids) == 0 {
		return nil
	}
	var rows []struct {
		GalgameID int64 `gorm:"column:galgame_id"`
		Overall   int   `gorm:"column:overall"`
		N         int   `gorm:"column:n"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT galgame_id, overall, COUNT(*)::int AS n
		  FROM galgame_rating
		 WHERE galgame_id IN ?
		 GROUP BY galgame_id, overall`, gids).Scan(&rows).Error; err != nil {
		return fmt.Errorf("forum rating histogram: %w", err)
	}
	byGid := make(map[int64]map[string]int, len(cands))
	for _, r := range rows {
		if r.N <= 0 {
			continue
		}
		b := byGid[r.GalgameID]
		if b == nil {
			b = map[string]int{}
			byGid[r.GalgameID] = b
		}
		b[strconv.Itoa(r.Overall)] += r.N
	}
	for i := range cands {
		cands[i].Buckets = byGid[cands[i].GalgameID]
	}
	return nil
}

func liveClaims(ctx context.Context, db *gorm.DB, gids []int64) (map[int64][]int64, error) {
	out := map[int64][]int64{}
	if len(gids) == 0 {
		return out, nil
	}
	var rows []struct {
		ID            int64 `gorm:"column:id"`
		ProductWorkID int64 `gorm:"column:product_work_id"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT id, product_work_id
		  FROM catalog_work
		 WHERE site = 'kungal'
		   AND (claim_state IS NULL OR claim_state = ?)
		   AND deleted_at IS NULL
		   AND product_work_id IN ?`,
		model.ClaimStateLive, gids).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("map forum claims: %w", err)
	}
	for _, r := range rows {
		out[r.ProductWorkID] = append(out[r.ProductWorkID], r.ID)
	}
	return out, nil
}

type ratingStats struct {
	Average *float64 `json:"average,omitempty"`
	Stdev   *float64 `json:"stdev,omitempty"`
}

func marshalBuckets(b map[string]int) []byte {
	kept := make(map[string]int, len(b))
	for k, n := range b {
		if n > 0 {
			kept[k] = n
		}
	}
	if len(kept) == 0 {
		return nil
	}
	out, err := json.Marshal(kept)
	if err != nil {
		return nil
	}
	return out
}

func marshalStats(avg, stdev float64) []byte {
	out, err := json.Marshal(ratingStats{Average: &avg, Stdev: &stdev})
	if err != nil {
		return nil
	}
	return out
}

// Every column this job owns belongs in BOTH the update list and the
// IS DISTINCT FROM tuple. Wave 205 added distribution/stats: had they been
// left out of the tuple, the first run after the migration would have skipped
// every work whose score had not moved since the last run — which is nearly all
// of them — and the new columns would have stayed NULL with the job reporting
// a clean `unchanged`.
func upsert(ctx context.Context, db *gorm.DB, workID int64, sourceID int16, c candidate) (bool, error) {
	res := db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "work_id"}, {Name: "source_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"score", "vote_count", "rank", "distribution", "stats", "updated_at"}),
		Where: clause.Where{Exprs: []clause.Expression{gorm.Expr(
			`(catalog_work_rating.score, catalog_work_rating.vote_count, catalog_work_rating.rank,
			  catalog_work_rating.distribution, catalog_work_rating.stats)
			 IS DISTINCT FROM (excluded.score, excluded.vote_count, excluded.rank,
			  excluded.distribution, excluded.stats)`)}},
	}).Create(&model.CatalogWorkRating{
		WorkID: workID, SourceID: sourceID, Score: c.Score, VoteCount: c.VoteCount,
		Distribution: marshalBuckets(c.Buckets), Stats: marshalStats(c.Score, c.Stdev),
	})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func staleRows(ctx context.Context, db *gorm.DB, sourceID int16, keep map[int64]bool) ([]int64, error) {
	var published []int64
	if err := db.WithContext(ctx).Raw(
		`SELECT work_id FROM catalog_work_rating WHERE source_id = ?`, sourceID).
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
