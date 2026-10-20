package editspec

import (
	"context"
	"fmt"
	"regexp"

	catmodel "api/internal/platform/catalog/model"
	"api/internal/platform/editing"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	FieldWorkPlatforms      = "catalog.work.platforms"
	FieldWorkPlatformsSuppr = FieldWorkPlatforms + editing.SuppressedFieldSuffix
)

// A work carries at most 10 platform rows in production (census 2026-10-20).
const (
	maxWorkPlatformElements = 64
	maxWorkPlatformSuppress = 30
)

func init() {
	upstreamLists[TypeWork] = append(upstreamLists[TypeWork], upstreamList{
		field: FieldWorkPlatforms, maxSuppressed: maxWorkPlatformSuppress,
		identity: editing.IdentitySpec{Segments: 2, KeyCheck: workPlatformKeyCheck},
	})
}

func workPlatformsFieldSpec() editing.FieldSpec {
	return editing.FieldSpec{
		Key: FieldWorkPlatforms, Kind: editing.KindList, DiffHint: editing.DiffHintItems,
		MaxElements: maxWorkPlatformElements,
		Value:       &editing.ValueSpec{Element: &editing.ElementSpec{Type: "text"}},
		Validate:    validateWorkPlatforms,
		Apply:       applyWorkPlatforms,
	}
}

var rePlatformKey = regexp.MustCompile(`^[a-z0-9]{1,16}$`)

func parseWorkPlatforms(v any) ([]string, error) {
	arr, err := asArrayN(v, "platform keys", maxWorkPlatformElements)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(arr))
	seen := make(map[string]struct{}, len(arr))
	for i, el := range arr {
		s, ok := el.(string)
		if !ok || !rePlatformKey.MatchString(s) {
			return nil, fmt.Errorf("element %d: must be a platform key from the platform vocabulary", i)
		}
		if _, dup := seen[s]; dup {
			return nil, fmt.Errorf("element %d: duplicate platform %q", i, s)
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out, nil
}

func validateWorkPlatforms(v any) error { _, err := parseWorkPlatforms(v); return err }

func applyWorkPlatforms(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	keys, err := parseWorkPlatforms(value)
	if err != nil {
		return fmt.Errorf("editspec: platforms: %w", err)
	}
	if err := assertWorkExists(ctx, tx, entityID); err != nil {
		return err
	}
	if len(keys) > 0 {
		var known []string
		if err := tx.WithContext(ctx).Model(&catmodel.CatalogPlatform{}).
			Where("key IN ? AND NOT is_deprecated", keys).Pluck("key", &known).Error; err != nil {
			return err
		}
		if len(known) != len(keys) {
			have := make(map[string]struct{}, len(known))
			for _, k := range known {
				have[k] = struct{}{}
			}
			for _, k := range keys {
				if _, ok := have[k]; !ok {
					return &editing.ValidationError{Key: FieldWorkPlatforms,
						Reason: fmt.Sprintf("%q is not an active platform", k)}
				}
			}
		}
		var hidden []string
		if err := tx.WithContext(ctx).Raw(`SELECT esr.identity_key FROM edit_suppressed_row esr
			WHERE esr.entity_type = ? AND esr.field_key = ? AND esr.entity_id = ? AND esr.identity_key IN ?`,
			TypeWork, FieldWorkPlatforms, entityID, platformIdentities(keys)).Scan(&hidden).Error; err != nil {
			return err
		}
		if len(hidden) > 0 {
			return &editing.ValidationError{Key: FieldWorkPlatforms,
				Reason: fmt.Sprintf("%s is suppressed on this work; lift the suppression in an edit of its own first", hidden[0])}
		}
	}
	if err := tx.WithContext(ctx).
		Where("work_id = ? AND source_id = ?", entityID, curatedSourceID).
		Delete(&catmodel.CatalogWorkPlatform{}).Error; err != nil {
		return err
	}
	if len(keys) == 0 {
		return nil
	}
	rows := make([]catmodel.CatalogWorkPlatform, 0, len(keys))
	for _, k := range keys {
		rows = append(rows, catmodel.CatalogWorkPlatform{WorkID: entityID, Platform: k, SourceID: curatedSourceID})
	}
	return tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}

func loadWorkPlatforms(ctx context.Context, db *gorm.DB, workID int64) ([]any, error) {
	var keys []string
	if err := db.WithContext(ctx).Model(&catmodel.CatalogWorkPlatform{}).
		Where("work_id = ? AND source_id = ?", workID, curatedSourceID).
		Order("platform").Pluck("platform", &keys).Error; err != nil {
		return nil, err
	}
	out := make([]any, 0, len(keys))
	for _, k := range keys {
		out = append(out, k)
	}
	return out, nil
}

func WorkPlatformIdentity(platform string) string { return "platform:" + platform }

func platformIdentities(keys []string) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, WorkPlatformIdentity(k))
	}
	return out
}

func WorkPlatformIdentitySQL(platformExpr string) string {
	return fmt.Sprintf(`('platform:' || %s)`, platformExpr)
}

func workPlatformKeyCheck(key string) error {
	parts, err := identityParts(key, "platform", 2)
	if err != nil {
		return err
	}
	if !rePlatformKey.MatchString(parts[1]) {
		return fmt.Errorf("%q is not an identity key of this field: %q is not a platform key", key, parts[1])
	}
	return nil
}

// NotSuppressedWorkPlatformSQL hides a platform from every source at once: the
// key names the platform, not the source that reported it.
func NotSuppressedWorkPlatformSQL(workIDExpr, platformExpr string) string {
	return notSuppressedSQL(TypeWork, FieldWorkPlatforms, workIDExpr, WorkPlatformIdentitySQL(platformExpr))
}
