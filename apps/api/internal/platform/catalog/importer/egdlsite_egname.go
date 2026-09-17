package importer

import (
	"fmt"
	"strconv"
	"strings"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/service"

	"golang.org/x/text/unicode/norm"
)

// egNameAlias reports whether the EG game name is filed as an alias of the work
// a DLsite SKU was minted as. This lane titles a work with the DLsite store
// name, and on 2026-09-17 370 of its 5,911 works had a live twin under the EG
// name instead (「【ゲームのみ】リリィとイザベラの館」 against a Bangumi
// 「リリィとイザベラの館」), which no title gate could see. The alias is filed only
// when one loose key contains the other, i.e. the store decorated EG's title;
// a name sharing nothing is a translation or a claim EG got wrong.
func egNameAlias(dlNorm, egNorm string) bool {
	d, e := looseGateKey(dlNorm), looseGateKey(egNorm)
	if d == e || !service.WorkDupeNormEligible(d) || !service.WorkDupeNormEligible(e) {
		return false
	}
	return strings.Contains(d, e) || strings.Contains(e, d)
}

func nfkcLower(s string) string { return strings.ToLower(norm.NFKC.String(s)) }

func (im *Importer) loadEGGameNames(ids []int64) (map[int64]string, error) {
	out := make(map[int64]string, len(ids))
	for start := 0; start < len(ids); start += 5000 {
		end := min(start+5000, len(ids))
		var rows []struct {
			ID   int64  `gorm:"column:id"`
			Name string `gorm:"column:gamename"`
		}
		if err := im.eg.Raw(`SELECT id, gamename FROM games
			WHERE id IN ? AND gamename IS NOT NULL AND btrim(gamename) <> ''`, ids[start:end]).
			Scan(&rows).Error; err != nil {
			return nil, fmt.Errorf("load eg game names: %w", err)
		}
		for _, r := range rows {
			out[r.ID] = r.Name
		}
	}
	return out, nil
}

func (im *Importer) attachEGNames(items []egdlItem) error {
	ids := make([]int64, 0, len(items))
	for _, it := range items {
		if !it.noEGRef {
			ids = append(ids, it.egGame)
		}
	}
	names, err := im.loadEGGameNames(ids)
	if err != nil {
		return err
	}
	for i := range items {
		if items[i].noEGRef {
			continue
		}
		items[i].egName = names[items[i].egGame]
		items[i].egNameFold = nfkcLower(items[i].egName)
	}
	return nil
}

func egAliasTitle(workID int64, it egdlItem) (model.CatalogWorkTitle, bool) {
	if it.egName == "" || !egNameAlias(it.nameFold, it.egNameFold) {
		return model.CatalogWorkTitle{}, false
	}
	return model.CatalogWorkTitle{
		WorkID: workID, Lang: "ja", Title: it.egName,
		Kind: model.WorkTitleKindAlias, Provenance: model.WorkTitleProvenanceSource,
	}, true
}

// ensureEGDLAliases files the alias on the works this lane minted before it
// filed one, so the title gates and the work-dedup census can see them.
func (im *Importer) ensureEGDLAliases(st *EGDLsiteStats) error {
	var rows []struct {
		WorkID  int64  `gorm:"column:work_id"`
		Display string `gorm:"column:display_name"`
		Game    string `gorm:"column:game"`
	}
	if err := im.catalog.Raw(`SELECT w.id AS work_id, w.display_name, r.external_id AS game
		FROM catalog_external_ref r
		JOIN catalog_work w ON w.id = r.entity_id AND w.deleted_at IS NULL
		WHERE r.entity_type = ? AND r.source_id = ? AND r.link_kind = ? AND r.matched_by = ?
		ORDER BY w.id`,
		model.EntityTypeWork, egSource, model.LinkKindProbable, ruleEGDLsite).Scan(&rows).Error; err != nil {
		return fmt.Errorf("load eg-dlsite works: %w", err)
	}
	ids := make([]int64, 0, len(rows))
	for _, r := range rows {
		if id, err := strconv.ParseInt(r.Game, 10, 64); err == nil {
			ids = append(ids, id)
		}
	}
	names, err := im.loadEGGameNames(ids)
	if err != nil {
		return err
	}
	var want []model.CatalogWorkTitle
	for _, r := range rows {
		id, err := strconv.ParseInt(r.Game, 10, 64)
		if err != nil {
			continue
		}
		name := names[id]
		if name == "" || !egNameAlias(nfkcLower(r.Display), nfkcLower(name)) {
			continue
		}
		want = append(want, model.CatalogWorkTitle{
			WorkID: r.WorkID, Lang: "ja", Title: name,
			Kind: model.WorkTitleKindAlias, Provenance: model.WorkTitleProvenanceSource,
		})
	}
	if len(want) == 0 {
		return nil
	}
	if im.dryRun {
		have, err := im.existingAliasTitles(want)
		if err != nil {
			return err
		}
		for _, t := range want {
			if !have[aliasKey(t.WorkID, t.Title)] {
				st.EGAliases++
			}
		}
		return nil
	}
	var touched []int64
	for start := 0; start < len(want); start += 1000 {
		batch := want[start:min(start+1000, len(want))]
		vals := make([]string, 0, len(batch))
		args := make([]any, 0, len(batch)*5)
		for _, t := range batch {
			vals = append(vals, "(?, ?, ?, ?, ?)")
			args = append(args, t.WorkID, t.Lang, t.Title, t.Kind, t.Provenance)
		}
		var ids []int64
		if err := im.catalog.Raw(`INSERT INTO catalog_work_title (work_id, lang, title, kind, provenance)
			VALUES `+strings.Join(vals, ", ")+` ON CONFLICT DO NOTHING RETURNING work_id`, args...).
			Scan(&ids).Error; err != nil {
			return fmt.Errorf("file eg aliases: %w", err)
		}
		touched = append(touched, ids...)
	}
	st.EGAliases += len(touched)
	if len(touched) == 0 {
		return nil
	}
	return touchWorks(im.catalog, touched)
}

func aliasKey(workID int64, title string) string {
	return strconv.FormatInt(workID, 10) + "\x00" + title
}

func (im *Importer) existingAliasTitles(want []model.CatalogWorkTitle) (map[string]bool, error) {
	ids := make([]int64, 0, len(want))
	for _, t := range want {
		ids = append(ids, t.WorkID)
	}
	out := map[string]bool{}
	for start := 0; start < len(ids); start += 5000 {
		var rows []struct {
			WorkID int64  `gorm:"column:work_id"`
			Title  string `gorm:"column:title"`
		}
		if err := im.catalog.Raw(`SELECT work_id, title FROM catalog_work_title
			WHERE kind = ? AND lang = 'ja' AND work_id IN ?`,
			model.WorkTitleKindAlias, ids[start:min(start+5000, len(ids))]).Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			out[aliasKey(r.WorkID, r.Title)] = true
		}
	}
	return out, nil
}
