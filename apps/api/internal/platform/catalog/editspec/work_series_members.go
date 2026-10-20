package editspec

import (
	"context"
	"fmt"

	catmodel "api/internal/platform/catalog/model"
	"api/internal/platform/editing"

	"gorm.io/gorm"
)

// FieldWorkSeriesMembers sets position and kind on the work's existing series
// memberships, curated and upstream alike, and never adds or removes one:
// joining a curated series is catalog.work.series_ids, leaving an upstream one
// is its .suppressed companion. Each changed column is stamped on its row,
// which is what seriesorder reads before rewriting the order.
//
// The key sorts after catalog.work.series_ids, so a proposal that joins a
// curated series and places the work in it applies the join first.
const FieldWorkSeriesMembers = "catalog.work.series_members"

// A work belongs to at most 3 series in production (census 2026-10-20).
const (
	maxWorkSeriesMemberElements = 20
	maxSeriesPosition           = 9999
)

type seriesMemberPlacement struct {
	SeriesID int64
	Position int16
	Kind     int16
}

func workSeriesMembersFieldSpec() editing.FieldSpec {
	table := catmodel.CatalogSeriesMember{}.TableName()
	return editing.FieldSpec{
		Key: FieldWorkSeriesMembers, Kind: editing.KindList, DiffHint: editing.DiffHintItems,
		MaxElements: maxWorkSeriesMemberElements,
		Value: &editing.ValueSpec{Element: &editing.ElementSpec{
			Type: "object",
			Members: []editing.ElementMember{
				{Key: "series_id", Type: "ref"},
				{Key: "position", Type: "int"},
				{Key: "kind", Type: "int", Vocabulary: "member_kind"},
			},
		}},
		Validate: validateWorkSeriesMembers,
		Apply:    applyWorkSeriesMembers,
		Provenance: []editing.ProvenanceTarget{
			{Table: table, Column: "position", Rows: seriesMemberRowsChanging("position")},
			{Table: table, Column: "kind", Rows: seriesMemberRowsChanging("kind")},
		},
	}
}

func parseWorkSeriesMembers(v any) ([]seriesMemberPlacement, error) {
	arr, err := asArrayN(v, "series membership objects", maxWorkSeriesMemberElements)
	if err != nil {
		return nil, err
	}
	out := make([]seriesMemberPlacement, 0, len(arr))
	seen := make(map[int64]struct{}, len(arr))
	for i, el := range arr {
		obj, err := asObject(el, i, "series_id", "position", "kind")
		if err != nil {
			return nil, err
		}
		if err := requireMembers(obj, i, "series_id", "position", "kind"); err != nil {
			return nil, err
		}
		seriesID, err := objInt(obj, "series_id", i, true)
		if err != nil {
			return nil, err
		}
		if seriesID <= 0 {
			return nil, fmt.Errorf("element %d: series_id must be a positive id", i)
		}
		if _, dup := seen[seriesID]; dup {
			return nil, fmt.Errorf("element %d: series %d appears twice", i, seriesID)
		}
		seen[seriesID] = struct{}{}
		position, err := objInt(obj, "position", i, true)
		if err != nil {
			return nil, err
		}
		if position < 0 || position > maxSeriesPosition {
			return nil, fmt.Errorf("element %d: position must be between 0 and %d", i, maxSeriesPosition)
		}
		kind, err := objInt(obj, "kind", i, true)
		if err != nil {
			return nil, err
		}
		switch int16(kind) {
		case catmodel.SeriesMemberKindUnknown, catmodel.SeriesMemberKindMain, catmodel.SeriesMemberKindFandisc,
			catmodel.SeriesMemberKindSideStory, catmodel.SeriesMemberKindCollection:
		default:
			return nil, fmt.Errorf("element %d: kind must be 0 (unknown), 1 (main), 2 (fandisc), 3 (side_story) or 4 (collection)", i)
		}
		out = append(out, seriesMemberPlacement{SeriesID: seriesID, Position: int16(position), Kind: int16(kind)})
	}
	return out, nil
}

func validateWorkSeriesMembers(v any) error { _, err := parseWorkSeriesMembers(v); return err }

func applyWorkSeriesMembers(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	placements, err := parseWorkSeriesMembers(value)
	if err != nil {
		return fmt.Errorf("editspec: series_members: %w", err)
	}
	if err := assertWorkExists(ctx, tx, entityID); err != nil {
		return err
	}
	for _, p := range placements {
		res := tx.WithContext(ctx).Exec(`UPDATE catalog_series_member sm
			SET position = ?, kind = ?, updated_at = now()
			WHERE sm.work_id = ? AND sm.series_id = ? AND `+NotSuppressedWorkSeriesSQL("sm"),
			p.Position, p.Kind, entityID, p.SeriesID)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return &editing.ValidationError{Key: FieldWorkSeriesMembers,
				Reason: fmt.Sprintf("series %d is not a series of this work; join a curated series through %s first",
					p.SeriesID, FieldWorkSeriesIDs)}
		}
	}
	return nil
}

func loadWorkSeriesMembers(ctx context.Context, db *gorm.DB, workID int64) ([]any, error) {
	var rows []catmodel.CatalogSeriesMember
	if err := db.WithContext(ctx).Raw(`SELECT sm.series_id, sm.position, sm.kind FROM catalog_series_member sm
		WHERE sm.work_id = ? AND `+NotSuppressedWorkSeriesSQL("sm")+` ORDER BY sm.series_id`,
		workID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]any, 0, len(rows))
	for _, r := range rows {
		out = append(out, map[string]any{
			"series_id": r.SeriesID, "position": int64(r.Position), "kind": int64(r.Kind),
		})
	}
	return out, nil
}

func seriesMemberRowsChanging(column string) func(context.Context, *gorm.DB, int64, any) ([]int64, error) {
	return func(ctx context.Context, tx *gorm.DB, entityID int64, value any) ([]int64, error) {
		placements, err := parseWorkSeriesMembers(value)
		if err != nil || len(placements) == 0 {
			return nil, err
		}
		want := make(map[int64]int16, len(placements))
		ids := make([]int64, 0, len(placements))
		for _, p := range placements {
			if column == "position" {
				want[p.SeriesID] = p.Position
			} else {
				want[p.SeriesID] = p.Kind
			}
			ids = append(ids, p.SeriesID)
		}
		var rows []struct {
			ID       int64 `gorm:"column:id"`
			SeriesID int64 `gorm:"column:series_id"`
			Value    int16 `gorm:"column:value"`
		}
		if err := tx.WithContext(ctx).Raw(`SELECT id, series_id, `+column+` AS value FROM catalog_series_member
			WHERE work_id = ? AND series_id IN ? ORDER BY id`, entityID, ids).Scan(&rows).Error; err != nil {
			return nil, err
		}
		var out []int64
		for _, r := range rows {
			if r.Value != want[r.SeriesID] {
				out = append(out, r.ID)
			}
		}
		return out, nil
	}
}

// SeriesMemberColumnIsHumanSQL is true for a catalog_series_member row whose
// column a person last wrote.
func SeriesMemberColumnIsHumanSQL(alias, column string) string {
	return "COALESCE(" + HumanFieldProvenanceSQL(alias+".field_provenance", column) + ", false)"
}
