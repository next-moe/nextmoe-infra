package main

import (
	"context"
	"slices"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

const idChunk = 2000

type pairKey struct {
	lang, title string
}

type lineage struct {
	losers       int
	snapshotless bool
	loserIDs     map[int64]struct{}
	loserPairs   map[pairKey]struct{}
	ownIDs       map[int64]struct{}
	ownPairs     map[pairKey]struct{}
}

func newLineage() *lineage {
	return &lineage{
		loserIDs: map[int64]struct{}{}, loserPairs: map[pairKey]struct{}{},
		ownIDs: map[int64]struct{}{}, ownPairs: map[pairKey]struct{}{},
	}
}

func (l *lineage) loserOrigin(t titleRow) bool {
	if _, ok := l.loserIDs[t.ID]; ok {
		return true
	}
	if _, ok := l.ownIDs[t.ID]; ok {
		return false
	}
	p := pairKey{t.Lang, t.Title}
	if _, ok := l.ownPairs[p]; ok {
		return false
	}
	_, ok := l.loserPairs[p]
	return ok
}

type lineageStats struct {
	Losers             int
	LosersWithSnapshot int
	LosersSnapshotless int
}

type snapshotTitle struct {
	EntityID int64   `gorm:"column:entity_id"`
	TitleID  *int64  `gorm:"column:title_id"`
	Lang     *string `gorm:"column:lang"`
	Title    *string `gorm:"column:title"`
}

const snapshotTitlesSQL = `
	SELECT r.entity_id, (t->>'id')::bigint AS title_id, t->>'lang' AS lang, t->>'title' AS title
	FROM catalog_revision r
	LEFT JOIN LATERAL jsonb_array_elements(
		CASE WHEN jsonb_typeof(r.snapshot->'titles') = 'array' THEN r.snapshot->'titles' ELSE '[]'::jsonb END
	) t ON true
	WHERE r.entity_type = ? AND r.action = ? AND r.entity_id IN ?`

func loadSnapshotTitles(ctx context.Context, db *gorm.DB, action int16, ids []int64) (map[int64][]snapshotTitle, error) {
	out := map[int64][]snapshotTitle{}
	for chunk := range slices.Chunk(ids, idChunk) {
		var rows []snapshotTitle
		if err := db.WithContext(ctx).Raw(snapshotTitlesSQL, model.EntityTypeWork, action, chunk).
			Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			out[r.EntityID] = append(out[r.EntityID], r)
		}
	}
	return out, nil
}

// A loser's own redirect is not used when a proposal exists: Unmerge repoints
// it at the rebuilt work, while the title rows the merge moved stay on the
// proposal's target.
func loadLineages(ctx context.Context, db *gorm.DB) (map[int64]*lineage, lineageStats, error) {
	var st lineageStats
	var redirects []struct {
		OldID     int64 `gorm:"column:old_id"`
		CurrentID int64 `gorm:"column:current_id"`
	}
	if err := db.WithContext(ctx).Raw(`SELECT old_id, current_id FROM catalog_redirect WHERE entity_type = ?`,
		model.EntityTypeWork).Scan(&redirects).Error; err != nil {
		return nil, st, err
	}
	var proposals []struct {
		SourceEntityID int64 `gorm:"column:source_entity_id"`
		TargetEntityID int64 `gorm:"column:target_entity_id"`
	}
	if err := db.WithContext(ctx).Raw(`SELECT source_entity_id, target_entity_id FROM catalog_merge_proposal
		WHERE entity_type = ? AND status = ? ORDER BY id`,
		model.EntityTypeWork, model.ProposalStatusExecuted).Scan(&proposals).Error; err != nil {
		return nil, st, err
	}

	current := make(map[int64]int64, len(redirects))
	for _, r := range redirects {
		current[r.OldID] = r.CurrentID
	}
	survivorsOf := map[int64][]int64{}
	attach := func(loser, survivor int64) {
		if loser == survivor || slices.Contains(survivorsOf[loser], survivor) {
			return
		}
		survivorsOf[loser] = append(survivorsOf[loser], survivor)
	}
	proposed := map[int64]struct{}{}
	for _, p := range proposals {
		proposed[p.SourceEntityID] = struct{}{}
		survivor := p.TargetEntityID
		if c, ok := current[survivor]; ok {
			survivor = c
		}
		attach(p.SourceEntityID, survivor)
	}
	for _, r := range redirects {
		if _, ok := proposed[r.OldID]; !ok {
			attach(r.OldID, r.CurrentID)
		}
	}

	losers := make([]int64, 0, len(survivorsOf))
	for id := range survivorsOf {
		losers = append(losers, id)
	}
	slices.Sort(losers)
	snaps, err := loadSnapshotTitles(ctx, db, model.RevisionActionMergedSource, losers)
	if err != nil {
		return nil, st, err
	}

	out := map[int64]*lineage{}
	for _, loser := range losers {
		snap, has := snaps[loser]
		st.Losers++
		if has {
			st.LosersWithSnapshot++
		} else {
			st.LosersSnapshotless++
		}
		for _, survivor := range survivorsOf[loser] {
			lin := out[survivor]
			if lin == nil {
				lin = newLineage()
				out[survivor] = lin
			}
			lin.losers++
			if !has {
				lin.snapshotless = true
				continue
			}
			for _, t := range snap {
				if t.TitleID == nil {
					continue
				}
				lin.loserIDs[*t.TitleID] = struct{}{}
				lin.loserPairs[pairKey{deref(t.Lang), deref(t.Title)}] = struct{}{}
			}
		}
	}

	survivors := make([]int64, 0, len(out))
	for id := range out {
		survivors = append(survivors, id)
	}
	slices.Sort(survivors)
	targets, err := loadSnapshotTitles(ctx, db, model.RevisionActionMergedTarget, survivors)
	if err != nil {
		return nil, st, err
	}
	for _, survivor := range survivors {
		lin := out[survivor]
		if lin.snapshotless {
			continue
		}
		for _, t := range targets[survivor] {
			if t.TitleID == nil {
				continue
			}
			if _, moved := lin.loserIDs[*t.TitleID]; moved {
				continue
			}
			lin.ownIDs[*t.TitleID] = struct{}{}
			lin.ownPairs[pairKey{deref(t.Lang), deref(t.Title)}] = struct{}{}
		}
	}
	return out, st, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
