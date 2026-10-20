package editspec

import (
	"context"
	"fmt"
	"strings"

	catmodel "api/internal/platform/catalog/model"
	"api/internal/platform/editing"

	"gorm.io/gorm"
)

const (
	FieldWorkCoverOrder      = "catalog.work.cover_order"
	FieldWorkScreenshotOrder = "catalog.work.screenshot_order"
)

const maxWorkMediaOrderElements = 300

func workMediaOrderFieldSpecs() []editing.FieldSpec {
	spec := func(key string, kind int16) editing.FieldSpec {
		return editing.FieldSpec{
			Key: key, Kind: editing.KindList, DiffHint: editing.DiffHintItems,
			MaxElements: maxWorkMediaOrderElements,
			Value:       &editing.ValueSpec{Element: &editing.ElementSpec{Type: "text"}},
			Validate:    validateWorkMediaOrder,
			Apply:       applyWorkMediaOrder(kind),
		}
	}
	return []editing.FieldSpec{
		spec(FieldWorkCoverOrder, catmodel.WorkMediaOrderCover),
		spec(FieldWorkScreenshotOrder, catmodel.WorkMediaOrderScreenshot),
	}
}

func parseWorkMediaOrder(v any) ([]string, error) {
	arr, err := asArrayN(v, "image hashes", maxWorkMediaOrderElements)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(arr))
	seen := make(map[string]struct{}, len(arr))
	for i, el := range arr {
		s, ok := el.(string)
		if !ok || strings.TrimSpace(s) == "" || len([]rune(s)) > maxHashRunes {
			return nil, fmt.Errorf("element %d: must be an image hash", i)
		}
		if _, dup := seen[s]; dup {
			return nil, fmt.Errorf("element %d: duplicate image hash", i)
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out, nil
}

func validateWorkMediaOrder(v any) error { _, err := parseWorkMediaOrder(v); return err }

// Fields apply in key order, so cover_order lands before covers: an image the
// same proposal uploads is not stored yet. Hashes are therefore not checked
// against the work; the read path ignores any it cannot match.
func applyWorkMediaOrder(kind int16) editing.ApplyFunc {
	return func(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
		hashes, err := parseWorkMediaOrder(value)
		if err != nil {
			return fmt.Errorf("editspec: media order: %w", err)
		}
		if err := assertWorkExists(ctx, tx, entityID); err != nil {
			return err
		}
		if err := tx.WithContext(ctx).
			Where("work_id = ? AND kind = ?", entityID, kind).
			Delete(&catmodel.CatalogWorkMediaOrder{}).Error; err != nil {
			return err
		}
		if len(hashes) == 0 {
			return nil
		}
		rows := make([]catmodel.CatalogWorkMediaOrder, 0, len(hashes))
		for i, h := range hashes {
			rows = append(rows, catmodel.CatalogWorkMediaOrder{
				WorkID: entityID, Kind: kind, ImageHash: h, Position: i,
			})
		}
		return tx.WithContext(ctx).Create(&rows).Error
	}
}

func loadWorkMediaOrder(kind int16) func(context.Context, *gorm.DB, int64) ([]any, error) {
	return func(ctx context.Context, db *gorm.DB, workID int64) ([]any, error) {
		var hashes []string
		if err := db.WithContext(ctx).Model(&catmodel.CatalogWorkMediaOrder{}).
			Where("work_id = ? AND kind = ?", workID, kind).
			Order("position").Pluck("image_hash", &hashes).Error; err != nil {
			return nil, err
		}
		out := make([]any, 0, len(hashes))
		for _, h := range hashes {
			out = append(out, h)
		}
		return out, nil
	}
}

// WorkMediaOrderJoinSQL left-joins a work's media order onto the cover or
// screenshot rows aliased `alias`; order by `mo.position NULLS LAST` first.
func WorkMediaOrderJoinSQL(alias string, kind int16) string {
	return fmt.Sprintf(`LEFT JOIN catalog_work_media_order mo
		ON mo.work_id = %[1]s.work_id AND mo.kind = %[2]d AND mo.image_hash = %[1]s.image_hash`, alias, kind)
}
