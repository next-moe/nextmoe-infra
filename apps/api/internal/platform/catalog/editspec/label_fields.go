package editspec

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	catmodel "api/internal/platform/catalog/model"
	"api/internal/platform/editing"
	"api/internal/platform/textnorm"

	"gorm.io/gorm"
)

const (
	FieldLabelLatin           = "catalog.label.latin"
	FieldLabelLang            = "catalog.label.lang"
	FieldLabelKind            = "catalog.label.kind"
	FieldLabelNote            = "catalog.label.note"
	FieldLabelLogo            = "catalog.label.logo_hash"
	FieldLabelAliases         = "catalog.label.aliases"
	FieldLabelAliasesSuppr    = FieldLabelAliases + editing.SuppressedFieldSuffix
	FieldLabelRelations       = "catalog.label.relations"
	FieldLabelRelationsSuppr  = FieldLabelRelations + editing.SuppressedFieldSuffix
	maxLabelRelations         = 200
	labelRelationIdentityHead = "relation"
)

var labelKindsAllowed = []int16{
	catmodel.LabelKindGameBrand, catmodel.LabelKindBunko, catmodel.LabelKindPublisher,
	catmodel.LabelKindAnimeStudio, catmodel.LabelKindDoujinCircle, catmodel.LabelKindGroup,
}

func labelProvenance(column string) []editing.ProvenanceTarget {
	return []editing.ProvenanceTarget{{Table: catmodel.CatalogLabel{}.TableName(), Column: column}}
}

// applyLabelColumn writes through a map for the reason applyCharacterColumn
// does: a single-column Update drops a nil value.
func applyLabelColumn(column string, conv func(any) (any, error)) editing.ApplyFunc {
	return func(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
		v, err := conv(value)
		if err != nil {
			return fmt.Errorf("editspec: %s: %w", column, err)
		}
		res := tx.WithContext(ctx).Model(&catmodel.CatalogLabel{}).
			Where("id = ?", entityID).Updates(map[string]any{column: v})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return editing.ErrEntityNotFound
		}
		return nil
	}
}

func labelScalarFields() []editing.FieldSpec {
	lang := charLang(FieldLabelLang, "lang")
	lang.Apply = applyLabelColumn("lang", asString)
	lang.Provenance = labelProvenance("lang")
	lang.Value = &editing.ValueSpec{Vocabulary: "olang"}

	return []editing.FieldSpec{
		{
			Key: FieldLabelLatin, Kind: editing.KindText, DiffHint: editing.DiffHintInline,
			Value: &editing.ValueSpec{Nullable: true},
			Validate: func(v any) error {
				if v == nil {
					return nil
				}
				return validateBoundedText(v, maxNameRunes, true)
			},
			Apply: applyLabelColumn("latin", func(v any) (any, error) {
				if v == nil {
					return nil, nil
				}
				return asString(v)
			}),
			Provenance: labelProvenance("latin"),
		},
		lang,
		{
			Key: FieldLabelKind, Kind: editing.KindEnum, DiffHint: editing.DiffHintInline,
			Value: &editing.ValueSpec{Vocabulary: "company_kind", Coded: true},
			Validate: func(v any) error {
				_, err := parseLabelKind(v)
				return err
			},
			Apply: applyLabelColumn("kind", func(v any) (any, error) {
				return parseLabelKind(v)
			}),
			Provenance: labelProvenance("kind"),
		},
		{
			Key: FieldLabelNote, Kind: editing.KindText, DiffHint: editing.DiffHintLines,
			Validate:   validateIntroText,
			Apply:      applyLabelColumn("note", asString),
			Provenance: labelProvenance("note"),
		},
		{
			Key: FieldLabelLogo, Kind: editing.KindImageHash, DiffHint: editing.DiffHintImage,
			Value: &editing.ValueSpec{Nullable: true},
			Validate: func(v any) error {
				_, err := parseNullableHash(v)
				return err
			},
			Apply: applyLabelColumn("logo_hash", func(v any) (any, error) {
				hash, err := parseNullableHash(v)
				if err != nil || hash == nil {
					return "", err
				}
				return *hash, nil
			}),
			Provenance: labelProvenance("logo_hash"),
		},
	}
}

func parseLabelKind(v any) (int16, error) {
	n, err := parseEnumI16(v, labelKindsAllowed)
	if err != nil {
		return 0, err
	}
	if n == nil {
		return 0, fmt.Errorf("must be one of %v", labelKindsAllowed)
	}
	return *n, nil
}

func labelAliasesFieldSpec() editing.FieldSpec {
	return editing.FieldSpec{
		Key: FieldLabelAliases, Kind: editing.KindList, DiffHint: editing.DiffHintItems,
		Identity: &editing.IdentitySpec{
			Segments: 4, TrailingText: true, KeyCheck: kindLangTextKeyCheck("alias"),
		},
		Value: &editing.ValueSpec{Element: &editing.ElementSpec{
			Type: "object",
			Members: []editing.ElementMember{
				{Key: "name", Type: "text"},
				{Key: "lang", Type: "enum", Vocabulary: "olang"},
				{Key: "kind", Type: "int", Vocabulary: "alias_kind"},
				{Key: "latin", Type: "text", Nullable: true},
				{Key: "primary", Type: "bool", Nullable: true},
			},
		}},
		Validate: validateCharacterAliases,
		Apply:    applyLabelAliases,
	}
}

func applyLabelAliases(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	aliases, err := parseEntityAliases(value)
	if err != nil {
		return fmt.Errorf("editspec: aliases: %w", err)
	}
	if err := firstEntity(ctx, tx, &catmodel.CatalogLabel{}, entityID, "id"); err != nil {
		return err
	}
	if len(aliases) > 0 {
		pairs := make([][]any, 0, len(aliases))
		for _, a := range aliases {
			pairs = append(pairs, []any{a.Name, a.Lang})
		}
		var clash []catmodel.CatalogLabelAlias
		if err := tx.WithContext(ctx).Select("name", "lang").
			Where("label_id = ? AND source_id IS DISTINCT FROM ?", entityID, curatedSourceID).
			Where("(name, lang) IN ?", pairs).Order("lang, name").Find(&clash).Error; err != nil {
			return err
		}
		if len(clash) > 0 {
			return &editing.ValidationError{Key: FieldLabelAliases,
				Reason: fmt.Sprintf("%q (%s) is already provided upstream for this company; to hide it use %s instead of re-adding it",
					clash[0].Name, clash[0].Lang, FieldLabelAliasesSuppr)}
		}
	}
	if err := tx.WithContext(ctx).
		Where("label_id = ? AND source_id = ? AND kind <> ?", entityID, curatedSourceID, catmodel.AliasKindSearchHint).
		Delete(&catmodel.CatalogLabelAlias{}).Error; err != nil {
		return err
	}
	if len(aliases) == 0 {
		return nil
	}
	source := curatedSourceID
	rows := make([]catmodel.CatalogLabelAlias, 0, len(aliases))
	for _, a := range aliases {
		row := catmodel.CatalogLabelAlias{
			LabelID: entityID, Name: a.Name, Lang: a.Lang, Kind: int16(a.Kind),
			IsPrimaryForLocale: a.Primary, SourceID: &source, Provenance: catmodel.AliasProvenanceSource,
		}
		if a.Latin != "" {
			latin := a.Latin
			row.Latin = &latin
		}
		rows = append(rows, row)
	}
	return tx.WithContext(ctx).Create(&rows).Error
}

// LabelAliasIdentity is the frozen row identity of catalog.label.aliases; it
// has the shape of CharacterAliasIdentity for the same reasons.
func LabelAliasIdentity(kind int16, lang, name string) string {
	return fmt.Sprintf("alias:%d:%s:%s", kind, lang, textnorm.Clean(name))
}

func LabelAliasIdentitySQL(alias string) string {
	return fmt.Sprintf(`('alias:' || %[1]s.kind || ':' || %[1]s.lang || ':' || %[2]s)`,
		alias, textnorm.CleanSQL(alias+".name"))
}

// NotSuppressedLabelAliasSQL is the read-path predicate, correlated on a
// catalog_label_alias alias.
func NotSuppressedLabelAliasSQL(alias string) string {
	return fmt.Sprintf(
		`NOT EXISTS (SELECT 1 FROM edit_suppressed_row esr
			WHERE esr.entity_type = '%s' AND esr.field_key = '%s'
			  AND esr.entity_id = %s.label_id AND esr.identity_key = %s)`,
		TypeLabel, FieldLabelAliases, alias, LabelAliasIdentitySQL(alias))
}

func loadCuratedLabelAliases(ctx context.Context, db *gorm.DB, labelID int64) ([]any, error) {
	var rows []catmodel.CatalogLabelAlias
	if err := db.WithContext(ctx).
		Where("label_id = ? AND source_id = ? AND kind <> ?", labelID, curatedSourceID, catmodel.AliasKindSearchHint).
		Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]any, 0, len(rows))
	for _, r := range rows {
		el := map[string]any{"name": r.Name, "lang": r.Lang, "kind": int64(r.Kind)}
		if r.Latin != nil && *r.Latin != "" {
			el["latin"] = *r.Latin
		}
		if r.IsPrimaryForLocale {
			el["primary"] = true
		}
		out = append(out, el)
	}
	return out, nil
}

func LabelRelationIdentity(relation int16, otherLabelID int64) string {
	return fmt.Sprintf("%s:%d:%d", labelRelationIdentityHead, relation, otherLabelID)
}

// InverseLabelRelation is the code the mirrored edge carries (see the
// LabelRelation pairs in the model).
func InverseLabelRelation(r int16) int16 {
	if r%2 == 1 {
		return r + 1
	}
	return r - 1
}

func validLabelRelation(r int64) bool {
	if r <= 0 || r > 32767 {
		return false
	}
	_, ok := catmodel.LabelRelationKey[int16(r)]
	return ok
}

func labelRelationKeyCheck(key string) error {
	parts := strings.Split(key, ":")
	if len(parts) != 3 || parts[0] != labelRelationIdentityHead {
		return fmt.Errorf("%q is not an identity key of this field: expected relation:<relation>:<label_id>", key)
	}
	r, err := strconv.ParseInt(parts[1], 10, 16)
	if err != nil || strconv.FormatInt(r, 10) != parts[1] || !validLabelRelation(r) {
		return fmt.Errorf("%q is not an identity key of this field: %q is not a relation code", key, parts[1])
	}
	id, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || strconv.FormatInt(id, 10) != parts[2] || id <= 0 {
		return fmt.Errorf("%q is not an identity key of this field: %q is not a canonical label id", key, parts[2])
	}
	return nil
}

func labelRelationSuppressedSQL(labelExpr, otherExpr, relationExpr string) string {
	return fmt.Sprintf(`EXISTS (SELECT 1 FROM edit_suppressed_row esr
		WHERE esr.entity_type = '%s' AND esr.field_key = '%s'
		  AND esr.entity_id = %s AND esr.identity_key = ('%s:' || %s || ':' || %s))`,
		TypeLabel, FieldLabelRelations, labelExpr, labelRelationIdentityHead, relationExpr, otherExpr)
}

const inverseRelationSQL = `(CASE WHEN r.relation % 2 = 1 THEN r.relation + 1 ELSE r.relation - 1 END)`

// LabelRelationEdgeSuppressedSQL is true for a catalog_label_relation row "r"
// that either endpoint suppressed. The graph is stored mirrored, so a fact a
// label hides appears once under its own id and once, inverted, under the other
// label's; both rows must go and both must stay out.
var LabelRelationEdgeSuppressedSQL = `(` +
	labelRelationSuppressedSQL("r.label_id", "r.other_label_id", "r.relation") + ` OR ` +
	labelRelationSuppressedSQL("r.other_label_id", "r.label_id", inverseRelationSQL) + `)`

// PurgeSuppressedLabelRelationsSQL takes the label id twice.
var PurgeSuppressedLabelRelationsSQL = `DELETE FROM catalog_label_relation r
	WHERE ` + NotCuratedLaneSQL("r.source_id") + ` AND (r.label_id = ? OR r.other_label_id = ?)
	  AND ` + LabelRelationEdgeSuppressedSQL

type labelRelation struct {
	Other    int64
	Relation int16
}

func parseLabelRelations(v any) ([]labelRelation, error) {
	arr, err := asArrayN(v, "relation objects", maxLabelRelations)
	if err != nil {
		return nil, err
	}
	out := make([]labelRelation, 0, len(arr))
	seen := make(map[labelRelation]struct{}, len(arr))
	for i, el := range arr {
		obj, err := asObject(el, i, "label_id", "relation")
		if err != nil {
			return nil, err
		}
		other, err := objInt(obj, "label_id", i, true)
		if err != nil {
			return nil, err
		}
		if other <= 0 {
			return nil, fmt.Errorf("element %d: label_id must be positive", i)
		}
		rel, err := objInt(obj, "relation", i, true)
		if err != nil {
			return nil, err
		}
		if !validLabelRelation(rel) {
			return nil, fmt.Errorf("element %d: relation %d is not a company relation code", i, rel)
		}
		r := labelRelation{Other: other, Relation: int16(rel)}
		if _, dup := seen[r]; dup {
			return nil, fmt.Errorf("element %d: duplicate (label_id, relation)", i)
		}
		seen[r] = struct{}{}
		out = append(out, r)
	}
	return out, nil
}

func validateLabelRelations(v any) error {
	_, err := parseLabelRelations(v)
	return err
}

func labelRelationsFieldSpec() editing.FieldSpec {
	return editing.FieldSpec{
		Key: FieldLabelRelations, Kind: editing.KindList, DiffHint: editing.DiffHintItems,
		MaxElements:   maxLabelRelations,
		MaxSuppressed: maxLabelRelations,
		Identity: &editing.IdentitySpec{
			Segments: 3, KeyCheck: labelRelationKeyCheck,
			Refs: []editing.IdentityRef{{Segment: 3, EntityTag: TagLabel}},
		},
		Value: &editing.ValueSpec{Element: &editing.ElementSpec{
			Type: "object",
			Members: []editing.ElementMember{
				{Key: "label_id", Type: "ref"},
				{Key: "relation", Type: "int", Vocabulary: "company_relation", Base: 1},
			},
		}},
		Validate: validateLabelRelations,
		Apply:    applyLabelRelations,
	}
}

// applyLabelRelations edits facts, and a fact is a mirrored PAIR of rows: the
// edge under this label and its inverse under the other one. Writing or
// dropping only one half would leave the other label's graph saying something
// this one no longer does, so every change here touches both rows, and the
// other label's snapshot reflects it the same way a shared roster row does.
func applyLabelRelations(ctx context.Context, tx *gorm.DB, labelID int64, value any) error {
	want, err := parseLabelRelations(value)
	if err != nil {
		return fmt.Errorf("editspec: relations: %w", err)
	}
	if err := firstEntity(ctx, tx, &catmodel.CatalogLabel{}, labelID, "id"); err != nil {
		return err
	}
	others := make([]int64, 0, len(want))
	for _, r := range want {
		if r.Other == labelID {
			return &editing.ValidationError{Key: FieldLabelRelations, Reason: "a company cannot be related to itself"}
		}
		others = append(others, r.Other)
	}
	if err := assertEntitiesExist(ctx, tx, &catmodel.CatalogLabel{}, uniqueIDs(others), FieldLabelRelations); err != nil {
		return err
	}
	for _, r := range want {
		var upstream int64
		if err := tx.WithContext(ctx).Model(&catmodel.CatalogLabelRelation{}).
			Where("label_id = ? AND other_label_id = ? AND relation = ? AND "+NotCuratedLaneSQL("source_id"),
				labelID, r.Other, r.Relation).Count(&upstream).Error; err != nil {
			return err
		}
		if upstream > 0 {
			return &editing.ValidationError{Key: FieldLabelRelations,
				Reason: fmt.Sprintf("relation %d to company %d is already provided upstream; to hide it use %s instead of re-adding it",
					r.Relation, r.Other, FieldLabelRelationsSuppr)}
		}
		var suppressed int64
		if err := tx.WithContext(ctx).Raw(`SELECT count(*) FROM (SELECT ?::bigint AS label_id, ?::bigint AS other_label_id, ?::smallint AS relation) r
			WHERE `+LabelRelationEdgeSuppressedSQL, labelID, r.Other, r.Relation).Scan(&suppressed).Error; err != nil {
			return err
		}
		if suppressed > 0 {
			return &editing.ValidationError{Key: FieldLabelRelations,
				Reason: fmt.Sprintf("relation %d to company %d is suppressed on one of the two companies; lift the suppression in an edit of its own first",
					r.Relation, r.Other)}
		}
	}
	var have []catmodel.CatalogLabelRelation
	if err := tx.WithContext(ctx).
		Where("label_id = ? AND "+CuratedLaneSQL("source_id"), labelID).Find(&have).Error; err != nil {
		return err
	}
	wanted := make(map[labelRelation]struct{}, len(want))
	for _, r := range want {
		wanted[r] = struct{}{}
	}
	for _, h := range have {
		if _, keep := wanted[labelRelation{Other: h.OtherLabelID, Relation: h.Relation}]; keep {
			continue
		}
		if err := tx.WithContext(ctx).Exec(`DELETE FROM catalog_label_relation
			WHERE source_id = ? AND ((label_id = ? AND other_label_id = ? AND relation = ?)
			                      OR (label_id = ? AND other_label_id = ? AND relation = ?))`,
			curatedSourceID, labelID, h.OtherLabelID, h.Relation,
			h.OtherLabelID, labelID, InverseLabelRelation(h.Relation)).Error; err != nil {
			return err
		}
	}
	for _, r := range want {
		if err := tx.WithContext(ctx).Exec(`INSERT INTO catalog_label_relation
			  (label_id, other_label_id, relation, source_id, matched_by, created_at)
			VALUES (?, ?, ?, ?, ?, now()), (?, ?, ?, ?, ?, now())
			ON CONFLICT DO NOTHING`,
			labelID, r.Other, r.Relation, curatedSourceID, curatedMatchedBy,
			r.Other, labelID, InverseLabelRelation(r.Relation), curatedSourceID, curatedMatchedBy).Error; err != nil {
			return err
		}
	}
	return nil
}

func labelRelationSuppressionSpec(relations editing.FieldSpec) editing.FieldSpec {
	spec := editing.SuppressedFieldSpec(TypeLabel, relations)
	reconcile := spec.Apply
	spec.Apply = func(ctx context.Context, tx *gorm.DB, labelID int64, value any) error {
		if err := reconcile(ctx, tx, labelID, value); err != nil {
			return err
		}
		var clash []catmodel.CatalogLabelRelation
		if err := tx.WithContext(ctx).Raw(`SELECT r.label_id, r.other_label_id, r.relation FROM catalog_label_relation r
			WHERE `+CuratedLaneSQL("r.source_id")+` AND (r.label_id = ? OR r.other_label_id = ?)
			  AND `+LabelRelationEdgeSuppressedSQL+` ORDER BY r.label_id, r.other_label_id, r.relation`,
			labelID, labelID).Scan(&clash).Error; err != nil {
			return err
		}
		if len(clash) > 0 {
			return &editing.ValidationError{Key: FieldLabelRelationsSuppr,
				Reason: fmt.Sprintf("relation %d between companies %d and %d is a curated fact; remove it from %s instead of suppressing it",
					clash[0].Relation, clash[0].LabelID, clash[0].OtherLabelID, FieldLabelRelations)}
		}
		return tx.WithContext(ctx).Exec(PurgeSuppressedLabelRelationsSQL, labelID, labelID).Error
	}
	return spec
}

func loadCuratedLabelRelations(ctx context.Context, db *gorm.DB, labelID int64) ([]any, error) {
	var rows []catmodel.CatalogLabelRelation
	if err := db.WithContext(ctx).
		Where("label_id = ? AND "+CuratedLaneSQL("source_id"), labelID).
		Order("other_label_id, relation").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]any, 0, len(rows))
	for _, r := range rows {
		out = append(out, map[string]any{"label_id": r.OtherLabelID, "relation": int64(r.Relation)})
	}
	return out, nil
}

// SuppressedLabelRelations answers, for a writer of catalog_label_relation,
// whether either endpoint suppressed an edge.
type SuppressedLabelRelations map[int64]map[string]struct{}

func (s SuppressedLabelRelations) Has(labelID, otherID int64, relation int16) bool {
	if _, ok := s[labelID][LabelRelationIdentity(relation, otherID)]; ok {
		return true
	}
	_, ok := s[otherID][LabelRelationIdentity(InverseLabelRelation(relation), labelID)]
	return ok
}

func LoadSuppressedLabelRelations(ctx context.Context, db *gorm.DB) (SuppressedLabelRelations, error) {
	keys, err := editing.AllSuppressedKeys(ctx, db, TypeLabel, FieldLabelRelations)
	if err != nil {
		return nil, fmt.Errorf("load suppressed label relations: %w", err)
	}
	return SuppressedLabelRelations(keys), nil
}

func uniqueIDs(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
