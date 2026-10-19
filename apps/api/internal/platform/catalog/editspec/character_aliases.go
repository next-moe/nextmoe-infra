package editspec

import (
	"context"
	"fmt"

	catmodel "api/internal/platform/catalog/model"
	"api/internal/platform/editing"
	"api/internal/platform/textnorm"

	"gorm.io/gorm"
)

type entityAlias struct {
	Name    string
	Lang    string
	Latin   string
	Kind    int64
	Primary bool
}

func parseEntityAliases(v any) ([]entityAlias, error) {
	arr, err := asArray(v, "alias objects")
	if err != nil {
		return nil, err
	}
	out := make([]entityAlias, 0, len(arr))
	seen := make(map[string]struct{}, len(arr))
	for i, el := range arr {
		obj, err := asObject(el, i, "name", "lang", "kind", "latin", "primary")
		if err != nil {
			return nil, err
		}
		name, err := objString(obj, "name", i, true, maxNameRunes)
		if err != nil {
			return nil, err
		}
		lang, err := objString(obj, "lang", i, true, 32)
		if err != nil {
			return nil, err
		}
		if _, allowed := olangAllowed[lang]; !allowed {
			return nil, fmt.Errorf("element %d: %q is not an allowed language", i, lang)
		}
		kind, err := objInt(obj, "kind", i, true)
		if err != nil {
			return nil, err
		}
		if kind != int64(catmodel.AliasKindTranslation) && kind != int64(catmodel.AliasKindSpellingVariant) {
			return nil, fmt.Errorf("element %d: kind must be 0 (translation) or 1 (spelling_variant); "+
				"kind 2 (search hint) is machine-owned and not editable", i)
		}
		latin, err := objString(obj, "latin", i, false, maxNameRunes)
		if err != nil {
			return nil, err
		}
		primary, err := objBool(obj, "primary", i)
		if err != nil {
			return nil, err
		}
		// The table's unique key is (character_id, name, lang) — kind is not in
		// it, so two elements differing only by kind would collide on insert.
		uniq := lang + "\x00" + name
		if _, dup := seen[uniq]; dup {
			return nil, fmt.Errorf("element %d: duplicate (name, lang)", i)
		}
		seen[uniq] = struct{}{}
		out = append(out, entityAlias{Name: name, Lang: lang, Latin: latin, Kind: kind, Primary: primary})
	}
	return out, nil
}

func validateCharacterAliases(v any) error {
	_, err := parseEntityAliases(v)
	return err
}

func applyCharacterAliases(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	aliases, err := parseEntityAliases(value)
	if err != nil {
		return fmt.Errorf("editspec: aliases: %w", err)
	}
	if err := assertCharacterExists(ctx, tx, entityID); err != nil {
		return err
	}
	if err := rejectUpstreamAliasCollisions(ctx, tx, entityID, aliases); err != nil {
		return err
	}
	if err := tx.WithContext(ctx).
		Where("character_id = ? AND source_id = ? AND kind <> ?",
			entityID, curatedSourceID, catmodel.AliasKindSearchHint).
		Delete(&catmodel.CatalogCharacterAlias{}).Error; err != nil {
		return err
	}
	if len(aliases) == 0 {
		return nil
	}
	source := curatedSourceID
	rows := make([]catmodel.CatalogCharacterAlias, 0, len(aliases))
	for _, a := range aliases {
		row := catmodel.CatalogCharacterAlias{
			CharacterID: entityID, Name: a.Name, Lang: a.Lang, Kind: int16(a.Kind),
			IsPrimaryForLocale: a.Primary,
			SourceID:           &source, Provenance: catmodel.AliasProvenanceSource,
		}
		if a.Latin != "" {
			latin := a.Latin
			row.Latin = &latin
		}
		rows = append(rows, row)
	}
	return tx.WithContext(ctx).Create(&rows).Error
}

// rejectUpstreamAliasCollisions makes an unwinnable insert an explicit 422.
// uq_catalog_character_alias_owner_name_lang does not carry source_id, so the
// curated lane and every importer share one key: a curated row on a (name, lang)
// an importer already owns can never exist, and the alternatives are a raw 23505
// out of the merge or the importers' own ON CONFLICT DO NOTHING, which saves the
// proposal and drops the row.
func rejectUpstreamAliasCollisions(ctx context.Context, tx *gorm.DB, entityID int64, aliases []entityAlias) error {
	if len(aliases) == 0 {
		return nil
	}
	pairs := make([][]any, 0, len(aliases))
	for _, a := range aliases {
		pairs = append(pairs, []any{a.Name, a.Lang})
	}
	var clash []catmodel.CatalogCharacterAlias
	if err := tx.WithContext(ctx).
		Select("name", "lang").
		Where("character_id = ? AND source_id IS DISTINCT FROM ?", entityID, curatedSourceID).
		Where("(name, lang) IN ?", pairs).
		Order("lang, name").
		Find(&clash).Error; err != nil {
		return err
	}
	if len(clash) == 0 {
		return nil
	}
	return &editing.ValidationError{
		Key: FieldCharacterAliases,
		Reason: fmt.Sprintf("%q (%s) is already provided upstream for this character; "+
			"to hide it use %s instead of re-adding it",
			clash[0].Name, clash[0].Lang, FieldCharacterAliasesSuppr),
	}
}

// CharacterAliasIdentity is the frozen row identity of
// catalog.character.aliases (doc 21 D10: registered means never redefined). It
// is derived from the row's content and never from catalog_character_alias.id,
// because the vndb roster importer deletes and re-inserts the same alias, which
// would expire the suppression exactly when it is needed. Kind is decimal and
// no lang in the table carries a colon, so the name is the unescaped remainder
// and needs no quoting. Nothing in the key is an entity id, so a character
// merge cannot invalidate it.
func CharacterAliasIdentity(kind int16, lang, name string) string {
	return fmt.Sprintf("alias:%d:%s:%s", kind, lang, textnorm.Clean(name))
}

// CharacterAliasIdentitySQL restates CharacterAliasIdentity for a
// catalog_character_alias alias — the one place the two definitions could drift
// apart, which is why TestCharacterAliasIdentitySQLMatchesGo recomputes both
// over real rows.
func CharacterAliasIdentitySQL(alias string) string {
	return fmt.Sprintf(`('alias:' || %[1]s.kind || ':' || %[1]s.lang || ':' || %[2]s)`,
		alias, textnorm.CleanSQL(alias+".name"))
}

// NotSuppressedCharacterAliasSQL is the read-path predicate, correlated on a
// catalog_character_alias alias.
func NotSuppressedCharacterAliasSQL(alias string) string {
	return fmt.Sprintf(
		`NOT EXISTS (SELECT 1 FROM edit_suppressed_row esr
			WHERE esr.entity_type = '%s' AND esr.field_key = '%s'
			  AND esr.entity_id = %s.character_id AND esr.identity_key = %s)`,
		TypeCharacter, FieldCharacterAliases, alias, CharacterAliasIdentitySQL(alias))
}

func loadSuppressedCharacterAliases(ctx context.Context, db *gorm.DB, characterID int64) ([]any, error) {
	return editing.LoadSuppressedValue(ctx, db, TypeCharacter, characterID, FieldCharacterAliases)
}

func loadCuratedCharacterAliases(ctx context.Context, db *gorm.DB, characterID int64) ([]any, error) {
	var rows []catmodel.CatalogCharacterAlias
	if err := db.WithContext(ctx).
		Where("character_id = ? AND source_id = ? AND kind <> ?",
			characterID, curatedSourceID, catmodel.AliasKindSearchHint).
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
