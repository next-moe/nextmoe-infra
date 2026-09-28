package editspec

import (
	"context"
	"fmt"
	"strings"

	catmodel "api/internal/platform/catalog/model"
	"api/internal/platform/catalog/perm"
	"api/internal/platform/editing"

	"gorm.io/gorm"
)

const (
	TypeCharacter = "catalog.character"

	FieldCharacterDisplayName   = "catalog.character.display_name"
	FieldCharacterLang          = "catalog.character.lang"
	FieldCharacterLatin         = "catalog.character.latin"
	FieldCharacterDescription   = "catalog.character.description"
	FieldCharacterGender        = "catalog.character.gender"
	FieldCharacterBirthdayMonth = "catalog.character.birthday_month"
	FieldCharacterBirthdayDay   = "catalog.character.birthday_day"
	FieldCharacterBloodType     = "catalog.character.blood_type"
	FieldCharacterHeightCm      = "catalog.character.height_cm"
	FieldCharacterWeightKg      = "catalog.character.weight_kg"
	FieldCharacterBustCm        = "catalog.character.bust_cm"
	FieldCharacterWaistCm       = "catalog.character.waist_cm"
	FieldCharacterHipCm         = "catalog.character.hip_cm"
	FieldCharacterCup           = "catalog.character.cup"

	FieldCharacterAliases      = "catalog.character.aliases"
	FieldCharacterAliasesSuppr = FieldCharacterAliases + editing.SuppressedFieldSuffix
	FieldCharacterIntros       = "catalog.character.intros"
)

const maxCupRunes = 8

func RegisterCharacter(reg *editing.Registry, db *gorm.DB) error {
	fields := characterFieldSpecs()

	// A character is shared by every work it appears in, so kungal keeps
	// automerge=never here — the blast radius of a direct edit is the taxonomy's,
	// not a single work's.
	kungalPolicy := editing.Policy{
		Propose:   editing.ProposeOpen,
		Review:    editing.ReviewPerm(string(perm.EditCharacterReview)),
		Automerge: editing.AutomergeNever,
	}
	letmoePolicy := editing.Policy{
		Propose:   editing.ProposeTrusted,
		Review:    editing.ReviewPerm(string(perm.EditCharacterReview)),
		Automerge: editing.AutomergeOwner,
	}
	everyField := func(p editing.Policy) map[string]editing.Policy {
		overlay := make(map[string]editing.Policy, len(fields))
		for _, f := range fields {
			overlay[f.Key] = p
		}
		return overlay
	}
	overlays := make(map[string]map[string]editing.Policy, len(letmoeSites)+1)
	for _, site := range letmoeSites {
		overlays[site] = everyField(letmoePolicy)
	}
	overlays["kungal"] = everyField(kungalPolicy)

	return reg.Register(editing.EntityTypeSpec{
		Family:       "catalog",
		Type:         TypeCharacter,
		LoadSnapshot: loadCharacterSnapshot(db),
		Txn:          txnOn(db),
		// A character belongs to no product site — nothing claims one the way a
		// site claims a work. The hook exists because editing.Register refuses an
		// automerge=owner policy without one, and returning nil is what makes the
		// letmoe overlay's automerge=owner mean "never" for this family.
		OwnerSite: func(ctx context.Context, entityID int64) (*string, error) {
			if err := assertCharacterExists(ctx, db, entityID); err != nil {
				return nil, err
			}
			return nil, nil
		},
		DefaultPolicy: editing.Policy{
			Propose:   editing.ProposePerm(string(perm.EditCharacter)),
			Review:    editing.ReviewPerm(string(perm.EditCharacterReview)),
			Automerge: editing.AutomergeNever,
		},
		SiteOverlays: overlays,
		Fields:       fields,
	})
}

func characterFieldSpecs() []editing.FieldSpec {
	aliases := editing.FieldSpec{
		Key: FieldCharacterAliases, Kind: editing.KindList, DiffHint: editing.DiffHintItems,
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
		Apply:    applyCharacterAliases,
	}
	return []editing.FieldSpec{
		charText(FieldCharacterDisplayName, "display_name", maxNameRunes),
		withValue(charLang(FieldCharacterLang, "lang"), &editing.ValueSpec{Vocabulary: "olang"}),
		withValue(charNullableText(FieldCharacterLatin, "latin", maxNameRunes), &editing.ValueSpec{Nullable: true}),
		charLongText(FieldCharacterDescription, "description"),
		withValue(charEnum(FieldCharacterGender, "gender",
			catmodel.GenderMale, catmodel.GenderFemale, catmodel.GenderOther),
			&editing.ValueSpec{Vocabulary: "gender", Coded: true, Base: 1, Nullable: true}),
		withValue(charInt(FieldCharacterBirthdayMonth, "birthday_month", 1, 12), &editing.ValueSpec{Nullable: true}),
		withValue(charInt(FieldCharacterBirthdayDay, "birthday_day", 1, 31), &editing.ValueSpec{Nullable: true}),
		withValue(charEnum(FieldCharacterBloodType, "blood_type",
			catmodel.BloodTypeA, catmodel.BloodTypeB, catmodel.BloodTypeAB, catmodel.BloodTypeO),
			&editing.ValueSpec{Vocabulary: "blood_type", Coded: true, Base: 1, Nullable: true}),
		withValue(charInt(FieldCharacterHeightCm, "height_cm", 1, 1000), &editing.ValueSpec{Nullable: true}),
		withValue(charInt(FieldCharacterWeightKg, "weight_kg", 1, 1000), &editing.ValueSpec{Nullable: true}),
		withValue(charInt(FieldCharacterBustCm, "bust_cm", 1, 500), &editing.ValueSpec{Nullable: true}),
		withValue(charInt(FieldCharacterWaistCm, "waist_cm", 1, 500), &editing.ValueSpec{Nullable: true}),
		withValue(charInt(FieldCharacterHipCm, "hip_cm", 1, 500), &editing.ValueSpec{Nullable: true}),
		withValue(charNullableText(FieldCharacterCup, "cup", maxCupRunes), &editing.ValueSpec{Nullable: true}),
		aliases,
		editing.SuppressedFieldSpec(TypeCharacter, aliases),
		{
			Key: FieldCharacterIntros, Kind: editing.KindList, DiffHint: editing.DiffHintLines,
			Value: &editing.ValueSpec{Element: &editing.ElementSpec{
				Type: "object",
				Members: []editing.ElementMember{
					{Key: "lang", Type: "enum", Vocabulary: "intro_lang"},
					{Key: "intro", Type: "text"},
				},
			}},
			Validate: validateIntros,
			Apply:    applyEntityIntros(introTableCharacter),
		},
	}
}

func withValue(f editing.FieldSpec, v *editing.ValueSpec) editing.FieldSpec {
	f.Value = v
	return f
}

func characterProvenance(column string) []editing.ProvenanceTarget {
	return []editing.ProvenanceTarget{{Table: catmodel.CatalogCharacter{}.TableName(), Column: column}}
}

func charText(key, column string, maxRunes int) editing.FieldSpec {
	return editing.FieldSpec{
		Key: key, Kind: editing.KindText, DiffHint: editing.DiffHintInline,
		Validate:   func(v any) error { return validateBoundedText(v, maxRunes, false) },
		Apply:      applyCharacterColumn(column, asString),
		Provenance: characterProvenance(column),
	}
}

func charLongText(key, column string) editing.FieldSpec {
	return editing.FieldSpec{
		Key: key, Kind: editing.KindText, DiffHint: editing.DiffHintLines,
		Validate:   validateIntroText,
		Apply:      applyCharacterColumn(column, asString),
		Provenance: characterProvenance(column),
	}
}

// charLang is the language column: not null, but the empty string is the
// recorded "unknown", so it is accepted alongside the allowed language tags.
func charLang(key, column string) editing.FieldSpec {
	return editing.FieldSpec{
		Key: key, Kind: editing.KindEnum, DiffHint: editing.DiffHintInline,
		Validate: func(v any) error {
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
		},
		Apply:      applyCharacterColumn(column, asString),
		Provenance: characterProvenance(column),
	}
}

func charNullableText(key, column string, maxRunes int) editing.FieldSpec {
	return editing.FieldSpec{
		Key: key, Kind: editing.KindText, DiffHint: editing.DiffHintInline,
		Validate: func(v any) error {
			if v == nil {
				return nil
			}
			return validateBoundedText(v, maxRunes, true)
		},
		Apply: applyCharacterColumn(column, func(v any) (any, error) {
			if v == nil {
				return nil, nil
			}
			return asString(v)
		}),
		Provenance: characterProvenance(column),
	}
}

func charInt(key, column string, min, max int16) editing.FieldSpec {
	return editing.FieldSpec{
		Key: key, Kind: editing.KindInt, DiffHint: editing.DiffHintInline,
		Validate: func(v any) error {
			_, err := parseNullableI16(v, min, max)
			return err
		},
		Apply:      applyCharacterColumn(column, nullableI16Converter(min, max)),
		Provenance: characterProvenance(column),
	}
}

func charEnum(key, column string, allowed ...int16) editing.FieldSpec {
	return editing.FieldSpec{
		Key: key, Kind: editing.KindEnum, DiffHint: editing.DiffHintInline,
		Validate: func(v any) error {
			_, err := parseEnumI16(v, allowed)
			return err
		},
		Apply: applyCharacterColumn(column, func(v any) (any, error) {
			n, err := parseEnumI16(v, allowed)
			if err != nil {
				return nil, err
			}
			if n == nil {
				return nil, nil
			}
			return *n, nil
		}),
		Provenance: characterProvenance(column),
	}
}

func validateBoundedText(v any, maxRunes int, allowEmpty bool) error {
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("must be a string")
	}
	if !allowEmpty && strings.TrimSpace(s) == "" {
		return fmt.Errorf("must not be empty")
	}
	if len([]rune(s)) > maxRunes {
		return fmt.Errorf("must be at most %d characters", maxRunes)
	}
	return nil
}

func parseNullableI16(v any, min, max int16) (*int16, error) {
	if v == nil {
		return nil, nil
	}
	f, ok := v.(float64)
	if !ok || f != float64(int64(f)) {
		return nil, fmt.Errorf("must be an integer or null")
	}
	n := int64(f)
	if n < int64(min) || n > int64(max) {
		return nil, fmt.Errorf("must be between %d and %d", min, max)
	}
	out := int16(n)
	return &out, nil
}

func nullableI16Converter(min, max int16) func(any) (any, error) {
	return func(v any) (any, error) {
		n, err := parseNullableI16(v, min, max)
		if err != nil {
			return nil, err
		}
		if n == nil {
			return nil, nil
		}
		return *n, nil
	}
}

func parseEnumI16(v any, allowed []int16) (*int16, error) {
	if v == nil {
		return nil, nil
	}
	f, ok := v.(float64)
	if !ok || f != float64(int64(f)) {
		return nil, fmt.Errorf("must be an integer or null")
	}
	n := int16(int64(f))
	for _, a := range allowed {
		if a == n {
			return &n, nil
		}
	}
	return nil, fmt.Errorf("must be one of %v, or null", allowed)
}

// applyCharacterColumn writes through a map so that a JSON null reaches the
// column as NULL: gorm's single-column Update drops a nil value, which would
// silently turn "clear this attribute" into a no-op.
func applyCharacterColumn(column string, conv func(any) (any, error)) editing.ApplyFunc {
	return func(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
		v, err := conv(value)
		if err != nil {
			return fmt.Errorf("editspec: %s: %w", column, err)
		}
		res := tx.WithContext(ctx).Model(&catmodel.CatalogCharacter{}).
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

func assertCharacterExists(ctx context.Context, db *gorm.DB, entityID int64) error {
	var c catmodel.CatalogCharacter
	return firstEntity(ctx, db, &c, entityID, "id")
}

func loadCharacterSnapshot(db *gorm.DB) func(context.Context, int64) (map[string]any, error) {
	return func(ctx context.Context, entityID int64) (map[string]any, error) {
		var c catmodel.CatalogCharacter
		if err := firstEntity(ctx, db, &c, entityID,
			"id", "display_name", "lang", "latin", "description", "gender",
			"birthday_month", "birthday_day", "blood_type",
			"height_cm", "weight_kg", "bust_cm", "waist_cm", "hip_cm", "cup"); err != nil {
			return nil, err
		}
		snapshot := map[string]any{
			FieldCharacterDisplayName:   c.DisplayName,
			FieldCharacterLang:          c.Lang,
			FieldCharacterLatin:         nullableString(c.Latin),
			FieldCharacterDescription:   c.Description,
			FieldCharacterGender:        nullableI16(c.Gender),
			FieldCharacterBirthdayMonth: nullableI16(c.BirthdayMonth),
			FieldCharacterBirthdayDay:   nullableI16(c.BirthdayDay),
			FieldCharacterBloodType:     nullableI16(c.BloodType),
			FieldCharacterHeightCm:      nullableI16(c.HeightCm),
			FieldCharacterWeightKg:      nullableI16(c.WeightKg),
			FieldCharacterBustCm:        nullableI16(c.BustCm),
			FieldCharacterWaistCm:       nullableI16(c.WaistCm),
			FieldCharacterHipCm:         nullableI16(c.HipCm),
			FieldCharacterCup:           nullableString(c.Cup),
		}
		for key, load := range map[string]func(context.Context, *gorm.DB, int64) ([]any, error){
			FieldCharacterAliases:      loadCuratedCharacterAliases,
			FieldCharacterAliasesSuppr: loadSuppressedCharacterAliases,
			FieldCharacterIntros:       loadCharacterIntros,
		} {
			value, err := load(ctx, db, entityID)
			if err != nil {
				return nil, err
			}
			snapshot[key] = value
		}
		return snapshot, nil
	}
}

func nullableString(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func nullableI16(p *int16) any {
	if p == nil {
		return nil
	}
	return int64(*p)
}

func nullableInt64(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}
