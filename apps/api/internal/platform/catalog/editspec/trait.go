package editspec

import (
	"context"
	"errors"
	"fmt"
	"strings"

	catmodel "api/internal/platform/catalog/model"
	"api/internal/platform/catalog/perm"
	"api/internal/platform/editing"

	"gorm.io/gorm"
)

const (
	TypeTrait = "catalog.trait"
	TagTrait  = "trait"

	FieldTraitName           = "catalog.trait.name"
	FieldTraitNameZh         = "catalog.trait.name_zh"
	FieldTraitAliases        = "catalog.trait.aliases"
	FieldTraitDescription    = "catalog.trait.description"
	FieldTraitDescriptionZh  = "catalog.trait.description_zh"
	FieldTraitGroup          = "catalog.trait.group_id"
	FieldTraitGroupOrder     = "catalog.trait.group_order"
	FieldTraitDefaultSpoiler = "catalog.trait.default_spoiler"
	FieldTraitApplicable     = "catalog.trait.applicable"
	FieldTraitSearchable     = "catalog.trait.searchable"
	FieldTraitSexual         = "catalog.trait.sexual"
	FieldTraitParents        = "catalog.trait.parents"
	FieldTraitParentsSuppr   = FieldTraitParents + editing.SuppressedFieldSuffix

	// p100 is 4 parents on one trait (2026-10).
	maxTraitParents = 20
)

var traitTable = columnTable{
	table:    catmodel.CatalogCharacterTrait{}.TableName(),
	newModel: func() any { return &catmodel.CatalogCharacterTrait{} },
}

// TraitParentSuppressedSQL is true when the (trait, parent) edge named by the
// two expressions is suppressed. As for tag parents, the edit deletes the
// importer's edge and chartraits must not write it back.
func TraitParentSuppressedSQL(traitIDExpr, parentIDExpr string) string {
	return fmt.Sprintf(`EXISTS (SELECT 1 FROM edit_suppressed_row esr
		WHERE esr.entity_type = '%s' AND esr.field_key = '%s'
		  AND esr.entity_id = %s AND esr.identity_key = ('parent:' || %s))`,
		TypeTrait, FieldTraitParents, traitIDExpr, parentIDExpr)
}

var traitParentEdges = parentEdges{
	table: "catalog_character_trait_parent", childColumn: "trait_id", entityLabel: "trait", field: FieldTraitParents,
	curatedOrigin: catmodel.TraitParentOriginCurated, suppressedSQL: TraitParentSuppressedSQL,
}

func RegisterTrait(reg *editing.Registry, db *gorm.DB) error {
	parents := traitParentsFieldSpec()
	fields := append(traitScalarFields(), parents, traitParentSuppressionSpec(parents))
	return reg.Register(editing.EntityTypeSpec{
		Family:        "catalog",
		Type:          TypeTrait,
		LoadSnapshot:  loadTraitSnapshot(db),
		DefaultPolicy: sharedEntityPolicy(perm.EditTaxonomyReview),
		Fields:        fields,
	})
}

func traitScalarFields() []editing.FieldSpec {
	name := traitTable.emptyableText(FieldTraitName, "name", maxNameRunes, editing.DiffHintInline)
	name.Validate = validateName

	nameZh := traitTable.emptyableText(FieldTraitNameZh, "name_zh", maxNameRunes, editing.DiffHintInline)
	nameZh.Apply = applyTraitNameZh

	groupOrder := editing.FieldSpec{
		Key: FieldTraitGroupOrder, Kind: editing.KindInt, DiffHint: editing.DiffHintInline,
		Validate: func(v any) error {
			_, err := parseGroupOrder(v)
			return err
		},
		Apply: traitTable.apply("gorder", func(v any) (any, error) {
			n, err := parseGroupOrder(v)
			if err != nil {
				return nil, err
			}
			return n, nil
		}),
		Provenance: traitTable.provenance("gorder"),
	}

	sexual := traitBool(FieldTraitSexual, "sexual")
	sexual.Policy = reviewerOnlyPolicy(perm.EditTaxonomyReview)
	sexual.Apply = rederivingSexualFamily(sexual.Apply)

	return []editing.FieldSpec{
		name,
		nameZh,
		{
			Key: FieldTraitAliases, Kind: editing.KindList, DiffHint: editing.DiffHintItems,
			Value:      &editing.ValueSpec{Element: &editing.ElementSpec{Type: "text"}},
			Validate:   validateAliases,
			Apply:      applyTraitAliases,
			Provenance: traitTable.provenance("alias"),
		},
		traitTable.emptyableText(FieldTraitDescription, "description", maxIntroRunes, editing.DiffHintLines),
		traitTable.emptyableText(FieldTraitDescriptionZh, "description_zh", maxIntroRunes, editing.DiffHintLines),
		{
			Key: FieldTraitGroup, Kind: editing.KindRef, DiffHint: editing.DiffHintInline,
			Value: &editing.ValueSpec{Nullable: true},
			Validate: func(v any) error {
				_, err := parseRefID(v, true)
				return err
			},
			Apply:      applyTraitGroup,
			Provenance: traitTable.provenance("group_tid"),
		},
		groupOrder,
		traitTable.codedEnum(FieldTraitDefaultSpoiler, "default_spoil", "spoiler", 0, false, 0, 1, 2),
		traitBool(FieldTraitApplicable, "applicable"),
		traitBool(FieldTraitSearchable, "searchable"),
		sexual,
	}
}

func traitBool(key, column string) editing.FieldSpec {
	return editing.FieldSpec{
		Key: key, Kind: editing.KindEnum, DiffHint: editing.DiffHintInline,
		Validate:   validateBool,
		Apply:      traitTable.apply(column, asBool),
		Provenance: traitTable.provenance(column),
	}
}

func parseGroupOrder(v any) (int16, error) {
	n, err := parseNullableI16(v, 0, 32767)
	if err != nil {
		return 0, err
	}
	if n == nil {
		return 0, fmt.Errorf("must not be null")
	}
	return *n, nil
}

func rederivingSexualFamily(inner editing.ApplyFunc) editing.ApplyFunc {
	return func(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
		if err := inner(ctx, tx, entityID, value); err != nil {
			return err
		}
		return tx.WithContext(ctx).Exec(catmodel.TraitSexualFamilySQL).Error
	}
}

// ingest-trait-zh only overwrites a name it recorded as machine-made, so a
// human name must carry the curated marker as well as the provenance stamp.
func applyTraitNameZh(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	s, err := asString(value)
	if err != nil {
		return fmt.Errorf("editspec: trait name_zh: %w", err)
	}
	res := tx.WithContext(ctx).Model(&catmodel.CatalogCharacterTrait{}).Where("id = ?", entityID).
		Updates(map[string]any{"name_zh": s, "name_zh_provenance": catmodel.TraitNameZhProvenanceCurated})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return editing.ErrEntityNotFound
	}
	return nil
}

func applyTraitAliases(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	aliases, err := parseAliases(value)
	if err != nil {
		return fmt.Errorf("editspec: trait aliases: %w", err)
	}
	return traitTable.apply("alias", func(any) (any, error) {
		return strings.Join(aliases, "\n"), nil
	})(ctx, tx, entityID, value)
}

func splitTraitAliases(raw string) []any {
	out := []any{}
	for _, a := range strings.Split(raw, "\n") {
		if a = strings.TrimSpace(a); a != "" {
			out = append(out, a)
		}
	}
	return out
}

// group_tid names the top-level trait by its VNDB id; a group is a trait with
// no group of its own.
func applyTraitGroup(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	target, err := parseRefID(value, true)
	if err != nil {
		return fmt.Errorf("editspec: trait group: %w", err)
	}
	var self catmodel.CatalogCharacterTrait
	if err := tx.WithContext(ctx).Select("id", "vndb_tid").First(&self, entityID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return editing.ErrEntityNotFound
		}
		return err
	}
	groupTID := ""
	if target != nil {
		if *target == entityID {
			return &editing.ValidationError{Key: FieldTraitGroup, Reason: "a trait cannot be its own group"}
		}
		var g catmodel.CatalogCharacterTrait
		err := tx.WithContext(ctx).Select("id", "vndb_tid", "group_tid").First(&g, *target).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &editing.ValidationError{Key: FieldTraitGroup, Reason: fmt.Sprintf("trait %d does not exist", *target)}
		}
		if err != nil {
			return err
		}
		if g.GroupTID != "" {
			return &editing.ValidationError{Key: FieldTraitGroup,
				Reason: fmt.Sprintf("trait %d is not a group (it belongs to a group itself)", *target)}
		}
		var members int64
		if err := tx.WithContext(ctx).Model(&catmodel.CatalogCharacterTrait{}).
			Where("group_tid = ? AND id <> ?", self.VndbTID, entityID).Count(&members).Error; err != nil {
			return err
		}
		if members > 0 {
			return &editing.ValidationError{Key: FieldTraitGroup,
				Reason: fmt.Sprintf("this trait is the group of %d other traits and cannot join a group itself", members)}
		}
		groupTID = g.VndbTID
	}
	return traitTable.apply("group_tid", func(any) (any, error) { return groupTID, nil })(ctx, tx, entityID, value)
}

func traitParentsFieldSpec() editing.FieldSpec {
	return editing.FieldSpec{
		Key: FieldTraitParents, Kind: editing.KindList, DiffHint: editing.DiffHintItems,
		MaxElements:   maxTraitParents,
		MaxSuppressed: maxTraitParents,
		Identity: &editing.IdentitySpec{Segments: 2, KeyCheck: parentKeyCheck,
			Refs: []editing.IdentityRef{{Segment: 2, EntityTag: TagTrait}}},
		Value: &editing.ValueSpec{Element: &editing.ElementSpec{Type: "ref"}},
		Validate: func(v any) error {
			_, err := parseTraitParentIDs(v)
			return err
		},
		Apply: rederivingSexualFamily(applyTraitParents),
	}
}

func parseTraitParentIDs(v any) ([]int64, error) {
	ids, err := parseIDList(v, "parent ids")
	if err != nil {
		return nil, err
	}
	if len(ids) > maxTraitParents {
		return nil, fmt.Errorf("at most %d parents", maxTraitParents)
	}
	return ids, nil
}

func applyTraitParents(ctx context.Context, tx *gorm.DB, traitID int64, value any) error {
	ids, err := parseTraitParentIDs(value)
	if err != nil {
		return fmt.Errorf("editspec: trait parents: %w", err)
	}
	if err := firstEntity(ctx, tx, &catmodel.CatalogCharacterTrait{}, traitID, "id"); err != nil {
		return err
	}
	if err := assertExistingIDs(ctx, tx, "catalog_character_trait", FieldTraitParents, "trait", traitID, ids); err != nil {
		return err
	}
	del := tx.WithContext(ctx).Where("trait_id = ? AND origin = ?", traitID, catmodel.TraitParentOriginCurated)
	if len(ids) > 0 {
		del = del.Where("parent_id NOT IN ?", ids)
	}
	if err := del.Delete(&catmodel.CatalogCharacterTraitParent{}).Error; err != nil {
		return err
	}
	for _, p := range ids {
		if err := tx.WithContext(ctx).Exec(`
			INSERT INTO catalog_character_trait_parent (trait_id, parent_id, origin)
			VALUES (?, ?, ?)
			ON CONFLICT (trait_id, parent_id) DO UPDATE SET origin = EXCLUDED.origin`,
			traitID, p, catmodel.TraitParentOriginCurated).Error; err != nil {
			return err
		}
	}
	if err := rejectParentCycle(ctx, tx, "catalog_character_trait_parent", "trait_id", FieldTraitParents, traitID); err != nil {
		return err
	}
	return rejectSuppressedCuratedParents(ctx, tx, traitParentEdges, traitID, FieldTraitParents)
}

func traitParentSuppressionSpec(parents editing.FieldSpec) editing.FieldSpec {
	spec := editing.SuppressedFieldSpec(TypeTrait, parents)
	reconcile := spec.Apply
	spec.Apply = rederivingSexualFamily(func(ctx context.Context, tx *gorm.DB, traitID int64, value any) error {
		if err := reconcile(ctx, tx, traitID, value); err != nil {
			return err
		}
		if err := rejectSuppressedCuratedParents(ctx, tx, traitParentEdges, traitID, FieldTraitParentsSuppr); err != nil {
			return err
		}
		return tx.WithContext(ctx).Exec(`DELETE FROM catalog_character_trait_parent p
			WHERE p.trait_id = ? AND p.origin = ? AND `+TraitParentSuppressedSQL("p.trait_id", "p.parent_id"),
			traitID, catmodel.TraitParentOriginVNDB).Error
	})
	return spec
}

func loadTraitSnapshot(db *gorm.DB) func(ctx context.Context, entityID int64) (map[string]any, error) {
	return func(ctx context.Context, entityID int64) (map[string]any, error) {
		var t catmodel.CatalogCharacterTrait
		if err := firstEntity(ctx, db, &t, entityID, "id", "name", "name_zh", "alias", "description",
			"description_zh", "group_tid", "gorder", "default_spoil", "applicable", "searchable", "sexual"); err != nil {
			return nil, err
		}
		var group any
		if t.GroupTID != "" {
			var ids []int64
			if err := db.WithContext(ctx).Raw(`SELECT id FROM catalog_character_trait WHERE vndb_tid = ?`, t.GroupTID).
				Scan(&ids).Error; err != nil {
				return nil, err
			}
			if len(ids) > 0 {
				group = ids[0]
			}
		}
		parents, err := loadCuratedTraitParents(ctx, db, entityID)
		if err != nil {
			return nil, err
		}
		suppressed, err := editing.LoadSuppressedValue(ctx, db, TypeTrait, entityID, FieldTraitParents)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			FieldTraitName:           t.Name,
			FieldTraitNameZh:         t.NameZh,
			FieldTraitAliases:        splitTraitAliases(t.Alias),
			FieldTraitDescription:    t.Description,
			FieldTraitDescriptionZh:  t.DescriptionZh,
			FieldTraitGroup:          group,
			FieldTraitGroupOrder:     int64(t.GOrder),
			FieldTraitDefaultSpoiler: int64(t.DefaultSpoil),
			FieldTraitApplicable:     t.Applicable,
			FieldTraitSearchable:     t.Searchable,
			FieldTraitSexual:         t.Sexual,
			FieldTraitParents:        parents,
			FieldTraitParentsSuppr:   suppressed,
		}, nil
	}
}

func loadCuratedTraitParents(ctx context.Context, db *gorm.DB, traitID int64) ([]any, error) {
	var ids []int64
	if err := db.WithContext(ctx).Raw(`SELECT parent_id FROM catalog_character_trait_parent
		WHERE trait_id = ? AND origin = ? ORDER BY parent_id`, traitID, catmodel.TraitParentOriginCurated).Scan(&ids).Error; err != nil {
		return nil, err
	}
	out := make([]any, 0, len(ids))
	for _, id := range ids {
		out = append(out, id)
	}
	return out, nil
}
