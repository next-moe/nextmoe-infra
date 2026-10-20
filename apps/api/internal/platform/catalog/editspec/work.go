package editspec

import (
	"context"
	"errors"
	"fmt"
	"strings"

	catmodel "api/internal/platform/catalog/model"
	"api/internal/platform/catalog/perm"
	"api/internal/platform/editing"

	"gorm.io/gorm"
)

const (
	TypeWork = "catalog.work"

	FieldWorkDisplayName   = "catalog.work.display_name"
	FieldWorkOLang         = "catalog.work.olang"
	FieldWorkContentRating = "catalog.work.content_rating"
	FieldWorkTitles        = "catalog.work.titles"
	FieldWorkTitlesSuppr   = FieldWorkTitles + editing.SuppressedFieldSuffix
	FieldWorkIntros        = "catalog.work.intros"
	FieldWorkDisplayNSFW   = "catalog.work.display_nsfw"
	FieldWorkTagIDs        = "catalog.work.tag_ids"
	FieldWorkTagIDsSuppr   = FieldWorkTagIDs + editing.SuppressedFieldSuffix
	FieldWorkLabels        = "catalog.work.labels"
	FieldWorkEngineIDs     = "catalog.work.engine_ids"
	FieldWorkSeriesIDs     = "catalog.work.series_ids"
	FieldWorkLinks         = "catalog.work.links"
	FieldWorkCovers        = "catalog.work.covers"
	FieldWorkScreenshots   = "catalog.work.screenshots"
	FieldWorkCredits       = "catalog.work.credits"
	FieldWorkCreditsSuppr  = FieldWorkCredits + editing.SuppressedFieldSuffix
	FieldWorkRoster        = "catalog.work.roster"
	FieldWorkRosterSuppr   = FieldWorkRoster + editing.SuppressedFieldSuffix
)

var olangAllowed = map[string]struct{}{}

func init() {
	for _, lang := range []string{
		"ar", "be", "bg", "ca", "cs", "da", "de", "el", "en", "eo", "es",
		"fi", "fr", "ga", "gd", "he", "hi", "hr", "hu", "id", "it", "iu",
		"ja", "ko", "la", "lt", "lv", "mk", "ms", "nl", "no", "pl",
		"pt-br", "pt-pt", "ro", "ru", "sk", "sl", "sr", "sv", "ta", "th",
		"tr", "uk", "ur", "vi", "zh", "zh-Hans", "zh-Hant",
	} {
		olangAllowed[lang] = struct{}{}
	}
}

func RegisterWork(reg *editing.Registry, db *gorm.DB) error {
	fields := append(append(workFieldSpecs(), upstreamMediaFieldSpecs()...), workMediaOrderFieldSpecs()...)

	return reg.Register(withUpstreamRows(db, editing.EntityTypeSpec{
		Family: "catalog",
		Type:   TypeWork,
		LoadSnapshot: func(ctx context.Context, entityID int64) (map[string]any, error) {
			var w catmodel.CatalogWork
			err := db.WithContext(ctx).
				Select("id", "display_name", "olang", "content_rating", "display_nsfw").
				First(&w, entityID).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, editing.ErrEntityNotFound
			}
			if err != nil {
				return nil, err
			}
			snapshot := map[string]any{
				FieldWorkDisplayName:   w.DisplayName,
				FieldWorkOLang:         w.OLang,
				FieldWorkContentRating: int64(w.ContentRating),
				FieldWorkDisplayNSFW:   w.DisplayNSFW,
			}
			for key, load := range map[string]func(context.Context, *gorm.DB, int64) ([]any, error){
				FieldWorkTitles:              loadTitles,
				FieldWorkTitlesSuppr:         loadSuppressedTitles,
				FieldWorkIntros:              loadIntros,
				FieldWorkTagIDs:              loadTagIDs,
				FieldWorkTagIDsSuppr:         loadSuppressedTagIDs,
				FieldWorkLabels:              loadLabels,
				FieldWorkEngineIDs:           loadEngineIDs,
				FieldWorkSeriesIDs:           loadSeriesIDs,
				FieldWorkLinks:               loadLinks,
				FieldWorkCovers:              loadCovers,
				FieldWorkScreenshots:         loadScreenshots,
				FieldWorkCoversUpstream:      loadUpstreamCovers,
				FieldWorkScreenshotsUpstream: loadUpstreamScreenshots,
				FieldWorkCoverOrder:          loadWorkMediaOrder(catmodel.WorkMediaOrderCover),
				FieldWorkScreenshotOrder:     loadWorkMediaOrder(catmodel.WorkMediaOrderScreenshot),
				FieldWorkCredits:             loadCredits,
				FieldWorkCreditsSuppr:        loadSuppressedCredits,
				FieldWorkRoster:              loadRoster,
				FieldWorkRosterSuppr:         loadSuppressedRoster,
				FieldWorkRelations:           loadWorkRelations,
				FieldWorkPlatforms:           loadWorkPlatforms,
				FieldWorkSeriesMembers:       loadWorkSeriesMembers,
				FieldWorkTagSpoilers:         loadWorkTagSpoilers,
			} {
				value, err := load(ctx, db, entityID)
				if err != nil {
					return nil, err
				}
				snapshot[key] = value
			}
			return snapshot, nil
		},
		OnMerge: buildWorkOnMerge(db),
		OwnerSite: func(ctx context.Context, entityID int64) (*string, error) {
			var w catmodel.CatalogWork
			err := db.WithContext(ctx).Select("id", "site").First(&w, entityID).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, editing.ErrEntityNotFound
			}
			if err != nil {
				return nil, err
			}
			return w.Site, nil
		},
		OwnerUserID: func(ctx context.Context, entityID int64) (*int64, error) {
			var w catmodel.CatalogWork
			err := db.WithContext(ctx).Select("id", "owner_user_id").First(&w, entityID).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, editing.ErrEntityNotFound
			}
			if err != nil {
				return nil, err
			}
			return w.OwnerUserID, nil
		},
		DefaultPolicy: ownedEntityPolicy(perm.EditWorkReview),
		Fields:        fields,
	}))
}

func workFieldSpecs() []editing.FieldSpec {
	titles := editing.FieldSpec{
		Key: FieldWorkTitles, Kind: editing.KindList, DiffHint: editing.DiffHintItems,
		Identity: &editing.IdentitySpec{
			Segments: 4, TrailingText: true, KeyCheck: kindLangTextKeyCheck("title"),
		},
		MaxElements: maxTitleElements,
		Value: &editing.ValueSpec{Element: &editing.ElementSpec{
			Type: "object",
			Members: []editing.ElementMember{
				{Key: "lang", Type: "enum", Vocabulary: "olang", Nullable: true},
				{Key: "title", Type: "text"},
				{Key: "kind", Type: "int", Vocabulary: "title_kind"},
				{Key: "latin", Type: "text", Nullable: true},
			},
		}},
		Validate: validateTitles,
		Apply:    applyTitles,
	}
	credits := editing.FieldSpec{
		Key: FieldWorkCredits, Kind: editing.KindList, DiffHint: editing.DiffHintItems,
		Identity: &editing.IdentitySpec{
			Segments: 4, KeyCheck: creditKeyCheck,
			Refs: []editing.IdentityRef{
				{Segment: 3, EntityTag: TagCreditName},
				{Segment: 4, EntityTag: TagCharacter, ZeroAbsent: true},
			},
		},
		MaxSuppressed: maxCreditSuppress,
		MaxElements:   maxCreditElements,
		Value: &editing.ValueSpec{Element: &editing.ElementSpec{
			Type: "object",
			Members: []editing.ElementMember{
				{Key: "role_id", Type: "ref"},
				{Key: "credit_name_id", Type: "ref"},
				{Key: "character_id", Type: "ref", Nullable: true},
				{Key: "note", Type: "text", Nullable: true},
				{Key: "spoiler", Type: "int", Vocabulary: "spoiler", Nullable: true},
				{Key: "release_id", Type: "ref", Nullable: true},
				{Key: "label_id", Type: "ref", Nullable: true},
			},
		}},
		Validate: validateCredits,
		Apply:    applyCredits,
	}
	// The roster is registered on catalog.work only, never on catalog.character.
	// Both faces render the same physical row, but the two entity types can only
	// key it differently (roster:<character_id> against a work id, versus
	// roster:<work_id> against a character id), and edit_suppressed_row would
	// hold both keys happily while each read path consults exactly one — an edge
	// hidden on the character page would keep rendering on the work page.
	roster := editing.FieldSpec{
		Key: FieldWorkRoster, Kind: editing.KindList, DiffHint: editing.DiffHintItems,
		Identity: &editing.IdentitySpec{
			Segments: 2, KeyCheck: rosterKeyCheck,
			Refs: []editing.IdentityRef{{Segment: 2, EntityTag: TagCharacter}},
		},
		MaxSuppressed: maxRosterSuppress,
		MaxElements:   maxRosterElements,
		Value: &editing.ValueSpec{Element: &editing.ElementSpec{
			Type: "object",
			Members: []editing.ElementMember{
				{Key: "character_id", Type: "ref"},
				{Key: "kind", Type: "int", Vocabulary: "roster_role"},
				{Key: "spoiler", Type: "int", Vocabulary: "spoiler"},
			},
		}},
		Validate: validateRoster,
		Apply:    applyRoster,
		Provenance: []editing.ProvenanceTarget{
			{
				Table: catmodel.CatalogWorkCharacter{}.TableName(), Column: "kind",
				Rows: rosterRowsWhoseValueChanges("kind", func(e rosterEdge) int16 { return e.Kind }),
			},
			{
				Table: catmodel.CatalogWorkCharacter{}.TableName(), Column: "spoiler",
				Rows: rosterRowsWhoseValueChanges("spoiler", func(e rosterEdge) int16 { return e.Spoiler }),
			},
		},
	}
	tags := editing.FieldSpec{
		Key: FieldWorkTagIDs, Kind: editing.KindList, DiffHint: editing.DiffHintItems,
		Identity: &editing.IdentitySpec{Segments: 2, KeyCheck: workTagKeyCheck},
		Value:    &editing.ValueSpec{Element: &editing.ElementSpec{Type: "ref"}},
		Validate: validateTagIDs,
		Apply:    applyTagIDs,
	}
	return []editing.FieldSpec{
		{
			Key: FieldWorkDisplayName, Kind: editing.KindText, DiffHint: editing.DiffHintInline,
			Validate:   validateDisplayName,
			Apply:      applyWorkColumn("display_name", asString),
			Provenance: workProvenance("display_name"),
		},
		{
			Key: FieldWorkOLang, Kind: editing.KindEnum, DiffHint: editing.DiffHintInline,
			Value:      &editing.ValueSpec{Vocabulary: "olang"},
			Validate:   validateOLang,
			Apply:      applyWorkColumn("olang", asString),
			Provenance: workProvenance("olang"),
		},
		{
			Key: FieldWorkContentRating, Kind: editing.KindEnum, DiffHint: editing.DiffHintInline,
			Value:      &editing.ValueSpec{Vocabulary: "content_rating", Coded: true},
			Validate:   validateContentRating,
			Apply:      applyWorkColumn("content_rating", asContentRating),
			Provenance: workProvenance("content_rating"),
		},
		titles,
		editing.SuppressedFieldSpec(TypeWork, titles),
		{
			Key: FieldWorkIntros, Kind: editing.KindList, DiffHint: editing.DiffHintLines,
			Value: &editing.ValueSpec{Element: &editing.ElementSpec{
				Type: "object",
				Members: []editing.ElementMember{
					{Key: "lang", Type: "enum", Vocabulary: "intro_lang"},
					{Key: "intro", Type: "text"},
				},
			}},
			Validate: validateIntros,
			Apply:    applyIntros,
		},
		{
			Key: FieldWorkDisplayNSFW, Kind: editing.KindEnum, DiffHint: editing.DiffHintInline,
			Validate:   validateBool,
			Apply:      applyWorkColumn("display_nsfw", asBool),
			Provenance: workProvenance("display_nsfw"),
		},
		tags,
		workTagSuppressionSpec(tags),
		{
			Key: FieldWorkLabels, Kind: editing.KindList, DiffHint: editing.DiffHintItems,
			Value: &editing.ValueSpec{Element: &editing.ElementSpec{
				Type: "object",
				Members: []editing.ElementMember{
					{Key: "label_id", Type: "ref"},
					{Key: "kind", Type: "int", Vocabulary: "attribution_role"},
				},
			}},
			Validate: validateLabels,
			Apply:    applyLabels,
		},
		{
			Key: FieldWorkEngineIDs, Kind: editing.KindList, DiffHint: editing.DiffHintItems,
			Value:    &editing.ValueSpec{Element: &editing.ElementSpec{Type: "ref"}},
			Validate: validateEngineIDs,
			Apply:    applyEngineIDs,
		},
		{
			Key: FieldWorkSeriesIDs, Kind: editing.KindList, DiffHint: editing.DiffHintItems,
			Value:    &editing.ValueSpec{Element: &editing.ElementSpec{Type: "ref"}},
			Validate: validateSeriesIDs,
			Apply:    applySeriesIDs,
		},
		{
			Key: FieldWorkLinks, Kind: editing.KindList, DiffHint: editing.DiffHintItems,
			Value:    &editing.ValueSpec{Element: &editing.ElementSpec{Type: "text"}},
			Validate: validateLinks,
			Apply:    applyLinks,
		},
		{
			Key: FieldWorkCovers, Kind: editing.KindList, DiffHint: editing.DiffHintImage,
			Value: &editing.ValueSpec{Element: &editing.ElementSpec{
				Type: "object",
				Members: []editing.ElementMember{
					{Key: "image_hash", Type: "imagehash"},
					{Key: "kind", Type: "text", Nullable: true},
					{Key: "portrait_pinned", Type: "bool", Nullable: true},
					{Key: "sexual", Type: "int", Vocabulary: "sexual", Nullable: true},
					{Key: "violence", Type: "int", Vocabulary: "violence", Nullable: true},
				},
			}},
			Validate: validateCovers,
			Apply:    applyCovers,
		},
		{
			Key: FieldWorkScreenshots, Kind: editing.KindList, DiffHint: editing.DiffHintImage,
			Value: &editing.ValueSpec{Element: &editing.ElementSpec{
				Type: "object",
				Members: []editing.ElementMember{
					{Key: "image_hash", Type: "imagehash"},
					{Key: "caption", Type: "text", Nullable: true},
					{Key: "sexual", Type: "int", Vocabulary: "sexual", Nullable: true},
					{Key: "violence", Type: "int", Vocabulary: "violence", Nullable: true},
				},
			}},
			Validate: validateScreenshots,
			Apply:    applyScreenshots,
		},
		credits,
		editing.SuppressedFieldSpec(TypeWork, credits),
		roster,
		editing.SuppressedFieldSpec(TypeWork, roster),
		workRelationsFieldSpec(),
		workPlatformsFieldSpec(),
		workSeriesMembersFieldSpec(),
		workTagSpoilersFieldSpec(),
	}
}

func validateDisplayName(v any) error {
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("must be a string")
	}
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("must not be empty")
	}
	if len([]rune(s)) > 500 {
		return fmt.Errorf("must be at most 500 characters")
	}
	return nil
}

func validateOLang(v any) error {
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("must be a string")
	}
	if _, allowed := olangAllowed[s]; !allowed {
		return fmt.Errorf("%q is not an allowed original language", s)
	}
	return nil
}

func validateContentRating(v any) error {
	f, ok := v.(float64)
	if !ok || f != float64(int64(f)) {
		return fmt.Errorf("must be an integer")
	}
	switch int16(f) {
	case catmodel.ContentRatingAllAges, catmodel.ContentRatingSensitive, catmodel.ContentRatingR18:
		return nil
	}
	return fmt.Errorf("must be 0 (all_ages), 1 (sensitive) or 2 (r18)")
}

func asString(v any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return nil, fmt.Errorf("must be a string")
	}
	return s, nil
}

func asContentRating(v any) (any, error) {
	switch n := v.(type) {
	case float64:
		return int16(n), nil
	case int64:
		return int16(n), nil
	}
	return nil, fmt.Errorf("must be a number")
}

func workProvenance(column string) []editing.ProvenanceTarget {
	return []editing.ProvenanceTarget{{Table: catmodel.CatalogWork{}.TableName(), Column: column}}
}

func applyWorkColumn(column string, conv func(any) (any, error)) editing.ApplyFunc {
	return func(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
		v, err := conv(value)
		if err != nil {
			return fmt.Errorf("editspec: %s: %w", column, err)
		}
		res := tx.WithContext(ctx).Model(&catmodel.CatalogWork{}).
			Where("id = ?", entityID).Update(column, v)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return editing.ErrEntityNotFound
		}
		return nil
	}
}
