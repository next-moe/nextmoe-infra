package editspec

import (
	"context"
	"fmt"

	catmodel "api/internal/platform/catalog/model"
	"api/internal/platform/editing"

	"gorm.io/gorm"
)

// The upstream media fields regrade rows the importers own: they update kind,
// pin, grades and caption in place and never add or remove a row. Adding and
// removing stays with the curated lane (catalog.work.covers / .screenshots) and
// hiding with the `.suppressed` companions. Each changed column is stamped on
// its own row, which is what the grade and pin jobs read before writing.
//
// The keys sort after catalog.work.covers and catalog.work.screenshots, so in a
// proposal that touches both the curated lane is already applied when the
// portrait-pin check below runs.
const (
	FieldWorkCoversUpstream      = "catalog.work.covers_upstream"
	FieldWorkScreenshotsUpstream = "catalog.work.screenshots_upstream"
)

const maxUpstreamMediaElements = 300

func upstreamMediaFieldSpecs() []editing.FieldSpec {
	coverTable := catmodel.CatalogWorkCover{}.TableName()
	shotTable := catmodel.CatalogWorkScreenshot{}.TableName()
	return []editing.FieldSpec{
		{
			Key: FieldWorkCoversUpstream, Kind: editing.KindList, DiffHint: editing.DiffHintImage,
			MaxElements: maxUpstreamMediaElements,
			Value: &editing.ValueSpec{Element: &editing.ElementSpec{
				Type: "object",
				Members: []editing.ElementMember{
					{Key: "image_hash", Type: "imagehash"},
					{Key: "kind", Type: "text"},
					{Key: "portrait_pinned", Type: "bool"},
					{Key: "sexual", Type: "int", Vocabulary: "sexual"},
					{Key: "violence", Type: "int", Vocabulary: "violence"},
				},
			}},
			Validate: validateUpstreamCovers,
			Apply:    applyUpstreamCovers,
			Provenance: []editing.ProvenanceTarget{
				{Table: coverTable, Column: "kind", Rows: upstreamCoverRowsChanging("kind")},
				{Table: coverTable, Column: "portrait_pinned", Rows: upstreamCoverRowsChanging("portrait_pinned")},
				{Table: coverTable, Column: "sexual", Rows: upstreamCoverRowsChanging("sexual")},
				{Table: coverTable, Column: "violence", Rows: upstreamCoverRowsChanging("violence")},
			},
		},
		{
			Key: FieldWorkScreenshotsUpstream, Kind: editing.KindList, DiffHint: editing.DiffHintImage,
			MaxElements: maxUpstreamMediaElements,
			Value: &editing.ValueSpec{Element: &editing.ElementSpec{
				Type: "object",
				Members: []editing.ElementMember{
					{Key: "image_hash", Type: "imagehash"},
					{Key: "caption", Type: "text"},
					{Key: "sexual", Type: "int", Vocabulary: "sexual"},
					{Key: "violence", Type: "int", Vocabulary: "violence"},
				},
			}},
			Validate: validateUpstreamScreenshots,
			Apply:    applyUpstreamScreenshots,
			Provenance: []editing.ProvenanceTarget{
				{Table: shotTable, Column: "caption", Rows: upstreamShotRowsChanging("caption")},
				{Table: shotTable, Column: "sexual", Rows: upstreamShotRowsChanging("sexual")},
				{Table: shotTable, Column: "violence", Rows: upstreamShotRowsChanging("violence")},
			},
		},
	}
}

func requireMembers(obj map[string]any, index int, keys ...string) error {
	for _, k := range keys {
		if _, ok := obj[k]; !ok {
			return fmt.Errorf("element %d: %s is required", index, k)
		}
	}
	return nil
}

// Every member is required: a grade left out would otherwise read as 0 and
// silently downgrade an upstream row's rating.
func parseUpstreamCovers(v any) ([]workCover, error) {
	arr, err := asArrayN(v, "cover objects", maxUpstreamMediaElements)
	if err != nil {
		return nil, err
	}
	for i, el := range arr {
		obj, err := asObject(el, i, "image_hash", "kind", "portrait_pinned", "sexual", "violence")
		if err != nil {
			return nil, err
		}
		if err := requireMembers(obj, i, "image_hash", "kind", "portrait_pinned", "sexual", "violence"); err != nil {
			return nil, err
		}
	}
	return parseCoversN(arr)
}

func parseUpstreamScreenshots(v any) ([]workScreenshot, error) {
	arr, err := asArrayN(v, "screenshot objects", maxUpstreamMediaElements)
	if err != nil {
		return nil, err
	}
	for i, el := range arr {
		obj, err := asObject(el, i, "image_hash", "caption", "sexual", "violence")
		if err != nil {
			return nil, err
		}
		if err := requireMembers(obj, i, "image_hash", "caption", "sexual", "violence"); err != nil {
			return nil, err
		}
	}
	return parseScreenshotsN(arr)
}

func validateUpstreamCovers(v any) error { _, err := parseUpstreamCovers(v); return err }

func validateUpstreamScreenshots(v any) error { _, err := parseUpstreamScreenshots(v); return err }

func applyUpstreamCovers(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	covers, err := parseUpstreamCovers(value)
	if err != nil {
		return fmt.Errorf("editspec: covers_upstream: %w", err)
	}
	if err := assertWorkExists(ctx, tx, entityID); err != nil {
		return err
	}
	pinned := false
	for _, c := range covers {
		res := tx.WithContext(ctx).Exec(`UPDATE catalog_work_cover
			SET kind = ?, portrait_pinned = ?, sexual = ?, violence = ?, updated_at = now()
			WHERE work_id = ? AND image_hash = ? AND `+NotCuratedLaneSQL("source_id"),
			c.Kind, c.PortraitPinned, c.Sexual, c.Violence, entityID, c.ImageHash)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return &editing.ValidationError{Key: FieldWorkCoversUpstream,
				Reason: fmt.Sprintf("image %s is not an upstream cover of this work", c.ImageHash)}
		}
		pinned = pinned || c.PortraitPinned
	}
	if !pinned {
		return nil
	}
	var curatedPins int64
	if err := tx.WithContext(ctx).Model(&catmodel.CatalogWorkCover{}).
		Where("work_id = ? AND portrait_pinned AND "+CuratedLaneSQL("source_id"), entityID).
		Count(&curatedPins).Error; err != nil {
		return err
	}
	if curatedPins > 0 {
		return &editing.ValidationError{Key: FieldWorkCoversUpstream,
			Reason: "a curated cover already holds the portrait pin; unpin it in catalog.work.covers first"}
	}
	return nil
}

func applyUpstreamScreenshots(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	shots, err := parseUpstreamScreenshots(value)
	if err != nil {
		return fmt.Errorf("editspec: screenshots_upstream: %w", err)
	}
	if err := assertWorkExists(ctx, tx, entityID); err != nil {
		return err
	}
	for _, s := range shots {
		res := tx.WithContext(ctx).Exec(`UPDATE catalog_work_screenshot
			SET caption = ?, sexual = ?, violence = ?, updated_at = now()
			WHERE work_id = ? AND image_hash = ? AND `+NotCuratedLaneSQL("source_id"),
			s.Caption, s.Sexual, s.Violence, entityID, s.ImageHash)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return &editing.ValidationError{Key: FieldWorkScreenshotsUpstream,
				Reason: fmt.Sprintf("image %s is not an upstream screenshot of this work", s.ImageHash)}
		}
	}
	return nil
}

func upstreamCoverRowsChanging(column string) func(context.Context, *gorm.DB, int64, any) ([]int64, error) {
	return func(ctx context.Context, tx *gorm.DB, entityID int64, value any) ([]int64, error) {
		covers, err := parseUpstreamCovers(value)
		if err != nil || len(covers) == 0 {
			return nil, err
		}
		want := make(map[string]any, len(covers))
		hashes := make([]string, 0, len(covers))
		for _, c := range covers {
			switch column {
			case "kind":
				want[c.ImageHash] = c.Kind
			case "portrait_pinned":
				want[c.ImageHash] = c.PortraitPinned
			case "sexual":
				want[c.ImageHash] = c.Sexual
			case "violence":
				want[c.ImageHash] = c.Violence
			}
			hashes = append(hashes, c.ImageHash)
		}
		var rows []catmodel.CatalogWorkCover
		if err := tx.WithContext(ctx).
			Where("work_id = ? AND image_hash IN ? AND "+NotCuratedLaneSQL("source_id"), entityID, hashes).
			Order("id").Find(&rows).Error; err != nil {
			return nil, err
		}
		var out []int64
		for _, r := range rows {
			var have any
			switch column {
			case "kind":
				have = r.Kind
			case "portrait_pinned":
				have = r.PortraitPinned
			case "sexual":
				have = r.Sexual
			case "violence":
				have = r.Violence
			}
			if have != want[r.ImageHash] {
				out = append(out, r.ID)
			}
		}
		return out, nil
	}
}

func upstreamShotRowsChanging(column string) func(context.Context, *gorm.DB, int64, any) ([]int64, error) {
	return func(ctx context.Context, tx *gorm.DB, entityID int64, value any) ([]int64, error) {
		shots, err := parseUpstreamScreenshots(value)
		if err != nil || len(shots) == 0 {
			return nil, err
		}
		want := make(map[string]any, len(shots))
		hashes := make([]string, 0, len(shots))
		for _, s := range shots {
			switch column {
			case "caption":
				want[s.ImageHash] = s.Caption
			case "sexual":
				want[s.ImageHash] = s.Sexual
			case "violence":
				want[s.ImageHash] = s.Violence
			}
			hashes = append(hashes, s.ImageHash)
		}
		var rows []catmodel.CatalogWorkScreenshot
		if err := tx.WithContext(ctx).
			Where("work_id = ? AND image_hash IN ? AND "+NotCuratedLaneSQL("source_id"), entityID, hashes).
			Order("id").Find(&rows).Error; err != nil {
			return nil, err
		}
		var out []int64
		for _, r := range rows {
			var have any
			switch column {
			case "caption":
				have = r.Caption
			case "sexual":
				have = r.Sexual
			case "violence":
				have = r.Violence
			}
			if have != want[r.ImageHash] {
				out = append(out, r.ID)
			}
		}
		return out, nil
	}
}

func loadUpstreamCovers(ctx context.Context, db *gorm.DB, workID int64) ([]any, error) {
	var rows []catmodel.CatalogWorkCover
	if err := db.WithContext(ctx).
		Where("work_id = ? AND "+NotCuratedLaneSQL("source_id"), workID).
		Order("source_id, sort_order, image_hash").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]any, 0, len(rows))
	for _, r := range rows {
		out = append(out, map[string]any{
			"image_hash": r.ImageHash, "kind": r.Kind, "portrait_pinned": r.PortraitPinned,
			"sexual": int64(r.Sexual), "violence": int64(r.Violence),
		})
	}
	return out, nil
}

func loadUpstreamScreenshots(ctx context.Context, db *gorm.DB, workID int64) ([]any, error) {
	var rows []catmodel.CatalogWorkScreenshot
	if err := db.WithContext(ctx).
		Where("work_id = ? AND "+NotCuratedLaneSQL("source_id"), workID).
		Order("source_id, sort_order, image_hash").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]any, 0, len(rows))
	for _, r := range rows {
		out = append(out, map[string]any{
			"image_hash": r.ImageHash, "caption": r.Caption,
			"sexual": int64(r.Sexual), "violence": int64(r.Violence),
		})
	}
	return out, nil
}
