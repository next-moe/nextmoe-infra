package censoredcovers

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

// The candidate gate deliberately ignores wiki-lineage rows (curated + their
// AI-upscaled 'upscale' twins): it judges the world AFTER the planned deletion
// of those rows, so ghosts can be pre-staged while the curated covers are
// still in place — the picker keeps serving real safe art until the deletion
// lands, and the SFW face flips straight from curated to ghost with no blank
// window in between. pkg* kinds are box scans the public picker vetoes.
const (
	pkgKinds          = "'pkgfront','pkgback','pkgmed','pkgcontent','pkgside'"
	wikiLineageOrSelf = "'curated','upscale','censored'"
)

type registry struct {
	censoredSource int16
}

func resolveRegistry(ctx context.Context, db *gorm.DB) (registry, error) {
	var r registry
	if err := db.WithContext(ctx).Raw(`SELECT id FROM catalog_source WHERE key = 'censored'`).Scan(&r.censoredSource).Error; err != nil {
		return r, fmt.Errorf("resolve censored source: %w", err)
	}
	if r.censoredSource == 0 {
		return r, fmt.Errorf("registry not seeded (censored source missing — run the catalog migration first)")
	}
	return r, nil
}

type candidate struct {
	WorkID int64 `gorm:"column:work_id"`
}

func loadCandidates(ctx context.Context, db *gorm.DB, reg registry, ids []int64) ([]candidate, error) {
	sql := `
		SELECT w.id AS work_id
		FROM catalog_work w
		WHERE w.deleted_at IS NULL
			AND EXISTS (
				SELECT 1 FROM catalog_work_cover c
				JOIN catalog_source cs ON cs.id = c.source_id
				WHERE c.work_id = w.id
					AND c.kind NOT IN (` + pkgKinds + `)
					AND cs.key NOT IN (` + wikiLineageOrSelf + `)
					AND c.sexual >= 2
			)
			AND NOT EXISTS (
				SELECT 1 FROM catalog_work_cover c
				JOIN catalog_source cs ON cs.id = c.source_id
				WHERE c.work_id = w.id
					AND c.kind NOT IN (` + pkgKinds + `)
					AND cs.key NOT IN (` + wikiLineageOrSelf + `)
					AND c.sexual < 2
			)
			AND NOT EXISTS (
				SELECT 1 FROM catalog_work_cover c
				WHERE c.work_id = w.id AND c.source_id = ?
			)`
	args := []any{reg.censoredSource}
	if len(ids) > 0 {
		sql += "\n\t\t\tAND w.id IN ?"
		args = append(args, ids)
	}
	sql += "\n\t\tORDER BY w.id"

	var out []candidate
	if err := db.WithContext(ctx).Raw(sql, args...).Scan(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

type explicitRow struct {
	ImageHash string `gorm:"column:image_hash"`
	Violence  int16  `gorm:"column:violence"`
}

func loadExplicitRows(ctx context.Context, db *gorm.DB, workID int64) ([]explicitRow, error) {
	var out []explicitRow
	err := db.WithContext(ctx).Raw(`
		SELECT c.image_hash, c.violence
		FROM catalog_work_cover c
		JOIN catalog_source cs ON cs.id = c.source_id
		WHERE c.work_id = ?
			AND c.kind NOT IN (`+pkgKinds+`)
			AND cs.key NOT IN (`+wikiLineageOrSelf+`)
			AND c.sexual >= 2
		ORDER BY c.sort_order, c.id`, workID).Scan(&out).Error
	return out, err
}

func ParseIDs(raw string) ([]int64, error) {
	var out []int64
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("--ids: %q is not a work id", part)
		}
		out = append(out, id)
	}
	return out, nil
}
