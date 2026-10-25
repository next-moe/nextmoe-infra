package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"regexp"
	"strings"
	"time"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/walkthrough"
	"api/internal/platform/provenance"

	"golang.org/x/text/unicode/norm"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const matchedByWalkthrough = "import:walkthrough"

var siteSource = map[string]int16{
	"seiya":            model.SourceSeiya,
	"sagaoz_foolmaker": model.SourceSagaozFool,
}

var sourceKey = map[int16]string{
	model.SourceSeiya:      "seiya",
	model.SourceSagaozFool: "sagaoz_foolmaker",
}

type applyStats struct {
	Written, Unchanged, Skipped int
	Pinned, Created             int
}

func runApply(ctx context.Context, src, cat *gorm.DB, entries []sourceEntry, apply bool) error {
	charts, err := gatedCharts(ctx, src, entryIDs(entries))
	if err != nil {
		return err
	}
	works, err := loadWorkMatches(ctx, src, keysOf(charts))
	if err != nil {
		return err
	}
	pins, err := loadMatches(ctx, src, keysOf(charts))
	if err != nil {
		return err
	}
	zh, err := loadTranslations(ctx, src, keysOf(charts))
	if err != nil {
		return err
	}
	meta, err := loadExtractionMeta(ctx, src, keysOf(charts))
	if err != nil {
		return err
	}
	byID := make(map[int64]sourceEntry, len(entries))
	for _, e := range entries {
		byID[e.ID] = e
	}
	mode := "DRY"
	if apply {
		mode = "APPLY"
	}
	fmt.Printf("\n=== import-walkthroughs apply (%s) gated=%d matched=%d ===\n", mode, len(charts), len(works))

	var st applyStats
	for _, id := range keysOf(charts) {
		workID, ok := works[id]
		if !ok {
			st.Skipped++
			continue
		}
		e := byID[id]
		c := *charts[id]
		mergeTranslations(&c, zh[id].strings)
		w := model.CatalogWalkthrough{
			WorkID:          workID,
			SourceID:        siteSource[e.Site],
			SourceURL:       e.SourceURL,
			SourceAuthor:    strings.TrimSpace(e.Author),
			SourceUpdatedOn: parseSourceDate(e.UpdatedOn),
			Status:          model.WalkthroughStatusLive,
			ChartVersion:    walkthrough.ChartVersion,
			SourceHash:      meta[id].InputHash,
			ExtractModel:    meta[id].Model,
			TranslateModel:  zh[id].model,
		}
		if !apply {
			st.Written++
			st.Pinned += len(pins[id])
			st.Created += len(c.Characters) - len(pins[id])
			continue
		}
		if err := cat.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return writeWalkthrough(tx, w, c, pins[id], &st)
		}); err != nil {
			return fmt.Errorf("entry %d: %w", id, err)
		}
	}
	fmt.Printf("written=%d unchanged=%d skipped_unmatched=%d characters_pinned=%d characters_created=%d\n",
		st.Written, st.Unchanged, st.Skipped, st.Pinned, st.Created)
	if !apply {
		fmt.Println("[dry run] nothing written — re-run with --apply")
	}
	return nil
}

func writeWalkthrough(tx *gorm.DB, w model.CatalogWalkthrough, c walkthrough.Chart, pins map[string]characterPin, st *applyStats) error {
	var existing model.CatalogWalkthrough
	err := tx.Where("source_id = ? AND source_url = ?", w.SourceID, w.SourceURL).Take(&existing).Error
	switch {
	case err == nil:
		w.ID = existing.ID
	case err != gorm.ErrRecordNotFound:
		return err
	}
	links := map[string]int64{}
	if w.ID != 0 {
		var rows []model.CatalogWalkthroughCharacter
		if err := tx.Where("walkthrough_id = ?", w.ID).Find(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			links[r.ChartKey] = r.CharacterID
		}
	}
	chart, err := json.Marshal(c)
	if err != nil {
		return err
	}
	w.Chart = datatypes.JSON(chart)
	if w.ID != 0 {
		var prev walkthrough.Chart
		if json.Unmarshal(existing.Chart, &prev) == nil && reflect.DeepEqual(prev, c) &&
			existing.WorkID == w.WorkID && existing.SourceHash == w.SourceHash && existing.TranslateModel == w.TranslateModel {
			st.Unchanged++
			return nil
		}
		if err := tx.Model(&existing).Select("work_id", "source_author", "source_updated_on", "chart_version",
			"chart", "source_hash", "extract_model", "translate_model", "updated_at").Updates(&w).Error; err != nil {
			return err
		}
	} else if err := tx.Create(&w).Error; err != nil {
		return err
	}
	st.Written++

	var keep []string
	for _, ch := range c.Characters {
		keep = append(keep, ch.Key)
		rule := ""
		id, linked := links[ch.Key]
		if p, ok := pins[ch.Key]; ok {
			id, rule = p.CharacterID, p.Rule
			st.Pinned++
		} else if !linked {
			created, err := createNameOnlyCharacter(tx, w.WorkID, w.SourceID, ch)
			if err != nil {
				return err
			}
			id, rule = created, matchedByWalkthrough
			st.Created++
		}
		if rule == "" {
			continue
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "walkthrough_id"}, {Name: "chart_key"}},
			DoUpdates: clause.AssignmentColumns([]string{"character_id", "matched_by"}),
		}).Create(&model.CatalogWalkthroughCharacter{
			WalkthroughID: w.ID, ChartKey: ch.Key, CharacterID: id, MatchedBy: rule,
		}).Error; err != nil {
			return err
		}
	}
	return tx.Where("walkthrough_id = ? AND chart_key NOT IN ?", w.ID, append(keep, "")).
		Delete(&model.CatalogWalkthroughCharacter{}).Error
}

func createNameOnlyCharacter(tx *gorm.DB, workID int64, source int16, ch walkthrough.Character) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	prov := map[string]json.RawMessage{}
	provenance.Prepend(prov, "display_name", sourceKey[source], now)
	pj, err := json.Marshal(prov)
	if err != nil {
		return 0, err
	}
	row := model.CatalogCharacter{DisplayName: ch.Name, Lang: "ja", FieldProvenance: datatypes.JSON(pj)}
	if err := tx.Create(&row).Error; err != nil {
		return 0, err
	}
	src := source
	var aliases []model.CatalogCharacterAlias
	for _, a := range ch.Aliases {
		if a = strings.TrimSpace(a); a != "" && nameKey(a) != nameKey(ch.Name) {
			aliases = append(aliases, model.CatalogCharacterAlias{
				CharacterID: row.ID, Name: a, Lang: "ja", Kind: model.AliasKindSpellingVariant,
				Provenance: model.AliasProvenanceSource, SourceID: &src,
			})
		}
	}
	if len(aliases) > 0 {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&aliases).Error; err != nil {
			return 0, err
		}
	}
	if err := tx.Exec(`INSERT INTO catalog_work_character (work_id, character_id, kind, spoiler, matched_by, created_at, updated_at)
VALUES (?, ?, ?, 0, ?, now(), now()) ON CONFLICT DO NOTHING`,
		workID, row.ID, model.WorkCharacterKindUnknown, matchedByWalkthrough).Error; err != nil {
		return 0, err
	}
	snap, _ := json.Marshal(map[string]any{"character": row, "aliases": aliases})
	return row.ID, tx.Create(&model.CatalogRevision{
		EntityType: model.EntityTypeCharacter, EntityID: row.ID, Revision: 1,
		Action: model.RevisionActionImported, Snapshot: snap, Note: matchedByWalkthrough,
	}).Error
}

func mergeTranslations(c *walkthrough.Chart, zh map[string]string) {
	tr := func(s *string) *string {
		if s == nil {
			return nil
		}
		if t, ok := zh[*s]; ok {
			return &t
		}
		return nil
	}
	for i := range c.Sections {
		c.Sections[i].TitleZh = tr(&c.Sections[i].Title)
	}
	for i := range c.Endings {
		c.Endings[i].NameZh = tr(&c.Endings[i].Name)
	}
	for i := range c.Segments {
		s := &c.Segments[i]
		s.ConditionZh = tr(s.Condition)
		for j := range s.Steps {
			st := &s.Steps[j]
			st.TextZh = tr(st.Text)
			st.AltZh = make([]*string, len(st.Alt))
			for k := range st.Alt {
				st.AltZh[k] = tr(&st.Alt[k])
			}
		}
	}
}

var sourceDate = regexp.MustCompile(`(\d{2,4})[/.\-年](\d{1,2})[/.\-月](\d{1,2})`)

func parseSourceDate(s string) *time.Time {
	m := sourceDate.FindStringSubmatch(norm.NFKC.String(s))
	if m == nil {
		return nil
	}
	y := m[1]
	if len(y) == 2 {
		y = "20" + y
	}
	t, err := time.Parse("2006-1-2", y+"-"+m[2]+"-"+m[3])
	if err != nil || t.Year() < 1990 || t.After(time.Now()) {
		return nil
	}
	return &t
}

type translationRow struct {
	model   string
	strings map[string]string
}

func loadTranslations(ctx context.Context, src *gorm.DB, ids []int64) (map[int64]translationRow, error) {
	var rows []struct {
		EntryID int64
		Model   string
		Strings string
	}
	if err := src.WithContext(ctx).Table("wt_stage.translation").
		Select("entry_id, model, strings::text AS strings").
		Where("prompt_version = ? AND error = '' AND entry_id IN ?", translatePromptVersion, ids).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]translationRow, len(rows))
	for _, r := range rows {
		m := map[string]string{}
		if err := json.Unmarshal([]byte(r.Strings), &m); err != nil {
			slog.Warn("translation unreadable", "entry", r.EntryID, "err", err)
			continue
		}
		out[r.EntryID] = translationRow{model: r.Model, strings: m}
	}
	return out, nil
}

func loadWorkMatches(ctx context.Context, src *gorm.DB, ids []int64) (map[int64]int64, error) {
	var rows []struct {
		EntryID int64
		WorkID  int64
	}
	if err := src.WithContext(ctx).Table("wt_stage.match").Select("entry_id, work_id").
		Where("entry_id IN ? AND work_id IS NOT NULL", ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]int64, len(rows))
	for _, r := range rows {
		out[r.EntryID] = r.WorkID
	}
	return out, nil
}

type extractionMeta struct {
	Model     string
	InputHash string
}

func loadExtractionMeta(ctx context.Context, src *gorm.DB, ids []int64) (map[int64]extractionMeta, error) {
	var rows []struct {
		EntryID   int64
		Model     string
		InputHash string
	}
	if err := src.WithContext(ctx).Table("wt_stage.extraction").Select("entry_id, model, input_hash").
		Where("prompt_version = ? AND entry_id IN ?", extractPromptVersion, ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]extractionMeta, len(rows))
	for _, r := range rows {
		out[r.EntryID] = extractionMeta{Model: r.Model, InputHash: r.InputHash}
	}
	return out, nil
}
