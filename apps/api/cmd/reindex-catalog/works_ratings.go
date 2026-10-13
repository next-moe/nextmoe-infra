package main

import (
	"fmt"

	catalogSearch "api/internal/platform/catalog/search"
	"api/internal/platform/catalog/search/spec"

	"gorm.io/gorm"
)

func loadWorkRatingSort(db *gorm.DB) (map[int64]map[string]float64, error) {
	var rows []struct {
		WorkID int64   `gorm:"column:work_id"`
		Source string  `gorm:"column:source"`
		Score  float64 `gorm:"column:score"`
		Votes  int     `gorm:"column:vote_count"`
		Mean   float64 `gorm:"column:mean"`
	}
	q := fmt.Sprintf(`SELECT r.work_id, s.key AS source, r.score::float8 AS score, r.vote_count,
			avg(r.score::float8) OVER (PARTITION BY r.source_id) AS mean
		FROM catalog_work_rating r
		JOIN catalog_source s ON s.id = r.source_id `+worksPopulationJoin+`
		WHERE s.key IN ?`, "r.work_id")
	if err := db.Raw(q, spec.RatingSources).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("work rating sort: %w", err)
	}
	m := make(map[int64]map[string]float64)
	for _, r := range rows {
		if m[r.WorkID] == nil {
			m[r.WorkID] = make(map[string]float64, 2)
		}
		m[r.WorkID][r.Source] = catalogSearch.RatingSortScore(r.Source, r.Score, r.Votes, r.Mean)
	}
	return m, nil
}
