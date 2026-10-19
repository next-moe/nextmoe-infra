package editspec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	catmodel "api/internal/platform/catalog/model"
	"api/internal/platform/catalog/perm"
	"api/internal/platform/editing"

	"gorm.io/gorm"
)

const (
	TypeLabel  = "catalog.label"
	TypeTag    = "catalog.tag"
	TypeEngine = "catalog.engine"
	TypeSeries = "catalog.series"

	FieldLabelName   = "catalog.label.name"
	FieldLabelIntros = "catalog.label.intros"
	FieldLabelLinks  = "catalog.label.links"

	FieldTagIntros = "catalog.tag.intros"

	FieldEngineName    = "catalog.engine.name"
	FieldEngineIntro   = "catalog.engine.intro"
	FieldEngineAliases = "catalog.engine.aliases"

	FieldSeriesName   = "catalog.series.name"
	FieldSeriesIntros = "catalog.series.intros"
)

const maxNameRunes = 300

func taxonomyPolicy() editing.Policy {
	return editing.Policy{
		Propose:   editing.ProposePerm(string(perm.EditTaxonomy)),
		Review:    editing.ReviewPerm(string(perm.EditTaxonomyReview)),
		Automerge: editing.AutomergeNever,
	}
}

// A taxonomy row fans out to every work referencing it, so kungal keeps
// automerge=never here even though its work overlay automerges on review perm.
func kungalTaxonomyOverlays(fields []editing.FieldSpec) map[string]map[string]editing.Policy {
	kungal := editing.Policy{
		Propose:   editing.ProposeOpen,
		Review:    editing.ReviewPerm(string(perm.EditTaxonomyReview)),
		Automerge: editing.AutomergeNever,
	}
	overlay := make(map[string]editing.Policy, len(fields))
	for _, f := range fields {
		overlay[f.Key] = kungal
	}
	return map[string]map[string]editing.Policy{"kungal": overlay}
}

func RegisterTaxonomy(reg *editing.Registry, db *gorm.DB) error {
	for _, register := range []func(*editing.Registry, *gorm.DB) error{
		registerLabel, registerTag, registerEngine, registerSeries,
	} {
		if err := register(reg, db); err != nil {
			return err
		}
	}
	return nil
}

func registerLabel(reg *editing.Registry, db *gorm.DB) error {
	fields := []editing.FieldSpec{
		{
			Key: FieldLabelName, Kind: editing.KindText, DiffHint: editing.DiffHintInline,
			Validate: validateName,
			Apply:    applyEntityColumn(&catmodel.CatalogLabel{}, "display_name", asString),
			Provenance: []editing.ProvenanceTarget{
				{Table: catmodel.CatalogLabel{}.TableName(), Column: "display_name"},
			},
		},
		{
			Key: FieldLabelIntros, Kind: editing.KindList, DiffHint: editing.DiffHintLines,
			Value: &editing.ValueSpec{Element: &editing.ElementSpec{
				Type: "object",
				Members: []editing.ElementMember{
					{Key: "lang", Type: "enum", Vocabulary: "intro_lang"},
					{Key: "intro", Type: "text"},
				},
			}},
			Validate: validateIntros,
			Apply:    applyEntityIntros(introTableLabel),
		},
		{
			Key: FieldLabelLinks, Kind: editing.KindList, DiffHint: editing.DiffHintItems,
			Value:    &editing.ValueSpec{Element: &editing.ElementSpec{Type: "text"}},
			Validate: validateLinks,
			Apply: func(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
				if err := firstEntity(ctx, tx, &catmodel.CatalogLabel{}, entityID, "id"); err != nil {
					return err
				}
				return applyLinksFor(ctx, tx, catmodel.EntityTypeLabel, entityID, value)
			},
		},
	}
	return reg.Register(editing.EntityTypeSpec{
		Family: "catalog", Type: TypeLabel,
		LoadSnapshot: func(ctx context.Context, entityID int64) (map[string]any, error) {
			var l catmodel.CatalogLabel
			if err := firstEntity(ctx, db, &l, entityID, "id", "display_name"); err != nil {
				return nil, err
			}
			intros, err := loadEntityIntros(ctx, db, introTableLabel, entityID)
			if err != nil {
				return nil, err
			}
			links, err := loadLinksFor(ctx, db, catmodel.EntityTypeLabel, entityID)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				FieldLabelName:   l.DisplayName,
				FieldLabelIntros: intros,
				FieldLabelLinks:  links,
			}, nil
		},
		DefaultPolicy: taxonomyPolicy(),
		SiteOverlays:  kungalTaxonomyOverlays(fields),
		Fields:        fields,
	})
}

func registerTag(reg *editing.Registry, db *gorm.DB) error {
	fields := []editing.FieldSpec{
		{
			Key: FieldTagIntros, Kind: editing.KindList, DiffHint: editing.DiffHintLines,
			Value: &editing.ValueSpec{Element: &editing.ElementSpec{
				Type: "object",
				Members: []editing.ElementMember{
					{Key: "lang", Type: "enum", Vocabulary: "intro_lang"},
					{Key: "intro", Type: "text"},
				},
			}},
			Validate: validateIntros,
			Apply:    applyEntityIntros(introTableTag),
		},
	}
	return reg.Register(editing.EntityTypeSpec{
		Family: "catalog", Type: TypeTag,
		LoadSnapshot: func(ctx context.Context, entityID int64) (map[string]any, error) {
			var t catmodel.CatalogTag
			if err := firstEntity(ctx, db, &t, entityID, "id", "name"); err != nil {
				return nil, err
			}
			intros, err := loadEntityIntros(ctx, db, introTableTag, entityID)
			if err != nil {
				return nil, err
			}
			return map[string]any{FieldTagIntros: intros}, nil
		},
		DefaultPolicy: taxonomyPolicy(),
		SiteOverlays:  kungalTaxonomyOverlays(fields),
		Fields:        fields,
	})
}

func registerEngine(reg *editing.Registry, db *gorm.DB) error {
	fields := []editing.FieldSpec{
		{
			Key: FieldEngineName, Kind: editing.KindText, DiffHint: editing.DiffHintInline,
			Validate: validateName,
			Apply:    applyEntityColumn(&catmodel.CatalogEngine{}, "name", asString),
		},
		{
			Key: FieldEngineIntro, Kind: editing.KindText, DiffHint: editing.DiffHintLines,
			Validate: validateIntroText,
			Apply:    applyEntityColumn(&catmodel.CatalogEngine{}, "description", asString),
		},
		{
			Key: FieldEngineAliases, Kind: editing.KindList, DiffHint: editing.DiffHintItems,
			Value:    &editing.ValueSpec{Element: &editing.ElementSpec{Type: "text"}},
			Validate: validateAliases,
			Apply:    applyEngineAliases,
		},
	}
	return reg.Register(editing.EntityTypeSpec{
		Family: "catalog", Type: TypeEngine,
		LoadSnapshot: func(ctx context.Context, entityID int64) (map[string]any, error) {
			var e catmodel.CatalogEngine
			if err := firstEntity(ctx, db, &e, entityID, "id", "name", "description", "aliases"); err != nil {
				return nil, err
			}
			aliases, err := decodeAliases(e.Aliases)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				FieldEngineName:    e.Name,
				FieldEngineIntro:   e.Description,
				FieldEngineAliases: aliases,
			}, nil
		},
		DefaultPolicy: taxonomyPolicy(),
		SiteOverlays:  kungalTaxonomyOverlays(fields),
		Fields:        fields,
	})
}

func registerSeries(reg *editing.Registry, db *gorm.DB) error {
	fields := []editing.FieldSpec{
		{
			Key: FieldSeriesName, Kind: editing.KindText, DiffHint: editing.DiffHintInline,
			Validate: validateName,
			Apply:    curatedOnly(applyEntityColumn(&catmodel.CatalogSeries{}, "display_name", asString)),
		},
		{
			Key: FieldSeriesIntros, Kind: editing.KindList, DiffHint: editing.DiffHintLines,
			Value: &editing.ValueSpec{Element: &editing.ElementSpec{
				Type: "object",
				Members: []editing.ElementMember{
					{Key: "lang", Type: "enum", Vocabulary: "intro_lang"},
					{Key: "intro", Type: "text"},
				},
			}},
			Validate: validateIntros,
			Apply:    applyEntityIntros(introTableSeries),
		},
	}
	return reg.Register(editing.EntityTypeSpec{
		Family: "catalog", Type: TypeSeries,
		LoadSnapshot: func(ctx context.Context, entityID int64) (map[string]any, error) {
			var s catmodel.CatalogSeries
			if err := firstEntity(ctx, db, &s, entityID, "id", "display_name"); err != nil {
				return nil, err
			}
			intros, err := loadEntityIntros(ctx, db, introTableSeries, entityID)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				FieldSeriesName:   s.DisplayName,
				FieldSeriesIntros: intros,
			}, nil
		},
		DefaultPolicy: taxonomyPolicy(),
		SiteOverlays:  kungalTaxonomyOverlays(fields),
		Fields:        fields,
	})
}

func validateName(v any) error {
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("must be a string")
	}
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("must not be empty")
	}
	if len([]rune(s)) > maxNameRunes {
		return fmt.Errorf("must be at most %d characters", maxNameRunes)
	}
	return nil
}

func validateIntroText(v any) error {
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("must be a string")
	}
	if len([]rune(s)) > maxIntroRunes {
		return fmt.Errorf("must be at most %d characters", maxIntroRunes)
	}
	return nil
}

func validateAliases(v any) error {
	_, err := parseAliases(v)
	return err
}

func parseAliases(v any) ([]string, error) {
	arr, err := asArray(v, "alias strings")
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
		s = strings.TrimSpace(s)
		if s == "" {
			return nil, fmt.Errorf("element %d: must not be empty", i)
		}
		if len([]rune(s)) > maxNameRunes {
			return nil, fmt.Errorf("element %d: must be at most %d characters", i, maxNameRunes)
		}
		if _, dup := seen[s]; dup {
			return nil, fmt.Errorf("element %d: duplicate alias", i)
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out, nil
}

func decodeAliases(raw []byte) ([]any, error) {
	if len(raw) == 0 {
		return []any{}, nil
	}
	var names []string
	if err := json.Unmarshal(raw, &names); err != nil {
		return []any{}, nil
	}
	out := make([]any, 0, len(names))
	for _, n := range names {
		out = append(out, n)
	}
	return out, nil
}

func applyEngineAliases(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	aliases, err := parseAliases(value)
	if err != nil {
		return fmt.Errorf("editspec: aliases: %w", err)
	}
	encoded, err := json.Marshal(aliases)
	if err != nil {
		return err
	}
	res := tx.WithContext(ctx).Model(&catmodel.CatalogEngine{}).
		Where("id = ?", entityID).Update("aliases", encoded)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return editing.ErrEntityNotFound
	}
	return nil
}

func curatedOnly(inner editing.ApplyFunc) editing.ApplyFunc {
	return func(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
		var n int64
		if err := tx.WithContext(ctx).Model(&catmodel.CatalogSeries{}).
			Where("id = ? AND source_id = ?", entityID, curatedSourceID).Count(&n).Error; err != nil {
			return err
		}
		if n == 0 {
			return &editing.ValidationError{
				Key: FieldSeriesName,
				Reason: "only a CURATED series can be renamed here " +
					"(an upstream series' name is reconciled by its importer)",
			}
		}
		return inner(ctx, tx, entityID, value)
	}
}

func applyEntityColumn(model any, column string, conv func(any) (any, error)) editing.ApplyFunc {
	return func(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
		v, err := conv(value)
		if err != nil {
			return fmt.Errorf("editspec: %s: %w", column, err)
		}
		res := tx.WithContext(ctx).Model(model).Where("id = ?", entityID).Update(column, v)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return editing.ErrEntityNotFound
		}
		return nil
	}
}

func firstEntity(ctx context.Context, db *gorm.DB, dest any, entityID int64, columns ...string) error {
	err := db.WithContext(ctx).Select(columns).First(dest, entityID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return editing.ErrEntityNotFound
	}
	return err
}
