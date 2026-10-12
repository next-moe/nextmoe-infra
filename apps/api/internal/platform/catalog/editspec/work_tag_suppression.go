package editspec

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"api/internal/platform/editing"

	"gorm.io/gorm"
)

func WorkTagIdentity(tagID int64) string {
	return "tag:" + strconv.FormatInt(tagID, 10)
}

func WorkTagIdentitySQL(tagIDExpr string) string {
	return fmt.Sprintf(`('tag:' || %s)`, tagIDExpr)
}

func workTagKeyCheck(key string) error {
	parts := strings.Split(key, ":")
	if len(parts) != 2 || parts[0] != "tag" {
		return fmt.Errorf("%q is not an identity key of this field: expected tag:<tag_id>", key)
	}
	n, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || strconv.FormatInt(n, 10) != parts[1] {
		return fmt.Errorf("%q is not an identity key of this field: %q is not a canonical id", key, parts[1])
	}
	if n <= 0 {
		return fmt.Errorf("%q is not an identity key of this field: tag_id must be positive", key)
	}
	return nil
}

func workTagSuppressedSQL(workTag, sourceMap string) string {
	return fmt.Sprintf(`EXISTS (SELECT 1 FROM edit_suppressed_row esr
		WHERE esr.entity_type = '%s' AND esr.field_key = '%s'
		  AND esr.entity_id = %s.work_id AND esr.identity_key = %s)`,
		TypeWork, FieldWorkTagIDs, workTag, WorkTagIdentitySQL(sourceMap+".tag_id"))
}

// PurgeSuppressedWorkTagsSQL takes one parameter, the work id.
var PurgeSuppressedWorkTagsSQL = `DELETE FROM catalog_work_tag wt
	USING catalog_tag_source_map m
	WHERE wt.work_id = ? AND m.source_id = wt.source_id AND m.source_name = wt.name
	  AND ` + workTagSuppressedSQL("wt", "m")

type SuppressedWorkTags map[int64]map[string]struct{}

func (s SuppressedWorkTags) Has(workID int64, name string) bool {
	_, ok := s[workID][name]
	return ok
}

// LoadSuppressedWorkTags returns, per work, the names of one source that
// resolve to a tag suppressed on that work. Tag suppression is enforced on
// write only — no read face filters catalog_work_tag — so every writer of that
// table must skip these pairs.
func LoadSuppressedWorkTags(ctx context.Context, db *gorm.DB, sourceID int16) (SuppressedWorkTags, error) {
	var rows []struct {
		WorkID int64  `gorm:"column:work_id"`
		Name   string `gorm:"column:name"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT esr.entity_id AS work_id, m.source_name AS name
		FROM edit_suppressed_row esr
		JOIN catalog_tag_source_map m
		  ON m.source_id = ? AND esr.identity_key = `+WorkTagIdentitySQL("m.tag_id")+`
		WHERE esr.entity_type = ? AND esr.field_key = ?`,
		sourceID, TypeWork, FieldWorkTagIDs).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load suppressed work tags: %w", err)
	}
	out := make(SuppressedWorkTags, len(rows))
	for _, r := range rows {
		names, ok := out[r.WorkID]
		if !ok {
			names = make(map[string]struct{}, 1)
			out[r.WorkID] = names
		}
		names[r.Name] = struct{}{}
	}
	return out, nil
}

func workTagSuppressionSpec(tags editing.FieldSpec) editing.FieldSpec {
	spec := editing.SuppressedFieldSpec(TypeWork, tags)
	reconcile := spec.Apply
	spec.Apply = func(ctx context.Context, tx *gorm.DB, workID int64, value any) error {
		if err := reconcile(ctx, tx, workID, value); err != nil {
			return err
		}
		if err := rejectSuppressedCuratedTags(ctx, tx, workID, FieldWorkTagIDsSuppr); err != nil {
			return err
		}
		return tx.WithContext(ctx).Exec(PurgeSuppressedWorkTagsSQL, workID).Error
	}
	return spec
}

// rejectSuppressedCuratedTags makes a curated tag edge and a suppression of the
// same tag an explicit 422 from whichever field created the contradiction.
// Dropping the edge instead would record a revision whose tag_ids snapshot
// holds a tag the table does not.
func rejectSuppressedCuratedTags(ctx context.Context, tx *gorm.DB, workID int64, field string) error {
	var clash []struct {
		ID   int64  `gorm:"column:id"`
		Name string `gorm:"column:name"`
	}
	if err := tx.WithContext(ctx).Raw(`
		SELECT DISTINCT t.id, t.name FROM catalog_work_tag wt
		JOIN catalog_tag_source_map m ON m.source_id = wt.source_id AND m.source_name = wt.name
		JOIN catalog_tag t ON t.id = m.tag_id
		WHERE wt.work_id = ? AND wt.source_id = ? AND `+workTagSuppressedSQL("wt", "m")+`
		ORDER BY t.id`, workID, curatedSourceID).Scan(&clash).Error; err != nil {
		return err
	}
	if len(clash) == 0 {
		return nil
	}
	reason := fmt.Sprintf("tag %d (%q) is suppressed on this work; lift it from %s in an edit of its own before assigning it",
		clash[0].ID, clash[0].Name, FieldWorkTagIDsSuppr)
	if field == FieldWorkTagIDsSuppr {
		reason = fmt.Sprintf("tag %d (%q) is assigned in this work's %s; remove it there instead of suppressing it",
			clash[0].ID, clash[0].Name, FieldWorkTagIDs)
	}
	return &editing.ValidationError{Key: field, Reason: reason}
}

func loadSuppressedTagIDs(ctx context.Context, db *gorm.DB, workID int64) ([]any, error) {
	return editing.LoadSuppressedValue(ctx, db, TypeWork, workID, FieldWorkTagIDs)
}
