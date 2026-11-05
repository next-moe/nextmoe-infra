package getchuattach

import (
	"fmt"
	"sort"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

func planFolds(snap snapshot, st *Stats) []plannedAction {
	byID := make(map[string]item, len(snap.items))
	for _, it := range snap.items {
		byID[it.GetchuID] = it
	}
	gids := make([]string, 0, len(snap.parallel))
	for gid := range snap.parallel {
		gids = append(gids, gid)
	}
	sort.Strings(gids)
	var out []plannedAction
	for _, gid := range gids {
		row := snap.parallel[gid]
		it, ok := byID[gid]
		if !ok {
			continue
		}
		if _, rejected := snap.rejected[rejKey(row.WorkID, gid)]; rejected {
			continue
		}
		pl, ok := counterpart(it, row.WorkID, snap)
		if !ok {
			st.ParallelKept++
			continue
		}
		p := plannedAction{
			Action: actionFold, GetchuID: gid, WorkID: row.WorkID, ReleaseID: pl.ReleaseID,
			FromID: row.ReleaseID, PlacedBy: pl.By, Tie: pl.Tie, Hits: []int64{row.WorkID},
		}
		st.Folds++
		countTie(st, p)
		out = append(out, p)
	}
	return out
}

func countPlacements(st *Stats, planned []plannedAction) {
	for _, p := range planned {
		if p.Action == actionAnchor && p.PlacedBy != "" {
			st.EditionAnchored++
			countTie(st, p)
		}
	}
}

func countTie(st *Stats, p plannedAction) {
	switch p.Tie {
	case tieTitle:
		st.TieTitle++
	case tieLowest:
		st.TieLowest++
	}
}

func writeFoldChunk(tx *gorm.DB, ids registryIDs, chunk []plannedAction, retired *int) (int, []int64, error) {
	moved := 0
	hosts := make([]int64, 0, len(chunk))
	target := map[int64]int64{}
	var from []int64
	for _, p := range chunk {
		res := tx.Exec(`UPDATE catalog_external_ref SET entity_id = ?
			WHERE entity_type = ? AND entity_id = ? AND source_id = ? AND external_id = ? AND link_kind = ?`,
			p.ReleaseID, model.EntityTypeRelease, p.FromID, ids.getchu, p.GetchuID, model.LinkKindExact)
		if res.Error != nil {
			return 0, nil, res.Error
		}
		if res.RowsAffected == 0 {
			continue
		}
		moved++
		hosts = append(hosts, p.WorkID)
		if _, seen := target[p.FromID]; !seen {
			from = append(from, p.FromID)
		}
		target[p.FromID] = p.ReleaseID
	}
	bare, bareArgs := parallelRowSQL("rel", ids.getchu)
	n := 0
	for _, id := range from {
		var gone []model.CatalogRelease
		args := append([]any{id, model.EntityTypeRelease}, bareArgs...)
		if err := tx.Raw(`UPDATE catalog_release rel SET deleted_at = now()
			WHERE rel.id = ? AND rel.deleted_at IS NULL
			  AND NOT EXISTS (SELECT 1 FROM catalog_external_ref e WHERE e.entity_type = ? AND e.entity_id = rel.id)
			  AND `+bare+`
			RETURNING rel.*`, args...).Scan(&gone).Error; err != nil {
			return 0, nil, err
		}
		if len(gone) == 0 {
			continue
		}
		if err := tx.Exec(`INSERT INTO catalog_revision (entity_type, entity_id, revision, action, snapshot, is_minor, note, created_at)
			SELECT ?, ?, coalesce(max(revision), 0) + 1, ?, ?, false, ?, now()
			FROM catalog_revision WHERE entity_type = ? AND entity_id = ?`,
			model.EntityTypeRelease, id, model.RevisionActionDeleted, releaseSnapshotJSON(gone[0]),
			fmt.Sprintf("getchu edition folded into release %d", target[id]),
			model.EntityTypeRelease, id).Error; err != nil {
			return 0, nil, err
		}
		n++
	}
	*retired += n
	return moved, hosts, nil
}
