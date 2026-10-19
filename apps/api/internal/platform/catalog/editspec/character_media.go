package editspec

import (
	"context"
	"errors"
	"fmt"
	"strings"

	catmodel "api/internal/platform/catalog/model"
	"api/internal/platform/editing"

	"gorm.io/gorm"
)

const (
	FieldCharacterImage      = "catalog.character.image_hash"
	FieldCharacterFigure     = "catalog.character.figure_hash"
	FieldCharacterInstanceOf = "catalog.character.instance_of"
)

// NotHumanFieldSQL is the fill-only writer's guard: a column whose last writer
// was a person is off limits, including when that person cleared it. A bare
// NOT over HumanFieldProvenanceSQL is NULL for an unstamped column and would
// skip every row that was never stamped at all.
func NotHumanFieldSQL(provenanceColumn, column string) string {
	return "NOT COALESCE(" + HumanFieldProvenanceSQL(provenanceColumn, column) + ", false)"
}

func parseNullableHash(v any) (*string, error) {
	if v == nil {
		return nil, nil
	}
	s, ok := v.(string)
	if !ok {
		return nil, fmt.Errorf("must be an image hash string or null")
	}
	if s == "" || strings.TrimSpace(s) != s || strings.ContainsAny(s, " \t\r\n") {
		return nil, fmt.Errorf("must be a non-empty image hash without whitespace, or null")
	}
	if len([]rune(s)) > maxHashRunes {
		return nil, fmt.Errorf("must be at most %d characters", maxHashRunes)
	}
	return &s, nil
}

// charImage writes a portrait slot. The *_source_hash twin records the original
// a background-removal job cut the current image from, so it is cleared with
// any hand-set value: it described an image that is no longer there.
func charImage(key, column, sourceColumn string) editing.FieldSpec {
	return editing.FieldSpec{
		Key: key, Kind: editing.KindImageHash, DiffHint: editing.DiffHintImage,
		Value: &editing.ValueSpec{Nullable: true},
		Validate: func(v any) error {
			_, err := parseNullableHash(v)
			return err
		},
		Apply: func(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
			hash, err := parseNullableHash(value)
			if err != nil {
				return fmt.Errorf("editspec: %s: %w", column, err)
			}
			var v any
			if hash != nil {
				v = *hash
			}
			res := tx.WithContext(ctx).Model(&catmodel.CatalogCharacter{}).
				Where("id = ?", entityID).Updates(map[string]any{column: v, sourceColumn: nil})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return editing.ErrEntityNotFound
			}
			return nil
		},
		Provenance: characterProvenance(column),
	}
}

func parseInstanceOf(v any) (*int64, error) {
	if v == nil {
		return nil, nil
	}
	f, ok := v.(float64)
	if !ok || f != float64(int64(f)) || f <= 0 {
		return nil, fmt.Errorf("must be a positive integer character id or null")
	}
	id := int64(f)
	return &id, nil
}

func charInstanceOfField() editing.FieldSpec {
	return editing.FieldSpec{
		Key: FieldCharacterInstanceOf, Kind: editing.KindRef, DiffHint: editing.DiffHintInline,
		Value: &editing.ValueSpec{Nullable: true},
		Validate: func(v any) error {
			_, err := parseInstanceOf(v)
			return err
		},
		Apply:      applyCharacterInstanceOf,
		Provenance: characterProvenance("instance_of"),
	}
}

// An instance is a variant of one main character (VNDB's "instance of"), and
// the relation is one level deep: a main is never itself an instance.
func applyCharacterInstanceOf(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	id, err := parseInstanceOf(value)
	if err != nil {
		return fmt.Errorf("editspec: instance_of: %w", err)
	}
	if id != nil {
		if *id == entityID {
			return &editing.ValidationError{Key: FieldCharacterInstanceOf, Reason: "a character cannot be an instance of itself"}
		}
		var main catmodel.CatalogCharacter
		err := tx.WithContext(ctx).Select("id", "instance_of").First(&main, *id).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &editing.ValidationError{Key: FieldCharacterInstanceOf, Reason: fmt.Sprintf("character %d does not exist", *id)}
		}
		if err != nil {
			return err
		}
		if main.InstanceOf != nil {
			return &editing.ValidationError{Key: FieldCharacterInstanceOf,
				Reason: fmt.Sprintf("character %d is itself an instance of %d; point at that main character instead", *id, *main.InstanceOf)}
		}
		var instances int64
		if err := tx.WithContext(ctx).Model(&catmodel.CatalogCharacter{}).
			Where("instance_of = ?", entityID).Count(&instances).Error; err != nil {
			return err
		}
		if instances > 0 {
			return &editing.ValidationError{Key: FieldCharacterInstanceOf,
				Reason: fmt.Sprintf("this character is the main of %d instance(s), so it cannot become an instance itself", instances)}
		}
	}
	return applyCharacterColumn("instance_of", func(any) (any, error) {
		if id == nil {
			return nil, nil
		}
		return *id, nil
	})(ctx, tx, entityID, value)
}
