package vndbtitles

import (
	"context"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

type loadedWork struct {
	WorkID          int64  `gorm:"column:work_id"`
	DisplayName     string `gorm:"column:display_name"`
	FieldProvenance []byte `gorm:"column:field_provenance"`
	VID             string `gorm:"column:vid"`
	OLang           string `gorm:"column:olang"`
	OLangTitle      string `gorm:"column:olang_title"`
	ENTitle         string `gorm:"column:en_title"`
}

func loadAnchored(ctx context.Context, db *gorm.DB, reg registry) ([]loadedWork, error) {
	var rows []loadedWork
	err := db.WithContext(ctx).Raw(`
		SELECT w.id AS work_id,
			w.display_name AS display_name,
			w.field_provenance AS field_provenance,
			r.external_id AS vid,
			coalesce(v.olang, '') AS olang,
			coalesce(t.title, '') AS olang_title,
			coalesce(en.title, '') AS en_title
		FROM catalog_work w
		JOIN catalog_external_ref r ON r.entity_type = ? AND r.entity_id = w.id
			AND r.source_id = ? AND r.link_kind = ? AND r.dead_at IS NULL
		LEFT JOIN src_vndb.vn v ON v.id = r.external_id
		LEFT JOIN src_vndb.vn_titles t ON t.id = v.id AND t.lang = v.olang
		LEFT JOIN src_vndb.vn_titles en ON en.id = v.id AND en.lang = '`+vndbLangEnglish+`' AND en.official
		WHERE w.deleted_at IS NULL AND w.status <> ?
		ORDER BY w.id, r.external_id`,
		model.EntityTypeWork, reg.vndbSource, model.LinkKindExact, model.WorkStatusMerged,
	).Scan(&rows).Error
	return rows, err
}

func classify(rows []loadedWork, limit int) (population []loadedWork, multi, missing int) {
	order := make([]int64, 0)
	byID := make(map[int64][]loadedWork)
	for _, r := range rows {
		if _, ok := byID[r.WorkID]; !ok {
			order = append(order, r.WorkID)
		}
		byID[r.WorkID] = append(byID[r.WorkID], r)
	}
	for _, id := range order {
		g := byID[id]
		if len(g) > 1 {
			multi++
			continue
		}
		r := g[0]
		if r.OLangTitle == "" {
			missing++
			continue
		}
		population = append(population, r)
	}
	if limit > 0 && limit < len(population) {
		population = population[:limit]
	}
	return population, multi, missing
}

func loadExistingTitles(ctx context.Context, db *gorm.DB, ids []int64) (map[int64][]existingTitle, error) {
	out := make(map[int64][]existingTitle, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	// Postgres binds at most 65,535 parameters per statement and the population
	// was 65,235 works on 2026-09-19, so one IN list would stop working within weeks.
	for start := 0; start < len(ids); start += titleLoadChunk {
		var rows []existingTitleRow
		if err := db.WithContext(ctx).Raw(
			`SELECT work_id, lang, title FROM catalog_work_title WHERE work_id IN ?`,
			ids[start:min(start+titleLoadChunk, len(ids))],
		).Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			out[r.WorkID] = append(out[r.WorkID], existingTitle{Lang: r.Lang, Title: r.Title})
		}
	}
	return out, nil
}

const titleLoadChunk = 5000

type existingTitleRow struct {
	WorkID int64  `gorm:"column:work_id"`
	Lang   string `gorm:"column:lang"`
	Title  string `gorm:"column:title"`
}
