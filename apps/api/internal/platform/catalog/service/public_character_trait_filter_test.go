package service

import (
	"testing"

	"api/internal/platform/catalog/model"
)

func TestCharacterTraitWhere(t *testing.T) {
	a := []int64{1, 2}
	b := []int64{3}
	where, args, empty := characterTraitWhere([][]int64{a, b}, false)
	if empty || len(where) != 2 || len(args) != 4 {
		t.Fatalf("all: where=%d args=%d empty=%v", len(where), len(args), empty)
	}
	if where[0] != characterTraitExistsSQL || where[1] != characterTraitExistsSQL {
		t.Fatalf("sql %v", where)
	}
	if args[1] != model.SpoilerNone || args[3] != model.SpoilerNone {
		t.Fatalf("spoiler %v", args)
	}

	where, args, empty = characterTraitWhere([][]int64{a, b}, true)
	if empty || len(where) != 1 || len(args) != 2 {
		t.Fatalf("any: where=%d args=%d empty=%v", len(where), len(args), empty)
	}
	union, ok := args[0].([]int64)
	if !ok || len(union) != 3 {
		t.Fatalf("union %v", args[0])
	}

	_, _, empty = characterTraitWhere([][]int64{a, nil}, false)
	if !empty {
		t.Fatal("all unknown is empty")
	}
	_, _, empty = characterTraitWhere([][]int64{nil, nil}, true)
	if !empty {
		t.Fatal("any all-unknown is empty")
	}
	where, args, empty = characterTraitWhere([][]int64{a, nil}, true)
	if empty || len(where) != 1 {
		t.Fatalf("any with unknown: empty=%v where=%d", empty, len(where))
	}
	union, ok = args[0].([]int64)
	if !ok || len(union) != 2 {
		t.Fatalf("any union %v", args[0])
	}
}
