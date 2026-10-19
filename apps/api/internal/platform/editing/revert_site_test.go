package editing_test

import (
	"errors"
	"testing"

	"api/internal/platform/editing"
)

const strictReview = "edit.test.owned.strict_review"

func revertEngine(t *testing.T, owners map[int64]*string) *editing.Engine {
	t.Helper()
	cleanTables(t)
	createWidget(t, 1)
	spec := ownedSpec(owners, nil)
	spec.DefaultPolicy = editing.Policy{
		Propose: editing.ProposeOpen, Review: editing.ReviewPerm(permReview), Automerge: editing.AutomergeNever,
	}
	reg := editing.NewRegistry()
	if err := reg.Register(spec); err != nil {
		t.Fatalf("register owned spec: %v", err)
	}
	return editing.NewEngine(testDB, reg)
}

func revertHistory(t *testing.T, eng *editing.Engine, actor editing.PolicyContext) {
	t.Helper()
	for _, name := range []string{"first", "second"} {
		prop, _, err := eng.CreateProposal(testCtx, editing.CreateProposalInput{
			EntityType: "test.owned", EntityID: 1,
			Patch: map[string]any{"test.owned.name": name}, Actor: actor,
		})
		if err != nil {
			t.Fatalf("create proposal %q: %v", name, err)
		}
		if _, err := eng.MergeProposal(testCtx, prop.ID, actor, ""); err != nil {
			t.Fatalf("merge proposal %q: %v", name, err)
		}
	}
}

func siteActor(uid int64, site string, perms ...string) editing.PolicyContext {
	a := actorWith(uid, 0, perms...)
	a.Site = site
	return a
}

func TestRevertNeedsReviewUnderTheEffectivePolicy(t *testing.T) {
	site := "owner-site"
	eng := revertEngine(t, map[int64]*string{1: &site})
	revertHistory(t, eng, siteActor(1, "owner-site", permReview))
	applyOverrides(t, eng, []editing.PolicyOverride{
		{EntityType: "test.owned", FieldKey: "test.owned.name", Review: strp(editing.ReviewPerm(strictReview))},
	})

	_, _, err := eng.Revert(testCtx, editing.RevertInput{
		EntityType: "test.owned", EntityID: 1, ToSeq: 1,
		Actor: siteActor(2, "owner-site", permReview),
	})
	var perm *editing.PermissionError
	if !errors.As(err, &perm) {
		t.Fatalf("the override names a review permission this caller does not hold; got %v", err)
	}
	if got := loadWidget(t, 1).Name; got != "second" {
		t.Fatalf("the refused revert must not have written: %q", got)
	}

	if _, _, err := eng.Revert(testCtx, editing.RevertInput{
		EntityType: "test.owned", EntityID: 1, ToSeq: 1,
		Actor: siteActor(3, "some-other-site", strictReview),
	}); err != nil {
		t.Fatalf("the overriding permission reverts from any site: %v", err)
	}
	if got := loadWidget(t, 1).Name; got != "first" {
		t.Fatalf("want first, got %q", got)
	}
}

func TestRevertOnAnUnownedEntity(t *testing.T) {
	eng := revertEngine(t, map[int64]*string{})
	actor := siteActor(1, "some-other-site", permReview)
	revertHistory(t, eng, actor)

	if _, _, err := eng.Revert(testCtx, editing.RevertInput{
		EntityType: "test.owned", EntityID: 1, ToSeq: 1, Actor: actor,
	}); err != nil {
		t.Fatalf("unowned entity reverts under the default policy: %v", err)
	}
	if got := loadWidget(t, 1).Name; got != "first" {
		t.Fatalf("want first, got %q", got)
	}
}
