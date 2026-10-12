package main

import (
	"cmp"
	"slices"

	"api/internal/platform/catalog/model"
)

const (
	reasonSnapshot    = "snapshot"
	reasonDisplayName = "display_name"

	actionDemote = "demote"
	actionDelete = "delete"
)

type titleRow struct {
	ID         int64  `gorm:"column:id"`
	WorkID     int64  `gorm:"column:work_id"`
	Lang       string `gorm:"column:lang"`
	Title      string `gorm:"column:title"`
	Latin      string `gorm:"column:latin"`
	Kind       int16  `gorm:"column:kind"`
	Provenance int16  `gorm:"column:provenance"`
	Suppressed bool   `gorm:"column:suppressed"`
}

type work struct {
	ID          int64
	OLang       string
	DisplayName string
	Claimed     bool
	Titles      []titleRow
}

type change struct {
	WorkID   int64   `json:"work_id"`
	TitleID  int64   `json:"title_id"`
	Lang     string  `json:"lang"`
	Title    string  `json:"title"`
	Latin    string  `json:"latin,omitempty"`
	FromKind int16   `json:"from_kind"`
	Action   string  `json:"action"`
	ToKind   *int16  `json:"to_kind"`
	ToLang   *string `json:"to_lang"`
	Reason   string  `json:"reason"`
	Claimed  bool    `json:"claimed"`
}

func eligible(t titleRow) bool {
	return !t.Suppressed && t.Provenance == model.WorkTitleProvenanceSource && t.Lang != ""
}

type planned struct {
	row    titleRow
	reason string
}

func planWork(w work, lin *lineage) []change {
	byLang := map[string][]titleRow{}
	var langs []string
	for _, t := range w.Titles {
		if !eligible(t) {
			continue
		}
		if _, seen := byLang[t.Lang]; !seen {
			langs = append(langs, t.Lang)
		}
		byLang[t.Lang] = append(byLang[t.Lang], t)
	}
	slices.Sort(langs)

	var demote []planned
	for _, lang := range langs {
		var losers []titleRow
		ownOfficial, ownAlias := false, false
		for _, t := range byLang[lang] {
			loser := t.Title != w.DisplayName && lin.loserOrigin(t)
			switch {
			case t.Kind == model.WorkTitleKindOfficial && loser:
				losers = append(losers, t)
			case t.Kind == model.WorkTitleKindOfficial:
				ownOfficial = true
			case t.Kind == model.WorkTitleKindAlias && !loser:
				ownAlias = true
			}
		}
		if len(losers) == 0 || !(ownOfficial || (ownAlias && lang != w.OLang)) {
			continue
		}
		for _, t := range losers {
			demote = append(demote, planned{t, reasonSnapshot})
		}
	}
	if len(demote) > 0 && officialsLeft(w.Titles, demote) == 0 {
		demote = nil
	}

	if lin.snapshotless {
		var match, others []titleRow
		for _, t := range byLang[w.OLang] {
			if t.Kind != model.WorkTitleKindOfficial || isPlanned(demote, t.ID) {
				continue
			}
			if t.Title == w.DisplayName {
				match = append(match, t)
			} else {
				others = append(others, t)
			}
		}
		if len(match) == 1 {
			for _, t := range others {
				demote = append(demote, planned{t, reasonDisplayName})
			}
		}
	}
	return toChanges(w, demote)
}

func isPlanned(demote []planned, id int64) bool {
	return slices.ContainsFunc(demote, func(p planned) bool { return p.row.ID == id })
}

func officialsLeft(titles []titleRow, demote []planned) int {
	n := 0
	for _, t := range titles {
		if eligible(t) && t.Kind == model.WorkTitleKindOfficial && !isPlanned(demote, t.ID) {
			n++
		}
	}
	return n
}

func toChanges(w work, demote []planned) []change {
	if len(demote) == 0 {
		return nil
	}
	aliases := map[string]struct{}{}
	for _, t := range w.Titles {
		if t.Kind == model.WorkTitleKindAlias && t.Lang == "" {
			aliases[t.Title] = struct{}{}
		}
	}
	slices.SortFunc(demote, func(a, b planned) int { return cmp.Compare(a.row.ID, b.row.ID) })

	out := make([]change, 0, len(demote))
	for _, p := range demote {
		c := change{
			WorkID: w.ID, TitleID: p.row.ID, Lang: p.row.Lang, Title: p.row.Title, Latin: p.row.Latin, FromKind: p.row.Kind,
			Action: actionDelete, Reason: p.reason, Claimed: w.Claimed,
		}
		if _, exists := aliases[p.row.Title]; !exists {
			kind, lang := model.WorkTitleKindAlias, ""
			c.Action, c.ToKind, c.ToLang = actionDemote, &kind, &lang
			aliases[p.row.Title] = struct{}{}
		}
		out = append(out, c)
	}
	return out
}

// shownTitle must elect as nativeWorkTitles + workLocalized do (ORDER BY
// provenance, kind, id; first official, alias or abbreviation row), or the
// leftover list reports a display the read face does not serve.
func shownTitle(titles []titleRow, changes []change, lang string) (titleRow, bool) {
	gone := make(map[int64]struct{}, len(changes))
	for _, c := range changes {
		gone[c.TitleID] = struct{}{}
	}
	var best titleRow
	found := false
	for _, t := range titles {
		if _, ok := gone[t.ID]; ok || t.Suppressed || t.Lang != lang || t.Kind > model.WorkTitleKindAbbreviation {
			continue
		}
		if !found || t.Provenance < best.Provenance ||
			t.Provenance == best.Provenance && (t.Kind < best.Kind || t.Kind == best.Kind && t.ID < best.ID) {
			best, found = t, true
		}
	}
	return best, found
}
