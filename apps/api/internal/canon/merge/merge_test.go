package merge_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"api/internal/canon/api"
	"api/internal/canon/canontest"
	"api/internal/canon/commit"
	"api/internal/canon/ent/bookwork"
	"api/internal/canon/ent/librarypartprogress"
	"api/internal/canon/ent/redirect"
	"api/internal/canon/ent/workpart"
	"api/internal/canon/ent/worktitle"
	"api/internal/canon/ent/xreflink"
	"api/internal/canon/identity"
	"api/internal/canon/merge"
)

func sv(x any) commit.Value { b, _ := json.Marshal(x); return commit.Value{Raw: b} }

func work(t *testing.T, s *commit.Service, title string) int64 {
	t.Helper()
	res, err := s.Apply(context.Background(), commit.Changeset{Actor: commit.Actor{Kind: commit.ActorSystem, Key: "test"}, Changes: []commit.Change{
		{Ref: "w", Entity: "work", Values: map[string]commit.Value{"medium": sv("novel")}},
		{Entity: "work_title", Values: map[string]commit.Value{"work_id": sv("ref:w"), "text": sv(title), "lang": sv("ja"), "kind": sv("official")}},
		{Entity: "work_part", Values: map[string]commit.Value{"work_id": sv("ref:w"), "kind": sv("volume"), "ordinal": sv(1)}},
		{Entity: "book_work", Values: map[string]commit.Value{"work_id": sv("ref:w"), "category": sv("light_novel")}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return res.IDs["w"]
}

func TestMergeAndUnmergeRoundTrip(t *testing.T) {
	client, _ := canontest.Client(t)
	ctx := context.Background()
	s := commit.New(client)
	tails := []string{canontest.Title("の正"), canontest.Title("の副"), canontest.Title("の旧")}
	survivor, loser, older := work(t, s, tails[0]), work(t, s, tails[1]), work(t, s, tails[2])
	if err := client.XrefLink.Create().SetSource("vndb").SetExternalID("v" + strconv.FormatInt(loser, 10)).SetEntity("work").SetEntityID(loser).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := merge.Merge(ctx, client, "work", older, loser, "user:1", "same series"); err != nil {
		t.Fatalf("older into loser: %v", err)
	}

	id, err := merge.Merge(ctx, client, "work", loser, survivor, "user:1", "same series")
	if err != nil {
		t.Fatal(err)
	}
	titles, _ := client.WorkTitle.Query().Where(worktitle.WorkID(survivor)).Count(ctx)
	parts, _ := client.WorkPart.Query().Where(workpart.WorkID(survivor)).Count(ctx)
	books, _ := client.BookWork.Query().Where(bookwork.WorkID(survivor)).Count(ctx)
	xr, _ := client.XrefLink.Query().Where(xreflink.EntityID(survivor)).Count(ctx)
	found, _ := identity.Find(ctx, client, "work", "novel", tails[1], 0)
	if len(found) != 1 || found[0].ID != survivor {
		t.Errorf("the loser's title finds the survivor: %+v", found)
	}
	if titles != 3 || parts != 3 || books != 1 || xr != 1 {
		t.Errorf("after merge: titles %d parts %d book_work %d xref %d", titles, parts, books, xr)
	}
	if to, ok, _ := merge.Resolve(ctx, client, "work", older); !ok || to != survivor {
		t.Errorf("the older redirect was not flattened: %d %v", to, ok)
	}
	h, _ := api.New(client)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v3/catalog/works/"+strconv.FormatInt(loser, 10), nil))
	var p map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	if rec.Code != http.StatusNotFound || p["code"] != "ENTITY_MERGED" || p["current_id"] != strconv.FormatInt(survivor, 10) {
		t.Errorf("merged id face: %d %v", rec.Code, p)
	}
	_, err = s.Apply(ctx, commit.Changeset{Actor: commit.Actor{Kind: commit.ActorAgent, Key: "program:ln-pilot"},
		Changes: []commit.Change{{Entity: "work", ID: loser, Values: map[string]commit.Value{"status": sv("complete")}}}})
	var refused commit.Refused
	if !errors.As(err, &refused) || refused[0].Code != "ENTITY_MERGED" {
		t.Errorf("agent write on a merged id: %v", err)
	}

	if err := merge.Unmerge(ctx, client, id, "user:1"); err != nil {
		t.Fatal(err)
	}
	if err := merge.Unmerge(ctx, client, id, "user:1"); !errors.Is(err, merge.ErrUndone) {
		t.Errorf("a second unmerge: %v", err)
	}
	if _, err := client.Work.Get(ctx, loser); err != nil {
		t.Fatalf("loser not restored: %v", err)
	}
	lt, _ := client.WorkTitle.Query().Where(worktitle.WorkID(loser)).Count(ctx)
	lb, _ := client.BookWork.Query().Where(bookwork.WorkID(loser)).Count(ctx)
	lx, _ := client.XrefLink.Query().Where(xreflink.EntityID(loser)).Count(ctx)
	if lt != 2 || lb != 1 || lx != 1 {
		t.Errorf("after unmerge: loser titles %d book_work %d xref %d", lt, lb, lx)
	}
	if to, ok, _ := merge.Resolve(ctx, client, "work", older); !ok || to != loser {
		t.Errorf("the older redirect should point at the loser again: %d %v", to, ok)
	}
	if n, _ := client.Redirect.Query().Where(redirect.OldID(loser), redirect.RevokedAtIsNil()).Count(ctx); n != 0 {
		t.Error("the merge redirect is still live")
	}
}

func TestPartMergeKeepsOneProgressPerReader(t *testing.T) {
	client, _ := canontest.Client(t)
	ctx := context.Background()
	s := commit.New(client)
	w := work(t, s, "巻")
	parts, err := client.WorkPart.Query().Where(workpart.WorkID(w)).IDs(ctx)
	if err != nil || len(parts) != 1 {
		t.Fatal(err)
	}
	other, err := client.WorkPart.Create().SetWorkID(w).SetKind("volume").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	survivor, loser := parts[0], other.ID
	for _, p := range []struct {
		uid  int64
		part int64
	}{{1, survivor}, {1, loser}, {2, loser}} {
		if err := client.LibraryPartProgress.Create().SetActorUID(p.uid).SetPartID(p.part).SetState("done").Exec(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := merge.Merge(ctx, client, "work_part", loser, survivor, "user:1", "same volume"); err != nil {
		t.Fatal(err)
	}
	n, _ := client.LibraryPartProgress.Query().Where(librarypartprogress.PartID(survivor)).Count(ctx)
	if n != 2 {
		t.Errorf("survivor progress rows %d, want one per reader (2)", n)
	}
}
