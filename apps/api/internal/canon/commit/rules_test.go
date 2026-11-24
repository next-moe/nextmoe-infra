package commit

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"api/internal/canon/canontest"
	"api/internal/canon/ent/revision"
	"api/internal/canon/identity"
	"api/internal/canon/registry"
)

func from(source string, tier registry.Tier, v any) Value {
	return Value{Raw: raw(v), Tier: tier, Source: source}
}

func title(text string) Changeset {
	return Changeset{Actor: agent, Changes: []Change{
		{Ref: "w", Entity: "work", Values: map[string]Value{"medium": official("novel")}},
		{Entity: "work_title", Values: map[string]Value{"work_id": official("ref:w"), "text": official(text), "lang": official("ja"), "kind": official("official")}},
	}}
}

func unique(s string) string { return canontest.Title(s) }

func TestSourcesOfOneTierThatDisagreeWaitForADecision(t *testing.T) {
	client, _ := canontest.Client(t)
	ctx := context.Background()
	s := New(client)
	res, err := s.Apply(ctx, book(isbn()))
	if err != nil {
		t.Fatal(err)
	}
	r := res.IDs["r"]
	write := func(a Actor, v Value) error {
		_, err := s.Apply(ctx, Changeset{Actor: a, Changes: []Change{{Entity: "release", ID: r, Values: map[string]Value{"released": v}}}})
		return err
	}
	if err := write(agent, from("kadokawa", registry.TierOfficial, "2025-08-09")); err != nil {
		t.Fatalf("the first sourced value: %v", err)
	}
	if err := write(agent, from("kadokawa", registry.TierOfficial, "2025-08-10")); err != nil {
		t.Errorf("a publisher's later page replaces its earlier one: %v", err)
	}
	err = write(agent, from("shogakukan", registry.TierOfficial, "2025-08-11"))
	if got := refusal(t, err); got.Code != "SAME_TIER_CONFLICT" {
		t.Errorf("two official sources: %+v", got)
	}
	if err := write(agent, from("shogakukan", registry.TierOfficial, "2025-08-10")); err != nil {
		t.Errorf("a source that agrees is no conflict: %v", err)
	}
	if err := write(agent, from("ndl", registry.TierRegistry, "2025-08-10")); err != nil {
		t.Errorf("a weaker source that agrees is no refusal: %v", err)
	}
	if err := write(Actor{Kind: ActorAgent, Key: agent.Key, Reviewer: "review:grok"}, from("shogakukan", registry.TierOfficial, "2025-08-11")); err != nil {
		t.Errorf("a reviewer settles it: %v", err)
	}
	rel, _ := client.Release.Get(ctx, r)
	st := rel.FieldState["released"].(map[string]any)
	if *rel.Released != "2025-08-11" || st["by"] != "agent" || st["reviewed_by"] != "review:grok" {
		t.Errorf("after the review: %v %v", *rel.Released, st)
	}
	if err := write(agent, from("shogakukan", registry.TierOfficial, "2025-08-12")); err != nil {
		t.Errorf("an approved value is not locked: %v", err)
	}
	n, _ := client.Revision.Query().Where(revision.ReviewedBy("review:grok"), revision.Entity("release"), revision.EntityID(r)).Count(ctx)
	if n != 1 {
		t.Errorf("revisions naming the reviewer: %d", n)
	}
}

func TestExpectVersionRefusesAStaleEdit(t *testing.T) {
	client, _ := canontest.Client(t)
	ctx := context.Background()
	s := New(client)
	res, err := s.Apply(ctx, book(isbn()))
	if err != nil {
		t.Fatal(err)
	}
	edit := func(v int, status string) error {
		_, err := s.Apply(ctx, Changeset{Actor: Actor{Kind: ActorHuman, Key: "user:1"}, Changes: []Change{
			{Entity: "work", ID: res.IDs["w"], ExpectVersion: v, Values: map[string]Value{"status": {Raw: raw(status)}}}}})
		return err
	}
	if err := edit(1, "complete"); err != nil {
		t.Fatal(err)
	}
	if got := refusal(t, edit(1, "hiatus")); got.Code != "VERSION_CONFLICT" {
		t.Errorf("stale edit: %+v", got)
	}
}

func TestANameAlreadyInCanonIsRefusedUntilToldApart(t *testing.T) {
	client, _ := canontest.Client(t)
	ctx := context.Background()
	s := New(client)
	name := unique("の冒険")
	first, err := s.Apply(ctx, title(name))
	if err != nil {
		t.Fatal(err)
	}
	for _, again := range []string{name, name + "　３", name + " : 外伝", name + "IV", " " + name + "!"} {
		_, err := s.Apply(ctx, title(again))
		got := refusal(t, err)
		if got.Code != "IDENTITY_UNSURE" || len(got.Candidates) != 1 || got.Candidates[0].ID != first.IDs["w"] {
			t.Errorf("%q: %+v", again, got)
		}
	}
	other := title(name)
	other.Changes[0].Values["medium"] = official("manga")
	if _, err := s.Apply(ctx, other); err != nil {
		t.Errorf("another medium is another work: %v", err)
	}
	apart := title(name)
	apart.Changes[0].Distinct = []string{fmt.Sprintf("canon:%d", first.IDs["w"])}
	if _, err := s.Apply(ctx, apart); err != nil {
		t.Errorf("told apart: %v", err)
	}
	human := title(name)
	human.Actor = Actor{Kind: ActorHuman, Key: "user:1"}
	if got := refusal(t, func() error { _, err := s.Apply(ctx, human); return err }()); len(got.Candidates) != 2 {
		t.Errorf("a person's create gets the candidates too: %+v", got)
	}
}

func TestANewEntityTakesOverALegacyID(t *testing.T) {
	client, db := canontest.Client(t)
	canontest.Legacy(t, db)
	ctx := context.Background()
	s := New(client)
	name := unique("のシャナ")
	const work, gone, manga = int64(9_000_000_001), int64(9_000_000_002), int64(9_000_000_003)
	for _, q := range []string{
		fmt.Sprintf(`INSERT INTO %s.catalog_work VALUES (%d, 3, '%s', NULL), (%d, 3, 'x', now()), (%d, 2, 'y', NULL)`, identity.LegacySchema, work, name, gone, manga),
		fmt.Sprintf(`DELETE FROM canon.work_title WHERE work_id >= %d`, work), fmt.Sprintf(`DELETE FROM canon.work WHERE id >= %d`, work),
		fmt.Sprintf(`DELETE FROM canon.identity_key WHERE entity_id >= %d`, work),
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	_, err := s.Apply(ctx, title(name))
	if got := refusal(t, err); got.Code != "IDENTITY_UNSURE" || got.Candidates[0].Token() != fmt.Sprintf("legacy:%d", work) {
		t.Fatalf("a legacy work is a candidate: %+v", got)
	}
	adopt := func(a Actor, id int64) error {
		cs := title(name)
		cs.Actor = a
		cs.Changes[0].AdoptID = id
		_, err := s.Apply(ctx, cs)
		return err
	}
	reviewed := Actor{Kind: ActorAgent, Key: "program:ln-pilot", Reviewer: "review:grok"}
	if got := refusal(t, adopt(agent, work)); got.Code != "PROPERTY_OUT_OF_SCOPE" {
		t.Errorf("an agent takes over no id on its own: %+v", got)
	}
	for _, id := range []int64{gone, manga, work + 100} {
		if got := refusal(t, adopt(reviewed, id)); got.Code != "IDENTITY_UNSURE" {
			t.Errorf("legacy %d must not be adoptable: %+v", id, got)
		}
	}
	if err := adopt(reviewed, work); err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if w, err := client.Work.Get(ctx, work); err != nil || w.Medium != "novel" {
		t.Errorf("the canon work under the legacy id: %+v %v", w, err)
	}
	if got := refusal(t, adopt(reviewed, work)); got.Code != "UNIQUE_CONFLICT" {
		t.Errorf("adopting twice: %+v", got)
	}
}

func TestCheckKeepsNothing(t *testing.T) {
	client, _ := canontest.Client(t)
	ctx := context.Background()
	s := New(client)
	before, _ := client.Work.Query().Count(ctx)
	if err := s.Check(ctx, title(unique("の本"))); err != nil {
		t.Fatal(err)
	}
	if after, _ := client.Work.Query().Count(ctx); after != before {
		t.Errorf("Check left %d works", after-before)
	}
	var r Refused
	if err := s.Check(ctx, Changeset{Actor: agent, Changes: []Change{{Entity: "work", ID: 1 << 60, Values: map[string]Value{"status": official("complete")}}}}); !errors.As(err, &r) {
		t.Errorf("Check refuses what Apply would: %v", err)
	}
}

func TestTwoCommitsOfOneTitleAtOnceMakeOneWork(t *testing.T) {
	client, _ := canontest.Client(t)
	ctx := context.Background()
	for range 5 {
		name := unique("の競走")
		errs := make(chan error, 4)
		for range cap(errs) {
			go func() {
				_, err := New(client).Apply(ctx, title(name))
				errs <- err
			}()
		}
		written := 0
		for range cap(errs) {
			err := <-errs
			var r Refused
			switch {
			case err == nil:
				written++
			case !errors.As(err, &r) || r[0].Code != "IDENTITY_UNSURE":
				t.Errorf("the losers learn of the winner: %v", err)
			}
		}
		if written != 1 {
			t.Errorf("%d works of one title", written)
		}
	}
}
