package vndbtags

import (
	"testing"
)

func TestDiffTagsInsertUpdateSameDelete(t *testing.T) {
	existing := []Existing{
		{ID: 1, Name: "keep", Spoiler: 0, Count: 2, Sexual: false},
		{ID: 2, Name: "spoil-only", Spoiler: 0, Count: 5, Sexual: true},
		{ID: 3, Name: "count-only", Spoiler: 1, Count: 1, Sexual: true},
		{ID: 4, Name: "gone", Spoiler: 2, Count: 9, Sexual: false},
	}
	desired := map[string]Desired{
		"keep":        {Spoiler: 0, Count: 2},
		"spoil-only":  {Spoiler: 1, Count: 5, Ero: true},
		"count-only":  {Spoiler: 1, Count: 4},
		"fresh-ero":   {Spoiler: 0, Count: 3, Ero: true},
		"fresh-cont":  {Spoiler: 0, Count: 1, Ero: false},
		"known-true":  {Spoiler: 0, Count: 2, Ero: false},
		"known-false": {Spoiler: 1, Count: 2, Ero: true},
	}
	sexual := map[string]bool{
		"known-true":  true,
		"known-false": false,
	}
	p := diffTags(existing, desired, sexual)

	if p.Same != 1 {
		t.Errorf("same=%d, want 1", p.Same)
	}
	if len(p.Deletes) != 1 || p.Deletes[0].Name != "gone" || p.Deletes[0].ID != 4 {
		t.Errorf("deletes=%+v, want gone id=4", p.Deletes)
	}
	if len(p.Updates) != 2 {
		t.Fatalf("updates=%+v, want 2", p.Updates)
	}
	byName := map[string]Update{}
	for _, u := range p.Updates {
		byName[u.Name] = u
	}
	if u := byName["spoil-only"]; u.OldSpoiler != 0 || u.NewSpoiler != 1 || u.OldCount != 5 || u.NewCount != 5 || !u.Sexual {
		t.Errorf("spoiler-only update: %+v", u)
	}
	if u := byName["count-only"]; u.OldCount != 1 || u.NewCount != 4 || u.OldSpoiler != 1 || u.NewSpoiler != 1 || !u.Sexual {
		t.Errorf("count-only update: %+v", u)
	}

	ins := map[string]Insert{}
	for _, i := range p.Inserts {
		ins[i.Name] = i
	}
	if len(ins) != 4 {
		t.Fatalf("inserts=%+v, want 4", p.Inserts)
	}
	if i := ins["fresh-ero"]; !i.Sexual || i.Count != 3 {
		t.Errorf("new ero name must take ero: %+v", i)
	}
	if i := ins["fresh-cont"]; i.Sexual {
		t.Errorf("new cont name must not be sexual: %+v", i)
	}
	if i := ins["known-true"]; !i.Sexual {
		t.Errorf("existing name sexual true must be reused: %+v", i)
	}
	if i := ins["known-false"]; i.Sexual {
		t.Errorf("existing name sexual false must be reused (not ero): %+v", i)
	}
}
