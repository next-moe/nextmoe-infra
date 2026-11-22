package commit

import (
	"context"
	"testing"

	"api/internal/canon/canontest"
	"api/internal/canon/ent/releasecontent"
	"api/internal/canon/ent/workpart"
)

// The paper test of plan 03 §8: other media fit the core without a core
// change, only module tables. A failure here means the core is wrong.
func TestPaperTestOtherMediaFitTheCore(t *testing.T) {
	client, _ := canontest.Client(t)
	ctx := context.Background()
	sys := Actor{Kind: ActorSystem, Key: "paper-test"}
	s := New(client)
	sv := func(x any) Value { return Value{Raw: raw(x)} }

	manga, err := s.Apply(ctx, Changeset{Actor: sys, Changes: []Change{
		{Ref: "m", Entity: "work", Values: map[string]Value{"medium": sv("manga"), "status": sv("ongoing")}},
		{Ref: "v1", Entity: "work_part", Values: map[string]Value{"work_id": sv("ref:m"), "kind": sv("volume"), "ordinal": sv(1)}},
		{Entity: "work_part", Values: map[string]Value{"work_id": sv("ref:m"), "parent_id": sv("ref:v1"), "kind": sv("chapter"), "ordinal": sv(1)}},
		{Entity: "work_part", Values: map[string]Value{"work_id": sv("ref:m"), "parent_id": sv("ref:v1"), "kind": sv("chapter"), "ordinal": sv(1.5), "label": sv("番外編")}},
		{Entity: "work_part", Values: map[string]Value{"work_id": sv("ref:m"), "kind": sv("chapter"), "ordinal": sv(9), "released": sv("2026-11-20")}},
		{Ref: "tank", Entity: "release", Values: map[string]Value{"lang": sv("ja"), "format": sv("paperback")}},
		{Entity: "release_content", Values: map[string]Value{"release_id": sv("ref:tank"), "work_id": sv("ref:m"), "part_id": sv("ref:v1")}},
	}})
	if err != nil {
		t.Fatalf("manga: %v", err)
	}
	chapters, _ := client.WorkPart.Query().Where(workpart.WorkID(manga.IDs["m"]), workpart.Kind("chapter")).Count(ctx)
	loose, _ := client.WorkPart.Query().Where(workpart.WorkID(manga.IDs["m"]), workpart.Kind("chapter"), workpart.ParentIDIsNil()).Count(ctx)
	if chapters != 3 || loose != 1 {
		t.Errorf("manga chapters %d, not yet in a volume %d", chapters, loose)
	}

	anime, err := s.Apply(ctx, Changeset{Actor: sys, Changes: []Change{
		{Ref: "a", Entity: "work", Values: map[string]Value{"medium": sv("anime")}},
		{Ref: "s1", Entity: "work_part", Values: map[string]Value{"work_id": sv("ref:a"), "kind": sv("season"), "ordinal": sv(1)}},
		{Entity: "work_part", Values: map[string]Value{"work_id": sv("ref:a"), "parent_id": sv("ref:s1"), "kind": sv("episode"), "ordinal": sv(1)}},
		{Entity: "work_part", Values: map[string]Value{"work_id": sv("ref:a"), "parent_id": sv("ref:s1"), "kind": sv("episode"), "ordinal": sv(2)}},
	}})
	if err != nil {
		t.Fatalf("anime: %v", err)
	}
	if n, _ := client.WorkPart.Query().Where(workpart.WorkID(anime.IDs["a"]), workpart.Kind("episode")).Count(ctx); n != 2 {
		t.Errorf("anime episodes %d", n)
	}

	set, err := s.Apply(ctx, Changeset{Actor: sys, Changes: []Change{
		{Ref: "g1", Entity: "work", Values: map[string]Value{"medium": sv("game")}},
		{Ref: "g2", Entity: "work", Values: map[string]Value{"medium": sv("game")}},
		{Ref: "box", Entity: "release", Values: map[string]Value{"lang": sv("ja"), "format": sv("digital"), "edition_kind": sv("limited")}},
		{Entity: "release_content", Values: map[string]Value{"release_id": sv("ref:box"), "work_id": sv("ref:g1"), "position": sv(0)}},
		{Entity: "release_content", Values: map[string]Value{"release_id": sv("ref:box"), "work_id": sv("ref:g2"), "portion": sv("partial")}},
	}})
	if err != nil {
		t.Fatalf("compilation: %v", err)
	}
	if n, _ := client.ReleaseContent.Query().Where(releasecontent.ReleaseID(set.IDs["box"])).Count(ctx); n != 2 {
		t.Errorf("a compilation holds %d works", n)
	}
}
