package importer

import (
	"sort"

	"api/internal/platform/catalog/charname"
	"api/internal/platform/catalog/model"
)

// sameWorkCharacters finds, for each new source character, the one character
// already on its works (roster or voice credit) whose name or alias folds to
// the same key and that carries no record of this source yet. The EG credit
// lane minted a fresh character for every EG id it had no anchor for, so a
// work with a VNDB/Bangumi roster showed each of its characters twice, once on
// the roster and once under "other characters" from the EG voice credit.
//
// A name that folds onto two characters of the same works stays unresolved and
// mints as before; the cross-source fold decides those.
func (im *Importer) sameWorkCharacters(source int16, items []charItem, worksOf map[string][]int64) (map[string]int64, error) {
	out := map[string]int64{}
	var workIDs []int64
	seen := map[int64]bool{}
	for _, it := range items {
		for _, w := range worksOf[it.extID] {
			if !seen[w] {
				seen[w] = true
				workIDs = append(workIDs, w)
			}
		}
	}
	if len(workIDs) == 0 {
		return out, nil
	}
	sort.Slice(workIDs, func(i, j int) bool { return workIDs[i] < workIDs[j] })

	type present struct {
		WorkID      int64  `gorm:"column:work_id"`
		CharacterID int64  `gorm:"column:character_id"`
		Name        string `gorm:"column:name"`
	}
	byWork := map[int64]map[string]map[int64]struct{}{}
	for start := 0; start < len(workIDs); start += 1000 {
		chunk := workIDs[start:min(start+1000, len(workIDs))]
		var rows []present
		if err := im.catalog.Raw(`
			WITH on_work AS (
				SELECT work_id, character_id FROM catalog_work_character WHERE work_id IN @works
				UNION
				SELECT work_id, character_id FROM catalog_credit WHERE character_id IS NOT NULL AND work_id IN @works
			),
			eligible AS (
				SELECT o.work_id, c.id, c.display_name
				FROM on_work o
				JOIN catalog_character c ON c.id = o.character_id AND c.deleted_at IS NULL
				WHERE NOT EXISTS (SELECT 1 FROM catalog_external_ref r
				                  WHERE r.entity_type = @char AND r.entity_id = c.id AND r.source_id = @source)
			)
			SELECT work_id, id AS character_id, display_name AS name FROM eligible
			UNION
			SELECT e.work_id, e.id, al.name FROM eligible e
			JOIN catalog_character_alias al ON al.character_id = e.id`,
			map[string]any{"works": chunk, "char": model.EntityTypeCharacter, "source": source}).
			Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			k := charname.Fold(r.Name)
			if k == "" {
				continue
			}
			if byWork[r.WorkID] == nil {
				byWork[r.WorkID] = map[string]map[int64]struct{}{}
			}
			if byWork[r.WorkID][k] == nil {
				byWork[r.WorkID][k] = map[int64]struct{}{}
			}
			byWork[r.WorkID][k][r.CharacterID] = struct{}{}
		}
	}

	claimed := map[int64]int{}
	for _, it := range items {
		k := charname.Fold(it.name)
		if k == "" {
			continue
		}
		hits := map[int64]struct{}{}
		for _, w := range worksOf[it.extID] {
			for id := range byWork[w][k] {
				hits[id] = struct{}{}
			}
		}
		if len(hits) != 1 {
			continue
		}
		for id := range hits {
			out[it.extID] = id
			claimed[id]++
		}
	}
	for ext, id := range out {
		if claimed[id] > 1 {
			delete(out, ext)
		}
	}
	return out, nil
}

// splitAttach moves the items that resolved to an existing character out of
// the mint list and into anchor attachments.
func splitAttach(items []charItem, targets map[string]int64) (mint []charItem, attach []anchorItem) {
	for _, it := range items {
		if id, ok := targets[it.extID]; ok {
			attach = append(attach, anchorItem{extID: it.extID, entityID: id})
			continue
		}
		mint = append(mint, it)
	}
	return mint, attach
}
