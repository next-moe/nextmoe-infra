package docsync

import (
	"context"
	"fmt"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	catalogSearch "api/internal/platform/catalog/search"

	"gorm.io/gorm"
)

const worksPopulationJoin = `JOIN catalog_work w ON w.id = %s AND w.deleted_at IS NULL
	AND w.medium_id = (SELECT id FROM catalog_medium WHERE key = 'galgame') AND w.status = 0`

func loadWorkEdgeIDs(db *gorm.DB, edge string, s scope) (map[int64][]int64, error) {
	var rows []struct {
		WorkID int64 `gorm:"column:work_id"`
		KeyID  int64 `gorm:"column:key_id"`
	}
	q := fmt.Sprintf(`SELECT DISTINCT e.work_id, e.key_id FROM %s `+worksPopulationJoin+
		` WHERE true`+s.and("e.work_id")+` ORDER BY e.work_id, e.key_id`, edge, "e.work_id")
	if err := db.Raw(q, s.args()...).Scan(&rows).Error; err != nil {
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
)

var (
	reindexLabelEdge  = `(SELECT vwl.label_id AS key_id, vwl.work_id FROM catalog_work_label vwl WHERE ` + editspec.NotSuppressedWorkLabelSQL("vwl") + `) e`
	reindexSeriesEdge = `(SELECT vsm.series_id AS key_id, vsm.work_id FROM catalog_series_member vsm WHERE ` + editspec.NotSuppressedWorkSeriesSQL("vsm") + `) e`
	reindexEngineEdge = `(SELECT engine_id AS key_id, work_id FROM ` + editspec.VisibleWorkEnginesSQL + ` x) e`
)

func loadWorkReleaseOrd(db *gorm.DB, s scope) (map[int64]int64, error) {
	var rows []struct {
		WorkID int64  `gorm:"column:work_id"`
		Ord    *int64 `gorm:"column:ord"`
	}
	q := fmt.Sprintf(`SELECT r.work_id, `+model.WorkReleaseOrdSQL("r", "")+` AS ord
		FROM catalog_release r `+worksPopulationJoin+`
		WHERE r.deleted_at IS NULL`+s.and("r.work_id")+`
		GROUP BY r.work_id`, "r.work_id")
	if err := db.Raw(q, s.args()...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[int64]int64, len(rows))
	for _, r := range rows {
		if r.Ord != nil {
			m[r.WorkID] = *r.Ord
		}
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

func loadWorksFacets(db *gorm.DB, s scope) (worksFacets, error) {
	var f worksFacets
	var err error
	if f.tagIDs, err = loadWorkEdgeIDs(db, reindexTagEdge, s); err != nil {
		return f, fmt.Errorf("work tag ids: %w", err)
	}
	if f.labelIDs, err = loadWorkEdgeIDs(db, reindexLabelEdge, s); err != nil {
		return f, fmt.Errorf("work label ids: %w", err)
	}
	if f.engineIDs, err = loadWorkEdgeIDs(db, reindexEngineEdge, s); err != nil {
		return f, fmt.Errorf("work engine ids: %w", err)
	}
	if f.seriesIDs, err = loadWorkEdgeIDs(db, reindexSeriesEdge, s); err != nil {
		return f, fmt.Errorf("work series ids: %w", err)
	}
	if f.releasedOrd, err = loadWorkReleaseOrd(db, s); err != nil {
		return f, fmt.Errorf("earliest release ordinal: %w", err)
	}
	return f, nil
}

func loadEdgeWorkCounts(db *gorm.DB, edge string, s scope) (map[int64]int, error) {
	var rows []struct {
		KeyID int64 `gorm:"column:key_id"`
		Cnt   int   `gorm:"column:cnt"`
	}
	q := fmt.Sprintf(`SELECT e.key_id, count(DISTINCT e.work_id) AS cnt
		FROM %s `+worksPopulationJoin+` WHERE true`+s.and("e.key_id")+` GROUP BY e.key_id`, edge, "e.work_id")
	if err := db.Raw(q, s.args()...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[int64]int, len(rows))
	for _, r := range rows {
		m[r.KeyID] = r.Cnt
	}
	return m, nil
}

func loadTagAliases(db *gorm.DB, s scope) (map[int64][]string, error) {
	var rows []struct {
		TagID int64  `gorm:"column:tag_id"`
		Name  string `gorm:"column:source_name"`
	}
	if err := db.Raw(`SELECT DISTINCT tag_id, source_name FROM catalog_tag_source_map WHERE true`+s.and("tag_id")+
		` ORDER BY tag_id, source_name`, s.args()...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[int64][]string)
	for _, r := range rows {
		m[r.TagID] = append(m[r.TagID], r.Name)
	}
	return m, nil
}

func reindexTags(ctx context.Context, db *gorm.DB, idx *catalogSearch.Indexer, batch int, s scope) ([]int64, error) {
	counts, err := loadEdgeWorkCounts(db, reindexTagEdge, s)
	if err != nil {
		return nil, err
	}
	srcs, keys, err := loadSources(db, model.EntityTypeTag, s)
	if err != nil {
		return nil, err
	}
	aliases, err := loadTagAliases(db, s)
	if err != nil {
		return nil, err
	}
	var indexed []int64
	lastID := int64(0)
	for {
		var rows []struct {
			ID   int64  `gorm:"column:id"`
			Name string `gorm:"column:name"`
			Tier int16  `gorm:"column:tier"`
			Kind int16  `gorm:"column:kind"`
		}
		if err := db.Raw(`SELECT id, name, tier, kind FROM catalog_tag WHERE id > ?`+s.and("id")+` ORDER BY id LIMIT ?`,
			s.pageArgs(lastID, batch)...).Scan(&rows).Error; err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			break
		}
		docs := make([]catalogSearch.EntityDoc, len(rows))
		for i, r := range rows {
			docs[i] = catalogSearch.BuildTagDoc(catalogSearch.TagDocInput{
				ID: r.ID, Name: r.Name, Tier: r.Tier, Kind: r.Kind,
				WorkCount: counts[r.ID], Sources: srcs[r.ID], SourceKeys: keys[r.ID],
				Aliases: aliases[r.ID],
			})
			indexed = append(indexed, r.ID)
		}
		if err := idx.UpsertBatch(ctx, catalogSearch.IndexTags, docs); err != nil {
			return nil, err
		}
		lastID = rows[len(rows)-1].ID
	}
	return indexed, nil
}
