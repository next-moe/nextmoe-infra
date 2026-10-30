package main

import (
	"api/internal/platform/catalog/walkthrough/extract"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"regexp"
	"strings"
	"time"

	"api/internal/platform/catalog/editspec"
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
	Pinned, Created, Retired    int
	Edited, HiddenForReview     int
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
		if review := meta[id].Review; review != nil {
			raw, err := json.Marshal(review)
			if err != nil {
				return err
			}
			w.Status, w.ImportReview = model.WalkthroughStatusHidden, datatypes.JSON(raw)
			st.HiddenForReview++
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
	if apply {
		if st.Retired, err = retireOrphanPlaceholders(cat.WithContext(ctx)); err != nil {
			return err
		}
	}
	fmt.Printf("written=%d unchanged=%d kept_edited=%d hidden_for_review=%d skipped_unmatched=%d characters_pinned=%d characters_created=%d placeholders_retired=%d\n",
		st.Written, st.Unchanged, st.Edited, st.HiddenForReview, st.Skipped, st.Pinned, st.Created, st.Retired)
	if !apply {
		fmt.Println("[dry run] nothing written — re-run with --apply")
	}
	return nil
}

func writeWalkthrough(tx *gorm.DB, w model.CatalogWalkthrough, c walkthrough.Chart, pins map[string]characterPin, st *applyStats) error {
	var existing model.CatalogWalkthrough
	curatedPins := false
	err := tx.Where("source_id = ? AND source_url = ?", w.SourceID, w.SourceURL).Take(&existing).Error
	switch {
	case err == nil:
		w.ID = existing.ID
		if provenance.IsHuman(provenance.FirstSource(existing.FieldProvenance, "chart")) {
			st.Edited++
			return nil
		}
		curatedPins = provenance.IsHuman(provenance.FirstSource(existing.FieldProvenance, "pins"))
		if provenance.IsHuman(provenance.FirstSource(existing.FieldProvenance, "status")) {
			w.Status = existing.Status
		}
		if provenance.IsHuman(provenance.FirstSource(existing.FieldProvenance, "work_id")) {
			w.WorkID = existing.WorkID
		}
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
			existing.WorkID == w.WorkID && existing.Status == w.Status && existing.SourceHash == w.SourceHash && existing.TranslateModel == w.TranslateModel &&
			sameReview(existing.ImportReview, w.ImportReview) && (curatedPins || pinsLinked(pins, links)) {
			st.Unchanged++
			return nil
		}
		if err := tx.Model(&existing).Select("work_id", "status", "source_author", "source_updated_on", "chart_version",
			"chart", "source_hash", "extract_model", "translate_model", "import_review", "updated_at").Updates(&w).Error; err != nil {
			return err
		}
	} else if err := tx.Create(&w).Error; err != nil {
		return err
	}
	st.Written++
	if curatedPins {
		keys := []string{""}
		for _, ch := range c.Characters {
			keys = append(keys, ch.Key)
		}
		return tx.Where("walkthrough_id = ? AND chart_key NOT IN ?", w.ID, keys).
			Delete(&model.CatalogWalkthroughCharacter{}).Error
	}

	var keep []string
	for _, ch := range c.Characters {
		keep = append(keep, ch.Key)
		rule := ""
		id, linked := links[ch.Key]
		if p, ok := pins[ch.Key]; ok {
			if linked && id == p.CharacterID {
				continue
			}
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

func sameReview(a, b datatypes.JSON) bool {
	var x, y *model.WalkthroughImportReview
	if len(a) > 0 && string(a) != "null" && json.Unmarshal(a, &x) != nil {
		return false
	}
	if len(b) > 0 && string(b) != "null" && json.Unmarshal(b, &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}

func orEmptyStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func pinsLinked(pins map[string]characterPin, links map[string]int64) bool {
	for k, p := range pins {
		if links[k] != p.CharacterID {
			return false
		}
	}
	return true
}

// retireOrphanPlaceholders soft-deletes the name-only characters this importer
// created that a re-match has unlinked from every chart. It retires only a
// placeholder nobody has touched since: every other table that can hold a
// character id must have no row for it, and that list is maintained by hand
// against merge_rehang.go's mergeRefs.
func retireOrphanPlaceholders(db *gorm.DB) (int, error) {
	var ids []int64
	if err := db.Raw(`
SELECT c.id FROM catalog_character c
WHERE c.deleted_at IS NULL
  AND EXISTS (SELECT 1 FROM catalog_work_character wc WHERE wc.character_id = c.id AND wc.matched_by = @wt)
  AND NOT EXISTS (SELECT 1 FROM catalog_work_character wc WHERE wc.character_id = c.id AND wc.matched_by <> @wt)
  AND NOT EXISTS (SELECT 1 FROM catalog_walkthrough_character l WHERE l.character_id = c.id)
  AND NOT EXISTS (SELECT 1 FROM catalog_revision r WHERE r.entity_type = @family AND r.entity_id = c.id AND r.note <> @wt)
  AND NOT EXISTS (SELECT 1 FROM edit_revision r WHERE r.entity_type = @edit AND r.entity_id = c.id)
  AND NOT EXISTS (SELECT 1 FROM edit_proposal p WHERE p.entity_type = @edit AND p.entity_id = c.id)
  AND NOT EXISTS (SELECT 1 FROM edit_suppressed_row x WHERE x.entity_type = @edit AND x.entity_id = c.id)
  AND NOT EXISTS (SELECT 1 FROM catalog_character i WHERE i.instance_of = c.id)
  AND NOT EXISTS (SELECT 1 FROM catalog_character_intro x WHERE x.character_id = c.id)
  AND NOT EXISTS (SELECT 1 FROM catalog_character_intro_panel_verdict x WHERE x.character_id = c.id)
  AND NOT EXISTS (SELECT 1 FROM catalog_character_trait_link x WHERE x.character_id = c.id)
  AND NOT EXISTS (SELECT 1 FROM catalog_credit x WHERE x.character_id = c.id)
  AND NOT EXISTS (SELECT 1 FROM catalog_entity_relation x WHERE x.entity_type = @family AND (x.a_id = c.id OR x.b_id = c.id))
  AND NOT EXISTS (SELECT 1 FROM catalog_entity_usage x WHERE x.entity_type = @family AND x.entity_id = c.id)
  AND NOT EXISTS (SELECT 1 FROM catalog_external_ref x WHERE x.entity_type = @family AND x.entity_id = c.id)
ORDER BY c.id`, map[string]any{"wt": matchedByWalkthrough, "family": model.EntityTypeCharacter, "edit": editspec.TypeCharacter}).
		Scan(&ids).Error; err != nil || len(ids) == 0 {
		return 0, err
	}
	return len(ids), db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`DELETE FROM catalog_work_character WHERE character_id IN ? AND matched_by = ?`, ids, matchedByWalkthrough).Error; err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE catalog_character SET deleted_at = now(), updated_at = now() WHERE id IN ?`, ids).Error; err != nil {
			return err
		}
		return tx.Exec(`
INSERT INTO catalog_revision (entity_type, entity_id, revision, action, snapshot, is_minor, note, created_at)
SELECT ?, c.id, coalesce((SELECT max(r.revision) FROM catalog_revision r WHERE r.entity_type = ? AND r.entity_id = c.id), 0) + 1,
  ?, to_jsonb(c), false, ?, now()
FROM catalog_character c WHERE c.id IN ?`,
			model.EntityTypeCharacter, model.EntityTypeCharacter, model.RevisionActionDeleted, matchedByWalkthrough, ids).Error
	})
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
	for i := range c.Saves {
		c.Saves[i].LabelZh = tr(&c.Saves[i].Label)
	}
	for i := range c.Entries {
		c.Entries[i].LabelZh = tr(&c.Entries[i].Label)
	}
	for i := range c.Legend {
		c.Legend[i].TextZh = tr(&c.Legend[i].Text)
	}
	for i := range c.Segments {
		s := &c.Segments[i]
		s.ConditionZh = tr(s.Condition)
		for j := range s.Steps {
			st := &s.Steps[j]
			st.TextZh = tr(st.Text)
			st.RemarkZh = tr(st.Remark)
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
	Review    *model.WalkthroughImportReview
}

func loadExtractionMeta(ctx context.Context, src *gorm.DB, ids []int64) (map[int64]extractionMeta, error) {
	var rows []struct {
		EntryID   int64
		Model     string
		InputHash string
		Verdict   string
		Metrics   string
	}
	if err := src.WithContext(ctx).Table(latestExtractions(settledVerdicts)).Select("entry_id, model, input_hash, verdict, coalesce(metrics::text, '{}') AS metrics").
		Where("entry_id IN ?", ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]extractionMeta, len(rows))
	for _, r := range rows {
		meta := extractionMeta{Model: r.Model, InputHash: r.InputHash}
		if r.Verdict == extract.VerdictReview {
			var m extract.Metrics
			if err := json.Unmarshal([]byte(r.Metrics), &m); err != nil {
				return nil, fmt.Errorf("entry %d metrics: %w", r.EntryID, err)
			}
			meta.Review = &model.WalkthroughImportReview{
				Missed: orEmptyStrings(m.Missed), EndMarkers: m.EndMarkers, EndingsReached: m.EndingsReached, Coverage: m.Coverage,
			}
		}
		out[r.EntryID] = meta
	}
	return out, nil
}
