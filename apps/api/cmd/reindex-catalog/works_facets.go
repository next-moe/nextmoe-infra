package main

import (
	"context"
	"fmt"
	"log/slog"

	"api/internal/platform/catalog/model"
	catalogSearch "api/internal/platform/catalog/search"

	"gorm.io/gorm"
)

const worksPopulationJoin = `JOIN catalog_work w ON w.id = %s AND w.deleted_at IS NULL
	AND w.medium_id = (SELECT id FROM catalog_medium WHERE key = 'galgame') AND w.status = 0`

func loadWorkEdgeIDs(db *gorm.DB, edge string) (map[int64][]int64, error) {
	var rows []struct {
		WorkID int64 `gorm:"column:work_id"`
		KeyID  int64 `gorm:"column:key_id"`
	}
	q := fmt.Sprintf(`SELECT DISTINCT e.work_id, e.key_id FROM %s `+worksPopulationJoin+
		` ORDER BY e.work_id, e.key_id`, edge, "e.work_id")
	if err := db.Raw(q).Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := map[int64][]int64{}
	for _, r := range rows {
		m[r.WorkID] = append(m[r.WorkID], r.KeyID)
	}
	return m, nil
}

const (
	reindexTagEdge = `(SELECT m.tag_id AS key_id, wt.work_id
		FROM catalog_work_tag wt
		JOIN catalog_tag_source_map m ON m.source_id = wt.source_id AND m.source_name = wt.name) e`
	reindexLabelEdge  = `(SELECT label_id AS key_id, work_id FROM catalog_work_label) e`
	reindexSeriesEdge = `(SELECT series_id AS key_id, work_id FROM catalog_series_member) e`
)

var reindexEngineEdge = `(SELECT engine_id AS key_id, work_id FROM ` + model.WorkEnginesSQL + ` x) e`

func loadEarliestReleaseOrd(db *gorm.DB) (map[int64]int64, error) {
	var rows []struct {
		WorkID int64 `gorm:"column:work_id"`
		Ord    int64 `gorm:"column:ord"`
	}
	q := fmt.Sprintf(`SELECT r.work_id,
			min(r.released_y::int * 10000 + coalesce(r.released_m,0)::int * 100 + coalesce(r.released_d,0)::int) AS ord
		FROM catalog_release r `+worksPopulationJoin+`
		WHERE r.released_y IS NOT NULL AND r.deleted_at IS NULL
		GROUP BY r.work_id`, "r.work_id")
	if err := db.Raw(q).Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[int64]int64, len(rows))
	for _, r := range rows {
		m[r.WorkID] = r.Ord
	}
	return m, nil
}

type worksFacets struct {
	tagIDs      map[int64][]int64
	labelIDs    map[int64][]int64
	engineIDs   map[int64][]int64
	seriesIDs   map[int64][]int64
	releasedOrd map[int64]int64
}

func loadWorksFacets(db *gorm.DB) (worksFacets, error) {
	var f worksFacets
	var err error
	if f.tagIDs, err = loadWorkEdgeIDs(db, reindexTagEdge); err != nil {
		return f, fmt.Errorf("work tag ids: %w", err)
	}
	if f.labelIDs, err = loadWorkEdgeIDs(db, reindexLabelEdge); err != nil {
		return f, fmt.Errorf("work label ids: %w", err)
	}
	if f.engineIDs, err = loadWorkEdgeIDs(db, reindexEngineEdge); err != nil {
		return f, fmt.Errorf("work engine ids: %w", err)
	}
	if f.seriesIDs, err = loadWorkEdgeIDs(db, reindexSeriesEdge); err != nil {
		return f, fmt.Errorf("work series ids: %w", err)
	}
	if f.releasedOrd, err = loadEarliestReleaseOrd(db); err != nil {
		return f, fmt.Errorf("earliest release ordinal: %w", err)
	}
	return f, nil
}

func loadTagWorkCounts(db *gorm.DB) (map[int64]int, error) {
	var rows []struct {
		TagID int64 `gorm:"column:tag_id"`
		Cnt   int   `gorm:"column:cnt"`
	}
	q := fmt.Sprintf(`SELECT e.key_id AS tag_id, count(DISTINCT e.work_id) AS cnt
		FROM %s `+worksPopulationJoin+` GROUP BY e.key_id`, reindexTagEdge, "e.work_id")
	if err := db.Raw(q).Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[int64]int, len(rows))
	for _, r := range rows {
		m[r.TagID] = r.Cnt
	}
	return m, nil
}

func reindexTags(ctx context.Context, db *gorm.DB, idx *catalogSearch.Indexer, batch int) error {
	counts, err := loadTagWorkCounts(db)
	if err != nil {
		return err
	}
	srcs, keys, err := loadSources(db, model.EntityTypeTag)
	if err != nil {
		return err
	}
	processed, lastID := 0, int64(0)
	for {
		var rows []struct {
			ID   int64  `gorm:"column:id"`
			Name string `gorm:"column:name"`
			Tier int16  `gorm:"column:tier"`
			Kind int16  `gorm:"column:kind"`
		}
		if err := db.Raw(`SELECT id, name, tier, kind FROM catalog_tag WHERE id > ? ORDER BY id LIMIT ?`,
			lastID, batch).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		docs := make([]catalogSearch.EntityDoc, len(rows))
		for i, r := range rows {
			docs[i] = catalogSearch.BuildTagDoc(catalogSearch.TagDocInput{
				ID: r.ID, Name: r.Name, Tier: r.Tier, Kind: r.Kind,
				WorkCount: counts[r.ID], Sources: srcs[r.ID], SourceKeys: keys[r.ID],
			})
		}
		if err := idx.UpsertBatch(ctx, catalogSearch.IndexTags, docs); err != nil {
			return err
		}
		processed += len(rows)
		lastID = rows[len(rows)-1].ID
	}
	slog.Info("reindexed", "index", catalogSearch.IndexTags, "docs", processed)
	return nil
}
