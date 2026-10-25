package editspec

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	catmodel "api/internal/platform/catalog/model"
	"api/internal/platform/catalog/perm"
	"api/internal/platform/catalog/walkthrough"
	"api/internal/platform/editing"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	BirthParamWorkID       = "work_id"
	BirthParamCreditNameID = "credit_name_id"
)

const maxDuplicateHints = 10

// Works are born through the claims mint lane (POST /v2/me/claims), which owns
// the duplicate gate, anchors and claim state; the engine does not mint works.
func RegisterBirths(reg *editing.Registry, _ *gorm.DB) error {
	births := map[string]editing.BirthSpec{
		TypeCharacter: {
			Required: []string{FieldCharacterDisplayName},
			Create: func(ctx context.Context, tx *gorm.DB, _, patch map[string]any) (int64, error) {
				row := catmodel.CatalogCharacter{DisplayName: birthText(patch, FieldCharacterDisplayName), Extra: datatypes.JSON(`{}`)}
				return insertRow(ctx, tx, &row, &row.ID)
			},
			Duplicates: normDuplicates(`
				SELECT id, display_name AS name FROM catalog_character WHERE deleted_at IS NULL AND display_name_norm = lower(normalize(?, NFKC))
				UNION SELECT c.id, c.display_name FROM catalog_character_alias a JOIN catalog_character c ON c.id = a.character_id
				 WHERE c.deleted_at IS NULL AND a.name_norm = lower(normalize(?, NFKC))`, FieldCharacterDisplayName),
		},
		TypeLabel: {
			Required: []string{FieldLabelName},
			Create: func(ctx context.Context, tx *gorm.DB, _, patch map[string]any) (int64, error) {
				row := catmodel.CatalogLabel{DisplayName: birthText(patch, FieldLabelName)}
				return insertRow(ctx, tx, &row, &row.ID)
			},
			Duplicates: normDuplicates(`
				SELECT id, display_name AS name FROM catalog_label WHERE deleted_at IS NULL AND display_name_norm = lower(normalize(?, NFKC))
				UNION SELECT l.id, l.display_name FROM catalog_label_alias a JOIN catalog_label l ON l.id = a.label_id
				 WHERE l.deleted_at IS NULL AND a.name_norm = lower(normalize(?, NFKC))`, FieldLabelName),
		},
		TypeCreditName: {
			Required: []string{FieldCreditNameName},
			Create: func(ctx context.Context, tx *gorm.DB, _, patch map[string]any) (int64, error) {
				row := catmodel.CatalogCreditName{Name: birthText(patch, FieldCreditNameName)}
				return insertRow(ctx, tx, &row, &row.ID)
			},
			Duplicates: normDuplicates(`
				SELECT id, name FROM catalog_credit_name WHERE name_norm = lower(normalize(?, NFKC))
				UNION SELECT cn.id, cn.name FROM catalog_name_alias a JOIN catalog_credit_name cn ON cn.id = a.credit_name_id
				 WHERE a.name_norm = lower(normalize(?, NFKC))`, FieldCreditNameName),
		},
		TypeEngine: {
			Required: []string{FieldEngineName},
			Check: func(ctx context.Context, db *gorm.DB, _, patch map[string]any) error {
				var n int64
				if err := db.WithContext(ctx).Model(&catmodel.CatalogEngine{}).
					Where("lower(name) = lower(?)", strings.TrimSpace(birthText(patch, FieldEngineName))).Count(&n).Error; err != nil {
					return err
				}
				if n > 0 {
					return &editing.ValidationError{Key: FieldEngineName, Reason: "an engine with this name already exists"}
				}
				return nil
			},
			Create: func(ctx context.Context, tx *gorm.DB, _, patch map[string]any) (int64, error) {
				row := catmodel.CatalogEngine{Name: birthText(patch, FieldEngineName), Aliases: datatypes.JSON(`[]`)}
				return insertRow(ctx, tx, &row, &row.ID)
			},
			Duplicates: normDuplicates(`SELECT id, name FROM catalog_engine WHERE lower(name) = lower(?) OR lower(name) = lower(?)`, FieldEngineName),
		},
		TypeSeries: {
			Required: []string{FieldSeriesName},
			Create: func(ctx context.Context, tx *gorm.DB, _, patch map[string]any) (int64, error) {
				row := catmodel.CatalogSeries{
					DisplayName: birthText(patch, FieldSeriesName),
					SourceID:    curatedSourceID,
					ExternalID:  "editor:" + uuid.NewString(),
				}
				return insertRow(ctx, tx, &row, &row.ID)
			},
			Duplicates: normDuplicates(`SELECT id, display_name AS name FROM catalog_series WHERE lower(normalize(display_name, NFKC)) = lower(normalize(?, NFKC))
				OR lower(normalize(display_name, NFKC)) = lower(normalize(?, NFKC))`, FieldSeriesName),
		},
		TypeRelease: {
			Params:   []editing.BirthParam{{Key: BirthParamWorkID, Kind: editing.KindRef, Required: true}},
			Required: []string{FieldReleaseKind},
			Check: func(ctx context.Context, db *gorm.DB, params, _ map[string]any) error {
				workID, err := birthRef(params, BirthParamWorkID)
				if err != nil {
					return err
				}
				if err := assertWorkExists(ctx, db, workID); err != nil {
					if errors.Is(err, editing.ErrEntityNotFound) {
						return &editing.ValidationError{Key: editing.CreateFieldKey, Reason: fmt.Sprintf("work %d does not exist", workID)}
					}
					return err
				}
				return nil
			},
			Create: func(ctx context.Context, tx *gorm.DB, params, patch map[string]any) (int64, error) {
				workID, err := birthRef(params, BirthParamWorkID)
				if err != nil {
					return 0, err
				}
				kind, ok := birthInt(patch[FieldReleaseKind])
				if !ok {
					return 0, &editing.ValidationError{Key: FieldReleaseKind, Reason: "required to create the entry"}
				}
				row := catmodel.CatalogRelease{WorkID: workID, Kind: int16(kind), Extra: datatypes.JSON(`{}`)}
				return insertRow(ctx, tx, &row, &row.ID)
			},
			Duplicates: func(ctx context.Context, db *gorm.DB, params, patch map[string]any) ([]editing.DuplicateHint, error) {
				workID, err := birthRef(params, BirthParamWorkID)
				if err != nil {
					return nil, nil
				}
				title := strings.TrimSpace(birthText(patch, FieldReleaseTitle))
				if title == "" {
					return nil, nil
				}
				var hints []editing.DuplicateHint
				err = db.WithContext(ctx).Raw(`SELECT id, coalesce(title, '') AS name FROM catalog_release
					WHERE work_id = ? AND deleted_at IS NULL AND lower(normalize(coalesce(title, ''), NFKC)) = lower(normalize(?, NFKC))
					ORDER BY id LIMIT ?`, workID, title, maxDuplicateHints).Scan(&hints).Error
				return hints, err
			},
		},
		TypeWalkthrough: {
			Params:   []editing.BirthParam{{Key: BirthParamWorkID, Kind: editing.KindRef, Required: true}},
			Required: []string{FieldWalkthroughChart, FieldWalkthroughLang},
			Check: func(ctx context.Context, db *gorm.DB, params, _ map[string]any) error {
				workID, err := birthRef(params, BirthParamWorkID)
				if err != nil {
					return err
				}
				if err := assertWorkExists(ctx, db, workID); err != nil {
					if errors.Is(err, editing.ErrEntityNotFound) {
						return &editing.ValidationError{Key: editing.CreateFieldKey, Reason: fmt.Sprintf("work %d does not exist", workID)}
					}
					return err
				}
				return nil
			},
			Create: func(ctx context.Context, tx *gorm.DB, params, _ map[string]any) (int64, error) {
				workID, err := birthRef(params, BirthParamWorkID)
				if err != nil {
					return 0, err
				}
				row := catmodel.CatalogWalkthrough{
					WorkID: workID, SourceID: catmodel.SourceNextMoe, SourceURL: catmodel.WalkthroughNativeURLPrefix + uuid.NewString(),
					ChartVersion: walkthrough.ChartVersion, Chart: datatypes.JSON(`{}`),
				}
				return insertRow(ctx, tx, &row, &row.ID)
			},
		},
		TypePerson: {
			Params: []editing.BirthParam{{Key: BirthParamCreditNameID, Kind: editing.KindRef, Required: true}},
			Check: func(ctx context.Context, db *gorm.DB, params, _ map[string]any) error {
				cnID, err := birthRef(params, BirthParamCreditNameID)
				if err != nil {
					return err
				}
				var cn catmodel.CatalogCreditName
				err = db.WithContext(ctx).Select("id", "person_id").First(&cn, cnID).Error
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return &editing.ValidationError{Key: editing.CreateFieldKey, Reason: fmt.Sprintf("credit name %d does not exist", cnID)}
				}
				if err != nil {
					return err
				}
				if cn.PersonID != nil {
					return &editing.ValidationError{Key: editing.CreateFieldKey,
						Reason: fmt.Sprintf("credit name %d already belongs to person %d; a person is born from an unattributed name", cnID, *cn.PersonID)}
				}
				return nil
			},
			Create: func(ctx context.Context, tx *gorm.DB, params, _ map[string]any) (int64, error) {
				cnID, err := birthRef(params, BirthParamCreditNameID)
				if err != nil {
					return 0, err
				}
				var cn catmodel.CatalogCreditName
				if err := tx.WithContext(ctx).Select("id", "name").First(&cn, cnID).Error; err != nil {
					return 0, err
				}
				row := catmodel.CatalogPerson{DisplayName: cn.Name, PrimaryCreditNameID: &cn.ID}
				if _, err := insertRow(ctx, tx, &row, &row.ID); err != nil {
					return 0, err
				}
				res := tx.WithContext(ctx).Model(&catmodel.CatalogCreditName{}).
					Where("id = ? AND person_id IS NULL", cnID).Update("person_id", row.ID)
				if res.Error != nil {
					return 0, res.Error
				}
				if res.RowsAffected != 1 {
					return 0, &editing.ValidationError{Key: editing.CreateFieldKey, Reason: fmt.Sprintf("credit name %d was attributed meanwhile", cnID)}
				}
				return row.ID, nil
			},
		},
		TypeTag: {
			Required: []string{FieldTagName},
			Policy:   vocabularyCreatePolicy(),
			Check: func(ctx context.Context, db *gorm.DB, _, patch map[string]any) error {
				var clash []int64
				if err := db.WithContext(ctx).Raw(`SELECT id FROM catalog_tag
					WHERE lower(normalize(name, NFKC)) = lower(normalize(?, NFKC)) ORDER BY id LIMIT 1`,
					strings.TrimSpace(birthText(patch, FieldTagName))).Scan(&clash).Error; err != nil {
					return err
				}
				if len(clash) > 0 {
					return &editing.ValidationError{Key: FieldTagName, Reason: fmt.Sprintf("tag %d already carries this name", clash[0])}
				}
				return nil
			},
			Create: func(ctx context.Context, tx *gorm.DB, _, patch map[string]any) (int64, error) {
				row := catmodel.CatalogTag{Name: strings.TrimSpace(birthText(patch, FieldTagName))}
				return insertRow(ctx, tx, &row, &row.ID)
			},
			Duplicates: normDuplicates(`SELECT id, name FROM catalog_tag WHERE lower(normalize(name, NFKC)) = lower(normalize(?, NFKC))
				OR lower(normalize(name, NFKC)) = lower(normalize(?, NFKC))`, FieldTagName),
		},
		TypeTrait: {
			Required: []string{FieldTraitName},
			Policy:   vocabularyCreatePolicy(),
			Check: func(ctx context.Context, db *gorm.DB, _, patch map[string]any) error {
				groupTID := ""
				if raw, ok := patch[FieldTraitGroup]; ok && raw != nil {
					gid, ok := birthInt(raw)
					if !ok {
						return &editing.ValidationError{Key: FieldTraitGroup, Reason: "must be a trait id or null"}
					}
					var g catmodel.CatalogCharacterTrait
					err := db.WithContext(ctx).Select("id", "vndb_tid").First(&g, gid).Error
					if errors.Is(err, gorm.ErrRecordNotFound) {
						return &editing.ValidationError{Key: FieldTraitGroup, Reason: fmt.Sprintf("trait %d does not exist", gid)}
					}
					if err != nil {
						return err
					}
					groupTID = g.VndbTID
				}
				var clash []int64
				if err := db.WithContext(ctx).Raw(`SELECT id FROM catalog_character_trait
					WHERE group_tid = ? AND lower(normalize(name, NFKC)) = lower(normalize(?, NFKC)) ORDER BY id LIMIT 1`,
					groupTID, strings.TrimSpace(birthText(patch, FieldTraitName))).Scan(&clash).Error; err != nil {
					return err
				}
				if len(clash) > 0 {
					return &editing.ValidationError{Key: FieldTraitName, Reason: fmt.Sprintf("trait %d already carries this name in the same group", clash[0])}
				}
				return nil
			},
			Create: func(ctx context.Context, tx *gorm.DB, _, patch map[string]any) (int64, error) {
				row := catmodel.CatalogCharacterTrait{
					VndbTID: "editor:" + uuid.NewString(),
					Name:    strings.TrimSpace(birthText(patch, FieldTraitName)),
				}
				return insertRow(ctx, tx, &row, &row.ID)
			},
			Duplicates: normDuplicates(`SELECT id, name FROM catalog_character_trait WHERE lower(normalize(name, NFKC)) = lower(normalize(?, NFKC))
				OR lower(normalize(name_zh, NFKC)) = lower(normalize(?, NFKC))`, FieldTraitName),
		},
	}
	for entityType, b := range births {
		if err := reg.AttachBirth(entityType, b); err != nil {
			return err
		}
	}
	return nil
}

// A new tag or trait changes what every list can filter by, so only reviewers
// may propose one, and a second reviewer applies it.
func vocabularyCreatePolicy() *editing.Policy {
	return &editing.Policy{
		Propose:   editing.ProposePerm(string(perm.EditTaxonomyReview)),
		Review:    editing.ReviewPerm(string(perm.EditTaxonomyReview)),
		Automerge: editing.AutomergeNever,
	}
}

func insertRow(ctx context.Context, tx *gorm.DB, row any, id *int64) (int64, error) {
	if err := tx.WithContext(ctx).Create(row).Error; err != nil {
		return 0, err
	}
	if *id == 0 {
		return 0, fmt.Errorf("editspec: birth insert returned no id")
	}
	return *id, nil
}

func birthText(patch map[string]any, key string) string {
	s, _ := patch[key].(string)
	return s
}

func birthInt(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		if n != float64(int64(n)) {
			return 0, false
		}
		return int64(n), true
	case int64:
		return n, true
	case int:
		return int64(n), true
	case string:
		id, err := strconv.ParseInt(n, 10, 64)
		return id, err == nil
	}
	return 0, false
}

func birthRef(params map[string]any, key string) (int64, error) {
	id, ok := birthInt(params[key])
	if !ok || id <= 0 {
		return 0, &editing.ValidationError{Key: editing.CreateFieldKey, Reason: fmt.Sprintf("param %q must be a catalog id", key)}
	}
	return id, nil
}

func normDuplicates(query, nameKey string) func(context.Context, *gorm.DB, map[string]any, map[string]any) ([]editing.DuplicateHint, error) {
	return func(ctx context.Context, db *gorm.DB, _ map[string]any, patch map[string]any) ([]editing.DuplicateHint, error) {
		name := strings.TrimSpace(birthText(patch, nameKey))
		if name == "" {
			return nil, nil
		}
		var hints []editing.DuplicateHint
		err := db.WithContext(ctx).Raw(`SELECT DISTINCT ON (id) id, name FROM (`+query+`) d ORDER BY id LIMIT ?`,
			name, name, maxDuplicateHints).Scan(&hints).Error
		return hints, err
	}
}
