package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"testing"

	"api/internal/canon/canontest"
	"api/internal/canon/commit"
	"api/internal/canon/registry"
	"api/internal/canon/takedown"
)

func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

func v(x any) commit.Value {
	return commit.Value{Raw: raw(x), Tier: registry.TierRegistry, Evidence: []commit.EvidenceRef{{Record: "ndl:x", Path: "p"}}}
}

func isbn() string {
	base := fmt.Sprintf("978%09d", rand.IntN(1_000_000_000))
	sum := 0
	for i, r := range base {
		n := int(r - '0')
		if i%2 == 1 {
			n *= 3
		}
		sum += n
	}
	return fmt.Sprintf("%s%d", base, (10-sum%10)%10)
}

func get(t *testing.T, h http.Handler, path string, out any) int {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if out != nil && rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}
	return rec.Code
}

func TestReadFaces(t *testing.T) {
	client, _ := canontest.Client(t)
	ctx := context.Background()
	code := isbn()
	res, err := commit.New(client).Apply(ctx, commit.Changeset{Actor: commit.Actor{Kind: commit.ActorAgent, Key: "program:ln-pilot"}, Changes: []commit.Change{
		{Ref: "w", Entity: "work", Values: map[string]commit.Value{"medium": v("novel")}},
		{Entity: "work_title", Values: map[string]commit.Value{"work_id": v("ref:w"), "text": v("アクセル・ワールド"), "lang": v("ja"), "kind": v("official")}},
		{Ref: "p", Entity: "work_part", Values: map[string]commit.Value{"work_id": v("ref:w"), "kind": v("volume"), "ordinal": v(28), "released": v("2025-08")}},
		{Ref: "r", Entity: "release", Values: map[string]commit.Value{"lang": v("ja"), "format": v("paperback"), "released": v("2025-08")}},
		{Entity: "release_content", Values: map[string]commit.Value{"release_id": v("ref:r"), "work_id": v("ref:w"), "part_id": v("ref:p")}},
		{Entity: "book_release", Values: map[string]commit.Value{"release_id": v("ref:r"), "isbn13": v(code)}},
		{Ref: "person", Entity: "person", Values: map[string]commit.Value{}},
		{Ref: "alias", Entity: "person_alias", Values: map[string]commit.Value{"person_id": v("ref:person"), "name": v("川原礫")}},
		{Entity: "credit", Values: map[string]commit.Value{"work_id": v("ref:w"), "alias_id": v("ref:alias"), "role": v("author")}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	h, _ := New(client)
	w := res.IDs["w"]

	var work Work
	if c := get(t, h, fmt.Sprintf("/v3/catalog/works/%d", w), &work); c != http.StatusOK {
		t.Fatalf("work: %d", c)
	}
	if len(work.Titles) != 1 || len(work.Parts) != 1 || work.Parts[0].ReleaseIDs[0] != id(res.IDs["r"]) ||
		len(work.Credits) != 1 || work.Credits[0].Alias.Name != "川原礫" || work.Credits[0].Alias.PersonID != id(res.IDs["person"]) {
		t.Errorf("work %+v", work)
	}
	var rels ReleaseList
	hyphenated := code[:3] + "-" + code[3:4] + "-" + code[4:]
	if c := get(t, h, "/v3/catalog/releases?isbn="+hyphenated, &rels); c != http.StatusOK || len(rels.Items) != 1 || rels.Items[0].Contents[0].WorkID != id(w) {
		t.Errorf("release by isbn: %d %+v", c, rels)
	}
	var people PersonList
	if c := get(t, h, fmt.Sprintf("/v3/catalog/persons?alias_ids=%d", res.IDs["alias"]), &people); c != http.StatusOK ||
		len(people.Items) != 1 || people.Items[0].Credits[0].WorkID == nil || *people.Items[0].Credits[0].WorkID != id(w) {
		t.Errorf("person by alias: %d %+v", c, people)
	}
	var page WorkList
	if c := get(t, h, "/v3/catalog/works?medium=novel&limit=1", &page); c != http.StatusOK || len(page.Items) != 1 {
		t.Errorf("work list: %d %+v", c, page)
	}

	td, err := takedown.Take(ctx, client, takedown.Notice{NoticeID: "trust:1", Scope: "entity", Entity: "work", EntityID: w, Reason: "rights holder request", By: "user:1"})
	if err != nil {
		t.Fatal(err)
	}
	if c := get(t, h, fmt.Sprintf("/v3/catalog/works/%d", w), nil); c != http.StatusGone {
		t.Errorf("taken-down work: %d", c)
	}
	if get(t, h, fmt.Sprintf("/v3/catalog/works?ids=%d", w), &page); len(page.Items) != 0 {
		t.Error("a taken-down work stays in the ids= lane")
	}
	_, err = commit.New(client).Apply(ctx, commit.Changeset{Actor: commit.Actor{Kind: commit.ActorAgent, Key: "program:ln-pilot"},
		Changes: []commit.Change{{Entity: "work", ID: w, Values: map[string]commit.Value{"status": v("complete")}}}})
	var refused commit.Refused
	if !errors.As(err, &refused) || refused[0].Code != "ENTITY_HELD" {
		t.Errorf("agent write on a held work: %v", err)
	}
	if err := takedown.Restore(ctx, client, td.ID, "user:1", "counter-notice accepted"); err != nil {
		t.Fatal(err)
	}
	if c := get(t, h, fmt.Sprintf("/v3/catalog/works/%d", w), nil); c != http.StatusOK {
		t.Errorf("restored work: %d", c)
	}
	if c := get(t, h, "/v3/catalog/works/999999999999", nil); c != http.StatusNotFound {
		t.Errorf("missing work: %d", c)
	}
}
