package vndbcovers

import (
	"context"
	"fmt"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

type registry struct {
	vndbSource    int16
	curatedSource int16
	galgameMedium int16
}

func resolveRegistry(ctx context.Context, db *gorm.DB) (registry, error) {
	var r registry
	if err := db.WithContext(ctx).Raw(`SELECT id FROM catalog_source WHERE key = 'vndb'`).Scan(&r.vndbSource).Error; err != nil {
		return r, fmt.Errorf("resolve vndb source: %w", err)
	}
	if err := db.WithContext(ctx).Raw(`SELECT id FROM catalog_source WHERE key = 'curated'`).Scan(&r.curatedSource).Error; err != nil {
		return r, fmt.Errorf("resolve curated source: %w", err)
	}
	if err := db.WithContext(ctx).Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&r.galgameMedium).Error; err != nil {
		return r, fmt.Errorf("resolve galgame medium: %w", err)
	}
	if r.vndbSource == 0 || r.curatedSource == 0 || r.galgameMedium == 0 {
		return r, fmt.Errorf("registry not seeded (vndb source=%d, curated source=%d, galgame medium=%d)",
			r.vndbSource, r.curatedSource, r.galgameMedium)
	}
	return r, nil
}

type candidate struct {
	WorkID int64  `gorm:"column:work_id"`
	VNDBID string `gorm:"column:vndb_id"`
}

// A work "has a cover" only when VNDB put one there. pkg* kinds are box scans
// the public picker vetoes, so they do not count.
//
// The first production run gated on NOT EXISTS(any cover row) and stalled at
// 13.3k of 65k vndb anchors: legacy-wiki rows kept 48k works out of the queue.
// The gate that replaced it still counted every other source as a cover, and on
// 2026-10-28 production held 3,456 vndb-anchored works with no vndb cover —
// 1,756 behind a Bangumi cover (mosaicked for adult games, v2868 the report)
// and most of the rest behind the blurred 'censored' stand-in.
func loadCandidates(ctx context.Context, db *gorm.DB, reg registry, ids []int64) ([]candidate, error) {
	sql := `
		SELECT DISTINCT ON (w.id) w.id AS work_id, r.external_id AS vndb_id
		FROM catalog_work w
		JOIN catalog_external_ref r ON r.entity_type = ? AND r.entity_id = w.id
			AND r.source_id = ? AND r.link_kind = ?
		WHERE w.medium_id = ? AND w.deleted_at IS NULL
			AND NOT EXISTS (
				SELECT 1 FROM catalog_work_cover c
				WHERE c.work_id = w.id AND c.source_id = ?
					AND c.kind NOT IN ('pkgfront','pkgback','pkgmed','pkgcontent','pkgside')
			)`
	args := []any{model.EntityTypeWork, reg.vndbSource, model.LinkKindExact, reg.galgameMedium, reg.vndbSource}
	if len(ids) > 0 {
		sql += "\n\t\t\tAND w.id IN ?"
		args = append(args, ids)
	}
	sql += "\n\t\tORDER BY w.id, r.external_id"

	var out []candidate
	if err := db.WithContext(ctx).Raw(sql, args...).Scan(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}
