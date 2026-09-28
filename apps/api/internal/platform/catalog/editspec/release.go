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
	TypeRelease = "catalog.release"

	FieldReleaseKind     = "catalog.release.kind"
	FieldReleaseTitle    = "catalog.release.title"
	FieldReleaseLang     = "catalog.release.lang"
	FieldReleasePlatform = "catalog.release.platform"
	FieldReleaseEngine   = "catalog.release.engine_id"
	FieldReleaseReleased = "catalog.release.released"
	FieldReleaseHidden   = "catalog.release.hidden"
)

var releaseLangExtra = map[string]struct{}{
	"ck": {}, "et": {}, "eu": {}, "fa": {}, "gl": {}, "kk": {},
}

var releasePlatformAllowed = map[string]struct{}{}

func init() {
	for _, code := range []string{
		"and", "bdp", "dos", "drc", "dvd", "fm7", "fmt", "gba", "gbc", "ios",
		"lin", "mac", "mob", "msx", "n3d", "nds", "nes", "oth", "p88", "p98",
		"pce", "pcf", "ps1", "ps2", "ps3", "ps4", "ps5", "psp", "psv", "sat",
		"scd", "sfc", "smd", "sw2", "swi", "tdo", "vnd", "web", "wii", "win",
		"wiu", "x1s", "x68", "xb1", "xb3", "xbo", "xxs",
	} {
		releasePlatformAllowed[code] = struct{}{}
	}
}

func RegisterRelease(reg *editing.Registry, db *gorm.DB) error {
	letmoePolicy := editing.Policy{
		Propose:   editing.ProposeTrusted,
		Review:    editing.ReviewPerm(string(perm.EditReleaseReview)),
		Automerge: editing.AutomergeOwner,
	}
	kungalPolicy := editing.Policy{
		Propose:     editing.ProposeOpen,
		Review:      editing.ReviewPerm(string(perm.EditReleaseReview)),
		Automerge:   editing.AutomergeReview,
		OwnerReview: true,
	}
	fields := releaseFieldSpecs()
	overlays := make(map[string]map[string]editing.Policy, len(letmoeSites)+1)
	for _, site := range letmoeSites {
		overlay := make(map[string]editing.Policy, len(fields))
		for _, f := range fields {
			overlay[f.Key] = letmoePolicy
		}
		overlays[site] = overlay
	}
	kungalOverlay := make(map[string]editing.Policy, len(fields))
	for _, f := range fields {
		kungalOverlay[f.Key] = kungalPolicy
	}
	overlays["kungal"] = kungalOverlay

	return reg.Register(editing.EntityTypeSpec{
		Family:       "catalog",
		Type:         TypeRelease,
		LoadSnapshot: loadReleaseSnapshot(db),
		Txn:          txnOn(db),
		OwnerSite: func(ctx context.Context, entityID int64) (*string, error) {
			w, err := parentWorkOfRelease(ctx, db, entityID, "id", "site")
			if err != nil {
				return nil, err
			}
			return w.Site, nil
		},
		OwnerUserID: func(ctx context.Context, entityID int64) (*int64, error) {
			w, err := parentWorkOfRelease(ctx, db, entityID, "id", "owner_user_id")
			if err != nil {
				return nil, err
			}
			return w.OwnerUserID, nil
		},
		DefaultPolicy: editing.Policy{
			Propose:   editing.ProposePerm(string(perm.EditRelease)),
			Review:    editing.ReviewPerm(string(perm.EditReleaseReview)),
			Automerge: editing.AutomergeNever,
		},
		SiteOverlays: overlays,
		Fields:       fields,
		OnMerge:      buildReleaseOnMerge(db),
	})
}

func releaseFieldSpecs() []editing.FieldSpec {
	return []editing.FieldSpec{
		{
			Key: FieldReleaseKind, Kind: editing.KindEnum, DiffHint: editing.DiffHintInline,
			Value:      &editing.ValueSpec{Vocabulary: "release_kind", Coded: true},
			Validate:   validateReleaseKind,
			Apply:      applyReleaseColumn("kind", asReleaseKind),
			Provenance: releaseProvenance("kind"),
		},
		{
			Key: FieldReleaseTitle, Kind: editing.KindText, DiffHint: editing.DiffHintInline,
			Value:    &editing.ValueSpec{Nullable: true},
			Validate: validateReleaseTitle,
			Apply: applyReleaseColumn("title", func(v any) (any, error) {
				if v == nil {
					return nil, nil
				}
				return asString(v)
			}),
			Provenance: releaseProvenance("title"),
		},
		{
			Key: FieldReleaseLang, Kind: editing.KindEnum, DiffHint: editing.DiffHintInline,
			Value:    &editing.ValueSpec{Vocabulary: "release_lang", Nullable: true},
			Validate: validateReleaseLang,
			Apply: applyReleaseColumn("lang", func(v any) (any, error) {
				if v == nil {
					return nil, nil
				}
				return asString(v)
			}),
			Provenance: releaseProvenance("lang"),
		},
		{
			Key: FieldReleasePlatform, Kind: editing.KindEnum, DiffHint: editing.DiffHintInline,
			Value:    &editing.ValueSpec{Vocabulary: "platform", Nullable: true},
			Validate: validateReleasePlatform,
			Apply: applyReleaseColumn("platform", func(v any) (any, error) {
				if v == nil {
					return nil, nil
				}
				return asString(v)
			}),
			Provenance: releaseProvenance("platform"),
		},
		{
			Key: FieldReleaseEngine, Kind: editing.KindRef, DiffHint: editing.DiffHintInline,
			Value:      &editing.ValueSpec{Nullable: true},
			Validate:   validateReleaseEngine,
			Apply:      applyReleaseEngine,
			Provenance: releaseProvenance("engine_id"),
		},
		{
			Key: FieldReleaseReleased, Kind: editing.KindDate, DiffHint: editing.DiffHintInline,
			Value:    &editing.ValueSpec{Nullable: true},
			Validate: validateReleased,
			Apply:    applyReleased,
			Provenance: []editing.ProvenanceTarget{
				{Table: catmodel.CatalogRelease{}.TableName(), Column: "released_y"},
				{Table: catmodel.CatalogRelease{}.TableName(), Column: "released_m"},
				{Table: catmodel.CatalogRelease{}.TableName(), Column: "released_d"},
			},
		},
		{
			Key: FieldReleaseHidden, Kind: editing.KindEnum, DiffHint: editing.DiffHintInline,
			Validate:   validateBool,
			Apply:      applyHidden,
			Provenance: releaseProvenance("deleted_at"),
		},
	}
}

func releaseProvenance(column string) []editing.ProvenanceTarget {
	return []editing.ProvenanceTarget{{Table: catmodel.CatalogRelease{}.TableName(), Column: column}}
}

func releaseLangOK(s string) bool {
	if _, ok := olangAllowed[s]; ok {
		return true
	}
	_, ok := releaseLangExtra[s]
	return ok
}

func validateReleaseKind(v any) error {
	f, ok := v.(float64)
	if !ok || f != float64(int64(f)) {
		return fmt.Errorf("must be an integer")
	}
	switch int16(f) {
	case catmodel.ReleaseKindDefault, catmodel.ReleaseKindDigital,
		catmodel.ReleaseKindPhysical, catmodel.ReleaseKindTrial, catmodel.ReleaseKindPatch:
		return nil
	}
	return fmt.Errorf("must be 0 (default), 1 (digital), 2 (physical), 3 (trial) or 4 (patch)")
}

func asReleaseKind(v any) (any, error) {
	if err := validateReleaseKind(v); err != nil {
		return nil, err
	}
	return int16(v.(float64)), nil
}

func validateReleaseTitle(v any) error {
	if v == nil {
		return nil
	}
	return validateBoundedText(v, maxNameRunes, true)
}

func validateReleaseLang(v any) error {
	if v == nil {
		return nil
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("must be a string or null")
	}
	if !releaseLangOK(s) {
		return fmt.Errorf("%q is not an allowed language", s)
	}
	return nil
}

func validateReleasePlatform(v any) error {
	if v == nil {
		return nil
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("must be a string or null")
	}
	if _, ok := releasePlatformAllowed[s]; !ok {
		return fmt.Errorf("%q is not an allowed platform", s)
	}
	return nil
}

func parseReleaseEngine(v any) (*int64, error) {
	if v == nil {
		return nil, nil
	}
	f, ok := v.(float64)
	if !ok || f != float64(int64(f)) || f <= 0 {
		return nil, fmt.Errorf("must be a positive integer engine id or null")
	}
	id := int64(f)
	return &id, nil
}

func validateReleaseEngine(v any) error {
	_, err := parseReleaseEngine(v)
	return err
}

func applyReleaseEngine(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	id, err := parseReleaseEngine(value)
	if err != nil {
		return fmt.Errorf("editspec: engine_id: %w", err)
	}
	if id != nil {
		if err := assertEntitiesExist(ctx, tx, &catmodel.CatalogEngine{}, []int64{*id}, FieldReleaseEngine); err != nil {
			return err
		}
	}
	return applyReleaseColumn("engine_id", func(any) (any, error) { return id, nil })(ctx, tx, entityID, value)
}

func validateReleased(v any) error {
	_, _, _, err := parseReleased(v)
	return err
}

func parseReleased(v any) (y, m, d *int16, err error) {
	if v == nil {
		return nil, nil, nil, nil
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, nil, nil, fmt.Errorf("must be a date object or null")
	}
	for k := range obj {
		switch k {
		case "y", "m", "d":
		default:
			return nil, nil, nil, fmt.Errorf("unknown key %q", k)
		}
	}
	rawY, hasY := obj["y"]
	if !hasY || rawY == nil {
		return nil, nil, nil, fmt.Errorf("y is required")
	}
	yn, err := parseDatePart(rawY, 1950, 2200, "y")
	if err != nil {
		return nil, nil, nil, err
	}
	rawM, hasM := obj["m"]
	if hasM && rawM != nil {
		mn, err := parseDatePart(rawM, 1, 12, "m")
		if err != nil {
			return nil, nil, nil, err
		}
		m = &mn
	}
	rawD, hasD := obj["d"]
	if hasD && rawD != nil {
		if m == nil {
			return nil, nil, nil, fmt.Errorf("d requires m")
		}
		dn, err := parseDatePart(rawD, 1, 31, "d")
		if err != nil {
			return nil, nil, nil, err
		}
		d = &dn
	}
	y = &yn
	return y, m, d, nil
}

func parseDatePart(v any, min, max int16, name string) (int16, error) {
	f, ok := v.(float64)
	if !ok || f != float64(int64(f)) {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	n := int16(int64(f))
	if n < min || n > max {
		return 0, fmt.Errorf("%s must be between %d and %d", name, min, max)
	}
	return n, nil
}

func applyReleased(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	y, m, d, err := parseReleased(value)
	if err != nil {
		return fmt.Errorf("editspec: released: %w", err)
	}
	res := tx.WithContext(ctx).Exec(
		`UPDATE catalog_release SET released_y = ?, released_m = ?, released_d = ? WHERE id = ?`,
		y, m, d, entityID)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return editing.ErrEntityNotFound
	}
	return nil
}

func applyHidden(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	v, err := asBool(value)
	if err != nil {
		return fmt.Errorf("editspec: hidden: %w", err)
	}
	res := tx.WithContext(ctx).Exec(
		`UPDATE catalog_release SET deleted_at = CASE WHEN ?::bool THEN COALESCE(deleted_at, now()) ELSE NULL END WHERE id = ?`,
		v, entityID)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return editing.ErrEntityNotFound
	}
	return nil
}

func applyReleaseColumn(column string, conv func(any) (any, error)) editing.ApplyFunc {
	return func(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
		v, err := conv(value)
		if err != nil {
			return fmt.Errorf("editspec: %s: %w", column, err)
		}
		res := tx.WithContext(ctx).Unscoped().Model(&catmodel.CatalogRelease{}).
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

func loadReleaseSnapshot(db *gorm.DB) func(context.Context, int64) (map[string]any, error) {
	return func(ctx context.Context, entityID int64) (map[string]any, error) {
		var r catmodel.CatalogRelease
		err := db.WithContext(ctx).Unscoped().
			Select("id", "kind", "title", "lang", "platform", "engine_id", "released_y", "released_m", "released_d", "deleted_at").
			First(&r, entityID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, editing.ErrEntityNotFound
		}
		if err != nil {
			return nil, err
		}
		return map[string]any{
			FieldReleaseKind:     int64(r.Kind),
			FieldReleaseTitle:    nullableString(r.Title),
			FieldReleaseLang:     nullableString(r.Lang),
			FieldReleasePlatform: nullableString(r.Platform),
			FieldReleaseEngine:   nullableInt64(r.EngineID),
			FieldReleaseReleased: releasedSnapshot(r.ReleasedY, r.ReleasedM, r.ReleasedD),
			FieldReleaseHidden:   r.DeletedAt.Valid,
		}, nil
	}
}

func releasedSnapshot(y, m, d *int16) any {
	if y == nil {
		return nil
	}
	out := map[string]any{"y": int64(*y)}
	if m != nil {
		out["m"] = int64(*m)
	}
	if d != nil {
		out["d"] = int64(*d)
	}
	return out
}

func parentWorkOfRelease(ctx context.Context, db *gorm.DB, releaseID int64, workColumns ...string) (*catmodel.CatalogWork, error) {
	var rel catmodel.CatalogRelease
	err := db.WithContext(ctx).Unscoped().Select("id", "work_id").First(&rel, releaseID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, editing.ErrEntityNotFound
	}
	if err != nil {
		return nil, err
	}
	var w catmodel.CatalogWork
	err = db.WithContext(ctx).Select(workColumns).First(&w, rel.WorkID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, editing.ErrEntityNotFound
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}
