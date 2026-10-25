package editspec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	catmodel "api/internal/platform/catalog/model"
	"api/internal/platform/catalog/perm"
	"api/internal/platform/catalog/walkthrough"
	"api/internal/platform/editing"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	TypeWalkthrough = "catalog.walkthrough"

	FieldWalkthroughChart  = "catalog.walkthrough.chart"
	FieldWalkthroughHidden = "catalog.walkthrough.hidden"
	FieldWalkthroughLang   = "catalog.walkthrough.lang"
	FieldWalkthroughPins   = "catalog.walkthrough.pins"
	FieldWalkthroughWork   = "catalog.walkthrough.work_id"
)

const (
	maxChartBytes          = 400_000
	maxChartCharacters     = 128
	maxChartSections       = 400
	maxChartEntries        = 200
	maxChartSaves          = 600
	maxChartEndings        = 600
	maxChartSegments       = 2000
	maxChartSteps          = 6000
	maxChartAliases        = 32
	maxChartAlts           = 16
	maxChartNameRunes      = 512
	maxChartLabelRunes     = 256
	maxChartTextRunes      = 1024
	maxChartConditionRunes = 1024
)

var chartKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_]{1,32}$`)

func RegisterWalkthrough(reg *editing.Registry, db *gorm.DB) error {
	return reg.Register(editing.EntityTypeSpec{
		Family:       "catalog",
		Type:         TypeWalkthrough,
		LoadSnapshot: loadWalkthroughSnapshot(db),
		OwnerSite: func(ctx context.Context, entityID int64) (*string, error) {
			w, err := parentWorkOfWalkthrough(ctx, db, entityID, "id", "site")
			if err != nil {
				return nil, err
			}
			return w.Site, nil
		},
		OwnerUserID: func(ctx context.Context, entityID int64) (*int64, error) {
			w, err := parentWorkOfWalkthrough(ctx, db, entityID, "id", "owner_user_id")
			if err != nil {
				return nil, err
			}
			return w.OwnerUserID, nil
		},
		DefaultPolicy: ownedEntityPolicy(perm.EditWalkthroughReview),
		Fields:        walkthroughFieldSpecs(),
	})
}

func walkthroughFieldSpecs() []editing.FieldSpec {
	return []editing.FieldSpec{
		{
			Key: FieldWalkthroughChart, Kind: editing.KindDocument, DiffHint: editing.DiffHintTree,
			Validate: func(v any) error {
				_, err := parseChart(v)
				return err
			},
			Apply:      applyWalkthroughChart,
			Provenance: walkthroughProvenance("chart"),
		},
		{
			Key: FieldWalkthroughHidden, Kind: editing.KindEnum, DiffHint: editing.DiffHintInline,
			Validate:   validateBool,
			Apply:      applyWalkthroughHidden,
			Provenance: walkthroughProvenance("status"),
		},
		{
			Key: FieldWalkthroughLang, Kind: editing.KindEnum, DiffHint: editing.DiffHintInline,
			Validate: validateWalkthroughLang,
			Apply: applyWalkthroughColumn("lang", func(v any) (any, error) {
				if err := validateWalkthroughLang(v); err != nil {
					return nil, err
				}
				return v, nil
			}),
			Provenance: walkthroughProvenance("lang"),
		},
		{
			// Applied after the chart (fields apply in key order), so a patch that
			// adds a character key and pins it in one go sees the new key.
			Key: FieldWalkthroughPins, Kind: editing.KindList, DiffHint: editing.DiffHintItems,
			Value: &editing.ValueSpec{Element: &editing.ElementSpec{Type: "object", Members: []editing.ElementMember{
				{Key: "chart_key", Type: "text"},
				{Key: "character_id", Type: "ref"},
			}}},
			MaxElements: maxChartCharacters,
			Validate: func(v any) error {
				_, err := parsePins(v)
				return err
			},
			Apply:      applyWalkthroughPins,
			Provenance: walkthroughProvenance("pins"),
		},
		{
			Key: FieldWalkthroughWork, Kind: editing.KindRef, DiffHint: editing.DiffHintInline,
			Validate: func(v any) error {
				_, err := parseRefID(v, false)
				return err
			},
			Apply:      applyWalkthroughWork,
			Provenance: walkthroughProvenance("work_id"),
		},
	}
}

func applyWalkthroughWork(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	id, err := parseRefID(value, false)
	if err != nil {
		return fmt.Errorf("editspec: work_id: %w", err)
	}
	if err := assertWorkExists(ctx, tx, *id); err != nil {
		if errors.Is(err, editing.ErrEntityNotFound) {
			return &editing.ValidationError{Key: FieldWalkthroughWork, Reason: fmt.Sprintf("work %d does not exist", *id)}
		}
		return err
	}
	return applyWalkthroughColumn("work_id", func(any) (any, error) { return *id, nil })(ctx, tx, entityID, value)
}

func walkthroughProvenance(column string) []editing.ProvenanceTarget {
	return []editing.ProvenanceTarget{{Table: catmodel.CatalogWalkthrough{}.TableName(), Column: column}}
}

func validateWalkthroughLang(v any) error {
	switch v {
	case catmodel.WalkthroughLangJa, catmodel.WalkthroughLangZhHans:
		return nil
	}
	return fmt.Errorf("must be %q or %q", catmodel.WalkthroughLangJa, catmodel.WalkthroughLangZhHans)
}

// parseChart decodes strictly: an unknown key is a client bug, and a chart that
// silently dropped it would still compare unequal to the stored one forever.
func parseChart(v any) (*walkthrough.Chart, error) {
	if _, ok := v.(map[string]any); !ok {
		return nil, fmt.Errorf("must be a chart object")
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if len(raw) > maxChartBytes {
		return nil, fmt.Errorf("chart is %d bytes, at most %d", len(raw), maxChartBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var c walkthrough.Chart
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("chart: %w", err)
	}
	walkthrough.FillNil(&c)
	if err := checkChartLimits(&c); err != nil {
		return nil, err
	}
	if problems := walkthrough.Validate(c); len(problems) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(problems[:min(len(problems), 5)], "; "))
	}
	return &c, nil
}

func checkChartLimits(c *walkthrough.Chart) error {
	counts := []struct {
		what  string
		n, at int
	}{
		{"characters", len(c.Characters), maxChartCharacters},
		{"sections", len(c.Sections), maxChartSections},
		{"entries", len(c.Entries), maxChartEntries},
		{"saves", len(c.Saves), maxChartSaves},
		{"endings", len(c.Endings), maxChartEndings},
		{"segments", len(c.Segments), maxChartSegments},
	}
	for _, n := range counts {
		if n.n > n.at {
			return fmt.Errorf("%s: %d, at most %d", n.what, n.n, n.at)
		}
	}
	type keyed struct {
		what string
		keys []string
	}
	groups := []keyed{
		{"characters", mapKeys(c.Characters, func(x walkthrough.Character) string { return x.Key })},
		{"sections", mapKeys(c.Sections, func(x walkthrough.Section) string { return x.Key })},
		{"entries", mapKeys(c.Entries, func(x walkthrough.Entry) string { return x.Key })},
		{"saves", mapKeys(c.Saves, func(x walkthrough.Save) string { return x.Key })},
		{"endings", mapKeys(c.Endings, func(x walkthrough.Ending) string { return x.Key })},
		{"segments", mapKeys(c.Segments, func(x walkthrough.Segment) string { return x.Key })},
	}
	for _, g := range groups {
		seen := make(map[string]bool, len(g.keys))
		for i, k := range g.keys {
			if !chartKeyPattern.MatchString(k) {
				return fmt.Errorf("%s[%d].key %q must match %s", g.what, i, k, chartKeyPattern)
			}
			if seen[k] {
				return fmt.Errorf("%s[%d].key %q is used twice", g.what, i, k)
			}
			seen[k] = true
		}
	}

	var errs []error
	text := func(where, s string, max int, required bool) {
		switch {
		case required && strings.TrimSpace(s) == "":
			errs = append(errs, fmt.Errorf("%s must not be empty", where))
		case utf8.RuneCountInString(s) > max:
			errs = append(errs, fmt.Errorf("%s must be at most %d characters", where, max))
		}
	}
	optText := func(where string, s *string, max int) {
		if s != nil {
			text(where, *s, max, false)
		}
	}
	for i, ch := range c.Characters {
		text(fmt.Sprintf("characters[%d].name", i), ch.Name, maxChartNameRunes, true)
		if len(ch.Aliases) > maxChartAliases {
			errs = append(errs, fmt.Errorf("characters[%d].aliases: at most %d", i, maxChartAliases))
		}
		for j, a := range ch.Aliases {
			text(fmt.Sprintf("characters[%d].aliases[%d]", i, j), a, maxChartNameRunes, true)
		}
	}
	for i, s := range c.Sections {
		text(fmt.Sprintf("sections[%d].title", i), s.Title, maxChartNameRunes, true)
		optText(fmt.Sprintf("sections[%d].title_zh", i), s.TitleZh, maxChartNameRunes)
	}
	for i, e := range c.Entries {
		text(fmt.Sprintf("entries[%d].label", i), e.Label, maxChartLabelRunes, true)
	}
	for i, s := range c.Saves {
		text(fmt.Sprintf("saves[%d].label", i), s.Label, maxChartLabelRunes, true)
	}
	for i, e := range c.Endings {
		text(fmt.Sprintf("endings[%d].name", i), e.Name, maxChartNameRunes, true)
		optText(fmt.Sprintf("endings[%d].name_zh", i), e.NameZh, maxChartNameRunes)
	}
	steps := 0
	for i, s := range c.Segments {
		optText(fmt.Sprintf("segments[%d].condition", i), s.Condition, maxChartConditionRunes)
		optText(fmt.Sprintf("segments[%d].condition_zh", i), s.ConditionZh, maxChartConditionRunes)
		steps += len(s.Steps)
		for j, st := range s.Steps {
			where := fmt.Sprintf("segments[%d].steps[%d]", i, j)
			optText(where+".text", st.Text, maxChartTextRunes)
			optText(where+".text_zh", st.TextZh, maxChartTextRunes)
			if len(st.Alt) > maxChartAlts {
				errs = append(errs, fmt.Errorf("%s.alt: at most %d", where, maxChartAlts))
			}
			if len(st.AltZh) != len(st.Alt) {
				errs = append(errs, fmt.Errorf("%s.alt_zh must have one entry per alt", where))
			}
			for k, a := range st.Alt {
				text(fmt.Sprintf("%s.alt[%d]", where, k), a, maxChartTextRunes, true)
			}
			for k, a := range st.AltZh {
				optText(fmt.Sprintf("%s.alt_zh[%d]", where, k), a, maxChartTextRunes)
			}
			if st.StepType == walkthrough.StepSave && (st.Text != nil || st.TextZh != nil || len(st.Alt) > 0 || st.Achievement) {
				errs = append(errs, fmt.Errorf("%s: a save step carries only save", where))
			}
			if st.StepType != walkthrough.StepSave && st.Save != nil {
				errs = append(errs, fmt.Errorf("%s: only a save step names a save", where))
			}
		}
	}
	if steps > maxChartSteps {
		errs = append(errs, fmt.Errorf("steps: %d, at most %d", steps, maxChartSteps))
	}
	if len(errs) > 0 {
		return errors.Join(errs[:min(len(errs), 5)]...)
	}
	return nil
}

func mapKeys[T any](xs []T, key func(T) string) []string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = key(x)
	}
	return out
}

func applyWalkthroughChart(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	c, err := parseChart(value)
	if err != nil {
		return &editing.ValidationError{Key: FieldWalkthroughChart, Reason: err.Error()}
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	res := tx.WithContext(ctx).Exec(`UPDATE catalog_walkthrough SET chart = ?::jsonb, chart_version = ?, updated_at = now() WHERE id = ?`,
		string(raw), walkthrough.ChartVersion, entityID)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return editing.ErrEntityNotFound
	}
	keys := mapKeys(c.Characters, func(x walkthrough.Character) string { return x.Key })
	return tx.WithContext(ctx).Where("walkthrough_id = ? AND chart_key NOT IN ?", entityID, append(keys, "")).
		Delete(&catmodel.CatalogWalkthroughCharacter{}).Error
}

func applyWalkthroughHidden(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	hidden, err := asBool(value)
	if err != nil {
		return fmt.Errorf("editspec: hidden: %w", err)
	}
	status := catmodel.WalkthroughStatusLive
	if hidden.(bool) {
		status = catmodel.WalkthroughStatusHidden
	}
	return applyWalkthroughColumn("status", func(any) (any, error) { return status, nil })(ctx, tx, entityID, value)
}

func applyWalkthroughColumn(column string, conv func(any) (any, error)) editing.ApplyFunc {
	return func(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
		v, err := conv(value)
		if err != nil {
			return fmt.Errorf("editspec: %s: %w", column, err)
		}
		res := tx.WithContext(ctx).Model(&catmodel.CatalogWalkthrough{}).Where("id = ?", entityID).
			Updates(map[string]any{column: v, "updated_at": gorm.Expr("now()")})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return editing.ErrEntityNotFound
		}
		return nil
	}
}

type walkthroughPin struct {
	ChartKey    string
	CharacterID int64
}

func parsePins(v any) ([]walkthroughPin, error) {
	list, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("must be a list")
	}
	if len(list) > maxChartCharacters {
		return nil, fmt.Errorf("at most %d pins", maxChartCharacters)
	}
	out := make([]walkthroughPin, 0, len(list))
	seen := map[string]bool{}
	for i, el := range list {
		obj, err := asObject(el, i, "chart_key", "character_id")
		if err != nil {
			return nil, err
		}
		key, err := objString(obj, "chart_key", i, true, 32)
		if err != nil {
			return nil, err
		}
		if !chartKeyPattern.MatchString(key) {
			return nil, fmt.Errorf("element %d: chart_key %q must match %s", i, key, chartKeyPattern)
		}
		if seen[key] {
			return nil, fmt.Errorf("element %d: chart_key %q is pinned twice", i, key)
		}
		seen[key] = true
		id, err := objInt(obj, "character_id", i, true)
		if err != nil {
			return nil, err
		}
		if id <= 0 {
			return nil, fmt.Errorf("element %d: character_id must be positive", i)
		}
		out = append(out, walkthroughPin{ChartKey: key, CharacterID: id})
	}
	return out, nil
}

func applyWalkthroughPins(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	pins, err := parsePins(value)
	if err != nil {
		return &editing.ValidationError{Key: FieldWalkthroughPins, Reason: err.Error()}
	}
	db := tx.WithContext(ctx)
	var w catmodel.CatalogWalkthrough
	if err := firstEntity(ctx, tx, &w, entityID, "id", "chart"); err != nil {
		return err
	}
	var c walkthrough.Chart
	if err := json.Unmarshal(w.Chart, &c); err != nil {
		return err
	}
	keys := mapKeys(c.Characters, func(x walkthrough.Character) string { return x.Key })
	ids := make([]int64, 0, len(pins))
	for _, p := range pins {
		if !slices.Contains(keys, p.ChartKey) {
			return &editing.ValidationError{Key: FieldWalkthroughPins, Reason: fmt.Sprintf("chart_key %q is not a character of the chart", p.ChartKey)}
		}
		ids = append(ids, p.CharacterID)
	}
	if err := assertEntitiesExist(ctx, tx, &catmodel.CatalogCharacter{}, ids, FieldWalkthroughPins); err != nil {
		return err
	}
	var existing []catmodel.CatalogWalkthroughCharacter
	if err := db.Where("walkthrough_id = ?", entityID).Find(&existing).Error; err != nil {
		return err
	}
	linked := make(map[string]int64, len(existing))
	for _, e := range existing {
		linked[e.ChartKey] = e.CharacterID
	}
	keep := []string{""}
	for _, p := range pins {
		keep = append(keep, p.ChartKey)
		if id, ok := linked[p.ChartKey]; ok && id == p.CharacterID {
			continue
		}
		if err := db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "walkthrough_id"}, {Name: "chart_key"}},
			DoUpdates: clause.AssignmentColumns([]string{"character_id", "matched_by"}),
		}).Create(&catmodel.CatalogWalkthroughCharacter{
			WalkthroughID: entityID, ChartKey: p.ChartKey, CharacterID: p.CharacterID,
			MatchedBy: catmodel.WalkthroughMatchedByCurated,
		}).Error; err != nil {
			return err
		}
	}
	return db.Where("walkthrough_id = ? AND chart_key NOT IN ?", entityID, keep).
		Delete(&catmodel.CatalogWalkthroughCharacter{}).Error
}

func loadWalkthroughSnapshot(db *gorm.DB) func(context.Context, int64) (map[string]any, error) {
	return func(ctx context.Context, entityID int64) (map[string]any, error) {
		var w catmodel.CatalogWalkthrough
		if err := firstEntity(ctx, db, &w, entityID, "id", "work_id", "chart", "status", "lang"); err != nil {
			return nil, err
		}
		var c walkthrough.Chart
		if err := json.Unmarshal(w.Chart, &c); err != nil {
			return nil, fmt.Errorf("walkthrough %d chart: %w", entityID, err)
		}
		walkthrough.FillNil(&c)
		chart, err := toJSONValue(c)
		if err != nil {
			return nil, err
		}
		var links []catmodel.CatalogWalkthroughCharacter
		if err := db.WithContext(ctx).Where("walkthrough_id = ?", entityID).Order("chart_key").Find(&links).Error; err != nil {
			return nil, err
		}
		pins := make([]any, 0, len(links))
		for _, l := range links {
			pins = append(pins, map[string]any{"chart_key": l.ChartKey, "character_id": l.CharacterID})
		}
		return map[string]any{
			FieldWalkthroughChart:  chart,
			FieldWalkthroughHidden: w.Status == catmodel.WalkthroughStatusHidden,
			FieldWalkthroughLang:   w.Lang,
			FieldWalkthroughPins:   pins,
			FieldWalkthroughWork:   w.WorkID,
		}, nil
	}
}

func toJSONValue(v any) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out any
	return out, json.Unmarshal(raw, &out)
}

func parentWorkOfWalkthrough(ctx context.Context, db *gorm.DB, walkthroughID int64, workColumns ...string) (*catmodel.CatalogWork, error) {
	var w catmodel.CatalogWalkthrough
	if err := firstEntity(ctx, db, &w, walkthroughID, "id", "work_id"); err != nil {
		return nil, err
	}
	var work catmodel.CatalogWork
	err := db.WithContext(ctx).Select(workColumns).First(&work, w.WorkID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, editing.ErrEntityNotFound
	}
	if err != nil {
		return nil, err
	}
	return &work, nil
}
