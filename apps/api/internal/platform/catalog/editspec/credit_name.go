package editspec

import (
	"context"
	"errors"
	"fmt"

	catmodel "api/internal/platform/catalog/model"
	"api/internal/platform/catalog/perm"
	"api/internal/platform/editing"
	"api/internal/platform/textnorm"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	TypeCreditName = "catalog.credit_name"

	FieldCreditNameName           = "catalog.credit_name.name"
	FieldCreditNameLatin          = "catalog.credit_name.latin"
	FieldCreditNameLang           = "catalog.credit_name.lang"
	FieldCreditNameKind           = "catalog.credit_name.kind"
	FieldCreditNameLinkVisibility = "catalog.credit_name.link_visibility"
	FieldCreditNameNote           = "catalog.credit_name.note"
	FieldCreditNamePerson         = "catalog.credit_name.person_id"
	FieldCreditNameAliases        = "catalog.credit_name.aliases"
	FieldCreditNameAliasesSuppr   = FieldCreditNameAliases + editing.SuppressedFieldSuffix
)

const maxNoteRunes = 2000

var creditNameColumns = columnTable{
	table:    catmodel.CatalogCreditName{}.TableName(),
	newModel: func() any { return &catmodel.CatalogCreditName{} },
}

func RegisterCreditName(reg *editing.Registry, db *gorm.DB) error {
	return reg.Register(editing.EntityTypeSpec{
		Family:        "catalog",
		Type:          TypeCreditName,
		LoadSnapshot:  loadCreditNameSnapshot(db),
		DefaultPolicy: sharedEntityPolicy(perm.EditPersonReview),
		Fields:        creditNameFieldSpecs(),
	})
}

func creditNameFieldSpecs() []editing.FieldSpec {
	aliases := editing.FieldSpec{
		Key: FieldCreditNameAliases, Kind: editing.KindList, DiffHint: editing.DiffHintItems,
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
		Apply:    applyCreditNameAliases,
	}
	return []editing.FieldSpec{
		{
			Key: FieldCreditNameName, Kind: editing.KindText, DiffHint: editing.DiffHintInline,
			Validate:   validateCreditNameName,
			Apply:      applyCreditNameName,
			Provenance: creditNameColumns.provenance("name"),
		},
		creditNameColumns.nullableText(FieldCreditNameLatin, "latin", maxNameRunes),
		creditNameColumns.lang(FieldCreditNameLang, "lang"),
		creditNameColumns.codedEnum(FieldCreditNameKind, "kind", "credit_name_kind", 0, false,
			catmodel.CreditNameKindMain, catmodel.CreditNameKindPenName,
			catmodel.CreditNameKindDistinctPersona, catmodel.CreditNameKindFormerName),
		creditNameColumns.codedEnum(FieldCreditNameLinkVisibility, "link_visibility", "link_visibility", 0, false,
			catmodel.LinkVisibilityPublic, catmodel.LinkVisibilityHidden),
		creditNameColumns.emptyableText(FieldCreditNameNote, "note", maxNoteRunes, editing.DiffHintLines),
		{
			Key: FieldCreditNamePerson, Kind: editing.KindRef, DiffHint: editing.DiffHintInline,
			Value: &editing.ValueSpec{Nullable: true},
			Validate: func(v any) error {
				_, err := parseRefID(v, true)
				return err
			},
			Apply:      applyCreditNamePerson,
			Provenance: creditNameColumns.provenance("person_id"),
		},
		aliases,
		editing.SuppressedFieldSpec(TypeCreditName, aliases),
	}
}

func validateCreditNameName(v any) error {
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("must be a string")
	}
	if textnorm.Clean(s) == "" {
		return fmt.Errorf("must not be empty")
	}
	if len([]rune(s)) > maxNameRunes {
		return fmt.Errorf("must be at most %d characters", maxNameRunes)
	}
	return nil
}

// A person's display name is its primary credit name's spelling, so renaming
// that name renames the person in the same write.
func applyCreditNameName(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	if err := validateCreditNameName(value); err != nil {
		return fmt.Errorf("editspec: credit name: %w", err)
	}
	name := textnorm.Clean(value.(string))
	res := tx.WithContext(ctx).Model(&catmodel.CatalogCreditName{}).Where("id = ?", entityID).
		Updates(map[string]any{"name": name})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return editing.ErrEntityNotFound
	}
	return tx.WithContext(ctx).Model(&catmodel.CatalogPerson{}).
		Where("primary_credit_name_id = ?", entityID).
		Update("display_name", name).Error
}

// applyCreditNamePerson attaches, reassigns or detaches a credit name. Credits
// keep pointing at the name either way (doc 10): moving a name moves every
// credit it carries to the new person without touching catalog_credit.
func applyCreditNamePerson(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	target, err := parseRefID(value, true)
	if err != nil {
		return fmt.Errorf("editspec: person_id: %w", err)
	}
	var cn catmodel.CatalogCreditName
	err = tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("id", "person_id").First(&cn, entityID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return editing.ErrEntityNotFound
	}
	if err != nil {
		return err
	}
	if target != nil {
		if err := assertPersonExists(ctx, tx, *target); err != nil {
			if errors.Is(err, editing.ErrEntityNotFound) {
				return &editing.ValidationError{Key: FieldCreditNamePerson, Reason: fmt.Sprintf("person %d does not exist", *target)}
			}
			return err
		}
	}
	if cn.PersonID != nil && (target == nil || *target != *cn.PersonID) {
		var others int64
		if err := tx.WithContext(ctx).Model(&catmodel.CatalogCreditName{}).
			Where("person_id = ? AND id <> ?", *cn.PersonID, entityID).Count(&others).Error; err != nil {
			return err
		}
		if others == 0 {
			return &editing.ValidationError{
				Key: FieldCreditNamePerson,
				Reason: fmt.Sprintf("this is the only credit name of person %d; merge that person instead of moving its last name away",
					*cn.PersonID),
			}
		}
	}
	if err := tx.WithContext(ctx).Model(&catmodel.CatalogCreditName{}).Where("id = ?", entityID).
		Updates(map[string]any{"person_id": target}).Error; err != nil {
		return err
	}
	if cn.PersonID != nil {
		if err := tx.WithContext(ctx).Exec(`
			UPDATE catalog_person p SET primary_credit_name_id = b.id, display_name = b.name
			FROM (SELECT cn.id, cn.name FROM catalog_credit_name cn
			      WHERE cn.person_id = ?
			      ORDER BY (SELECT count(*) FROM catalog_credit c WHERE c.credit_name_id = cn.id) DESC, cn.id
			      LIMIT 1) b
			WHERE p.id = ? AND p.primary_credit_name_id = ?`,
			*cn.PersonID, *cn.PersonID, entityID).Error; err != nil {
			return err
		}
	}
	if target != nil {
		if err := tx.WithContext(ctx).Exec(`
			UPDATE catalog_person p SET primary_credit_name_id = cn.id, display_name = cn.name
			FROM catalog_credit_name cn
			WHERE p.id = ? AND p.primary_credit_name_id IS NULL AND cn.id = ?`,
			*target, entityID).Error; err != nil {
			return err
		}
	}
	return nil
}

func assertCreditNameExists(ctx context.Context, db *gorm.DB, entityID int64) error {
	var cn catmodel.CatalogCreditName
	return firstEntity(ctx, db, &cn, entityID, "id")
}

func applyCreditNameAliases(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	aliases, err := parseEntityAliases(value)
	if err != nil {
		return fmt.Errorf("editspec: credit name aliases: %w", err)
	}
	if err := assertCreditNameExists(ctx, tx, entityID); err != nil {
		return err
	}
	if len(aliases) > 0 {
		pairs := make([][]any, 0, len(aliases))
		for _, a := range aliases {
			pairs = append(pairs, []any{a.Name, a.Lang})
		}
		var clash []catmodel.CatalogNameAlias
		if err := tx.WithContext(ctx).Select("name", "lang").
			Where("credit_name_id = ? AND source_id IS DISTINCT FROM ?", entityID, curatedSourceID).
			Where("(name, lang) IN ?", pairs).Order("lang, name").Find(&clash).Error; err != nil {
			return err
		}
		if len(clash) > 0 {
			return &editing.ValidationError{
				Key: FieldCreditNameAliases,
				Reason: fmt.Sprintf("%q (%s) is already provided upstream for this credit name; to hide it use %s instead of re-adding it",
					clash[0].Name, clash[0].Lang, FieldCreditNameAliasesSuppr),
			}
		}
	}
	if err := tx.WithContext(ctx).
		Where("credit_name_id = ? AND source_id = ? AND kind <> ?", entityID, curatedSourceID, catmodel.AliasKindSearchHint).
		Delete(&catmodel.CatalogNameAlias{}).Error; err != nil {
		return err
	}
	if len(aliases) == 0 {
		return nil
	}
	source := curatedSourceID
	rows := make([]catmodel.CatalogNameAlias, 0, len(aliases))
	for _, a := range aliases {
		row := catmodel.CatalogNameAlias{
			CreditNameID: entityID, Name: a.Name, Lang: a.Lang, Kind: int16(a.Kind),
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

// NotSuppressedNameAliasSQL is the read-path predicate, correlated on a
// catalog_name_alias alias. The identity has the character aliases' shape, so
// it reuses their formula.
func NotSuppressedNameAliasSQL(alias string) string {
	return fmt.Sprintf(
		`NOT EXISTS (SELECT 1 FROM edit_suppressed_row esr
			WHERE esr.entity_type = '%s' AND esr.field_key = '%s'
			  AND esr.entity_id = %s.credit_name_id AND esr.identity_key = %s)`,
		TypeCreditName, FieldCreditNameAliases, alias, CharacterAliasIdentitySQL(alias))
}

func loadCuratedNameAliases(ctx context.Context, db *gorm.DB, creditNameID int64) ([]any, error) {
	var rows []catmodel.CatalogNameAlias
	if err := db.WithContext(ctx).
		Where("credit_name_id = ? AND source_id = ? AND kind <> ?", creditNameID, curatedSourceID, catmodel.AliasKindSearchHint).
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

func loadCreditNameSnapshot(db *gorm.DB) func(context.Context, int64) (map[string]any, error) {
	return func(ctx context.Context, entityID int64) (map[string]any, error) {
		var cn catmodel.CatalogCreditName
		if err := firstEntity(ctx, db, &cn, entityID,
			"id", "name", "latin", "lang", "kind", "link_visibility", "note", "person_id"); err != nil {
			return nil, err
		}
		aliases, err := loadCuratedNameAliases(ctx, db, entityID)
		if err != nil {
			return nil, err
		}
		suppressed, err := editing.LoadSuppressedValue(ctx, db, TypeCreditName, entityID, FieldCreditNameAliases)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			FieldCreditNameName:           cn.Name,
			FieldCreditNameLatin:          nullableString(cn.Latin),
			FieldCreditNameLang:           cn.Lang,
			FieldCreditNameKind:           int64(cn.Kind),
			FieldCreditNameLinkVisibility: int64(cn.LinkVisibility),
			FieldCreditNameNote:           cn.Note,
			FieldCreditNamePerson:         nullableInt64(cn.PersonID),
			FieldCreditNameAliases:        aliases,
			FieldCreditNameAliasesSuppr:   suppressed,
		}, nil
	}
}
