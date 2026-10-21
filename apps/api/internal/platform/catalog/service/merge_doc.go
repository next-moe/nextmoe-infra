package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"api/internal/platform/catalog/model"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	mergeDocMode         = "mode"
	mergeDocHideReleases = "hide_releases"

	ResolutionSource = "source"
	ResolutionTarget = "target"
)

var ErrMergeChoice = errors.New("catalog merge: invalid choice")

// MergeChoices are what a requester (and later a reviewer) decides for one
// merge: which side's value survives for the fields in mergeChoiceFields, and
// which of the source's releases to hide once they land on the target.
type MergeChoices struct {
	Fields       map[string]string
	HideReleases []int64
}

type mergeDoc struct {
	mode string
	MergeChoices
}

var mergeChoiceFields = map[int16][]string{
	model.EntityTypeWork:       {"display_name", "olang"},
	model.EntityTypeCharacter:  {"display_name", "latin", "lang", "gender", "image_hash"},
	model.EntityTypeLabel:      {"display_name", "latin", "lang", "note", "logo_hash"},
	model.EntityTypePerson:     {"display_name", "gender", "birth", "primary_credit_name_id"},
	model.EntityTypeCreditName: {"latin", "lang", "note"},
}

func parseMergeDoc(raw datatypes.JSON) (mergeDoc, error) {
	doc := mergeDoc{MergeChoices: MergeChoices{Fields: map[string]string{}}}
	if len(raw) == 0 {
		return doc, nil
	}
	var flat map[string]json.RawMessage
	if err := json.Unmarshal(raw, &flat); err != nil {
		return doc, fmt.Errorf("parse field_resolution: %w", err)
	}
	for key, value := range flat {
		switch key {
		case mergeDocMode:
			if err := json.Unmarshal(value, &doc.mode); err != nil {
				return doc, fmt.Errorf("parse field_resolution mode: %w", err)
			}
		case mergeDocHideReleases:
			if err := json.Unmarshal(value, &doc.HideReleases); err != nil {
				return doc, fmt.Errorf("parse field_resolution hide_releases: %w", err)
			}
		default:
			var side string
			if err := json.Unmarshal(value, &side); err != nil {
				return doc, fmt.Errorf("parse field_resolution %s: %w", key, err)
			}
			doc.Fields[key] = side
		}
	}
	return doc, nil
}

func (d mergeDoc) encode() (datatypes.JSON, error) {
	flat := map[string]any{}
	for key, side := range d.Fields {
		if side == ResolutionSource {
			flat[key] = side
		}
	}
	if d.mode != "" {
		flat[mergeDocMode] = d.mode
	}
	if len(d.HideReleases) > 0 {
		ids := slices.Clone(d.HideReleases)
		slices.Sort(ids)
		flat[mergeDocHideReleases] = slices.Compact(ids)
	}
	raw, err := json.Marshal(flat)
	return datatypes.JSON(raw), err
}

// validateChoices rejects a field the family cannot resolve, a side other
// than source/target, and a release that is not the source work's own.
func validateChoices(db *gorm.DB, entityType int16, src int64, c MergeChoices) error {
	allowed := mergeChoiceFields[entityType]
	for key, side := range c.Fields {
		if !slices.Contains(allowed, key) {
			return fmt.Errorf("%w: field %q cannot be chosen when merging this family", ErrMergeChoice, key)
		}
		if side != ResolutionSource && side != ResolutionTarget {
			return fmt.Errorf("%w: field %q must be source or target", ErrMergeChoice, key)
		}
	}
	if len(c.HideReleases) == 0 {
		return nil
	}
	if entityType != model.EntityTypeWork {
		return fmt.Errorf("%w: only a work merge hides releases", ErrMergeChoice)
	}
	var owned []int64
	if err := db.Raw(`SELECT id FROM catalog_release WHERE work_id = ? AND deleted_at IS NULL AND id IN ?`,
		src, c.HideReleases).Scan(&owned).Error; err != nil {
		return err
	}
	for _, id := range c.HideReleases {
		if !slices.Contains(owned, id) {
			return fmt.Errorf("%w: release %d is not a live release of the source work", ErrMergeChoice, id)
		}
	}
	return nil
}

func choicesDoc(mode string, c MergeChoices) mergeDoc {
	fields := map[string]string{}
	for key, side := range c.Fields {
		fields[strings.TrimSpace(key)] = side
	}
	return mergeDoc{mode: mode, MergeChoices: MergeChoices{Fields: fields, HideReleases: c.HideReleases}}
}
