package editspec

import (
	"context"
	"fmt"

	catmodel "api/internal/platform/catalog/model"
	"api/internal/platform/editing"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	coverKindMax  = 64
	maxSafetyAxis = 2
)

type workCover struct {
	ImageHash      string
	Kind           string
	PortraitPinned bool
	Sexual         int16
	Violence       int16
}

type workScreenshot struct {
	ImageHash string
	Caption   string
	Sexual    int16
	Violence  int16
}

func safetyAxis(obj map[string]any, key string, index int) (int16, error) {
	n, err := objInt(obj, key, index, false)
	if err != nil {
		return 0, err
	}
	if n < 0 || n > maxSafetyAxis {
		return 0, fmt.Errorf("element %d: %s must be 0, 1 or 2", index, key)
	}
	return int16(n), nil
}

func parseCovers(v any) ([]workCover, error) {
	arr, err := asArray(v, "cover objects")
	if err != nil {
		return nil, err
	}
	return parseCoversN(arr)
}

func parseCoversN(arr []any) ([]workCover, error) {
	out := make([]workCover, 0, len(arr))
	seen := make(map[string]struct{}, len(arr))
	pinned := 0
	for i, el := range arr {
		obj, err := asObject(el, i, "image_hash", "kind", "portrait_pinned", "sexual", "violence")
		if err != nil {
			return nil, err
		}
		hash, err := objString(obj, "image_hash", i, true, maxHashRunes)
		if err != nil {
			return nil, err
		}
		if _, dup := seen[hash]; dup {
			return nil, fmt.Errorf("element %d: duplicate image_hash", i)
		}
		seen[hash] = struct{}{}
		kind, err := objString(obj, "kind", i, false, coverKindMax)
		if err != nil {
			return nil, err
		}
		pin, err := objBool(obj, "portrait_pinned", i)
		if err != nil {
			return nil, err
		}
		if pin {
			pinned++
		}
		sexual, err := safetyAxis(obj, "sexual", i)
		if err != nil {
			return nil, err
		}
		violence, err := safetyAxis(obj, "violence", i)
		if err != nil {
			return nil, err
		}
		out = append(out, workCover{
			ImageHash: hash, Kind: kind, PortraitPinned: pin,
			Sexual: sexual, Violence: violence,
		})
	}
	if pinned > 1 {
		return nil, fmt.Errorf("at most one cover may set portrait_pinned")
	}
	return out, nil
}

func validateCovers(v any) error { _, err := parseCovers(v); return err }

func applyCovers(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	covers, err := parseCovers(value)
	if err != nil {
		return fmt.Errorf("editspec: covers: %w", err)
	}
	if err := assertWorkExists(ctx, tx, entityID); err != nil {
		return err
	}
	if err := rejectUpstreamCoverCollisions(ctx, tx, entityID, covers); err != nil {
		return err
	}
	if err := tx.WithContext(ctx).
		Where("work_id = ? AND source_id = ?", entityID, curatedSourceID).
		Delete(&catmodel.CatalogWorkCover{}).Error; err != nil {
		return err
	}
	if len(covers) == 0 {
		return nil
	}
	rows := make([]catmodel.CatalogWorkCover, 0, len(covers))
	for i, c := range covers {
		rows = append(rows, catmodel.CatalogWorkCover{
			WorkID: entityID, ImageHash: c.ImageHash, SortOrder: i,
			Kind: c.Kind, PortraitPinned: c.PortraitPinned,
			Sexual: c.Sexual, Violence: c.Violence, SourceID: curatedSourceID,
		})
	}
	return tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}

// rejectUpstreamCoverCollisions makes an unwinnable insert an explicit 422.
// uq_catalog_work_cover does not carry source_id, so the curated lane and every
// importer share one key.
func rejectUpstreamCoverCollisions(ctx context.Context, tx *gorm.DB, entityID int64, covers []workCover) error {
	if len(covers) == 0 {
		return nil
	}
	hashes := make([]string, len(covers))
	for i, c := range covers {
		hashes[i] = c.ImageHash
	}
	var clash []catmodel.CatalogWorkCover
	if err := tx.WithContext(ctx).
		Select("image_hash").
		Where("work_id = ? AND source_id IS DISTINCT FROM ?", entityID, curatedSourceID).
		Where("image_hash IN ?", hashes).
		Order("image_hash").
		Find(&clash).Error; err != nil {
		return err
	}
	if len(clash) == 0 {
		return nil
	}
	return &editing.ValidationError{
		Key: FieldWorkCovers,
		Reason: fmt.Sprintf("image %s is already collected upstream for this work; "+
			"regrade it through %s instead",
			clash[0].ImageHash, FieldWorkCoversUpstream),
	}
}

func loadCovers(ctx context.Context, db *gorm.DB, workID int64) ([]any, error) {
	var rows []catmodel.CatalogWorkCover
	if err := db.WithContext(ctx).
		Where("work_id = ? AND source_id = ?", workID, curatedSourceID).
		Order("sort_order, image_hash").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]any, 0, len(rows))
	for _, r := range rows {
		el := map[string]any{"image_hash": r.ImageHash}
		if r.Kind != "" {
			el["kind"] = r.Kind
		}
		if r.PortraitPinned {
			el["portrait_pinned"] = true
		}
		if r.Sexual != 0 {
			el["sexual"] = int64(r.Sexual)
		}
		if r.Violence != 0 {
			el["violence"] = int64(r.Violence)
		}
		out = append(out, el)
	}
	return out, nil
}

func parseScreenshots(v any) ([]workScreenshot, error) {
	arr, err := asArray(v, "screenshot objects")
	if err != nil {
		return nil, err
	}
	return parseScreenshotsN(arr)
}

func parseScreenshotsN(arr []any) ([]workScreenshot, error) {
	out := make([]workScreenshot, 0, len(arr))
	seen := make(map[string]struct{}, len(arr))
	for i, el := range arr {
		obj, err := asObject(el, i, "image_hash", "caption", "sexual", "violence")
		if err != nil {
			return nil, err
		}
		hash, err := objString(obj, "image_hash", i, true, maxHashRunes)
		if err != nil {
			return nil, err
		}
		if _, dup := seen[hash]; dup {
			return nil, fmt.Errorf("element %d: duplicate image_hash", i)
		}
		seen[hash] = struct{}{}
		caption, err := objString(obj, "caption", i, false, maxCaptionRunes)
		if err != nil {
			return nil, err
		}
		sexual, err := safetyAxis(obj, "sexual", i)
		if err != nil {
			return nil, err
		}
		violence, err := safetyAxis(obj, "violence", i)
		if err != nil {
			return nil, err
		}
		out = append(out, workScreenshot{
			ImageHash: hash, Caption: caption, Sexual: sexual, Violence: violence,
		})
	}
	return out, nil
}

func validateScreenshots(v any) error { _, err := parseScreenshots(v); return err }

func applyScreenshots(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	shots, err := parseScreenshots(value)
	if err != nil {
		return fmt.Errorf("editspec: screenshots: %w", err)
	}
	if err := assertWorkExists(ctx, tx, entityID); err != nil {
		return err
	}
	if err := rejectUpstreamScreenshotCollisions(ctx, tx, entityID, shots); err != nil {
		return err
	}
	if err := tx.WithContext(ctx).
		Where("work_id = ? AND source_id = ?", entityID, curatedSourceID).
		Delete(&catmodel.CatalogWorkScreenshot{}).Error; err != nil {
		return err
	}
	if len(shots) == 0 {
		return nil
	}
	rows := make([]catmodel.CatalogWorkScreenshot, 0, len(shots))
	for i, s := range shots {
		rows = append(rows, catmodel.CatalogWorkScreenshot{
			WorkID: entityID, ImageHash: s.ImageHash, SortOrder: i,
			Caption: s.Caption, Sexual: s.Sexual, Violence: s.Violence,
			SourceID: curatedSourceID,
		})
	}
	return tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}

// rejectUpstreamScreenshotCollisions makes an unwinnable insert an explicit 422.
// uq_catalog_work_screenshot does not carry source_id, so the curated lane and
// every importer share one key.
func rejectUpstreamScreenshotCollisions(ctx context.Context, tx *gorm.DB, entityID int64, shots []workScreenshot) error {
	if len(shots) == 0 {
		return nil
	}
	hashes := make([]string, len(shots))
	for i, s := range shots {
		hashes[i] = s.ImageHash
	}
	var clash []catmodel.CatalogWorkScreenshot
	if err := tx.WithContext(ctx).
		Select("image_hash").
		Where("work_id = ? AND source_id IS DISTINCT FROM ?", entityID, curatedSourceID).
		Where("image_hash IN ?", hashes).
		Order("image_hash").
		Find(&clash).Error; err != nil {
		return err
	}
	if len(clash) == 0 {
		return nil
	}
	return &editing.ValidationError{
		Key: FieldWorkScreenshots,
		Reason: fmt.Sprintf("image %s is already collected upstream for this work; "+
			"regrade it through %s instead",
			clash[0].ImageHash, FieldWorkScreenshotsUpstream),
	}
}

func loadScreenshots(ctx context.Context, db *gorm.DB, workID int64) ([]any, error) {
	var rows []catmodel.CatalogWorkScreenshot
	if err := db.WithContext(ctx).
		Where("work_id = ? AND source_id = ?", workID, curatedSourceID).
		Order("sort_order, image_hash").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]any, 0, len(rows))
	for _, r := range rows {
		el := map[string]any{"image_hash": r.ImageHash}
		if r.Caption != "" {
			el["caption"] = r.Caption
		}
		if r.Sexual != 0 {
			el["sexual"] = int64(r.Sexual)
		}
		if r.Violence != 0 {
			el["violence"] = int64(r.Violence)
		}
		out = append(out, el)
	}
	return out, nil
}
