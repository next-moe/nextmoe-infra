package editing_test

import (
	"encoding/json"
	"errors"
	"testing"

	"api/internal/platform/editing"
)

func TestAmendStackingAndUnset(t *testing.T) {
	e := newEngine(t)
	createWidget(t, 1)
	prop, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: "test.widget", EntityID: 1,
		Patch: map[string]any{fName: "A", fOLang: "en", fRating: float64(2)},
		Actor: editorActor(100),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	a1, err := e.AmendProposal(testCtx, prop.ID, editing.AmendInput{
		Set: map[string]any{fName: "B"}, Note: "typo", Actor: reviewerActor(200),
	})
	if err != nil {
		t.Fatalf("amend 1: %v", err)
	}
	a2, err := e.AmendProposal(testCtx, prop.ID, editing.AmendInput{
		Set: map[string]any{fOpen: "added"}, Unset: []string{fOLang}, Actor: reviewerActor(201),
	})
	if err != nil {
		t.Fatalf("amend 2: %v", err)
	}
	if a1.Seq != 1 || a2.Seq != 2 {
		t.Fatalf("amendment seqs: %d, %d", a1.Seq, a2.Seq)
	}

	_, amendments, eff, err := e.GetProposal(testCtx, prop.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(amendments) != 2 {
		t.Fatalf("amendments: %d", len(amendments))
	}
	want := map[string]any{fName: "B", fRating: float64(2), fOpen: "added"}
	if len(eff) != len(want) {
		t.Fatalf("effective patch: %v", eff)
	}
	for k, v := range want {
		if eff[k] != v {
			t.Fatalf("effective[%s] = %v, want %v", k, eff[k], v)
		}
	}

	rev, err := e.MergeProposal(testCtx, prop.ID, reviewerActor(202), "")
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if rev.ActorUID != 100 || rev.AmenderUID == nil || *rev.AmenderUID != 201 {
		t.Fatalf("double signature: actor=%d amender=%v", rev.ActorUID, rev.AmenderUID)
	}
	w := loadWidget(t, 1)
	if w.Name != "B" || w.OLang != "ja" || w.Rating != 2 || w.OpenNote != "added" {
		t.Fatalf("merged state: %+v", w)
	}
}

func TestAmendValidation(t *testing.T) {
	e := newEngine(t)
	createWidget(t, 1)
	prop, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: "test.widget", EntityID: 1,
		Patch: map[string]any{fName: "A"}, Actor: editorActor(100),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	reviewer := reviewerActor(200)

	if _, err := e.AmendProposal(testCtx, prop.ID, editing.AmendInput{Actor: reviewer}); !errors.Is(err, editing.ErrEmptyDelta) {
		t.Fatalf("empty delta: %v", err)
	}
	var permErr *editing.PermissionError
	if _, err := e.AmendProposal(testCtx, prop.ID, editing.AmendInput{
		Set: map[string]any{fName: "B"}, Actor: editorActor(100),
	}); !errors.As(err, &permErr) {
		t.Fatalf("amend needs the review rule: %v", err)
	}
	var unknownErr *editing.UnknownFieldError
	if _, err := e.AmendProposal(testCtx, prop.ID, editing.AmendInput{
		Set: map[string]any{"test.widget.nope": "x"}, Actor: reviewer,
	}); !errors.As(err, &unknownErr) {
		t.Fatalf("unknown set key: %v", err)
	}
	var lockedErr *editing.LockedFieldError
	if _, err := e.AmendProposal(testCtx, prop.ID, editing.AmendInput{
		Set: map[string]any{fLocked: "smuggle"}, Actor: reviewer,
	}); !errors.As(err, &lockedErr) {
		t.Fatalf("locked field via amend: %v", err)
	}
	var valErr *editing.ValidationError
	if _, err := e.AmendProposal(testCtx, prop.ID, editing.AmendInput{
		Set: map[string]any{fLegacy: "x"}, Actor: reviewer,
	}); !errors.As(err, &valErr) {
		t.Fatalf("deprecated field via amend: %v", err)
	}
	if _, err := e.AmendProposal(testCtx, prop.ID, editing.AmendInput{
		Unset: []string{fOLang}, Actor: reviewer,
	}); !errors.As(err, &valErr) {
		t.Fatalf("unset of a key outside the patch: %v", err)
	}
	if _, err := e.AmendProposal(testCtx, prop.ID, editing.AmendInput{
		Set: map[string]any{fName: "B"}, Unset: []string{fName}, Actor: reviewer,
	}); !errors.As(err, &valErr) {
		t.Fatalf("set∩unset: %v", err)
	}
	if _, err := e.AmendProposal(testCtx, prop.ID, editing.AmendInput{
		Unset: []string{fName}, Actor: reviewer,
	}); err != nil {
		t.Fatalf("unset all: %v", err)
	}
	if _, err := e.MergeProposal(testCtx, prop.ID, reviewer, ""); !errors.Is(err, editing.ErrEmptyPatch) {
		t.Fatalf("merge of an emptied patch: %v", err)
	}
}

func TestRebaseFastForwardAndConflict(t *testing.T) {
	e := newEngine(t)
	createWidget(t, 1)
	reviewer := reviewerActor(200)

	p1, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: "test.widget", EntityID: 1,
		Patch: map[string]any{fName: "P1 name"}, Actor: editorActor(100),
	})
	if err != nil {
		t.Fatalf("p1: %v", err)
	}
	if _, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: "test.widget", EntityID: 1,
		Patch: map[string]any{fOpen: "drift"}, Actor: anonActor(300),
	}); err != nil {
		t.Fatalf("drift: %v", err)
	}
	if _, err := e.MergeProposal(testCtx, p1.ID, reviewer, ""); err != nil {
		t.Fatalf("fast-forward merge: %v", err)
	}

	p2, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: "test.widget", EntityID: 1,
		Patch: map[string]any{fName: "P2 name"}, Actor: editorActor(101),
	})
	if err != nil {
		t.Fatalf("p2: %v", err)
	}
	p3, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: "test.widget", EntityID: 1,
		Patch: map[string]any{fName: "P3 name"}, Actor: editorActor(102),
	})
	if err != nil {
		t.Fatalf("p3: %v", err)
	}
	if _, err := e.MergeProposal(testCtx, p3.ID, reviewer, ""); err != nil {
		t.Fatalf("merge p3: %v", err)
	}

	var conflict *editing.ConflictError
	_, err = e.MergeProposal(testCtx, p2.ID, reviewer, "")
	if !errors.As(err, &conflict) {
		t.Fatalf("want conflict, got %v", err)
	}
	if len(conflict.Keys) != 1 || conflict.Keys[0] != fName {
		t.Fatalf("conflict keys: %v", conflict.Keys)
	}

	if _, err := e.AmendProposal(testCtx, p2.ID, editing.AmendInput{
		Set: map[string]any{fName: "P2 name, rebased"}, Actor: reviewer,
	}); err != nil {
		t.Fatalf("amend: %v", err)
	}
	if _, err := e.MergeProposal(testCtx, p2.ID, reviewer, ""); err != nil {
		t.Fatalf("merge after amend: %v", err)
	}
	if w := loadWidget(t, 1); w.Name != "P2 name, rebased" {
		t.Fatalf("final name: %q", w.Name)
	}
}

func TestRevertLoop(t *testing.T) {
	e := newEngine(t)
	createWidget(t, 1)
	reviewer := reviewerActor(200)

	mustDirect := func(patch map[string]any, uid int64) *editing.Revision {
		t.Helper()
		_, rev, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
			EntityType: "test.widget", EntityID: 1, Patch: patch, Actor: actorWith(uid, 0),
		})
		if err != nil || rev == nil {
			t.Fatalf("direct edit: rev=%v err=%v", rev, err)
		}
		return rev
	}
	rev1 := mustDirect(map[string]any{fOpen: "one"}, 300)
	mustDirect(map[string]any{fOpen: "two"}, 301)

	prop, rev3, err := e.Revert(testCtx, editing.RevertInput{
		EntityType: "test.widget", EntityID: 1, ToSeq: 1, Note: "vandalism", Actor: reviewer,
	})
	if err != nil {
		t.Fatalf("revert: %v", err)
	}
	if rev3.Seq != 3 || rev3.Action != editing.ActionReverted {
		t.Fatalf("revert revision: seq=%d action=%d", rev3.Seq, rev3.Action)
	}
	if prop.Status != editing.StatusMerged {
		t.Fatal("revert proposal must close as merged")
	}
	if w := loadWidget(t, 1); w.OpenNote != "one" {
		t.Fatalf("reverted state: %q", w.OpenNote)
	}
	revs, err := e.ListRevisions(testCtx, "test.widget", 1, 0)
	if err != nil || len(revs) != 3 {
		t.Fatalf("revisions: %d err=%v", len(revs), err)
	}
	var snap1, snap3 map[string]any
	if err := json.Unmarshal(rev1.Snapshot, &snap1); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(rev3.Snapshot, &snap3); err != nil {
		t.Fatal(err)
	}
	for k, v := range snap1 {
		if snap3[k] != v {
			t.Fatalf("snapshot mismatch at %s: %v vs %v", k, snap3[k], v)
		}
	}

	if _, _, err := e.Revert(testCtx, editing.RevertInput{
		EntityType: "test.widget", EntityID: 1, ToSeq: 3, Actor: reviewer,
	}); !errors.Is(err, editing.ErrNoEffectiveChanges) {
		t.Fatalf("revert to current: %v", err)
	}
	var permErr *editing.PermissionError
	if _, _, err := e.Revert(testCtx, editing.RevertInput{
		EntityType: "test.widget", EntityID: 1, ToSeq: 2, Actor: anonActor(300),
	}); !errors.As(err, &permErr) {
		t.Fatalf("revert without review: %v", err)
	}
	if _, _, err := e.Revert(testCtx, editing.RevertInput{
		EntityType: "test.widget", EntityID: 1, ToSeq: 99, Actor: reviewer,
	}); !errors.Is(err, editing.ErrRevisionNotFound) {
		t.Fatalf("revert to missing seq: %v", err)
	}
}

func TestChangedFieldsPrecision(t *testing.T) {
	e := newEngine(t)
	createWidget(t, 1)
	reviewer := reviewerActor(200)

	if _, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: "test.widget", EntityID: 1,
		Patch: map[string]any{fOpen: "same"}, Actor: anonActor(300),
	}); err != nil {
		t.Fatal(err)
	}
	prop, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: "test.widget", EntityID: 1,
		Patch: map[string]any{fOpen: "same", fName: "changed"}, Actor: editorActor(100),
	})
	if err != nil {
		t.Fatal(err)
	}
	rev, err := e.MergeProposal(testCtx, prop.ID, reviewer, "")
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	var changed []string
	if err := json.Unmarshal(rev.ChangedFields, &changed); err != nil {
		t.Fatal(err)
	}
	if len(changed) != 1 || changed[0] != fName {
		t.Fatalf("changed_fields: %v", changed)
	}

	prop, _, err = e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: "test.widget", EntityID: 1,
		Patch: map[string]any{fName: "changed"}, Actor: editorActor(100),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.MergeProposal(testCtx, prop.ID, reviewer, ""); !errors.Is(err, editing.ErrNoEffectiveChanges) {
		t.Fatalf("all-no-op merge: %v", err)
	}
	var before int64
	testDB.Model(&editing.Proposal{}).Count(&before)
	if _, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: "test.widget", EntityID: 1,
		Patch: map[string]any{fOpen: "same"}, Actor: anonActor(300),
	}); !errors.Is(err, editing.ErrNoEffectiveChanges) {
		t.Fatalf("no-op direct edit: %v", err)
	}
	var after int64
	testDB.Model(&editing.Proposal{}).Count(&after)
	if after != before {
		t.Fatal("failed direct edit must not leave a proposal row")
	}
}

func TestCreateEdgeCases(t *testing.T) {
	e := newEngine(t)
	createWidget(t, 1)

	if _, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: "test.widget", EntityID: 1, Patch: map[string]any{}, Actor: editorActor(100),
	}); !errors.Is(err, editing.ErrEmptyPatch) {
		t.Fatalf("empty patch: %v", err)
	}
	if _, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: "test.unknown", EntityID: 1, Patch: map[string]any{fName: "x"}, Actor: editorActor(100),
	}); !errors.Is(err, editing.ErrUnknownEntityType) {
		t.Fatalf("unknown type: %v", err)
	}
	var unknownErr *editing.UnknownFieldError
	if _, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: "test.widget", EntityID: 1, Patch: map[string]any{"test.widget.ghost": "x"}, Actor: editorActor(100),
	}); !errors.As(err, &unknownErr) {
		t.Fatalf("unknown field: %v", err)
	}
	var lockedErr *editing.LockedFieldError
	if _, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: "test.widget", EntityID: 1, Patch: map[string]any{fLocked: "x"}, Actor: reviewerActor(200),
	}); !errors.As(err, &lockedErr) {
		t.Fatalf("locked field: %v", err)
	}
	var valErr *editing.ValidationError
	if _, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: "test.widget", EntityID: 1, Patch: map[string]any{fRating: float64(99)}, Actor: editorActor(100),
	}); !errors.As(err, &valErr) {
		t.Fatalf("invalid value: %v", err)
	}
	var permErr *editing.PermissionError
	if _, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: "test.widget", EntityID: 1, Patch: map[string]any{fName: "x"}, Actor: anonActor(300),
	}); !errors.As(err, &permErr) {
		t.Fatalf("propose without perm: %v", err)
	}
	if _, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: "test.widget", EntityID: 404, Patch: map[string]any{fName: "x"}, Actor: editorActor(100),
	}); !errors.Is(err, editing.ErrEntityNotFound) {
		t.Fatalf("missing entity: %v", err)
	}
	if _, _, _, err := e.GetProposal(testCtx, 12345); !errors.Is(err, editing.ErrProposalNotFound) {
		t.Fatalf("missing proposal: %v", err)
	}
}

func TestEffectivePatchesMatchGetProposal(t *testing.T) {
	e := newEngine(t)
	createWidget(t, 1)
	createWidget(t, 2)
	amended, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: "test.widget", EntityID: 1,
		Patch: map[string]any{fName: "A", fOLang: "en"},
		Actor: editorActor(100),
	})
	if err != nil {
		t.Fatalf("create amended: %v", err)
	}
	plain, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: "test.widget", EntityID: 2,
		Patch: map[string]any{fName: "P"},
		Actor: editorActor(100),
	})
	if err != nil {
		t.Fatalf("create plain: %v", err)
	}
	if _, err := e.AmendProposal(testCtx, amended.ID, editing.AmendInput{
		Set: map[string]any{fName: "B"}, Actor: reviewerActor(200),
	}); err != nil {
		t.Fatalf("amend 1: %v", err)
	}
	if _, err := e.AmendProposal(testCtx, amended.ID, editing.AmendInput{
		Unset: []string{fOLang}, Actor: reviewerActor(200),
	}); err != nil {
		t.Fatalf("amend 2: %v", err)
	}

	got, err := e.EffectivePatches(testCtx, []editing.Proposal{*plain, *amended})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{amended.ID, plain.ID} {
		_, _, want, err := e.GetProposal(testCtx, id)
		if err != nil {
			t.Fatal(err)
		}
		a, _ := json.Marshal(want)
		b, _ := json.Marshal(got[id])
		if string(a) != string(b) {
			t.Fatalf("proposal %d: batch %s, detail %s", id, b, a)
		}
	}
	if got[amended.ID][fName] != "B" || len(got[amended.ID]) != 1 {
		t.Fatalf("both amendments folded, in order: %v", got[amended.ID])
	}
}
