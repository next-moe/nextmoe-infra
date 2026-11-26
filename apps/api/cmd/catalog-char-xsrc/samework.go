package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"

	"api/internal/platform/catalog/charname"

	"gorm.io/gorm"
)

type sameWorkStats struct {
	Keys, Merges, Ambiguous, Instance, Qualified, SharedSource, Conflicting int
}

// buildSameWork pairs a character that is on a work through a voice credit
// alone with the one roster character of that work whose name folds to the
// same key, when no third character on the work answers to it. It is the rule
// the credit importers now apply before minting, run over what they minted
// before they did.
func buildSameWork(db *gorm.DB) (map[int64]int64, map[int64]*charInfo, sameWorkStats, error) {
	var st sameWorkStats
	var rows []struct {
		WorkID      int64 `gorm:"column:work_id"`
		CharacterID int64 `gorm:"column:character_id"`
		CreditOnly  bool  `gorm:"column:credit_only"`
	}
	if err := db.Raw(`SELECT * FROM (` + presentOnWorkSQL() + `) p ORDER BY work_id, character_id`).Scan(&rows).Error; err != nil {
		return nil, nil, st, fmt.Errorf("characters on works: %w", err)
	}
	ids := map[int64]bool{}
	for _, r := range rows {
		ids[r.CharacterID] = true
	}
	info, err := loadCharInfo(db, ids)
	if err != nil {
		return nil, nil, st, err
	}

	type member struct {
		id         int64
		creditOnly bool
	}
	byWorkKey := map[int64]map[string][]member{}
	for _, r := range rows {
		c := info[r.CharacterID]
		if c == nil {
			continue
		}
		keys := map[string]bool{}
		for _, n := range append([]string{c.Name}, c.Aliases...) {
			if k := charname.Fold(n); k != "" {
				keys[k] = true
			}
		}
		if byWorkKey[r.WorkID] == nil {
			byWorkKey[r.WorkID] = map[string][]member{}
		}
		for k := range keys {
			byWorkKey[r.WorkID][k] = append(byWorkKey[r.WorkID][k], member{r.CharacterID, r.CreditOnly})
		}
	}

	target := map[int64]map[int64]bool{}
	for _, keys := range byWorkKey {
		for _, ms := range keys {
			if len(ms) < 2 {
				continue
			}
			st.Keys++
			if len(ms) != 2 || ms[0].creditOnly == ms[1].creditOnly {
				st.Ambiguous++
				continue
			}
			src, dst := ms[0], ms[1]
			if !src.creditOnly {
				src, dst = dst, src
			}
			a, b := info[src.id], info[dst.id]
			switch {
			case a.Instance || b.Instance:
				st.Instance++
				continue
			case qualifiedPair(a.Name, b.Name):
				st.Qualified++
				continue
			case sharesSource(a.Sources, b.Sources):
				st.SharedSource++
				continue
			}
			if target[src.id] == nil {
				target[src.id] = map[int64]bool{}
			}
			target[src.id][dst.id] = true
		}
	}

	out := map[int64]int64{}
	for src, dsts := range target {
		if len(dsts) != 1 {
			st.Conflicting++
			continue
		}
		for dst := range dsts {
			out[src] = dst
		}
	}
	for src, dst := range out {
		if _, chained := out[dst]; chained {
			delete(out, src)
			st.Conflicting++
		}
	}
	st.Merges = len(out)
	return out, info, st, nil
}

// sameWorkPairs carries the decided merges into emit-all as edges, so they and
// the judged ones fold into one worklist with one survivor per group.
func sameWorkPairs(merges map[int64]int64, info map[int64]*charInfo) []pairMeta {
	out := make([]pairMeta, 0, len(merges))
	for src, dst := range merges {
		a, b := info[min(src, dst)], info[max(src, dst)]
		out = append(out, pairMeta{
			A: a.ID, B: b.ID, AName: a.Name, BName: b.Name,
			ASources: a.Sources, BSources: b.Sources,
			ARich: richness{Img: a.HasImage, NAliases: len(a.Aliases)},
			BRich: richness{Img: b.HasImage, NAliases: len(b.Aliases)},
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].A < out[j].A || (out[i].A == out[j].A && out[i].B < out[j].B) })
	return out
}

func writePairs(path string, pairs []pairMeta) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, p := range pairs {
		if err := enc.Encode(p); err != nil {
			return err
		}
	}
	return nil
}

func sharesSource(a, b []string) bool {
	for _, s := range a {
		if slices.Contains(b, s) {
			return true
		}
	}
	return false
}

func runSameWork(db *gorm.DB, worklistPath string, out io.Writer) error {
	merges, _, st, err := buildSameWork(db)
	if err != nil {
		return err
	}
	bySurvivor := map[int64][]int64{}
	for src, dst := range merges {
		bySurvivor[dst] = append(bySurvivor[dst], src)
	}
	survivors := make([]int64, 0, len(bySurvivor))
	for dst := range bySurvivor {
		survivors = append(survivors, dst)
	}
	sort.Slice(survivors, func(i, j int) bool { return survivors[i] < survivors[j] })

	f, err := os.Create(worklistPath)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, dst := range survivors {
		srcs := bySurvivor[dst]
		sort.Slice(srcs, func(i, j int) bool { return srcs[i] < srcs[j] })
		if err := enc.Encode(map[string]any{"class": "character", "survivor": dst, "sources": srcs}); err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "same-work keys=%d merges=%d groups=%d ambiguous=%d instance=%d qualified=%d shared_source=%d conflicting=%d\n",
		st.Keys, st.Merges, len(survivors), st.Ambiguous, st.Instance, st.Qualified, st.SharedSource, st.Conflicting)
	return nil
}
