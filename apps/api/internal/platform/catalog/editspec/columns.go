package editspec

import (
	"context"
	"fmt"

	"api/internal/platform/editing"

	"gorm.io/gorm"
)

type columnTable struct {
	table    string
	newModel func() any
}

func (t columnTable) provenance(column string) []editing.ProvenanceTarget {
	return []editing.ProvenanceTarget{{Table: t.table, Column: column}}
}

// gorm's single-column Update drops a nil value; the map is what lets a JSON
// null clear the column (see applyCharacterColumn).
func (t columnTable) apply(column string, conv func(any) (any, error)) editing.ApplyFunc {
	return func(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
		v, err := conv(value)
		if err != nil {
			return fmt.Errorf("editspec: %s.%s: %w", t.table, column, err)
		}
		res := tx.WithContext(ctx).Model(t.newModel()).
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

func (t columnTable) nullableInt(key, column string, min, max int16) editing.FieldSpec {
	return editing.FieldSpec{
		Key: key, Kind: editing.KindInt, DiffHint: editing.DiffHintInline,
		Validate: func(v any) error {
			_, err := parseNullableI16(v, min, max)
			return err
		},
		Apply:      t.apply(column, nullableI16Converter(min, max)),
		Provenance: t.provenance(column),
		Value:      &editing.ValueSpec{Nullable: true},
	}
}

func (t columnTable) codedEnum(key, column, vocabulary string, base int, nullable bool, allowed ...int16) editing.FieldSpec {
	parse := func(v any) (*int16, error) {
		if v == nil && !nullable {
			return nil, fmt.Errorf("must not be null")
		}
		return parseEnumI16(v, allowed)
	}
	return editing.FieldSpec{
		Key: key, Kind: editing.KindEnum, DiffHint: editing.DiffHintInline,
		Validate: func(v any) error {
			_, err := parse(v)
			return err
		},
		Apply: t.apply(column, func(v any) (any, error) {
			n, err := parse(v)
			if err != nil {
				return nil, err
			}
			if n == nil {
				return nil, nil
			}
			return *n, nil
		}),
		Provenance: t.provenance(column),
		Value:      &editing.ValueSpec{Vocabulary: vocabulary, Coded: true, Base: base, Nullable: nullable},
	}
}

func (t columnTable) nullableText(key, column string, maxRunes int) editing.FieldSpec {
	return editing.FieldSpec{
		Key: key, Kind: editing.KindText, DiffHint: editing.DiffHintInline,
		Validate: func(v any) error {
			if v == nil {
				return nil
			}
			return validateBoundedText(v, maxRunes, true)
		},
		Apply: t.apply(column, func(v any) (any, error) {
			if v == nil {
				return nil, nil
			}
			return asString(v)
		}),
		Provenance: t.provenance(column),
		Value:      &editing.ValueSpec{Nullable: true},
	}
}

func (t columnTable) emptyableText(key, column string, maxRunes int, hint string) editing.FieldSpec {
	return editing.FieldSpec{
		Key: key, Kind: editing.KindText, DiffHint: hint,
		Validate:   func(v any) error { return validateBoundedText(v, maxRunes, true) },
		Apply:      t.apply(column, asString),
		Provenance: t.provenance(column),
	}
}

func (t columnTable) lang(key, column string) editing.FieldSpec {
	return editing.FieldSpec{
		Key: key, Kind: editing.KindEnum, DiffHint: editing.DiffHintInline,
		Validate:   validateOptionalLang,
		Apply:      t.apply(column, asString),
		Provenance: t.provenance(column),
		Value:      &editing.ValueSpec{Vocabulary: "olang"},
	}
}

func validateOptionalLang(v any) error {
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("must be a string")
	}
	if s == "" {
		return nil
	}
	if _, allowed := olangAllowed[s]; !allowed {
		return fmt.Errorf("%q is not an allowed language", s)
	}
	return nil
}

func (t columnTable) imageHash(key, column string) editing.FieldSpec {
	return editing.FieldSpec{
		Key: key, Kind: editing.KindImageHash, DiffHint: editing.DiffHintImage,
		Validate:   validateImageHashValue,
		Apply:      t.apply(column, asString),
		Provenance: t.provenance(column),
	}
}

func validateImageHashValue(v any) error {
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("must be an image hash string; send \"\" to clear the image")
	}
	if len([]rune(s)) > maxHashRunes {
		return fmt.Errorf("must be at most %d characters", maxHashRunes)
	}
	return nil
}
