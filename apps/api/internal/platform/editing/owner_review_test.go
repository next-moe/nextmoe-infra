package editing_test

import (
	"errors"
	"testing"

	"api/internal/platform/editing"
)

func ownerActor(uid int64, site string) editing.PolicyContext {
	pc := anonActor(uid)
	pc.Site = site
	pc.IsEntityOwner = true
	return pc
}

func TestOwnerReviewPolicy(t *testing.T) {
	perm := editing.Policy{Propose: editing.ProposeOpen, Review: editing.ReviewPerm(permReview), Automerge: editing.AutomergeNever}
	withOwner := perm
	withOwner.OwnerReview = true

	owner := ownerActor(1, "anywhere")
	nonOwner := anonActor(2)

	if !withOwner.AllowsReview(owner) {
		t.Error("OwnerReview + IsEntityOwner must pass review")
	}
	if withOwner.AllowsReview(nonOwner) {
		t.Error("OwnerReview without ownership must not pass review")
	}
	if perm.AllowsReview(owner) {
		t.Error("ownership without OwnerReview must not pass review")
	}
	if !withOwner.AllowsReview(reviewerActor(3)) {
		t.Error("review perm must still pass on an OwnerReview policy")
	}
}

func TestOwnerReviewEngine(t *testing.T) {
	e := newEngine(t)
	createWidget(t, 1)
	applyOverrides(t, e, ownerReviewsName())

	propose := func(t *testing.T, key string, value any, uid int64) *editing.Proposal {
		t.Helper()
		prop, rev, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
			EntityType: "test.widget", EntityID: 1,
			Patch: map[string]any{key: value}, Actor: anonActor(uid),
		})
		if err != nil {
			t.Fatalf("propose %s: %v", key, err)
		}
		if rev != nil {
			t.Fatalf("proposal must stay open, got automerge")
		}
		return prop
	}
	owner := ownerActor(400, "any-site")

	prop := propose(t, fName, "proposed name", 300)
	if _, err := e.AmendProposal(testCtx, prop.ID, editing.AmendInput{
		Set: map[string]any{fName: "owner-corrected name"}, Note: "fix", Actor: owner,
	}); err != nil {
		t.Fatalf("owner amend: %v", err)
	}
	rev, err := e.MergeProposal(testCtx, prop.ID, owner, "")
	if err != nil {
		t.Fatalf("owner merge: %v", err)
	}
	if rev.ActorUID != 300 || rev.AmenderUID == nil || *rev.AmenderUID != 400 {
		t.Fatalf("double signature: %+v", rev)
	}
	if w := loadWidget(t, 1); w.Name != "owner-corrected name" {
		t.Fatalf("apply: %+v", w)
	}

	prop = propose(t, fName, "declined name", 300)
	if err := e.DeclineProposal(testCtx, prop.ID, owner, "not this"); err != nil {
		t.Fatalf("owner decline: %v", err)
	}

	prop = propose(t, fTrusted, "note", 301)
	var permErr *editing.PermissionError
	if _, err := e.MergeProposal(testCtx, prop.ID, owner, ""); !errors.As(err, &permErr) {
		t.Fatalf("owner on non-OwnerReview field must 403: %v", err)
	}

	proj, err := e.SchemaProjection(testCtx, "test.widget", 1, owner)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range proj {
		want := p.Key == fName
		if p.CanReview != want {
			t.Errorf("owner projection %s: can_review=%v, want %v", p.Key, p.CanReview, want)
		}
	}
	proj, err = e.SchemaProjection(testCtx, "test.widget", 1, anonActor(5))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range proj {
		if p.CanReview {
			t.Errorf("non-owner projection %s: can_review must be false", p.Key)
		}
	}

	prop = propose(t, fName, "after the override is gone", 302)
	applyOverrides(t, e, nil)
	if _, err := e.MergeProposal(testCtx, prop.ID, owner, ""); !errors.As(err, &permErr) {
		t.Fatalf("owner without an OwnerReview policy must 403: %v", err)
	}
}
