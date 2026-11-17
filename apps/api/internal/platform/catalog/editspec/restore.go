package editspec

import (
	"context"

	catmodel "api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

func restoreCovers(ctx context.Context, db *gorm.DB, entityID int64, value any) (any, error) {
	return dropUpstreamMedia(ctx, db, &catmodel.CatalogWorkCover{}, entityID, value)
}

func restoreScreenshots(ctx context.Context, db *gorm.DB, entityID int64, value any) (any, error) {
	return dropUpstreamMedia(ctx, db, &catmodel.CatalogWorkScreenshot{}, entityID, value)
}

func dropUpstreamMedia(ctx context.Context, db *gorm.DB, table any, entityID int64, value any) (any, error) {
	arr, ok := value.([]any)
	if !ok || len(arr) == 0 {
		return value, nil
	}
	hashes := make([]string, 0, len(arr))
	for _, el := range arr {
		if obj, ok := el.(map[string]any); ok {
			if h, ok := obj["image_hash"].(string); ok {
				hashes = append(hashes, h)
			}
		}
	}
	var held []string
	if err := db.WithContext(ctx).Model(table).
		Where("work_id = ? AND source_id IS DISTINCT FROM ?", entityID, curatedSourceID).
		Where("image_hash IN ?", hashes).
		Pluck("image_hash", &held).Error; err != nil {
		return nil, err
	}
	upstream := make(map[string]struct{}, len(held))
	for _, h := range held {
		upstream[h] = struct{}{}
	}
	return keepElements(arr, func(el any) bool {
		obj, _ := el.(map[string]any)
		h, _ := obj["image_hash"].(string)
		_, gone := upstream[h]
		return !gone
	}), nil
}

func restoreLabels(ctx context.Context, db *gorm.DB, entityID int64, value any) (any, error) {
	arr, ok := value.([]any)
	if !ok || len(arr) == 0 {
		return value, nil
	}
	var held []catmodel.CatalogWorkLabel
	if err := db.WithContext(ctx).Select("label_id", "kind").
		Where("work_id = ? AND source_id IS DISTINCT FROM ?", entityID, curatedSourceID).
		Find(&held).Error; err != nil {
		return nil, err
	}
	upstream := make(map[[2]int64]struct{}, len(held))
	for _, r := range held {
		upstream[[2]int64{r.LabelID, int64(r.Kind)}] = struct{}{}
	}
	return keepElements(arr, func(el any) bool {
		obj, _ := el.(map[string]any)
		id, _ := objInt(obj, "label_id", 0, false)
		kind, _ := objInt(obj, "kind", 0, false)
		_, gone := upstream[[2]int64{id, kind}]
		return !gone
	}), nil
}

func restoreWorkLinks(ctx context.Context, db *gorm.DB, entityID int64, value any) (any, error) {
	return restoreLinksFor(ctx, db, catmodel.EntityTypeWork, entityID, value)
}

func restoreLabelLinks(ctx context.Context, db *gorm.DB, entityID int64, value any) (any, error) {
	return restoreLinksFor(ctx, db, catmodel.EntityTypeLabel, entityID, value)
}

func restoreLinksFor(ctx context.Context, db *gorm.DB, entityType int16, entityID int64, value any) (any, error) {
	arr, ok := value.([]any)
	if !ok || len(arr) == 0 {
		return value, nil
	}
	var held []struct {
		SourceKey  string `gorm:"column:key"`
		ExternalID string `gorm:"column:external_id"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT s.key, r.external_id
		FROM catalog_external_ref r
		JOIN catalog_source s ON s.id = r.source_id
		WHERE r.entity_type = ? AND r.entity_id = ? AND r.matched_by <> ?`,
		entityType, entityID, curatedMatchedBy).Scan(&held).Error; err != nil {
		return nil, err
	}
	upstream := make(map[[2]string]struct{}, len(held))
	for _, r := range held {
		upstream[[2]string{r.SourceKey, r.ExternalID}] = struct{}{}
	}
	return keepElements(arr, func(el any) bool {
		s, _ := el.(string)
		cl, ok := classifyWorkLink(s)
		if !ok {
			return true
		}
		_, gone := upstream[[2]string{cl.SourceKey, cl.ExternalID}]
		return !gone
	}), nil
}

func keepElements(arr []any, keep func(any) bool) []any {
	out := make([]any, 0, len(arr))
	for _, el := range arr {
		if keep(el) {
			out = append(out, el)
		}
	}
	return out
}
