package getchuchars

import (
	"regexp"
	"sort"
	"strings"

	"golang.org/x/text/unicode/norm"
)

const (
	MatchDisplayName = "name"
	MatchAlias       = "alias"
	MatchReading     = "reading"
)

var reSpace = regexp.MustCompile(`[[:space:]　]`)

func normKey(s string) string {
	return strings.ToLower(norm.NFKC.String(reSpace.ReplaceAllString(s, "")))
}

type rosterIndex struct {
	byName  map[string][]int64
	byAlias map[string][]int64
	bundle  bool
}

func buildIndex(rows []rosterRow) map[string]*rosterIndex {
	out := map[string]*rosterIndex{}
	for _, r := range rows {
		idx := out[r.GetchuID]
		if idx == nil {
			idx = &rosterIndex{byName: map[string][]int64{}, byAlias: map[string][]int64{}}
			out[r.GetchuID] = idx
		}
		idx.bundle = idx.bundle || r.Bundle
		if r.KeyName != "" {
			idx.byName[r.KeyName] = appendUnique(idx.byName[r.KeyName], r.CharacterID)
		}
		if r.KeyAlias != "" {
			idx.byAlias[r.KeyAlias] = appendUnique(idx.byAlias[r.KeyAlias], r.CharacterID)
		}
	}
	return out
}

func appendUnique(xs []int64, v int64) []int64 {
	for _, x := range xs {
		if x == v {
			return xs
		}
	}
	return append(xs, v)
}

type MatchStats struct {
	Input        int
	NoWork       int
	Bundle       int
	Matched      int
	ByName       int
	ByAlias      int
	ByReading    int
	Ambiguous    int
	Collided     int
	Deduped      int
	NoNameInWork int
}

func match(chars []getchuChar, idx map[string]*rosterIndex) ([]Candidate, MatchStats) {
	st := MatchStats{Input: len(chars)}
	type hit struct {
		cand Candidate
		ok   bool
	}
	hits := make([]hit, len(chars))
	claims := map[int64]int{}

	for i, c := range chars {
		ri := idx[c.GetchuID]
		if ri == nil {
			st.NoWork++
			continue
		}
		if ri.bundle {
			st.Bundle++
			continue
		}
		nk, rk := normKey(c.Name), normKey(c.Reading)

		var ids []int64
		var by string
		switch {
		case len(ri.byName[nk]) > 0:
			ids, by = ri.byName[nk], MatchDisplayName
		case len(ri.byAlias[nk]) > 0:
			ids, by = ri.byAlias[nk], MatchAlias
		case rk != "" && len(ri.byName[rk]) > 0:
			ids, by = ri.byName[rk], MatchReading
		case rk != "" && len(ri.byAlias[rk]) > 0:
			ids, by = ri.byAlias[rk], MatchReading
		default:
			st.NoNameInWork++
			continue
		}
		if len(ids) != 1 {
			st.Ambiguous++
			continue
		}
		hits[i] = hit{Candidate{
			CharacterID: ids[0], GetchuID: c.GetchuID, Ordinal: c.Ordinal,
			Name: c.Name, Profile: c.Profile, Attrs: c.Attrs, MatchedBy: by,
		}, true}
		claims[ids[0]]++
	}

	byChar := map[int64][]Candidate{}
	for _, h := range hits {
		if h.ok {
			byChar[h.cand.CharacterID] = append(byChar[h.cand.CharacterID], h.cand)
		}
	}

	out := make([]Candidate, 0, len(chars))
	for _, group := range byChar {
		if len(group) > 1 {
			sameItem := true
			for _, c := range group[1:] {
				if c.GetchuID != group[0].GetchuID {
					sameItem = false
					break
				}
			}
			if sameItem {
				st.Collided += len(group)
				continue
			}
			st.Deduped += len(group) - 1
			group = []Candidate{withEditions(richest(group), group)}
		} else {
			group = []Candidate{withEditions(group[0], group)}
		}
		h := struct{ cand Candidate }{group[0]}
		st.Matched++
		switch h.cand.MatchedBy {
		case MatchDisplayName:
			st.ByName++
		case MatchAlias:
			st.ByAlias++
		case MatchReading:
			st.ByReading++
		}
		out = append(out, h.cand)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CharacterID != out[j].CharacterID {
			return out[i].CharacterID < out[j].CharacterID
		}
		return out[i].Ordinal < out[j].Ordinal
	})
	return out, st
}

func withEditions(chosen Candidate, group []Candidate) Candidate {
	eds := make([]Edition, 0, len(group))
	eds = append(eds, Edition{GetchuID: chosen.GetchuID, Ordinal: chosen.Ordinal})
	rest := make([]Candidate, 0, len(group))
	for _, c := range group {
		if c.GetchuID != chosen.GetchuID || c.Ordinal != chosen.Ordinal {
			rest = append(rest, c)
		}
	}
	sort.Slice(rest, func(i, j int) bool {
		if rest[i].GetchuID != rest[j].GetchuID {
			return rest[i].GetchuID < rest[j].GetchuID
		}
		return rest[i].Ordinal < rest[j].Ordinal
	})
	for _, c := range rest {
		eds = append(eds, Edition{GetchuID: c.GetchuID, Ordinal: c.Ordinal})
	}
	chosen.Editions = eds
	return chosen
}

func richest(group []Candidate) Candidate {
	best := group[0]
	for _, c := range group[1:] {
		switch {
		case len(c.Profile) > len(best.Profile):
			best = c
		case len(c.Profile) == len(best.Profile):
			if c.GetchuID < best.GetchuID || (c.GetchuID == best.GetchuID && c.Ordinal < best.Ordinal) {
				best = c
			}
		}
	}
	return best
}
