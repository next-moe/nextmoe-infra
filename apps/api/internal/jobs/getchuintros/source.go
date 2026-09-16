package getchuintros

import (
	"context"
	"fmt"

	"api/internal/jobs/workpop"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"

	"gorm.io/gorm"
)

type anchorRow struct {
	WorkID   int64  `gorm:"column:work_id"`
	GetchuID string `gorm:"column:getchu_id"`
	Bundle   bool   `gorm:"column:bundle"`
}

func loadAnchors(ctx context.Context, db *gorm.DB, source int16, pop workpop.Population, limit, offset int) ([]anchorRow, error) {
	site, err := workpop.Predicate(pop, "w")
	if err != nil {
		return nil, err
	}
	var out []anchorRow
	err = db.WithContext(ctx).Raw(`
		SELECT DISTINCT w.id AS work_id, r.external_id AS getchu_id,
			`+repository.BundleReleaseSQL("rel")+` AS bundle
		FROM catalog_work w
		JOIN catalog_release rel ON rel.work_id = w.id AND rel.deleted_at IS NULL
		JOIN catalog_external_ref r ON r.entity_type = ? AND r.entity_id = rel.id
			AND r.source_id = ? AND r.link_kind = ?
		WHERE w.deleted_at IS NULL AND `+site+`
		ORDER BY w.id, r.external_id`,
		model.EntityTypeRelease, source, model.LinkKindExact).Scan(&out).Error
	if err != nil {
		return nil, fmt.Errorf("load getchu anchors: %w", err)
	}
	return window(out, limit, offset), nil
}

func window(rows []anchorRow, limit, offset int) []anchorRow {
	if limit <= 0 && offset <= 0 {
		return rows
	}
	var (
		out      []anchorRow
		nth      int
		lastWork int64
	)
	for _, r := range rows {
		if r.WorkID != lastWork {
			lastWork = r.WorkID
			nth++
			if limit > 0 && nth > offset+limit {
				break
			}
		}
		if nth > offset {
			out = append(out, r)
		}
	}
	return out
}

func loadStories(ctx context.Context, gdb *gorm.DB) (map[string]string, error) {
	var rows []struct {
		GetchuID string `gorm:"column:getchu_id"`
		Story    string `gorm:"column:story"`
	}
	err := gdb.WithContext(ctx).Raw(`
		SELECT getchu_id, story FROM items WHERE btrim(coalesce(story,'')) <> ''`).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("read staging items: %w", err)
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[r.GetchuID] = r.Story
	}
	return out, nil
}

func preloadExistingLangs(ctx context.Context, db *gorm.DB, workIDs []int64) (map[int64]map[string]bool, error) {
	out := map[int64]map[string]bool{}
	if len(workIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		WorkID int64  `gorm:"column:work_id"`
		Lang   string `gorm:"column:lang"`
	}
	if err := db.WithContext(ctx).
		Raw(`SELECT work_id, lang FROM catalog_work_intro WHERE work_id IN ?`, workIDs).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		set := out[r.WorkID]
		if set == nil {
			set = map[string]bool{}
			out[r.WorkID] = set
		}
		set[r.Lang] = true
	}
	return out, nil
}
