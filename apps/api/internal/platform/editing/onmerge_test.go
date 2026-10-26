package editing_test

import (
	"context"
	"fmt"
	"testing"

	"api/internal/platform/editing"
)

type mergeCall struct {
	entityID   int64
	actorUID   int64
	amenderUID *int64
	action     int16
}

func newEngineOnMerge(t *testing.T, onMerge func(context.Context, editing.MergeEvent) error) *editing.Engine {
	t.Helper()
	cleanTables(t)
	spec := widgetSpec(testDB)
	spec.OnMerge = onMerge
	reg := editing.NewRegistry()
	if err := reg.Register(spec); err != nil {
		t.Fatalf("register widget spec: %v", err)
	}
	return editing.NewEngine(testDB, reg)
}

func TestOnMergeFiresOnEveryMergePath(t *testing.T) {
	var calls []mergeCall
	e := newEngineOnMerge(t, func(_ context.Context, ev editing.MergeEvent) error {
		calls = append(calls, mergeCall{ev.EntityID, ev.ActorUID, ev.AmenderUID, ev.Action})
		return nil
	})
	createWidget(t, 1)

	if _, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: "test.widget", EntityID: 1,
		Patch: map[string]any{fOpen: "hi"}, Actor: anonActor(10),
	}); err != nil {
		t.Fatalf("direct edit: %v", err)
	}

	prop, rev, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: "test.widget", EntityID: 1,
		Patch: map[string]any{fName: "X"}, Actor: editorActor(20),
	})
	if err != nil {
		t.Fatalf("open proposal: %v", err)
	}
	if rev != nil {
		t.Fatal("perm-gated automerge=never proposal should stay open")
	}
	if _, err := e.AmendProposal(testCtx, prop.ID, editing.AmendInput{
		Set: map[string]any{fName: "Y"}, Actor: reviewerActor(30),
	}); err != nil {
		t.Fatalf("amend: %v", err)
	}
	if _, err := e.MergeProposal(testCtx, prop.ID, reviewerActor(30), ""); err != nil {
		t.Fatalf("merge: %v", err)
	}

	if _, _, err := e.Revert(testCtx, editing.RevertInput{
		EntityType: "test.widget", EntityID: 1, ToSeq: 1, Actor: reviewerActor(30),
	}); err != nil {
		t.Fatalf("revert: %v", err)
	}

	if len(calls) != 3 {
		t.Fatalf("want 3 OnMerge calls (direct, merge, revert), got %d: %+v", len(calls), calls)
	}
	if c := calls[0]; c.entityID != 1 || c.actorUID != 10 || c.amenderUID != nil || c.action != editing.ActionDirect {
		t.Fatalf("direct call = %+v", c)
	}
	if c := calls[1]; c.actorUID != 20 || c.amenderUID == nil || *c.amenderUID != 30 || c.action != editing.ActionMerged {
		t.Fatalf("merge call = %+v (amender=%v)", c, c.amenderUID)
	}
	if c := calls[2]; c.actorUID != 30 || c.amenderUID != nil || c.action != editing.ActionReverted {
		t.Fatalf("revert call = %+v", c)
	}
}

func TestOnMergeNilIsNoOp(t *testing.T) {
	e := newEngineOnMerge(t, nil)
	createWidget(t, 1)
	if _, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: "test.widget", EntityID: 1,
		Patch: map[string]any{fOpen: "landed"}, Actor: anonActor(10),
	}); err != nil {
		t.Fatalf("direct edit with nil OnMerge: %v", err)
	}
	if w := loadWidget(t, 1); w.OpenNote != "landed" {
		t.Fatalf("merge did not land: open_note=%q", w.OpenNote)
	}
}

func TestOnMergeErrorDoesNotRollBack(t *testing.T) {
	e := newEngineOnMerge(t, func(context.Context, editing.MergeEvent) error {
		return fmt.Errorf("reindex boom")
	})
	createWidget(t, 1)
	if _, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: "test.widget", EntityID: 1,
		Patch: map[string]any{fOpen: "kept"}, Actor: anonActor(10),
	}); err != nil {
		t.Fatalf("caller must not see the hook error: %v", err)
	}
	if w := loadWidget(t, 1); w.OpenNote != "kept" {
		t.Fatalf("hook failure rolled the merge back: open_note=%q", w.OpenNote)
	}
	revs, err := e.ListRevisions(testCtx, "test.widget", 1, 10)
	if err != nil || len(revs) != 1 {
		t.Fatalf("want 1 durable revision, got %d (err=%v)", len(revs), err)
	}
}

func TestOnMergePanicDoesNotCrash(t *testing.T) {
	e := newEngineOnMerge(t, func(context.Context, editing.MergeEvent) error {
		panic("hook panic")
	})
	createWidget(t, 1)
	if _, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: "test.widget", EntityID: 1,
		Patch: map[string]any{fOpen: "safe"}, Actor: anonActor(10),
	}); err != nil {
		t.Fatalf("a panicking hook must not surface as a caller error: %v", err)
	}
	if w := loadWidget(t, 1); w.OpenNote != "safe" {
		t.Fatalf("panic rolled the merge back: open_note=%q", w.OpenNote)
	}
}

func TestRegistryObserverSeesEveryMergeWithoutAnOnMergeHook(t *testing.T) {
	cleanTables(t)
	reg := editing.NewRegistry()
	if err := reg.Register(widgetSpec(testDB)); err != nil {
		t.Fatalf("register widget spec: %v", err)
	}
	type seen struct {
		entityType string
		entityID   int64
	}
	var calls []seen
	reg.Observe(func(_ context.Context, entityType string, ev editing.MergeEvent) {
		calls = append(calls, seen{entityType, ev.EntityID})
	})
	reg.Observe(func(context.Context, string, editing.MergeEvent) { panic("observer boom") })
	e := editing.NewEngine(testDB, reg)
	createWidget(t, 7)

	if _, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: "test.widget", EntityID: 7,
		Patch: map[string]any{fOpen: "observed"}, Actor: anonActor(10),
	}); err != nil {
		t.Fatalf("direct edit: %v", err)
	}
	if len(calls) != 1 || calls[0] != (seen{"test.widget", 7}) {
		t.Fatalf("observer calls = %+v", calls)
	}
	if w := loadWidget(t, 7); w.OpenNote != "observed" {
		t.Fatalf("a panicking observer undid the merge: open_note=%q", w.OpenNote)
	}
}
