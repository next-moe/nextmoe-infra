package main

import (
	"cmp"
	"slices"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
)

const (
	reasonSnapshot         = "snapshot"
	reasonDisplayName      = "display_name"
	reasonOwnAliasPromoted = "own_alias_promoted"

	actionDemote  = "demote"
	actionDelete  = "delete"
	actionPromote = "promote"
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
	Suppressed  map[string]struct{}
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

	var demote, promote []planned
	for _, lang := range langs {
		var losers, ownAliases []titleRow
		ownOfficial := false
		for _, t := range byLang[lang] {
			loser := t.Title != w.DisplayName && lin.loserOrigin(t)
			switch {
			case t.Kind == model.WorkTitleKindOfficial && loser:
				losers = append(losers, t)
			case t.Kind == model.WorkTitleKindOfficial:
				ownOfficial = true
			case t.Kind == model.WorkTitleKindAlias && !loser:
				ownAliases = append(ownAliases, t)
			}
		}
		if len(losers) == 0 {
			continue
		}
		if !ownOfficial {
			heir, ok := pickHeir(w, ownAliases, losers)
			if !ok {
				continue
			}
			promote = append(promote, planned{heir, reasonOwnAliasPromoted})
		}
		for _, t := range losers {
			demote = append(demote, planned{t, reasonSnapshot})
		}
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
	return toChanges(w, demote, promote)
}

func isPlanned(demote []planned, id int64) bool {
	return slices.ContainsFunc(demote, func(p planned) bool { return p.row.ID == id })
}

func pickHeir(w work, aliases, losers []titleRow) (titleRow, bool) {
	var heirs []titleRow
	for _, a := range aliases {
		if editspec.TitleSuppressed(w.Suppressed, model.WorkTitleKindOfficial, a.Lang, a.Title) {
			continue
		}
		taken := slices.ContainsFunc(w.Titles, func(t titleRow) bool {
			return t.Kind == model.WorkTitleKindOfficial && t.Lang == a.Lang && t.Title == a.Title &&
				!slices.ContainsFunc(losers, func(l titleRow) bool { return l.ID == t.ID })
		})
		if !taken {
			heirs = append(heirs, a)
		}
	}
	if len(heirs) == 0 {
		return titleRow{}, false
	}
	if i := slices.IndexFunc(heirs, func(t titleRow) bool { return t.Title == w.DisplayName }); i >= 0 {
		return heirs[i], true
	}
	return slices.MinFunc(heirs, func(a, b titleRow) int { return cmp.Compare(a.ID, b.ID) }), true
}

func toChanges(w work, demote, promote []planned) []change {
	if len(demote) == 0 {
		return nil
	}
	aliases := map[string]struct{}{}
	for _, t := range w.Titles {
		if t.Kind == model.WorkTitleKindAlias && t.Lang == "" {
			aliases[t.Title] = struct{}{}
		}
	}
	byID := func(a, b planned) int { return cmp.Compare(a.row.ID, b.row.ID) }
	slices.SortFunc(demote, byID)
	slices.SortFunc(promote, byID)

	out := make([]change, 0, len(demote)+len(promote))
	for _, p := range demote {
		c := newChange(w, p, actionDelete)
		if _, exists := aliases[p.row.Title]; !exists {
			kind, lang := model.WorkTitleKindAlias, ""
			c.Action, c.ToKind, c.ToLang = actionDemote, &kind, &lang
			aliases[p.row.Title] = struct{}{}
		}
		out = append(out, c)
	}
	for _, p := range promote {
		c := newChange(w, p, actionPromote)
		kind, lang := model.WorkTitleKindOfficial, p.row.Lang
		c.ToKind, c.ToLang = &kind, &lang
		out = append(out, c)
	}
	return out
}

func newChange(w work, p planned, action string) change {
	return change{
		WorkID: w.ID, TitleID: p.row.ID, Lang: p.row.Lang, Title: p.row.Title, Latin: p.row.Latin,
		FromKind: p.row.Kind, Action: action, Reason: p.reason, Claimed: w.Claimed,
	}
}

// shownTitle must elect as nativeWorkTitles + workLocalized do (ORDER BY
// provenance, kind, id; first official, alias or abbreviation row), or the
// leftover list reports a display the read face does not serve.
func shownTitle(titles []titleRow, changes []change, lang string) (titleRow, bool) {
	applied := make(map[int64]change, len(changes))
	for _, c := range changes {
		applied[c.TitleID] = c
	}
	var best titleRow
	found := false
	for _, t := range titles {
		if c, ok := applied[t.ID]; ok {
			if c.Action != actionPromote {
				continue
			}
			t.Kind = *c.ToKind
		}
		if t.Suppressed || t.Lang != lang || t.Kind > model.WorkTitleKindAbbreviation {
			continue
		}
		if !found || t.Provenance < best.Provenance ||
			t.Provenance == best.Provenance && (t.Kind < best.Kind || t.Kind == best.Kind && t.ID < best.ID) {
			best, found = t, true
		}
	}
	return best, found
}
