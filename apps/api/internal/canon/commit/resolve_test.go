package commit

import (
	"slices"
	"testing"
)

func row() Changeset {
	v := func(x any) Value { return Value{Raw: raw(x)} }
	return Changeset{Changes: []Change{
		{Ref: "work", Entity: "work", Values: map[string]Value{"medium": v("novel")}},
		{Ref: "work.title", Entity: "work_title", Values: map[string]Value{"work_id": v("ref:work"), "text": v("t")}},
		{Ref: "person.a", Entity: "person", Values: map[string]Value{}},
		{Ref: "alias.a", Entity: "person_alias", Values: map[string]Value{"person_id": v("ref:person.a"), "name": v("n")}},
		{Ref: "credit.0", Entity: "credit", Values: map[string]Value{"work_id": v("ref:work"), "alias_id": v("ref:alias.a")}},
		{Ref: "release.isbn.1", Entity: "release", Values: map[string]Value{"released": v("2025-08"), "lang": v("ja")}},
		{Ref: "book.isbn.1", Entity: "book_release", Values: map[string]Value{"release_id": v("ref:release.isbn.1")}},
	}}
}

func refs(cs Changeset) []string {
	var out []string
	for _, ch := range cs.Changes {
		out = append(out, ch.Ref)
	}
	return out
}

func TestOmitting(t *testing.T) {
	for _, c := range []struct {
		omit    []string
		want    []string
		unknown int
	}{
		{[]string{"credit.0"}, []string{"work", "work.title", "release.isbn.1", "book.isbn.1"}, 0},
		{[]string{"release.isbn.1"}, []string{"work", "work.title", "person.a", "alias.a", "credit.0"}, 0},
		{[]string{"release.isbn.1.released"}, refs(row()), 0},
		{[]string{"release.isbn.1.price"}, refs(row()), 1},
		// The credit goes with the work, and the name nothing credits with it.
		{[]string{"work"}, []string{"release.isbn.1", "book.isbn.1"}, 0},
	} {
		got, unknown := row().Omitting(c.omit...)
		if !slices.Equal(refs(got), c.want) || len(unknown) != c.unknown {
			t.Errorf("omit %v: %v unknown %v, want %v", c.omit, refs(got), unknown, c.want)
		}
	}
	got, _ := row().Omitting("release.isbn.1.released")
	for _, ch := range got.Changes {
		if _, has := ch.Values["released"]; has {
			t.Error("the omitted property is still there")
		}
	}
	if _, has := row().Changes[5].Values["released"]; !has {
		t.Error("omitting changed the original")
	}
}

func TestCiting(t *testing.T) {
	if got := refs(row().Citing("alias.a", 7)); !slices.Equal(got, []string{"work", "work.title", "credit.0", "release.isbn.1", "book.isbn.1"}) {
		t.Errorf("citing an alias drops its person: %v", got)
	}
	cited := row().Citing("work", 9)
	if got := refs(cited); !slices.Equal(got, []string{"person.a", "alias.a", "credit.0", "release.isbn.1", "book.isbn.1"}) {
		t.Errorf("citing a work drops what describes it: %v", got)
	}
}
