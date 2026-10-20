package editspec

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	catmodel "api/internal/platform/catalog/model"
	"api/internal/platform/editing"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	FieldReleasePlatforms   = "catalog.release.platforms"
	FieldReleaseLanguages   = "catalog.release.languages"
	FieldReleaseFreeware    = "catalog.release.freeware"
	FieldReleaseOfficial    = "catalog.release.official"
	FieldReleaseMinAge      = "catalog.release.minage"
	FieldReleaseLabels      = "catalog.release.labels"
	FieldReleaseLabelsSuppr = FieldReleaseLabels + editing.SuppressedFieldSuffix
)

const (
	extraPlatforms = "platforms"
	extraLanguages = "languages"
	extraFreeware  = "freeware"
	extraOfficial  = "official"
	extraMinAge    = "minage"

	releaseLabelIdentityHead = "label"

	maxReleaseCodes  = 64
	maxReleaseLabels = 50
	maxReleaseMinAge = 18
)

func releaseExtraFieldSpecs() []editing.FieldSpec {
	labels := releaseLabelsFieldSpec()
	return []editing.FieldSpec{
		{
			Key: FieldReleasePlatforms, Kind: editing.KindList, DiffHint: editing.DiffHintItems,
			MaxElements: maxReleaseCodes,
			Value:       &editing.ValueSpec{Vocabulary: "platform", Element: &editing.ElementSpec{Type: "text"}},
			Validate: func(v any) error {
				_, err := parseReleaseCodes(v, validateReleasePlatform)
				return err
			},
			Apply: applyReleasePlatforms,
			Provenance: []editing.ProvenanceTarget{
				{Table: catmodel.CatalogRelease{}.TableName(), Column: "platform"},
				{Table: catmodel.CatalogRelease{}.TableName(), Column: extraPlatforms},
			},
		},
		{
			Key: FieldReleaseLanguages, Kind: editing.KindList, DiffHint: editing.DiffHintItems,
			MaxElements: maxReleaseCodes,
			Value:       &editing.ValueSpec{Vocabulary: "release_lang", Element: &editing.ElementSpec{Type: "text"}},
			Validate: func(v any) error {
				_, err := parseReleaseCodes(v, validateReleaseLang)
				return err
			},
			Apply: func(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
				codes, err := parseReleaseCodes(value, validateReleaseLang)
				if err != nil {
					return fmt.Errorf("editspec: languages: %w", err)
				}
				return setReleaseExtra(ctx, tx, entityID, extraLanguages, codesOrNil(codes))
			},
			Provenance: releaseProvenance(extraLanguages),
		},
		releaseExtraBool(FieldReleaseFreeware, extraFreeware),
		releaseExtraBool(FieldReleaseOfficial, extraOfficial),
		{
			Key: FieldReleaseMinAge, Kind: editing.KindInt, DiffHint: editing.DiffHintInline,
			Value: &editing.ValueSpec{Nullable: true},
			Validate: func(v any) error {
				_, err := parseNullableI16(v, 0, maxReleaseMinAge)
				return err
			},
			Apply: func(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
				n, err := parseNullableI16(value, 0, maxReleaseMinAge)
				if err != nil {
					return fmt.Errorf("editspec: minage: %w", err)
				}
				if n == nil {
					return setReleaseExtra(ctx, tx, entityID, extraMinAge, nil)
				}
				return setReleaseExtra(ctx, tx, entityID, extraMinAge, *n)
			},
			Provenance: releaseProvenance(extraMinAge),
		},
		labels,
		releaseLabelSuppressionSpec(labels),
	}
}

func releaseExtraBool(key, extraKey string) editing.FieldSpec {
	return editing.FieldSpec{
		Key: key, Kind: editing.KindEnum, DiffHint: editing.DiffHintInline,
		Value: &editing.ValueSpec{Nullable: true},
		Validate: func(v any) error {
			if v == nil {
				return nil
			}
			return validateBool(v)
		},
		Apply: func(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
			if value == nil {
				return setReleaseExtra(ctx, tx, entityID, extraKey, nil)
			}
			b, err := asBool(value)
			if err != nil {
				return fmt.Errorf("editspec: %s: %w", extraKey, err)
			}
			return setReleaseExtra(ctx, tx, entityID, extraKey, b)
		},
		Provenance: releaseProvenance(extraKey),
	}
}

func parseReleaseCodes(v any, check func(any) error) ([]string, error) {
	arr, err := asArrayN(v, "codes", maxReleaseCodes)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(arr))
	seen := make(map[string]struct{}, len(arr))
	for i, el := range arr {
		s, ok := el.(string)
		if !ok {
			return nil, fmt.Errorf("element %d: must be a string", i)
		}
		if err := check(s); err != nil {
			return nil, fmt.Errorf("element %d: %w", i, err)
		}
		if _, dup := seen[s]; dup {
			return nil, fmt.Errorf("element %d: duplicate %q", i, s)
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out, nil
}

func codesOrNil(codes []string) any {
	if len(codes) == 0 {
		return nil
	}
	return codes
}

// catalog_release.platform is the head of this list: the importers write
// platform = platforms[0], and the dlsite lane's fill guard and the feed filters
// read the column, so writing only the list would leave a stale head behind.
func applyReleasePlatforms(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	codes, err := parseReleaseCodes(value, validateReleasePlatform)
	if err != nil {
		return fmt.Errorf("editspec: platforms: %w", err)
	}
	if err := setReleaseExtra(ctx, tx, entityID, extraPlatforms, codesOrNil(codes)); err != nil {
		return err
	}
	var head any
	if len(codes) > 0 {
		head = codes[0]
	}
	return tx.WithContext(ctx).Exec(`UPDATE catalog_release SET platform = ? WHERE id = ?`, head, entityID).Error
}

func setReleaseExtra(ctx context.Context, tx *gorm.DB, entityID int64, key string, value any) error {
	var res *gorm.DB
	if value == nil {
		res = tx.WithContext(ctx).Exec(`UPDATE catalog_release SET extra = extra - ? WHERE id = ?`, key, entityID)
	} else {
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		res = tx.WithContext(ctx).Exec(
			`UPDATE catalog_release SET extra = jsonb_set(extra, ARRAY[?::text], ?::jsonb, true) WHERE id = ?`,
			key, string(raw), entityID)
	}
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return editing.ErrEntityNotFound
	}
	return nil
}

type releaseExtras struct {
	Platforms []string `json:"platforms"`
	Languages []string `json:"languages"`
	Freeware  *bool    `json:"freeware"`
	Official  *bool    `json:"official"`
	MinAge    *int16   `json:"minage"`
}

func decodeReleaseExtras(raw datatypes.JSON) releaseExtras {
	var e releaseExtras
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &e)
	}
	return e
}

// Some writers set the platform head without the list (4,791 releases in prod
// on 2026-10-20); reading them as [head] keeps a no-op save from diffing as a
// change.
func releasePlatformsValue(e releaseExtras, head *string) []any {
	codes := e.Platforms
	if len(codes) == 0 && head != nil && *head != "" {
		codes = []string{*head}
	}
	return stringsToAny(codes)
}

func stringsToAny(in []string) []any {
	out := make([]any, 0, len(in))
	for _, s := range in {
		out = append(out, s)
	}
	return out
}

func nullableBool(p *bool) any {
	if p == nil {
		return nil
	}
	return *p
}

func releaseLabelsFieldSpec() editing.FieldSpec {
	return editing.FieldSpec{
		Key: FieldReleaseLabels, Kind: editing.KindList, DiffHint: editing.DiffHintItems,
		MaxElements:   maxReleaseLabels,
		MaxSuppressed: maxReleaseLabels,
		Identity: &editing.IdentitySpec{
			Segments: 3, KeyCheck: releaseLabelKeyCheck,
			Refs: []editing.IdentityRef{{Segment: 2, EntityTag: TagLabel}},
		},
		Value: &editing.ValueSpec{Element: &editing.ElementSpec{
			Type: "object",
			Members: []editing.ElementMember{
				{Key: "label_id", Type: "ref"},
				{Key: "kind", Type: "int", Vocabulary: "attribution_role"},
			},
		}},
		Validate: func(v any) error {
			_, err := parseReleaseLabels(v)
			return err
		},
		Apply: applyReleaseLabels,
	}
}

func parseReleaseLabels(v any) ([]workLabel, error) {
	labels, err := parseLabels(v)
	if err != nil {
		return nil, err
	}
	if len(labels) > maxReleaseLabels {
		return nil, fmt.Errorf("must contain at most %d elements", maxReleaseLabels)
	}
	return labels, nil
}

// ReleaseLabelIdentity is the frozen row identity of catalog.release.labels:
// label:<label_id>:<kind>. The label id follows company merges.
func ReleaseLabelIdentity(labelID int64, kind int16) string {
	return fmt.Sprintf("%s:%d:%d", releaseLabelIdentityHead, labelID, kind)
}

func releaseLabelKeyCheck(key string) error {
	parts := strings.Split(key, ":")
	if len(parts) != 3 || parts[0] != releaseLabelIdentityHead {
		return fmt.Errorf("%q is not an identity key of this field: expected label:<label_id>:<kind>", key)
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || strconv.FormatInt(id, 10) != parts[1] || id <= 0 {
		return fmt.Errorf("%q is not an identity key of this field: %q is not a canonical label id", key, parts[1])
	}
	kind, err := strconv.ParseInt(parts[2], 10, 16)
	if err != nil || strconv.FormatInt(kind, 10) != parts[2] {
		return fmt.Errorf("%q is not an identity key of this field: %q is not a numeric kind", key, parts[2])
	}
	switch int16(kind) {
	case catmodel.WorkLabelKindCircle, catmodel.WorkLabelKindPublisher,
		catmodel.WorkLabelKindDeveloper, catmodel.WorkLabelKindBrand:
		return nil
	}
	return fmt.Errorf("%q is not an identity key of this field: %q is not an attribution role", key, parts[2])
}

func ReleaseLabelIdentitySQL(alias string) string {
	return fmt.Sprintf(`('%s:' || %[2]s.label_id || ':' || %[2]s.kind)`, releaseLabelIdentityHead, alias)
}

// NotSuppressedReleaseLabelSQL is the read-path predicate, correlated on a
// catalog_release_label alias.
func NotSuppressedReleaseLabelSQL(alias string) string {
	return fmt.Sprintf(
		`NOT EXISTS (SELECT 1 FROM edit_suppressed_row esr
			WHERE esr.entity_type = '%s' AND esr.field_key = '%s'
			  AND esr.entity_id = %s.release_id AND esr.identity_key = %s)`,
		TypeRelease, FieldReleaseLabels, alias, ReleaseLabelIdentitySQL(alias))
}

func applyReleaseLabels(ctx context.Context, tx *gorm.DB, releaseID int64, value any) error {
	labels, err := parseReleaseLabels(value)
	if err != nil {
		return fmt.Errorf("editspec: labels: %w", err)
	}
	if err := assertReleaseExists(ctx, tx, releaseID); err != nil {
		return err
	}
	if len(labels) > 0 {
		ids := make([]int64, 0, len(labels))
		keys := make([]string, 0, len(labels))
		pairs := make([][]any, 0, len(labels))
		for _, l := range labels {
			ids = append(ids, l.LabelID)
			keys = append(keys, ReleaseLabelIdentity(l.LabelID, l.Kind))
			pairs = append(pairs, []any{l.LabelID, l.Kind})
		}
		if err := assertEntitiesExist(ctx, tx, &catmodel.CatalogLabel{}, uniqueIDs(ids), FieldReleaseLabels); err != nil {
			return err
		}
		var clash []catmodel.CatalogReleaseLabel
		if err := tx.WithContext(ctx).Select("label_id", "kind").
			Where("release_id = ? AND "+NotCuratedLaneSQL("source_id"), releaseID).
			Where("(label_id, kind) IN ?", pairs).Order("label_id, kind").Find(&clash).Error; err != nil {
			return err
		}
		if len(clash) > 0 {
			return &editing.ValidationError{Key: FieldReleaseLabels,
				Reason: fmt.Sprintf("company %d (kind %d) is already attached upstream; to hide it use %s instead of re-adding it",
					clash[0].LabelID, clash[0].Kind, FieldReleaseLabelsSuppr)}
		}
		var hidden []string
		if err := tx.WithContext(ctx).Model(&editing.SuppressedRow{}).
			Where("entity_type = ? AND entity_id = ? AND field_key = ? AND identity_key IN ?",
				TypeRelease, releaseID, FieldReleaseLabels, keys).
			Order("identity_key").Pluck("identity_key", &hidden).Error; err != nil {
			return err
		}
		if len(hidden) > 0 {
			return &editing.ValidationError{Key: FieldReleaseLabels,
				Reason: fmt.Sprintf("%s is suppressed on this release; lift the suppression in an edit of its own first", hidden[0])}
		}
	}
	if err := tx.WithContext(ctx).
		Where("release_id = ? AND "+CuratedLaneSQL("source_id"), releaseID).
		Delete(&catmodel.CatalogReleaseLabel{}).Error; err != nil {
		return err
	}
	if len(labels) == 0 {
		return nil
	}
	src := curatedSourceID
	rows := make([]catmodel.CatalogReleaseLabel, 0, len(labels))
	for _, l := range labels {
		rows = append(rows, catmodel.CatalogReleaseLabel{ReleaseID: releaseID, LabelID: l.LabelID, Kind: l.Kind, SourceID: &src})
	}
	return tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}

func releaseLabelSuppressionSpec(labels editing.FieldSpec) editing.FieldSpec {
	spec := editing.SuppressedFieldSpec(TypeRelease, labels)
	reconcile := spec.Apply
	spec.Apply = func(ctx context.Context, tx *gorm.DB, releaseID int64, value any) error {
		if err := assertReleaseExists(ctx, tx, releaseID); err != nil {
			return err
		}
		if err := reconcile(ctx, tx, releaseID, value); err != nil {
			return err
		}
		var clash []catmodel.CatalogReleaseLabel
		if err := tx.WithContext(ctx).Raw(`SELECT rl.label_id, rl.kind FROM catalog_release_label rl
			WHERE rl.release_id = ? AND `+CuratedLaneSQL("rl.source_id")+`
			  AND NOT `+NotSuppressedReleaseLabelSQL("rl")+` ORDER BY rl.label_id, rl.kind`,
			releaseID).Scan(&clash).Error; err != nil {
			return err
		}
		if len(clash) > 0 {
			return &editing.ValidationError{Key: FieldReleaseLabelsSuppr,
				Reason: fmt.Sprintf("company %d (kind %d) is a curated attribution; remove it from %s instead of suppressing it",
					clash[0].LabelID, clash[0].Kind, FieldReleaseLabels)}
		}
		return nil
	}
	return spec
}

func assertReleaseExists(ctx context.Context, tx *gorm.DB, releaseID int64) error {
	var n int64
	if err := tx.WithContext(ctx).Unscoped().Model(&catmodel.CatalogRelease{}).
		Where("id = ?", releaseID).Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		return editing.ErrEntityNotFound
	}
	return nil
}

func loadReleaseLabels(ctx context.Context, db *gorm.DB, releaseID int64) ([]any, error) {
	var rows []catmodel.CatalogReleaseLabel
	if err := db.WithContext(ctx).
		Where("release_id = ? AND "+CuratedLaneSQL("source_id"), releaseID).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].LabelID != rows[j].LabelID {
			return rows[i].LabelID < rows[j].LabelID
		}
		return rows[i].Kind < rows[j].Kind
	})
	out := make([]any, 0, len(rows))
	for _, r := range rows {
		out = append(out, map[string]any{"label_id": r.LabelID, "kind": int64(r.Kind)})
	}
	return out, nil
}
