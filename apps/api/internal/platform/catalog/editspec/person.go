package editspec

import (
	"context"
	"errors"
	"fmt"

	catmodel "api/internal/platform/catalog/model"
	"api/internal/platform/catalog/perm"
	"api/internal/platform/editing"

	"gorm.io/gorm"
)

const (
	TypePerson = "catalog.person"

	FieldPersonGender      = "catalog.person.gender"
	FieldPersonBirthYear   = "catalog.person.birth_year"
	FieldPersonBirthMonth  = "catalog.person.birth_month"
	FieldPersonBirthDay    = "catalog.person.birth_day"
	FieldPersonPhoto       = "catalog.person.photo"
	FieldPersonPrimaryName = "catalog.person.primary_credit_name_id"
	FieldPersonIntros      = "catalog.person.intros"
	FieldPersonLinks       = "catalog.person.links"
	FieldPersonLinksSuppr  = FieldPersonLinks + editing.SuppressedFieldSuffix
)

var personColumns = columnTable{
	table:    catmodel.CatalogPerson{}.TableName(),
	newModel: func() any { return &catmodel.CatalogPerson{} },
}

func RegisterPerson(reg *editing.Registry, db *gorm.DB) error {
	return reg.Register(editing.EntityTypeSpec{
		Family:        "catalog",
		Type:          TypePerson,
		LoadSnapshot:  loadPersonSnapshot(db),
		DefaultPolicy: sharedEntityPolicy(perm.EditPersonReview),
		Fields:        personFieldSpecs(),
	})
}

func personFieldSpecs() []editing.FieldSpec {
	links := editing.FieldSpec{
		Key: FieldPersonLinks, Kind: editing.KindList, DiffHint: editing.DiffHintItems,
		Identity: &editing.IdentitySpec{Segments: 3, TrailingText: true, KeyCheck: personLinkKeyCheck},
		Value:    &editing.ValueSpec{Element: &editing.ElementSpec{Type: "text"}},
		Validate: validatePersonLinks,
		Apply:    applyPersonLinks,
	}
	return []editing.FieldSpec{
		personColumns.codedEnum(FieldPersonGender, "gender", "gender", 1, true,
			catmodel.GenderMale, catmodel.GenderFemale, catmodel.GenderOther),
		personColumns.nullableInt(FieldPersonBirthYear, "birth_y", 1000, 2100),
		personColumns.nullableInt(FieldPersonBirthMonth, "birth_m", 1, 12),
		personColumns.nullableInt(FieldPersonBirthDay, "birth_d", 1, 31),
		personColumns.imageHash(FieldPersonPhoto, "photo_hash"),
		{
			Key: FieldPersonPrimaryName, Kind: editing.KindRef, DiffHint: editing.DiffHintInline,
			Validate:   validatePositiveRef,
			Apply:      applyPersonPrimaryName,
			Provenance: personColumns.provenance("primary_credit_name_id"),
		},
		{
			Key: FieldPersonIntros, Kind: editing.KindList, DiffHint: editing.DiffHintLines,
			Value: &editing.ValueSpec{Element: &editing.ElementSpec{
				Type: "object",
				Members: []editing.ElementMember{
					{Key: "lang", Type: "enum", Vocabulary: "intro_lang"},
					{Key: "intro", Type: "text"},
				},
			}},
			Validate: validateIntros,
			Apply:    applyEntityIntros(introTablePerson),
		},
		links,
		personLinksSuppressedSpec(links),
	}
}

func personLinksSuppressedSpec(links editing.FieldSpec) editing.FieldSpec {
	spec := editing.SuppressedFieldSpec(TypePerson, links)
	apply := spec.Apply
	spec.Apply = func(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
		if err := rejectAnchorSuppression(ctx, tx, entityID, value); err != nil {
			return err
		}
		return apply(ctx, tx, entityID, value)
	}
	return spec
}

func rejectAnchorSuppression(ctx context.Context, tx *gorm.DB, personID int64, value any) error {
	arr, ok := value.([]any)
	if !ok || len(arr) == 0 {
		return nil
	}
	keys := make([]string, 0, len(arr))
	for _, el := range arr {
		if s, ok := el.(string); ok {
			keys = append(keys, s)
		}
	}
	var anchor string
	if err := tx.WithContext(ctx).Raw(`
		SELECT 'link:' || s.key || ':' || r.external_id
		FROM catalog_external_ref r JOIN catalog_source s ON s.id = r.source_id
		WHERE r.entity_type = ? AND r.entity_id = ? AND r.link_kind <> ?
		  AND 'link:' || s.key || ':' || r.external_id IN ?
		ORDER BY 1 LIMIT 1`,
		catmodel.EntityTypePerson, personID, catmodel.LinkKindRelated, keys).Scan(&anchor).Error; err != nil {
		return err
	}
	if anchor == "" {
		return nil
	}
	return &editing.ValidationError{
		Key:    FieldPersonLinksSuppr,
		Reason: fmt.Sprintf("%q is an identity anchor, not a related link; anchors are corrected by splitting or detaching, not by hiding", anchor),
	}
}

func validatePositiveRef(v any) error {
	_, err := parseRefID(v, false)
	return err
}

func parseRefID(v any, nullable bool) (*int64, error) {
	if v == nil {
		if nullable {
			return nil, nil
		}
		return nil, fmt.Errorf("must be an id")
	}
	f, ok := v.(float64)
	if !ok || f != float64(int64(f)) || f < 1 {
		if nullable {
			return nil, fmt.Errorf("must be a positive integer id or null")
		}
		return nil, fmt.Errorf("must be a positive integer id")
	}
	id := int64(f)
	return &id, nil
}

// The person's display name is the primary credit name's spelling; the two
// columns are written together so neither can drift from the other.
func applyPersonPrimaryName(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	id, err := parseRefID(value, false)
	if err != nil {
		return fmt.Errorf("editspec: primary credit name: %w", err)
	}
	var name catmodel.CatalogCreditName
	err = tx.WithContext(ctx).Select("id", "person_id", "name").First(&name, *id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && (name.PersonID == nil || *name.PersonID != entityID)) {
		return &editing.ValidationError{
			Key:    FieldPersonPrimaryName,
			Reason: fmt.Sprintf("credit name %d is not one of this person's names", *id),
		}
	}
	if err != nil {
		return err
	}
	res := tx.WithContext(ctx).Model(&catmodel.CatalogPerson{}).Where("id = ?", entityID).
		Updates(map[string]any{"primary_credit_name_id": name.ID, "display_name": name.Name})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return editing.ErrEntityNotFound
	}
	return nil
}

var introTablePerson = introTable{
	ownerCol:      "person_id",
	hasProvenance: true,
	empty:         func() any { return &catmodel.CatalogPersonIntro{} },
	newRow: func(id int64, lang, intro string) any {
		return &catmodel.CatalogPersonIntro{
			PersonID: id, Lang: lang, Intro: intro,
			SourceID: curatedSourceID, Provenance: catmodel.IntroProvenanceSource,
		}
	},
	read: func(ctx context.Context, db *gorm.DB, ownerID int64) (map[string]string, error) {
		var rows []catmodel.CatalogPersonIntro
		if err := curatedIntroScope(ctx, db, "person_id", ownerID).
			Where("provenance = ?", catmodel.IntroProvenanceSource).
			Find(&rows).Error; err != nil {
			return nil, err
		}
		out := make(map[string]string, len(rows))
		for _, r := range rows {
			out[r.Lang] = r.Intro
		}
		return out, nil
	},
}

func assertPersonExists(ctx context.Context, db *gorm.DB, entityID int64) error {
	var p catmodel.CatalogPerson
	return firstEntity(ctx, db, &p, entityID, "id")
}

func loadPersonSnapshot(db *gorm.DB) func(context.Context, int64) (map[string]any, error) {
	return func(ctx context.Context, entityID int64) (map[string]any, error) {
		var p catmodel.CatalogPerson
		if err := firstEntity(ctx, db, &p, entityID,
			"id", "gender", "birth_y", "birth_m", "birth_d", "photo_hash", "primary_credit_name_id"); err != nil {
			return nil, err
		}
		snapshot := map[string]any{
			FieldPersonGender:      nullableI16(p.Gender),
			FieldPersonBirthYear:   nullableI16(p.BirthY),
			FieldPersonBirthMonth:  nullableI16(p.BirthM),
			FieldPersonBirthDay:    nullableI16(p.BirthD),
			FieldPersonPhoto:       p.PhotoHash,
			FieldPersonPrimaryName: nullableInt64(p.PrimaryCreditNameID),
		}
		for key, load := range map[string]func(context.Context, *gorm.DB, int64) ([]any, error){
			FieldPersonIntros: func(ctx context.Context, db *gorm.DB, id int64) ([]any, error) {
				return loadEntityIntros(ctx, db, introTablePerson, id)
			},
			FieldPersonLinks:      loadCuratedPersonLinks,
			FieldPersonLinksSuppr: loadSuppressedPersonLinks,
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
