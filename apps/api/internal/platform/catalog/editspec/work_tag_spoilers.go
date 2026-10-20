package editspec

import (
	"context"
	"fmt"

	catmodel "api/internal/platform/catalog/model"
	"api/internal/platform/editing"

	"gorm.io/gorm"
)

// FieldWorkTagSpoilers sets the spoiler level of a tag on this work across
// every row that maps to it, curated and upstream, and never adds or removes a
// tag: that is catalog.work.tag_ids and its .suppressed companion. The level is
// stamped per row, which is what vndbtags reads before refreshing it.
const FieldWorkTagSpoilers = "catalog.work.tag_spoilers"

// A work carries at most 271 distinct tags in production (census 2026-10-20).
const maxWorkTagSpoilerElements = 600

type tagSpoiler struct {
	TagID   int64
	Spoiler int16
}

func workTagSpoilersFieldSpec() editing.FieldSpec {
	return editing.FieldSpec{
		Key: FieldWorkTagSpoilers, Kind: editing.KindList, DiffHint: editing.DiffHintItems,
		MaxElements: maxWorkTagSpoilerElements,
		Value: &editing.ValueSpec{Element: &editing.ElementSpec{
			Type: "object",
			Members: []editing.ElementMember{
				{Key: "tag_id", Type: "ref"},
				{Key: "spoiler", Type: "int", Vocabulary: "spoiler"},
			},
		}},
		Validate: validateWorkTagSpoilers,
		Apply:    applyWorkTagSpoilers,
		Provenance: []editing.ProvenanceTarget{{
			Table: catmodel.CatalogWorkTag{}.TableName(), Column: "spoiler", Rows: workTagRowsWhoseSpoilerChanges,
		}},
	}
}

func parseWorkTagSpoilers(v any) ([]tagSpoiler, error) {
	arr, err := asArrayN(v, "tag spoiler objects", maxWorkTagSpoilerElements)
	if err != nil {
		return nil, err
	}
	out := make([]tagSpoiler, 0, len(arr))
	seen := make(map[int64]struct{}, len(arr))
	for i, el := range arr {
		obj, err := asObject(el, i, "tag_id", "spoiler")
		if err != nil {
			return nil, err
		}
		if err := requireMembers(obj, i, "tag_id", "spoiler"); err != nil {
			return nil, err
		}
		tagID, err := objInt(obj, "tag_id", i, true)
		if err != nil {
			return nil, err
		}
		if tagID <= 0 {
			return nil, fmt.Errorf("element %d: tag_id must be a positive id", i)
		}
		if _, dup := seen[tagID]; dup {
			return nil, fmt.Errorf("element %d: tag %d appears twice", i, tagID)
		}
		seen[tagID] = struct{}{}
		spoiler, err := objInt(obj, "spoiler", i, true)
		if err != nil {
			return nil, err
		}
		switch int16(spoiler) {
		case catmodel.SpoilerNone, catmodel.SpoilerMild, catmodel.SpoilerSevere:
		default:
			return nil, fmt.Errorf("element %d: spoiler must be 0 (none), 1 (minor) or 2 (major)", i)
		}
		out = append(out, tagSpoiler{TagID: tagID, Spoiler: int16(spoiler)})
	}
	return out, nil
}

func validateWorkTagSpoilers(v any) error { _, err := parseWorkTagSpoilers(v); return err }

func applyWorkTagSpoilers(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	spoilers, err := parseWorkTagSpoilers(value)
	if err != nil {
		return fmt.Errorf("editspec: tag_spoilers: %w", err)
	}
	if err := assertWorkExists(ctx, tx, entityID); err != nil {
		return err
	}
	for _, s := range spoilers {
		res := tx.WithContext(ctx).Exec(`UPDATE catalog_work_tag wt SET spoiler = ?, updated_at = now()
			FROM catalog_tag_source_map m
			WHERE m.source_id = wt.source_id AND m.source_name = wt.name AND m.tag_id = ? AND wt.work_id = ?`,
			s.Spoiler, s.TagID, entityID)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return &editing.ValidationError{Key: FieldWorkTagSpoilers,
				Reason: fmt.Sprintf("tag %d is not on this work; add it through %s first", s.TagID, FieldWorkTagIDs)}
		}
	}
	return nil
}

func loadWorkTagSpoilers(ctx context.Context, db *gorm.DB, workID int64) ([]any, error) {
	var rows []struct {
		TagID   int64 `gorm:"column:tag_id"`
		Spoiler int16 `gorm:"column:spoiler"`
	}
	if err := db.WithContext(ctx).Raw(`SELECT m.tag_id, max(wt.spoiler) AS spoiler FROM catalog_work_tag wt
		JOIN catalog_tag_source_map m ON m.source_id = wt.source_id AND m.source_name = wt.name
		WHERE wt.work_id = ? GROUP BY m.tag_id ORDER BY m.tag_id`, workID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]any, 0, len(rows))
	for _, r := range rows {
		out = append(out, map[string]any{"tag_id": r.TagID, "spoiler": int64(r.Spoiler)})
	}
	return out, nil
}

func workTagRowsWhoseSpoilerChanges(ctx context.Context, tx *gorm.DB, entityID int64, value any) ([]int64, error) {
	spoilers, err := parseWorkTagSpoilers(value)
	if err != nil || len(spoilers) == 0 {
		return nil, err
	}
	want := make(map[int64]int16, len(spoilers))
	ids := make([]int64, 0, len(spoilers))
	for _, s := range spoilers {
		want[s.TagID] = s.Spoiler
		ids = append(ids, s.TagID)
	}
	var rows []struct {
		ID      int64 `gorm:"column:id"`
		TagID   int64 `gorm:"column:tag_id"`
		Spoiler int16 `gorm:"column:spoiler"`
	}
	if err := tx.WithContext(ctx).Raw(`SELECT wt.id, m.tag_id, wt.spoiler FROM catalog_work_tag wt
		JOIN catalog_tag_source_map m ON m.source_id = wt.source_id AND m.source_name = wt.name
		WHERE wt.work_id = ? AND m.tag_id IN ? ORDER BY wt.id`, entityID, ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	var out []int64
	for _, r := range rows {
		if r.Spoiler != want[r.TagID] {
			out = append(out, r.ID)
		}
	}
	return out, nil
}

// WorkTagSpoilerIsHumanSQL is true for a catalog_work_tag row whose spoiler a
// person last set.
func WorkTagSpoilerIsHumanSQL(alias string) string {
	return "COALESCE(" + HumanFieldProvenanceSQL(alias+".field_provenance", "spoiler") + ", false)"
}
