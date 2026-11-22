package commit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"testing"

	"api/internal/canon/canontest"
	"api/internal/canon/ent/revision"
	"api/internal/canon/registry"
)

func raw(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func official(v any) Value {
	return Value{Raw: raw(v), Tier: registry.TierOfficial, Evidence: []EvidenceRef{{Snapshot: "h", LineFrom: 1, LineTo: 1}}}
}

var agent = Actor{Kind: ActorAgent, Key: "ln-pilot"}

// isbn returns a fresh valid ISBN-13 so reruns on a shared test database do not collide.
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

func book(code string) Changeset {
	return Changeset{Actor: agent, Note: "pilot", Changes: []Change{
		{Ref: "w", Entity: "work", Values: map[string]Value{"medium": official("novel"), "status": official("ongoing")}},
		{Entity: "work_title", Values: map[string]Value{"work_id": official("ref:w"), "text": official("アクセル・ワールド"), "lang": official("ja"), "kind": official("official")}},
		{Ref: "v", Entity: "work_part", Values: map[string]Value{"work_id": official("ref:w"), "kind": official("volume"), "ordinal": official(28), "released": official("2025-08")}},
		{Ref: "r", Entity: "release", Values: map[string]Value{"lang": official("ja"), "format": official("paperback"), "released": official("2025-08-08")}},
		{Entity: "release_content", Values: map[string]Value{"release_id": official("ref:r"), "work_id": official("ref:w"), "part_id": official("ref:v")}},
		{Entity: "book_release", Values: map[string]Value{"release_id": official("ref:r"), "isbn13": official(code), "price_amount": official("924"), "price_currency": official("JPY")}},
	}}
}

func refusal(t *testing.T, err error) Refusal {
	t.Helper()
	var r Refused
	if !errors.As(err, &r) || len(r) == 0 {
		t.Fatalf("want a refusal, got %v", err)
	}
	return r[0]
}

func TestApplyCreatesAChangesetAtomically(t *testing.T) {
	client, _ := canontest.Client(t)
	ctx := context.Background()
	s := New(client)
	code := isbn()

	res, err := s.Apply(ctx, book(code))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	w, err := client.Work.Get(ctx, res.IDs["w"])
	if err != nil || w.Medium != "novel" || w.FieldState["medium"].(map[string]any)["assurance"] != "official" {
		t.Fatalf("work %+v %v", w, err)
	}
	n, _ := client.Revision.Query().Where(revision.EntityID(res.IDs["w"]), revision.Entity("work")).Count(ctx)
	if n != 1 {
		t.Errorf("work revisions = %d, want 1", n)
	}

	before, _ := client.Work.Query().Count(ctx)
	_, err = s.Apply(ctx, book(code))
	if r := refusal(t, err); r.Code != "UNIQUE_CONFLICT" || r.Property != "isbn13" {
		t.Errorf("duplicate isbn: %+v", r)
	}
	if after, _ := client.Work.Query().Count(ctx); after != before {
		t.Errorf("a refused changeset left %d works behind", after-before)
	}
}

func TestApplyRefusesWhatTheActorMayNotWrite(t *testing.T) {
	client, _ := canontest.Client(t)
	ctx := context.Background()
	s := New(client)
	res, err := s.Apply(ctx, book(isbn()))
	if err != nil {
		t.Fatal(err)
	}
	w := res.IDs["w"]

	cases := []struct {
		name string
		cs   Changeset
		code string
	}{
		{"internal field", Changeset{Actor: agent, Changes: []Change{{Entity: "work", ID: w, Values: map[string]Value{"display_safety": official("explicit")}}}}, "PROPERTY_OUT_OF_SCOPE"},
		{"community tier", Changeset{Actor: agent, Changes: []Change{{Entity: "work", ID: w, Values: map[string]Value{"status": {Raw: raw("complete"), Tier: registry.TierCommunity}}}}}, "TIER_NOT_ALLOWED"},
		{"unknown term", Changeset{Actor: agent, Changes: []Change{{Entity: "work", ID: w, Values: map[string]Value{"status": official("abandoned")}}}}, "VOCAB_TERM_UNKNOWN"},
		{"unknown property", Changeset{Actor: agent, Changes: []Change{{Entity: "work", ID: w, Values: map[string]Value{"rating": official(5)}}}}, "PROPERTY_UNKNOWN"},
		{"dangling ref", Changeset{Actor: agent, Changes: []Change{{Entity: "work_title", Values: map[string]Value{"work_id": official("ref:nope"), "text": official("x"), "lang": official("ja"), "kind": official("official")}}}}, "VALUE_NOT_ENTAILED"},
		{"missing entity", Changeset{Actor: agent, Changes: []Change{{Entity: "work", ID: w + 1_000_000, Values: map[string]Value{"status": official("complete")}}}}, "ENTITY_MERGED"},
	}
	for _, c := range cases {
		_, err := s.Apply(ctx, c.cs)
		if r := refusal(t, err); r.Code != c.code {
			t.Errorf("%s: got %s %s, want %s", c.name, r.Code, r.Detail, c.code)
		}
	}

	human := Changeset{Actor: Actor{Kind: ActorHuman, Key: "user:1"}, Changes: []Change{{Entity: "work", ID: w, Values: map[string]Value{"status": {Raw: raw("complete")}}}}}
	if _, err := s.Apply(ctx, human); err != nil {
		t.Fatalf("human edit: %v", err)
	}
	_, err = s.Apply(ctx, Changeset{Actor: agent, Changes: []Change{{Entity: "work", ID: w, Values: map[string]Value{"status": official("ongoing")}}}})
	if r := refusal(t, err); r.Code != "HUMAN_LOCKED" {
		t.Errorf("agent over a human edit: %+v", r)
	}
	got, _ := client.Work.Get(ctx, w)
	if got.Status != "complete" || got.Version != 2 {
		t.Errorf("after the human edit: status %s version %d", got.Status, got.Version)
	}
}
