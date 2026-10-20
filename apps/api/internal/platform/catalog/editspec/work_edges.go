package editspec

import (
	"context"
	"errors"
	"fmt"

	catmodel "api/internal/platform/catalog/model"
	"api/internal/platform/editing"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func assertWorkExists(ctx context.Context, tx *gorm.DB, entityID int64) error {
	var w catmodel.CatalogWork
	err := tx.WithContext(ctx).Select("id").First(&w, entityID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return editing.ErrEntityNotFound
	}
	return err
}

func parseIDList(v any, what string) ([]int64, error) {
	arr, err := asArray(v, what)
	if err != nil {
		return nil, err
	}
	out := make([]int64, 0, len(arr))
	seen := make(map[int64]struct{}, len(arr))
	for i, el := range arr {
		var id int64
		switch n := el.(type) {
		case float64:
			if n != float64(int64(n)) {
				return nil, fmt.Errorf("element %d: must be an integer id", i)
			}
			id = int64(n)
		case int64:
			id = n
		default:
			return nil, fmt.Errorf("element %d: must be an integer id", i)
		}
		if id <= 0 {
			return nil, fmt.Errorf("element %d: id must be positive", i)
		}
		if _, dup := seen[id]; dup {
			return nil, fmt.Errorf("element %d: duplicate id %d", i, id)
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, nil
}

func validateTagIDs(v any) error    { _, err := parseIDList(v, "tag ids"); return err }
func validateEngineIDs(v any) error { _, err := parseIDList(v, "engine ids"); return err }
func validateSeriesIDs(v any) error { _, err := parseIDList(v, "series ids"); return err }

func applyTagIDs(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	ids, err := parseIDList(value, "tag ids")
	if err != nil {
		return fmt.Errorf("editspec: tag_ids: %w", err)
	}
	if err := assertWorkExists(ctx, tx, entityID); err != nil {
		return err
	}
	var tags []catmodel.CatalogTag
	if len(ids) > 0 {
		if err := tx.WithContext(ctx).Select("id", "name", "sexual").
			Where("id IN ?", ids).Find(&tags).Error; err != nil {
			return err
		}
		if len(tags) != len(ids) {
			return &editing.ValidationError{
				Key:    FieldWorkTagIDs,
				Reason: fmt.Sprintf("%d of %d ids do not exist", len(ids)-len(tags), len(ids)),
			}
		}
	}
	// A kept tag keeps its row: catalog.work.tag_spoilers stamps the spoiler on it.
	leaving := `DELETE FROM catalog_work_tag wt WHERE wt.work_id = ? AND wt.source_id = ?`
	args := []any{entityID, curatedSourceID}
	if len(ids) > 0 {
		leaving += ` AND NOT EXISTS (SELECT 1 FROM catalog_tag_source_map m
			WHERE m.source_id = wt.source_id AND m.source_name = wt.name AND m.tag_id IN ?)`
		args = append(args, ids)
	}
	if err := tx.WithContext(ctx).Exec(leaving, args...).Error; err != nil {
		return err
	}
	if len(tags) == 0 {
		return nil
	}
	var kept []int64
	if err := tx.WithContext(ctx).Raw(`SELECT DISTINCT m.tag_id FROM catalog_work_tag wt
		JOIN catalog_tag_source_map m ON m.source_id = wt.source_id AND m.source_name = wt.name
		WHERE wt.work_id = ? AND wt.source_id = ?`, entityID, curatedSourceID).Scan(&kept).Error; err != nil {
		return err
	}
	keptSet := make(map[int64]struct{}, len(kept))
	for _, id := range kept {
		keptSet[id] = struct{}{}
	}
	rows := make([]catmodel.CatalogWorkTag, 0, len(tags))
	maps := make([]catmodel.CatalogTagSourceMap, 0, len(tags))
	for _, t := range tags {
		if _, ok := keptSet[t.ID]; ok {
			continue
		}
		rows = append(rows, catmodel.CatalogWorkTag{
			WorkID: entityID, Name: t.Name, SourceID: curatedSourceID,
			Sexual: t.Sexual,
		})
		maps = append(maps, catmodel.CatalogTagSourceMap{
			SourceID: curatedSourceID, SourceName: t.Name, TagID: t.ID,
		})
	}
	// Every tag edge reaches its canonical through catalog_tag_source_map, so a
	// canonical whose name never was a curated SOURCE name has no map row: wave
	// 208 found 534 of them, and a tag the editor assigned from that set landed
	// in catalog_work_tag invisible to the tag page, the facets and the snapshot
	// below. The identity row is what keeps the map the single truth.
	if len(rows) > 0 {
		if err := tx.WithContext(ctx).Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "source_id"}, {Name: "source_name"}},
			DoNothing: true,
		}).Create(&maps).Error; err != nil {
			return err
		}
		if err := tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error; err != nil {
			return err
		}
	}
	return rejectSuppressedCuratedTags(ctx, tx, entityID, FieldWorkTagIDs)
}

// Resolving by name instead of through the map dropped every curated row whose
// source name the canonicalization renamed (wave 208: 38 names, 1,205 rows) —
// invisible in the editor, and deleted by the full-replace write above on the
// next save.
func loadTagIDs(ctx context.Context, db *gorm.DB, workID int64) ([]any, error) {
	var ids []int64
	if err := db.WithContext(ctx).Raw(`
		SELECT DISTINCT m.tag_id FROM catalog_work_tag wt
		JOIN catalog_tag_source_map m ON m.source_id = wt.source_id AND m.source_name = wt.name
		WHERE wt.work_id = ? AND wt.source_id = ?
		ORDER BY m.tag_id`, workID, curatedSourceID).Scan(&ids).Error; err != nil {
		return nil, err
	}
	return int64sToAny(ids), nil
}

type workLabel struct {
	LabelID int64
	Kind    int16
}

func parseLabels(v any) ([]workLabel, error) {
	arr, err := asArray(v, "label objects")
	if err != nil {
		return nil, err
	}
	out := make([]workLabel, 0, len(arr))
	seen := make(map[[2]int64]struct{}, len(arr))
	for i, el := range arr {
		obj, err := asObject(el, i, "label_id", "kind")
		if err != nil {
			return nil, err
		}
		labelID, err := objInt(obj, "label_id", i, true)
		if err != nil {
			return nil, err
		}
		if labelID <= 0 {
			return nil, fmt.Errorf("element %d: label_id must be positive", i)
		}
		kind, err := objInt(obj, "kind", i, true)
		if err != nil {
			return nil, err
		}
		switch int16(kind) {
		case catmodel.WorkLabelKindCircle, catmodel.WorkLabelKindPublisher,
			catmodel.WorkLabelKindDeveloper, catmodel.WorkLabelKindBrand:
		default:
			return nil, fmt.Errorf("element %d: kind must be 0 (circle), 1 (publisher), 2 (developer) or 3 (brand)", i)
		}
		key := [2]int64{labelID, kind}
		if _, dup := seen[key]; dup {
			return nil, fmt.Errorf("element %d: duplicate (label_id, kind)", i)
		}
		seen[key] = struct{}{}
		out = append(out, workLabel{LabelID: labelID, Kind: int16(kind)})
	}
	return out, nil
}

func validateLabels(v any) error { _, err := parseLabels(v); return err }

func applyLabels(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	labels, err := parseLabels(value)
	if err != nil {
		return fmt.Errorf("editspec: labels: %w", err)
	}
	if err := assertWorkExists(ctx, tx, entityID); err != nil {
		return err
	}
	if len(labels) > 0 {
		ids := make([]int64, 0, len(labels))
		for _, l := range labels {
			ids = append(ids, l.LabelID)
		}
		if err := assertEntitiesExist(ctx, tx, &catmodel.CatalogLabel{}, ids, FieldWorkLabels); err != nil {
			return err
		}
	}
	if err := rejectUpstreamLabelCollisions(ctx, tx, entityID, labels); err != nil {
		return err
	}
	if err := tx.WithContext(ctx).
		Where("work_id = ? AND source_id = ?", entityID, curatedSourceID).
		Delete(&catmodel.CatalogWorkLabel{}).Error; err != nil {
		return err
	}
	if len(labels) == 0 {
		return nil
	}
	src := curatedSourceID
	rows := make([]catmodel.CatalogWorkLabel, 0, len(labels))
	for _, l := range labels {
		rows = append(rows, catmodel.CatalogWorkLabel{
			WorkID: entityID, LabelID: l.LabelID, Kind: l.Kind, SourceID: &src,
		})
	}
	return tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}

// rejectUpstreamLabelCollisions makes an unwinnable insert an explicit 422.
// catalog_work_label's identity is (work_id, label_id, kind) and does not carry
// source_id (NULL is a machine row, which is why the predicate is IS DISTINCT FROM).
func rejectUpstreamLabelCollisions(ctx context.Context, tx *gorm.DB, entityID int64, labels []workLabel) error {
	if len(labels) == 0 {
		return nil
	}
	pairs := make([][]any, 0, len(labels))
	for _, l := range labels {
		pairs = append(pairs, []any{l.LabelID, l.Kind})
	}
	var clash []catmodel.CatalogWorkLabel
	if err := tx.WithContext(ctx).
		Select("label_id", "kind").
		Where("work_id = ? AND source_id IS DISTINCT FROM ?", entityID, curatedSourceID).
		Where("(label_id, kind) IN ?", pairs).
		Order("label_id, kind").
		Find(&clash).Error; err != nil {
		return err
	}
	if len(clash) == 0 {
		return nil
	}
	return &editing.ValidationError{
		Key: FieldWorkLabels,
		Reason: fmt.Sprintf("label %d (kind %d) is already attached upstream; "+
			"upstream label edges cannot be re-added through the editor yet",
			clash[0].LabelID, clash[0].Kind),
	}
}

func loadLabels(ctx context.Context, db *gorm.DB, workID int64) ([]any, error) {
	var rows []catmodel.CatalogWorkLabel
	if err := db.WithContext(ctx).
		Where("work_id = ? AND source_id = ?", workID, curatedSourceID).
		Order("label_id, kind").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]any, 0, len(rows))
	for _, r := range rows {
		out = append(out, map[string]any{"label_id": r.LabelID, "kind": int64(r.Kind)})
	}
	return out, nil
}

func applyEngineIDs(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	ids, err := parseIDList(value, "engine ids")
	if err != nil {
		return fmt.Errorf("editspec: engine_ids: %w", err)
	}
	if err := assertWorkExists(ctx, tx, entityID); err != nil {
		return err
	}
	if err := assertEntitiesExist(ctx, tx, &catmodel.CatalogEngine{}, ids, FieldWorkEngineIDs); err != nil {
		return err
	}
	if err := tx.WithContext(ctx).
		Where("work_id = ? AND source_id = ?", entityID, curatedSourceID).
		Delete(&catmodel.CatalogWorkEngine{}).Error; err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	rows := make([]catmodel.CatalogWorkEngine, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, catmodel.CatalogWorkEngine{
			WorkID: entityID, EngineID: id, SourceID: curatedSourceID,
		})
	}
	return tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}

func loadEngineIDs(ctx context.Context, db *gorm.DB, workID int64) ([]any, error) {
	var ids []int64
	if err := db.WithContext(ctx).Model(&catmodel.CatalogWorkEngine{}).
		Where("work_id = ? AND source_id = ?", workID, curatedSourceID).
		Order("engine_id").Pluck("engine_id", &ids).Error; err != nil {
		return nil, err
	}
	return int64sToAny(ids), nil
}

func applySeriesIDs(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	ids, err := parseIDList(value, "series ids")
	if err != nil {
		return fmt.Errorf("editspec: series_ids: %w", err)
	}
	if err := assertWorkExists(ctx, tx, entityID); err != nil {
		return err
	}
	if len(ids) > 0 {
		var n int64
		if err := tx.WithContext(ctx).Model(&catmodel.CatalogSeries{}).
			Where("id IN ? AND source_id = ?", ids, curatedSourceID).Count(&n).Error; err != nil {
			return err
		}
		if int(n) != len(ids) {
			return &editing.ValidationError{
				Key: FieldWorkSeriesIDs,
				Reason: "every id must be an existing CURATED series " +
					"(an upstream series' membership is reconciled by its importer and cannot be edited here)",
			}
		}
	}
	// Only the memberships that leave are deleted: a kept one carries the
	// position and kind catalog.work.series_members may have set on it.
	leaving := `DELETE FROM catalog_series_member m
		USING catalog_series s
		WHERE m.series_id = s.id AND m.work_id = ? AND s.source_id = ?`
	args := []any{entityID, curatedSourceID}
	if len(ids) > 0 {
		leaving += ` AND m.series_id NOT IN ?`
		args = append(args, ids)
	}
	if err := tx.WithContext(ctx).Exec(leaving, args...).Error; err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	rows := make([]catmodel.CatalogSeriesMember, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, catmodel.CatalogSeriesMember{SeriesID: id, WorkID: entityID})
	}
	return tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}

func loadSeriesIDs(ctx context.Context, db *gorm.DB, workID int64) ([]any, error) {
	var ids []int64
	if err := db.WithContext(ctx).Raw(`
		SELECT m.series_id FROM catalog_series_member m
		JOIN catalog_series s ON s.id = m.series_id
		WHERE m.work_id = ? AND s.source_id = ?
		ORDER BY m.series_id`, workID, curatedSourceID).Scan(&ids).Error; err != nil {
		return nil, err
	}
	return int64sToAny(ids), nil
}

func assertEntitiesExist(ctx context.Context, tx *gorm.DB, model any, ids []int64, field string) error {
	ids = uniqueIDs(ids)
	if len(ids) == 0 {
		return nil
	}
	var n int64
	if err := tx.WithContext(ctx).Model(model).Where("id IN ?", ids).Count(&n).Error; err != nil {
		return err
	}
	if int(n) != len(ids) {
		return &editing.ValidationError{
			Key:    field,
			Reason: fmt.Sprintf("%d of %d ids do not exist", len(ids)-int(n), len(ids)),
		}
	}
	return nil
}

func int64sToAny(ids []int64) []any {
	out := make([]any, 0, len(ids))
	for _, id := range ids {
		out = append(out, id)
	}
	return out
}
