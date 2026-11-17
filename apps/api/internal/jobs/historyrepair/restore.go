package historyrepair

import (
	"context"
	"encoding/json"
	"fmt"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const restoreNote = "restore covers an editor uploaded that the 2026-09 wiki-lineage cleanup deleted without a revision"

type lostCover struct {
	WorkID  int64
	Element datatypes.JSON
}

// lostCoversSQL finds covers a catalog-engine revision added that the work's
// latest revision still lists but no row holds. No revision removed them, so
// something outside the engine did; the galgame-era covers the cleanup meant
// to delete are excluded by requiring a catalog-engine revision to have added
// the hash.
const lostCoversSQL = `
WITH r AS (
  SELECT entity_id, created_at, proposal_id, changed_fields,
         snapshot -> @key AS cur,
         lag(snapshot -> @key) OVER (PARTITION BY entity_id ORDER BY seq) AS prev
    FROM edit_revision WHERE entity_type = @type
), added AS (
  SELECT DISTINCT r.entity_id, e ->> 'image_hash' AS h
    FROM r CROSS JOIN jsonb_array_elements(r.cur) e
   WHERE r.created_at >= @cut AND r.proposal_id IS NOT NULL AND jsonb_exists(r.changed_fields, @key)
     AND NOT EXISTS (SELECT 1 FROM jsonb_array_elements(COALESCE(r.prev, '[]'::jsonb)) p
                      WHERE p ->> 'image_hash' = e ->> 'image_hash')
), latest AS (
  SELECT DISTINCT ON (entity_id) entity_id, snapshot -> @key AS covers
    FROM edit_revision WHERE entity_type = @type ORDER BY entity_id, seq DESC
)
SELECT a.entity_id AS work_id, el AS element
  FROM added a
  JOIN catalog_work w ON w.id = a.entity_id AND w.status = @live AND w.deleted_at IS NULL
  JOIN latest l ON l.entity_id = a.entity_id
  CROSS JOIN jsonb_array_elements(l.covers) el
 WHERE el ->> 'image_hash' = a.h
   AND NOT EXISTS (SELECT 1 FROM catalog_work_cover c WHERE c.work_id = a.entity_id AND c.image_hash = a.h)
 ORDER BY a.entity_id, a.h`

// RestoreLostCovers puts those covers back through the editing engine, one
// revision per work, appended to the work's current curated covers.
func RestoreLostCovers(ctx context.Context, db *gorm.DB, engine *editing.Engine, actor editing.PolicyContext, apply bool) (*Stats, error) {
	st := newStats("restore-covers", apply)
	var lost []lostCover
	if err := db.WithContext(ctx).Raw(lostCoversSQL, map[string]any{
		"key": editspec.FieldWorkCovers, "type": editspec.TypeWork, "cut": Cut, "live": model.WorkStatusLive,
	}).Scan(&lost).Error; err != nil {
		return nil, err
	}
	st.Scanned = len(lost)
	byWork := map[int64][]map[string]any{}
	for _, l := range lost {
		var el map[string]any
		if err := json.Unmarshal(l.Element, &el); err != nil {
			return nil, err
		}
		if _, ok := byWork[l.WorkID]; !ok {
			st.Works = append(st.Works, l.WorkID)
		}
		byWork[l.WorkID] = append(byWork[l.WorkID], el)
	}
	for _, workID := range st.Works {
		current, err := engine.CurrentSnapshot(ctx, editspec.TypeWork, workID)
		if err != nil {
			return nil, fmt.Errorf("work %d: %w", workID, err)
		}
		covers, _ := current[editspec.FieldWorkCovers].([]any)
		pinned := false
		for _, c := range covers {
			if obj, _ := c.(map[string]any); obj["portrait_pinned"] == true {
				pinned = true
			}
		}
		next := append([]any{}, covers...)
		for _, el := range byWork[workID] {
			if pinned {
				delete(el, "portrait_pinned")
			}
			if el["portrait_pinned"] == true {
				pinned = true
			}
			next = append(next, el)
		}
		st.sample("work %d: +%d cover(s), curated lane %d -> %d", workID, len(byWork[workID]), len(covers), len(next))
		if !apply {
			continue
		}
		_, rev, err := engine.CreateProposal(ctx, editing.CreateProposalInput{
			EntityType: editspec.TypeWork, EntityID: workID,
			Patch: map[string]any{editspec.FieldWorkCovers: next},
			Note:  restoreNote, Actor: actor,
		})
		if err != nil {
			st.Problems = append(st.Problems, fmt.Sprintf("work %d: %v", workID, err))
			continue
		}
		if rev == nil {
			st.Problems = append(st.Problems, fmt.Sprintf("work %d: proposal opened instead of merging", workID))
			continue
		}
		st.Written += len(byWork[workID])
	}
	return st, nil
}
