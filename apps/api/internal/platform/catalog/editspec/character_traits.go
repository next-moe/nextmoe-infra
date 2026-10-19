package editspec

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	catmodel "api/internal/platform/catalog/model"
	"api/internal/platform/editing"

	"gorm.io/gorm"
)

const (
	FieldCharacterTraits      = "catalog.character.traits"
	FieldCharacterTraitsSuppr = FieldCharacterTraits + editing.SuppressedFieldSuffix

	// p100 is 227 links on one character (2026-10).
	maxCharacterTraits = 300
	maxTraitSpoiler    = 2
)

func CharacterTraitIdentity(traitID int64) string {
	return "trait:" + strconv.FormatInt(traitID, 10)
}

func characterTraitKeyCheck(key string) error {
	parts := strings.Split(key, ":")
	if len(parts) != 2 || parts[0] != "trait" {
		return fmt.Errorf("%q is not an identity key of this field: expected trait:<trait_id>", key)
	}
	n, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || strconv.FormatInt(n, 10) != parts[1] || n <= 0 {
		return fmt.Errorf("%q is not an identity key of this field: %q is not a canonical trait id", key, parts[1])
	}
	return nil
}

// CharacterTraitSuppressedSQL is true when the (character, trait) pair named by
// the two expressions is suppressed. Trait suppression is enforced on write, as
// for work tags: no read face filters catalog_character_trait_link, so every
// writer of that table must skip these pairs.
func CharacterTraitSuppressedSQL(characterIDExpr, traitIDExpr string) string {
	return fmt.Sprintf(`EXISTS (SELECT 1 FROM edit_suppressed_row esr
		WHERE esr.entity_type = '%s' AND esr.field_key = '%s'
		  AND esr.entity_id = %s AND esr.identity_key = ('trait:' || %s))`,
		TypeCharacter, FieldCharacterTraits, characterIDExpr, traitIDExpr)
}

// PurgeSuppressedCharacterTraitsSQL takes one parameter, the character id.
var PurgeSuppressedCharacterTraitsSQL = `DELETE FROM catalog_character_trait_link l
	WHERE l.character_id = ? AND ` + NotCuratedLaneSQL("l.source_id") + `
	  AND ` + CharacterTraitSuppressedSQL("l.character_id", "l.trait_id")

type characterTrait struct {
	TraitID int64
	Spoiler int16
	Lie     bool
}

func parseCharacterTraits(v any) ([]characterTrait, error) {
	arr, err := asArrayN(v, "trait objects", maxCharacterTraits)
	if err != nil {
		return nil, err
	}
	out := make([]characterTrait, 0, len(arr))
	seen := make(map[int64]struct{}, len(arr))
	for i, el := range arr {
		obj, err := asObject(el, i, "trait_id", "spoiler", "lie")
		if err != nil {
			return nil, err
		}
		id, err := objInt(obj, "trait_id", i, true)
		if err != nil {
			return nil, err
		}
		if id <= 0 {
			return nil, fmt.Errorf("element %d: trait_id must be positive", i)
		}
		if _, dup := seen[id]; dup {
			return nil, fmt.Errorf("element %d: duplicate trait_id %d", i, id)
		}
		seen[id] = struct{}{}
		spoiler, err := objInt(obj, "spoiler", i, true)
		if err != nil {
			return nil, err
		}
		if spoiler < 0 || spoiler > maxTraitSpoiler {
			return nil, fmt.Errorf("element %d: spoiler must be between 0 and %d", i, maxTraitSpoiler)
		}
		if _, ok := obj["lie"]; !ok {
			return nil, fmt.Errorf("element %d: lie is required", i)
		}
		lie, err := objBool(obj, "lie", i)
		if err != nil {
			return nil, err
		}
		out = append(out, characterTrait{TraitID: id, Spoiler: int16(spoiler), Lie: lie})
	}
	return out, nil
}

func validateCharacterTraits(v any) error {
	_, err := parseCharacterTraits(v)
	return err
}

func characterTraitsFieldSpec() editing.FieldSpec {
	return editing.FieldSpec{
		Key: FieldCharacterTraits, Kind: editing.KindList, DiffHint: editing.DiffHintItems,
		MaxElements:   maxCharacterTraits,
		MaxSuppressed: maxCharacterTraits,
		Identity:      &editing.IdentitySpec{Segments: 2, KeyCheck: characterTraitKeyCheck},
		Value: &editing.ValueSpec{Element: &editing.ElementSpec{
			Type: "object",
			Members: []editing.ElementMember{
				{Key: "trait_id", Type: "ref"},
				{Key: "spoiler", Type: "int", Vocabulary: "spoiler"},
				{Key: "lie", Type: "bool"},
			},
		}},
		Validate: validateCharacterTraits,
		Apply:    applyCharacterTraits,
	}
}

func characterTraitSuppressionSpec(traits editing.FieldSpec) editing.FieldSpec {
	spec := editing.SuppressedFieldSpec(TypeCharacter, traits)
	reconcile := spec.Apply
	spec.Apply = func(ctx context.Context, tx *gorm.DB, characterID int64, value any) error {
		if err := reconcile(ctx, tx, characterID, value); err != nil {
			return err
		}
		if err := rejectSuppressedCuratedTraits(ctx, tx, characterID, FieldCharacterTraitsSuppr); err != nil {
			return err
		}
		return tx.WithContext(ctx).Exec(PurgeSuppressedCharacterTraitsSQL, characterID).Error
	}
	return spec
}

// applyCharacterTraits owns the curated rows only. The table's unique key is
// (character_id, trait_id) with no source in it, so a curated element on a
// trait the importer already linked takes that row over; chartraits leaves a
// curated row alone, and dropping the element later hands the link back to
// the importer's next run rather than hiding it — hiding is the .suppressed
// companion's job.
func applyCharacterTraits(ctx context.Context, tx *gorm.DB, characterID int64, value any) error {
	traits, err := parseCharacterTraits(value)
	if err != nil {
		return fmt.Errorf("editspec: traits: %w", err)
	}
	if err := assertCharacterExists(ctx, tx, characterID); err != nil {
		return err
	}
	if err := assertApplicableTraits(ctx, tx, traits); err != nil {
		return err
	}
	ids := make([]int64, 0, len(traits))
	for _, t := range traits {
		ids = append(ids, t.TraitID)
	}
	del := tx.WithContext(ctx).
		Where("character_id = ? AND "+CuratedLaneSQL("source_id"), characterID)
	if len(ids) > 0 {
		del = del.Where("trait_id NOT IN ?", ids)
	}
	if err := del.Delete(&catmodel.CatalogCharacterTraitLink{}).Error; err != nil {
		return err
	}
	for _, t := range traits {
		if err := tx.WithContext(ctx).Exec(`
			INSERT INTO catalog_character_trait_link
			  (character_id, trait_id, spoiler_level, lie, source_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, now(), now())
			ON CONFLICT (character_id, trait_id) DO UPDATE SET
			  spoiler_level = EXCLUDED.spoiler_level, lie = EXCLUDED.lie,
			  source_id = EXCLUDED.source_id, updated_at = now()`,
			characterID, t.TraitID, t.Spoiler, t.Lie, curatedSourceID).Error; err != nil {
			return err
		}
	}
	return rejectSuppressedCuratedTraits(ctx, tx, characterID, FieldCharacterTraits)
}

func assertApplicableTraits(ctx context.Context, tx *gorm.DB, traits []characterTrait) error {
	if len(traits) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(traits))
	for _, t := range traits {
		ids = append(ids, t.TraitID)
	}
	var rows []catmodel.CatalogCharacterTrait
	if err := tx.WithContext(ctx).Select("id", "name", "applicable").
		Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return err
	}
	found := make(map[int64]catmodel.CatalogCharacterTrait, len(rows))
	for _, r := range rows {
		found[r.ID] = r
	}
	for _, id := range ids {
		r, ok := found[id]
		if !ok {
			return &editing.ValidationError{Key: FieldCharacterTraits, Reason: fmt.Sprintf("trait %d does not exist", id)}
		}
		if !r.Applicable {
			return &editing.ValidationError{Key: FieldCharacterTraits,
				Reason: fmt.Sprintf("trait %d (%q) is a group heading and cannot be applied to a character", id, r.Name)}
		}
	}
	return nil
}

// rejectSuppressedCuratedTraits makes a curated link and a suppression of the
// same trait an explicit 422 from whichever field created the contradiction.
func rejectSuppressedCuratedTraits(ctx context.Context, tx *gorm.DB, characterID int64, field string) error {
	var clash []int64
	if err := tx.WithContext(ctx).Raw(`
		SELECT l.trait_id FROM catalog_character_trait_link l
		WHERE l.character_id = ? AND `+CuratedLaneSQL("l.source_id")+`
		  AND `+CharacterTraitSuppressedSQL("l.character_id", "l.trait_id")+`
		ORDER BY l.trait_id`, characterID).Scan(&clash).Error; err != nil {
		return err
	}
	if len(clash) == 0 {
		return nil
	}
	reason := fmt.Sprintf("trait %d is suppressed on this character; lift it from %s in an edit of its own before assigning it",
		clash[0], FieldCharacterTraitsSuppr)
	if field == FieldCharacterTraitsSuppr {
		reason = fmt.Sprintf("trait %d is assigned in this character's %s; remove it there instead of suppressing it",
			clash[0], FieldCharacterTraits)
	}
	return &editing.ValidationError{Key: field, Reason: reason}
}

func loadCuratedCharacterTraits(ctx context.Context, db *gorm.DB, characterID int64) ([]any, error) {
	var rows []catmodel.CatalogCharacterTraitLink
	if err := db.WithContext(ctx).
		Where("character_id = ? AND "+CuratedLaneSQL("source_id"), characterID).
		Order("trait_id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]any, 0, len(rows))
	for _, r := range rows {
		out = append(out, map[string]any{
			"trait_id": r.TraitID, "spoiler": int64(r.SpoilerLevel), "lie": r.Lie,
		})
	}
	return out, nil
}

func loadSuppressedCharacterTraits(ctx context.Context, db *gorm.DB, characterID int64) ([]any, error) {
	return editing.LoadSuppressedValue(ctx, db, TypeCharacter, characterID, FieldCharacterTraits)
}
