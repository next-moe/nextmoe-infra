package main

import (
	"context"
	"fmt"
	"io"
	"sort"

	"api/internal/platform/catalog/llmsuggest"
	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

type seedKeyPair struct {
	a, b   int64
	reason int16
}

func runSeedKeys(ctx context.Context, db *gorm.DB, w io.Writer, _ int64, limit int, run bool) error {
	stripped, declared, err := discoverSeedKeyPairs(ctx, db)
	if err != nil {
		return err
	}
	union := map[[2]int64]int16{}
	for k := range stripped {
		union[k] = model.CandidateReasonNameFuzzy
	}
	for k := range declared {
		union[k] = model.CandidateReasonSharedExternalID
	}

	existing, err := loadExistingWorkPairs(ctx, db)
	if err != nil {
		return err
	}
	refs, err := loadExactWorkRefs(ctx, db, unionIDs(union))
	if err != nil {
		return err
	}
	exempt := map[int16]struct{}{}
	for _, id := range model.IdentityVetoExemptSourceIDsFor(model.EntityTypeWork) {
		exempt[id] = struct{}{}
	}

	var planned []seedKeyPair
	existingSkips, conflictSkips := 0, 0
	for k, reason := range union {
		if existing[k] {
			existingSkips++
			continue
		}
		if conflictingExactWorkRefs(k[0], k[1], refs, exempt) {
			conflictSkips++
			continue
		}
		planned = append(planned, seedKeyPair{a: k[0], b: k[1], reason: reason})
	}
	sort.Slice(planned, func(i, j int) bool {
		if planned[i].a != planned[j].a {
			return planned[i].a < planned[j].a
		}
		return planned[i].b < planned[j].b
	})

	if limit < 0 {
		limit = 0
	}
	limited := 0
	if limit < len(planned) {
		limited = len(planned) - limit
		planned = planned[:limit]
	}

	written := 0
	if run && len(planned) > 0 {
		rows := make([]model.CatalogMatchCandidate, len(planned))
		for i, p := range planned {
			rows[i] = model.CatalogMatchCandidate{
				EntityType: model.EntityTypeWork, AID: p.a, BID: p.b,
				Reason: p.reason, Status: model.CandidateStatusPending,
			}
		}
		written, err = insertCandidates(ctx, db, rows)
		if err != nil {
			return err
		}
	}

	mode := "DRY-RUN"
	if run {
		mode = "APPLIED"
	}
	fmt.Fprintf(w, "%s [seed-keys] stripped_pairs=%d declared_pairs=%d existing_skips=%d conflict_skips=%d limited=%d written=%d\n",
		mode, len(stripped), len(declared), existingSkips, conflictSkips, limited, written)
	return nil
}

func discoverSeedKeyPairs(ctx context.Context, db *gorm.DB) (stripped, declared map[[2]int64]struct{}, err error) {
	stripped = map[[2]int64]struct{}{}
	declared = map[[2]int64]struct{}{}
	type seedWork struct {
		ID          int64  `gorm:"column:id"`
		MediumID    int16  `gorm:"column:medium_id"`
		DisplayName string `gorm:"column:display_name"`
	}
	var works []seedWork
	// Galgame only: over every medium a dry run on 2026-09-18 found 21,065
	// stripped pairs, about 20,000 of them among the 144,523 ASMR works, where
	// circles reuse one title for different works.
	if err = db.WithContext(ctx).Raw(`
		SELECT w.id, w.medium_id, w.display_name FROM catalog_work w
		JOIN catalog_medium m ON m.id = w.medium_id AND m.key = 'galgame'
		WHERE w.deleted_at IS NULL AND w.status IN (?, ?)`,
		model.WorkStatusLive, model.WorkStatusStub).Scan(&works).Error; err != nil {
		return nil, nil, err
	}
	eligible := map[int64]seedWork{}
	for _, w := range works {
		eligible[w.ID] = w
	}

	holders, err := llmsuggest.LoadStrippedHolders(db)
	if err != nil {
		return nil, nil, err
	}
	seen := map[[2]int64]struct{}{}
	for _, hs := range holders {
		if len(hs) != 2 {
			continue
		}
		var ids []int64
		for id := range hs {
			ids = append(ids, id)
		}
		a, b := ids[0], ids[1]
		wa, oka := eligible[a]
		wb, okb := eligible[b]
		if !oka || !okb || wa.MediumID != wb.MediumID {
			continue
		}
		got := llmsuggest.StrippedKey(wa.DisplayName, wb.DisplayName, holders)
		if got == "" || len(holders[got]) != 2 {
			continue
		}
		lo, hi := min(a, b), max(a, b)
		k := [2]int64{lo, hi}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		stripped[k] = struct{}{}
	}

	idx, err := llmsuggest.LoadDeclaredIndex(db)
	if err != nil {
		return nil, nil, err
	}
	for _, p := range idx.ExclusiveWorkPairs() {
		wa, oka := eligible[p[0]]
		wb, okb := eligible[p[1]]
		if !oka || !okb || wa.MediumID != wb.MediumID {
			continue
		}
		if sub, _ := idx.Hit(p[0], p[1]); sub == "" {
			continue
		}
		declared[[2]int64{p[0], p[1]}] = struct{}{}
	}
	return stripped, declared, nil
}

func loadExistingWorkPairs(ctx context.Context, db *gorm.DB) (map[[2]int64]bool, error) {
	var rows []struct {
		A int64 `gorm:"column:a_id"`
		B int64 `gorm:"column:b_id"`
	}
	if err := db.WithContext(ctx).Raw(
		`SELECT a_id, b_id FROM catalog_match_candidate WHERE entity_type = ?`,
		model.EntityTypeWork).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := map[[2]int64]bool{}
	for _, r := range rows {
		out[[2]int64{r.A, r.B}] = true
	}
	return out, nil
}

type exactWorkRef struct {
	WorkID     int64  `gorm:"column:entity_id"`
	SourceID   int16  `gorm:"column:source_id"`
	ExternalID string `gorm:"column:external_id"`
}

func loadExactWorkRefs(ctx context.Context, db *gorm.DB, ids []int64) (map[int64][]exactWorkRef, error) {
	out := map[int64][]exactWorkRef{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []exactWorkRef
	if err := db.WithContext(ctx).Raw(`
		SELECT entity_id, source_id, external_id FROM catalog_external_ref
		WHERE entity_type = ? AND link_kind = ? AND dead_at IS NULL AND entity_id IN ?`,
		model.EntityTypeWork, model.LinkKindExact, ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.WorkID] = append(out[r.WorkID], r)
	}
	return out, nil
}

func unionIDs(union map[[2]int64]int16) []int64 {
	seen := map[int64]struct{}{}
	var ids []int64
	for k := range union {
		for _, id := range []int64{k[0], k[1]} {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	return ids
}

func conflictingExactWorkRefs(a, b int64, refs map[int64][]exactWorkRef, exempt map[int16]struct{}) bool {
	bySrc := map[int16][]string{}
	for _, r := range refs[b] {
		if _, ok := exempt[r.SourceID]; ok {
			continue
		}
		bySrc[r.SourceID] = append(bySrc[r.SourceID], r.ExternalID)
	}
	for _, r := range refs[a] {
		if _, ok := exempt[r.SourceID]; ok {
			continue
		}
		for _, ext := range bySrc[r.SourceID] {
			if ext != r.ExternalID {
				return true
			}
		}
	}
	return false
}
