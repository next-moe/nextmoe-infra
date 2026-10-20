package editspec

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	catmodel "api/internal/platform/catalog/model"
	"api/internal/platform/editing"

	"gorm.io/gorm"
)

const (
	FieldWorkLabelsSuppr      = FieldWorkLabels + editing.SuppressedFieldSuffix
	FieldWorkEngineIDsSuppr   = FieldWorkEngineIDs + editing.SuppressedFieldSuffix
	FieldWorkSeriesIDsSuppr   = FieldWorkSeriesIDs + editing.SuppressedFieldSuffix
	FieldWorkLinksSuppr       = FieldWorkLinks + editing.SuppressedFieldSuffix
	FieldWorkCoversSuppr      = FieldWorkCovers + editing.SuppressedFieldSuffix
	FieldWorkScreenshotsSuppr = FieldWorkScreenshots + editing.SuppressedFieldSuffix
	FieldWorkIntrosSuppr      = FieldWorkIntros + editing.SuppressedFieldSuffix
	FieldLabelLinksSuppr      = FieldLabelLinks + editing.SuppressedFieldSuffix
	FieldLabelIntrosSuppr     = FieldLabelIntros + editing.SuppressedFieldSuffix
	FieldCharacterIntrosSuppr = FieldCharacterIntros + editing.SuppressedFieldSuffix
)

// Sized at roughly twice the production per-entity maximum of the parent table
// (census 2026-10-19: labels 14, engines 5, series 3, work links 47, label links
// 21, covers 104, screenshots 79, work intros 7, character intros 6, label
// intros 4). A cap below the real row count would make some upstream rows
// impossible to hide.
const (
	maxWorkLabelSuppress      = 40
	maxWorkEngineSuppress     = 20
	maxWorkSeriesSuppress     = 20
	maxWorkLinkSuppress       = 100
	maxLabelLinkSuppress      = 50
	maxWorkCoverSuppress      = 200
	maxWorkScreenshotSuppress = 200
	maxIntroSuppress          = 30
)

type upstreamList struct {
	field         string
	identity      editing.IdentitySpec
	maxSuppressed int
	guard         func(ctx context.Context, tx *gorm.DB, entityID int64, keys []string) error
}

var upstreamLists = map[string][]upstreamList{
	TypeWork: {
		{field: FieldWorkLabels, maxSuppressed: maxWorkLabelSuppress, identity: editing.IdentitySpec{
			Segments: 3, KeyCheck: numericKeyCheck("label", 2),
			Refs: []editing.IdentityRef{{Segment: 2, EntityTag: TagLabel}},
		}},
		{field: FieldWorkEngineIDs, maxSuppressed: maxWorkEngineSuppress, identity: editing.IdentitySpec{
			Segments: 2, KeyCheck: numericKeyCheck("engine", 1),
		}},
		{field: FieldWorkSeriesIDs, maxSuppressed: maxWorkSeriesSuppress, identity: editing.IdentitySpec{
			Segments: 3, TrailingText: true, KeyCheck: sourceTextKeyCheck("series"),
		}},
		{field: FieldWorkLinks, maxSuppressed: maxWorkLinkSuppress, identity: editing.IdentitySpec{
			Segments: 3, TrailingText: true, KeyCheck: sourceTextKeyCheck("link"),
		}, guard: refuseAnchorSuppression(catmodel.EntityTypeWork)},
		{field: FieldWorkCovers, maxSuppressed: maxWorkCoverSuppress, identity: editing.IdentitySpec{
			Segments: 2, KeyCheck: imageKeyCheck("cover"),
		}},
		{field: FieldWorkScreenshots, maxSuppressed: maxWorkScreenshotSuppress, identity: editing.IdentitySpec{
			Segments: 2, KeyCheck: imageKeyCheck("screenshot"),
		}},
		{field: FieldWorkIntros, maxSuppressed: maxIntroSuppress, identity: introIdentitySpec()},
	},
	TypeLabel: {
		{field: FieldLabelLinks, maxSuppressed: maxLabelLinkSuppress, identity: editing.IdentitySpec{
			Segments: 3, TrailingText: true, KeyCheck: sourceTextKeyCheck("link"),
		}, guard: refuseAnchorSuppression(catmodel.EntityTypeLabel)},
		{field: FieldLabelIntros, maxSuppressed: maxIntroSuppress, identity: introIdentitySpec()},
	},
	TypeCharacter: {
		{field: FieldCharacterIntros, maxSuppressed: maxIntroSuppress, identity: introIdentitySpec()},
	},
}

func introIdentitySpec() editing.IdentitySpec {
	return editing.IdentitySpec{Segments: 3, KeyCheck: introKeyCheck}
}

// withUpstreamRows gives a registration the `.suppressed` companion of every
// list field in upstreamLists, and the companion's snapshot value. Wrapping the
// spec keeps each family's own field list free of the bookkeeping, and a field
// named in upstreamLists that the family does not register is a bug that
// TestUpstreamListsNameRegisteredFields catches.
func withUpstreamRows(db *gorm.DB, spec editing.EntityTypeSpec) editing.EntityTypeSpec {
	lists := upstreamLists[spec.Type]
	if len(lists) == 0 {
		return spec
	}
	byField := make(map[string]upstreamList, len(lists))
	for _, l := range lists {
		byField[l.field] = l
	}
	fields := make([]editing.FieldSpec, 0, len(spec.Fields)+len(lists))
	for _, f := range spec.Fields {
		l, ok := byField[f.Key]
		if !ok {
			fields = append(fields, f)
			continue
		}
		identity := l.identity
		f.Identity = &identity
		f.MaxSuppressed = l.maxSuppressed
		companion := editing.SuppressedFieldSpec(spec.Type, f)
		if l.guard != nil {
			companion.Apply = guardedSuppression(companion, l)
		}
		fields = append(fields, f, companion)
	}
	spec.Fields = fields

	load := spec.LoadSnapshot
	entityType := spec.Type
	spec.LoadSnapshot = func(ctx context.Context, entityID int64) (map[string]any, error) {
		snap, err := load(ctx, entityID)
		if err != nil {
			return nil, err
		}
		for _, l := range lists {
			v, err := editing.LoadSuppressedValue(ctx, db, entityType, entityID, l.field)
			if err != nil {
				return nil, err
			}
			snap[editing.SuppressedFieldKey(l.field)] = v
		}
		return snap, nil
	}
	return spec
}

func guardedSuppression(companion editing.FieldSpec, l upstreamList) editing.ApplyFunc {
	inner := companion.Apply
	limit := companion.MaxSuppressed
	return func(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
		keys, err := editing.ParseSuppressedKeys(value, limit)
		if err != nil {
			return &editing.ValidationError{Key: companion.Key, Reason: err.Error()}
		}
		if err := l.guard(ctx, tx, entityID, keys); err != nil {
			return err
		}
		return inner(ctx, tx, entityID, value)
	}
}

// An exact link is the entity's identity anchor at that source: importers match
// on it and the work face derives its upstream ids from it. Hiding one from the
// link faces would leave the anchor working and invisible, so a wrong anchor is
// corrected through the match review queue instead.
func refuseAnchorSuppression(entityType int16) func(context.Context, *gorm.DB, int64, []string) error {
	return func(ctx context.Context, tx *gorm.DB, entityID int64, keys []string) error {
		if len(keys) == 0 {
			return nil
		}
		var anchors []string
		if err := tx.WithContext(ctx).Raw(`
			SELECT `+ExternalRefIdentitySQL("r")+` FROM catalog_external_ref r
			WHERE r.entity_type = ? AND r.entity_id = ? AND r.link_kind = ?
			  AND `+ExternalRefIdentitySQL("r")+` IN ?`,
			entityType, entityID, catmodel.LinkKindExact, keys).Scan(&anchors).Error; err != nil {
			return err
		}
		if len(anchors) == 0 {
			return nil
		}
		return &editing.ValidationError{
			Key: linksFieldKey(entityType) + editing.SuppressedFieldSuffix,
			Reason: fmt.Sprintf("%s is this entity's identity anchor at its source; "+
				"anchors are corrected through the match review queue, not hidden", anchors[0]),
		}
	}
}

func identityParts(key, prefix string, segments int) ([]string, error) {
	parts := strings.SplitN(key, ":", segments)
	if len(parts) != segments || parts[0] != prefix {
		return nil, fmt.Errorf("%q is not an identity key of this field: expected %s:… with %d segments", key, prefix, segments)
	}
	return parts, nil
}

func numericKeyCheck(prefix string, ids int) func(string) error {
	return func(key string) error {
		parts, err := identityParts(key, prefix, ids+1)
		if err != nil {
			return err
		}
		for _, p := range parts[1:] {
			if _, err := strconv.ParseInt(p, 10, 64); err != nil {
				return fmt.Errorf("%q is not an identity key of this field: %q is not an integer", key, p)
			}
		}
		return nil
	}
}

func sourceTextKeyCheck(prefix string) func(string) error {
	return func(key string) error {
		parts, err := identityParts(key, prefix, 3)
		if err != nil {
			return err
		}
		if _, err := strconv.ParseInt(parts[1], 10, 16); err != nil {
			return fmt.Errorf("%q is not an identity key of this field: %q is not a source id", key, parts[1])
		}
		if parts[2] == "" {
			return fmt.Errorf("%q is not an identity key of this field: the external id is empty", key)
		}
		return nil
	}
}

var reImageHashKey = regexp.MustCompile(`^[0-9A-Za-z]{16,128}$`)

func imageKeyCheck(prefix string) func(string) error {
	return func(key string) error {
		parts, err := identityParts(key, prefix, 2)
		if err != nil {
			return err
		}
		if !reImageHashKey.MatchString(parts[1]) {
			return fmt.Errorf("%q is not an identity key of this field: %q is not an image hash", key, parts[1])
		}
		return nil
	}
}

var reIntroLang = regexp.MustCompile(`^[A-Za-z-]{0,16}$`)

func introKeyCheck(key string) error {
	parts, err := identityParts(key, "intro", 3)
	if err != nil {
		return err
	}
	if _, err := strconv.ParseInt(parts[1], 10, 16); err != nil {
		return fmt.Errorf("%q is not an identity key of this field: %q is not a source id", key, parts[1])
	}
	if !reIntroLang.MatchString(parts[2]) {
		return fmt.Errorf("%q is not an identity key of this field: %q is not a language code", key, parts[2])
	}
	return nil
}

func WorkLabelIdentity(labelID int64, kind int16) string {
	return fmt.Sprintf("label:%d:%d", labelID, kind)
}

func WorkLabelIdentitySQL(alias string) string {
	return fmt.Sprintf(`('label:' || %[1]s.label_id || ':' || %[1]s.kind)`, alias)
}

func WorkEngineIdentity(engineID int64) string { return fmt.Sprintf("engine:%d", engineID) }

func WorkEngineIdentitySQL(engineIDExpr string) string {
	return fmt.Sprintf(`('engine:' || %s)`, engineIDExpr)
}

// Series rows are rebuilt with new ids by workseries and derivedseries, so the
// key is the series' (source, external_id) anchor, never catalog_series.id.
func SeriesIdentity(sourceID int16, externalID string) string {
	return fmt.Sprintf("series:%d:%s", sourceID, externalID)
}

func SeriesIdentitySQL(seriesAlias string) string {
	return fmt.Sprintf(`('series:' || %[1]s.source_id || ':' || %[1]s.external_id)`, seriesAlias)
}

func ExternalRefIdentity(sourceID int16, externalID string) string {
	return fmt.Sprintf("link:%d:%s", sourceID, externalID)
}

func ExternalRefIdentitySQL(alias string) string {
	return fmt.Sprintf(`('link:' || %[1]s.source_id || ':' || %[1]s.external_id)`, alias)
}

func CoverIdentity(imageHash string) string      { return "cover:" + imageHash }
func ScreenshotIdentity(imageHash string) string { return "screenshot:" + imageHash }

func CoverIdentitySQL(alias string) string { return fmt.Sprintf(`('cover:' || %s.image_hash)`, alias) }
func ScreenshotIdentitySQL(alias string) string {
	return fmt.Sprintf(`('screenshot:' || %s.image_hash)`, alias)
}

func IntroIdentity(sourceID int16, lang string) string {
	return fmt.Sprintf("intro:%d:%s", sourceID, lang)
}

func IntroIdentitySQL(alias string) string {
	return fmt.Sprintf(`('intro:' || %[1]s.source_id || ':' || %[1]s.lang)`, alias)
}

func notSuppressedSQL(entityType, fieldKey, entityIDExpr, identityExpr string) string {
	return fmt.Sprintf(`NOT EXISTS (SELECT 1 FROM edit_suppressed_row esr
		WHERE esr.entity_type = '%s' AND esr.field_key = '%s'
		  AND esr.entity_id = %s AND esr.identity_key = %s)`,
		entityType, fieldKey, entityIDExpr, identityExpr)
}

// NotSuppressedWorkLabelSQL is the read-path predicate, correlated on a
// catalog_work_label alias.
func NotSuppressedWorkLabelSQL(alias string) string {
	return notSuppressedSQL(TypeWork, FieldWorkLabels, alias+".work_id", WorkLabelIdentitySQL(alias))
}

func NotSuppressedWorkEngineSQL(workIDExpr, engineIDExpr string) string {
	return notSuppressedSQL(TypeWork, FieldWorkEngineIDs, workIDExpr, WorkEngineIdentitySQL(engineIDExpr))
}

// VisibleWorkEnginesSQL is catmodel.WorkEnginesSQL without the edges a person
// hid on the work. It covers the release-derived half of the union too: a work
// shows an engine through either path, and hiding it has to stop both.
var VisibleWorkEnginesSQL = `(SELECT ve.work_id, ve.engine_id FROM ` + catmodel.WorkEnginesSQL + ` ve WHERE ` +
	NotSuppressedWorkEngineSQL("ve.work_id", "ve.engine_id") + `)`

// NotSuppressedWorkSeriesSQL is correlated on a catalog_series_member alias.
func NotSuppressedWorkSeriesSQL(memberAlias string) string {
	return fmt.Sprintf(`NOT EXISTS (SELECT 1 FROM edit_suppressed_row esr
		JOIN catalog_series ss ON ss.id = %[1]s.series_id
		WHERE esr.entity_type = '%[2]s' AND esr.field_key = '%[3]s'
		  AND esr.entity_id = %[1]s.work_id AND esr.identity_key = %[4]s)`,
		memberAlias, TypeWork, FieldWorkSeriesIDs, SeriesIdentitySQL("ss"))
}

// NotSuppressedExternalRefSQL is correlated on a catalog_external_ref alias and
// is a no-op for every entity type without a links field.
func NotSuppressedExternalRefSQL(alias string) string {
	return fmt.Sprintf(`NOT EXISTS (SELECT 1 FROM edit_suppressed_row esr
		WHERE esr.entity_id = %[1]s.entity_id AND esr.identity_key = %[2]s
		  AND ((%[1]s.entity_type = %[3]d AND esr.entity_type = '%[4]s' AND esr.field_key = '%[5]s')
		    OR (%[1]s.entity_type = %[6]d AND esr.entity_type = '%[7]s' AND esr.field_key = '%[8]s')))`,
		alias, ExternalRefIdentitySQL(alias),
		catmodel.EntityTypeWork, TypeWork, FieldWorkLinks,
		catmodel.EntityTypeLabel, TypeLabel, FieldLabelLinks)
}

func NotSuppressedWorkCoverSQL(alias string) string {
	return notSuppressedSQL(TypeWork, FieldWorkCovers, alias+".work_id", CoverIdentitySQL(alias))
}

func NotSuppressedWorkScreenshotSQL(alias string) string {
	return notSuppressedSQL(TypeWork, FieldWorkScreenshots, alias+".work_id", ScreenshotIdentitySQL(alias))
}

func NotSuppressedWorkIntroSQL(alias string) string {
	return notSuppressedSQL(TypeWork, FieldWorkIntros, alias+".work_id", IntroIdentitySQL(alias))
}

func NotSuppressedCharacterIntroSQL(alias string) string {
	return notSuppressedSQL(TypeCharacter, FieldCharacterIntros, alias+".character_id", IntroIdentitySQL(alias))
}

func NotSuppressedLabelIntroSQL(alias string) string {
	return notSuppressedSQL(TypeLabel, FieldLabelIntros, alias+".label_id", IntroIdentitySQL(alias))
}

// NotSuppressedIntroTableSQL is the predicate for an intro table referenced by
// its own name, and TRUE for an intro table with no suppressible field.
func NotSuppressedIntroTableSQL(table string) string {
	switch table {
	case catmodel.CatalogWorkIntro{}.TableName():
		return NotSuppressedWorkIntroSQL(table)
	case catmodel.CatalogCharacterIntro{}.TableName():
		return NotSuppressedCharacterIntroSQL(table)
	case catmodel.CatalogLabelIntro{}.TableName():
		return NotSuppressedLabelIntroSQL(table)
	}
	return "TRUE"
}
