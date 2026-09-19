package vndbtags

import "sort"

type Existing struct {
	ID      int64
	Name    string
	Spoiler int16
	Count   int
	Sexual  bool
}

type Insert struct {
	Name    string
	Spoiler int16
	Count   int
	Sexual  bool
}

type Update struct {
	ID         int64
	Name       string
	OldSpoiler int16
	NewSpoiler int16
	OldCount   int
	NewCount   int
	Sexual     bool
}

type Delete struct {
	ID      int64
	Name    string
	Spoiler int16
	Count   int
}

type Plan struct {
	Inserts []Insert
	Updates []Update
	Deletes []Delete
	Same    int
}

func (p Plan) hasWrites() bool {
	return len(p.Inserts)+len(p.Updates)+len(p.Deletes) > 0
}

func diffTags(existing []Existing, desired map[string]Desired, sexualByName map[string]bool) Plan {
	byName := make(map[string]Existing, len(existing))
	for _, e := range existing {
		byName[e.Name] = e
	}
	var p Plan
	for name, e := range byName {
		d, ok := desired[name]
		if !ok {
			p.Deletes = append(p.Deletes, Delete{
				ID: e.ID, Name: e.Name, Spoiler: e.Spoiler, Count: e.Count,
			})
			continue
		}
		if e.Spoiler == d.Spoiler && e.Count == d.Count {
			p.Same++
			continue
		}
		p.Updates = append(p.Updates, Update{
			ID: e.ID, Name: e.Name,
			OldSpoiler: e.Spoiler, NewSpoiler: d.Spoiler,
			OldCount: e.Count, NewCount: d.Count,
			Sexual: e.Sexual,
		})
	}
	for name, d := range desired {
		if _, ok := byName[name]; ok {
			continue
		}
		sexual, ok := sexualByName[name]
		if !ok {
			sexual = d.Ero
		}
		p.Inserts = append(p.Inserts, Insert{
			Name: name, Spoiler: d.Spoiler, Count: d.Count, Sexual: sexual,
		})
	}
	sort.Slice(p.Inserts, func(i, j int) bool { return p.Inserts[i].Name < p.Inserts[j].Name })
	sort.Slice(p.Updates, func(i, j int) bool { return p.Updates[i].Name < p.Updates[j].Name })
	sort.Slice(p.Deletes, func(i, j int) bool { return p.Deletes[i].Name < p.Deletes[j].Name })
	return p
}
