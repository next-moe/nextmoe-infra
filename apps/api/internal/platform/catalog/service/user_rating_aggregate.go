package service

import (
	"context"
	stderrors "errors"
	"fmt"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"

	"gorm.io/gorm"
)

type RatingAggregateStats struct {
	Rated   int
	Written int
	Deleted int
}

func RefreshRatingAggregates(ctx context.Context, db *gorm.DB, apply bool) (RatingAggregateStats, error) {
	var st RatingAggregateStats
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		if st, err = RefreshRatingAggregatesTx(tx); err != nil {
			return err
		}
		if !apply {
			return errRatingAggregateDryRun
		}
		return nil
	})
	if err == errRatingAggregateDryRun {
		err = nil
	}
	return st, err
}

func RefreshRatingAggregatesTx(tx *gorm.DB) (RatingAggregateStats, error) {
	var st RatingAggregateStats
	if err := tx.Raw(`SELECT count(DISTINCT work_id) FROM catalog_user_work_rating`).Scan(&st.Rated).Error; err != nil {
		return st, err
	}
	res, err := refreshRatingAggregates(tx, nil)
	st.Written, st.Deleted = res.written, res.deleted
	return st, err
}

var errRatingAggregateDryRun = stderrors.New("dry run")

type ratingAggregateResult struct{ written, deleted int }

func refreshRatingAggregates(tx *gorm.DB, workIDs []int64) (ratingAggregateResult, error) {
	upsert, prune := ratingAggregateSQL(workIDs != nil)
	var args []any
	if workIDs != nil {
		args = []any{workIDs}
	}
	var written, deleted []int64
	if err := tx.Raw(upsert, append(args, args...)...).Scan(&written).Error; err != nil {
		return ratingAggregateResult{}, fmt.Errorf("upsert nextmoe ratings: %w", err)
	}
	if err := tx.Raw(prune, args...).Scan(&deleted).Error; err != nil {
		return ratingAggregateResult{}, fmt.Errorf("prune nextmoe ratings: %w", err)
	}
	if err := repository.TouchWorks(tx.Statement.Context, tx, append(written, deleted...)); err != nil {
		return ratingAggregateResult{}, err
	}
	return ratingAggregateResult{written: len(written), deleted: len(deleted)}, nil
}

func ratingAggregateSQL(scoped bool) (upsert, prune string) {
	rated, published := "", ""
	if scoped {
		rated, published = "WHERE work_id IN ?", "AND r.work_id IN ?"
	}
	upsert = fmt.Sprintf(`
		WITH prior AS (SELECT avg(overall)::float8 AS mean FROM catalog_user_work_rating),
		per AS (
		    SELECT work_id, count(*)::int AS n, sum(overall)::float8 AS total,
		           avg(overall)::float8 AS average, stddev_samp(overall)::float8 AS stdev
		      FROM catalog_user_work_rating %[1]s
		     GROUP BY work_id
		),
		hist AS (
		    SELECT work_id, jsonb_object_agg(overall::text, n) AS distribution
		      FROM (SELECT work_id, overall, count(*)::int AS n
		              FROM catalog_user_work_rating %[1]s
		             GROUP BY work_id, overall) h
		     GROUP BY work_id
		)
		INSERT INTO catalog_work_rating (work_id, source_id, score, vote_count, distribution, stats, created_at, updated_at)
		SELECT per.work_id, src.id,
		       round(((%[2]d * prior.mean + per.total) / (%[2]d + per.n))::numeric, 2),
		       per.n, hist.distribution,
		       jsonb_strip_nulls(jsonb_build_object(
		           'average', round(per.average::numeric, 2)::float8,
		           'stdev', round(per.stdev::numeric, 2)::float8)),
		       now(), now()
		  FROM per
		  JOIN hist USING (work_id)
		  CROSS JOIN prior
		  CROSS JOIN (SELECT id FROM catalog_source WHERE key = 'nextmoe') src
		ON CONFLICT (work_id, source_id) DO UPDATE
		   SET score = EXCLUDED.score, vote_count = EXCLUDED.vote_count,
		       distribution = EXCLUDED.distribution, stats = EXCLUDED.stats, updated_at = now()
		 WHERE (catalog_work_rating.score, catalog_work_rating.vote_count,
		        catalog_work_rating.distribution, catalog_work_rating.stats)
		       IS DISTINCT FROM (EXCLUDED.score, EXCLUDED.vote_count, EXCLUDED.distribution, EXCLUDED.stats)
		RETURNING work_id`, rated, model.RatingPriorVotes)
	prune = fmt.Sprintf(`
		DELETE FROM catalog_work_rating r
		 USING catalog_source src
		 WHERE src.key = 'nextmoe' AND r.source_id = src.id %s
		   AND NOT EXISTS (SELECT 1 FROM catalog_user_work_rating u WHERE u.work_id = r.work_id)
		RETURNING r.work_id`, published)
	return upsert, prune
}
