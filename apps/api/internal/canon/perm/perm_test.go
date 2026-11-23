package perm

import (
	"slices"
	"testing"

	"api/internal/canon/registry"
	"api/internal/platform/authz"
)

func TestEveryTodoKindHasADecidingKey(t *testing.T) {
	for _, k := range registry.TodoKinds {
		if _, ok := ForTodo(k); !ok {
			t.Errorf("todo kind %q has no key; its todos could never be settled", k)
		}
	}
}

func TestMergedFollowsTheOverlayRule(t *testing.T) {
	got := Merged(Bundles, []Override{
		{"editor", string(TodoValue), "grant"},
		{"moderator", string(Takedown), "deny"},
		{"ren", string(Takedown), "deny"},
		{"editor", "catalog.review", "grant"},
		{"editor", string(TodoValue), "grant"},
	})
	r := authz.NewResolver(got)
	cases := []struct {
		role string
		p    authz.Permission
		want bool
	}{
		{"editor", TodoValue, true},
		{"editor", TodoIdentity, false},
		{"editor", "catalog.review", false},
		{"moderator", Takedown, false},
		{"moderator", Merge, true},
		{"ren", Takedown, true},
		{"admin", Takedown, true},
		{"user", TodoValue, false},
	}
	for _, c := range cases {
		if r.Can([]string{c.role}, c.p) != c.want {
			t.Errorf("%s %s: want %v", c.role, c.p, c.want)
		}
	}
	if n := len(got["editor"]); n != 1 {
		t.Errorf("a repeated grant must not duplicate: editor holds %v", got["editor"])
	}
	if !slices.Contains(Bundles["moderator"], Takedown) {
		t.Error("Merged changed the code bundles it was given")
	}
}
