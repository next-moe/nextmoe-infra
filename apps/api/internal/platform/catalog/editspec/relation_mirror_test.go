package editspec_test

import (
	"encoding/json"
	"errors"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"
	"api/internal/platform/editorigin"
)

func revisionsOf(t *testing.T, entityType string, id int64) []editing.Revision {
	t.Helper()
	var revs []editing.Revision
	if err := testDB.Where("entity_type = ? AND entity_id = ?", entityType, id).
		Order("seq").Find(&revs).Error; err != nil {
		t.Fatal(err)
	}
	return revs
}

func changedFields(t *testing.T, rev editing.Revision) []string {
	t.Helper()
	var keys []string
	if err := json.Unmarshal(rev.ChangedFields, &keys); err != nil {
		t.Fatal(err)
	}
	return keys
}

func snapshotField(t *testing.T, raw []byte, key string) any {
	t.Helper()
	var snap map[string]any
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	return snap[key]
}

func TestWorkRelationEditGivesTheOtherWorkARevision(t *testing.T) {
	e := newEngine(t)
	a := createWork(t, "前作")
	b := createWork(t, "続編")

	pending, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: editspec.TypeWork, EntityID: a.ID,
		Patch: map[string]any{editspec.FieldWorkRelations: []any{
			map[string]any{"work_id": float64(b.ID), "relation": "remake_of"},
		}},
		Actor: realActor(100),
	})
	if err != nil {
		t.Fatal(err)
	}

	mergeField(t, e, b.ID, editspec.FieldWorkRelations,
		[]any{map[string]any{"work_id": float64(a.ID), "relation": "sequel_of"}})

	revs := revisionsOf(t, editspec.TypeWork, a.ID)
	if len(revs) != 1 {
		t.Fatalf("the other end needs exactly one revision, got %d", len(revs))
	}
	sameJSON(t, "changed fields", changedFields(t, revs[0]), []string{editspec.FieldWorkRelations})
	var onB editing.Proposal
	if err := testDB.Where("entity_id = ? AND status = ?", b.ID, editing.StatusMerged).First(&onB).Error; err != nil {
		t.Fatal(err)
	}
	if revs[0].ProposalID == nil || *revs[0].ProposalID != onB.ID || revs[0].ActorUID != 100 {
		t.Fatalf("the revision must credit b's proposal and its author: %+v", revs[0])
	}
	sameJSON(t, "snapshot on a", snapshotField(t, revs[0].Snapshot, editspec.FieldWorkRelations),
		[]any{map[string]any{"work_id": b.ID, "relation": "sequel_of", "reverse": true}})
	sameJSON(t, "baseline on a", snapshotField(t, revs[0].BaselineSnapshot, editspec.FieldWorkRelations), []any{})

	var conflict *editing.ConflictError
	if _, err := e.MergeProposal(testCtx, pending.ID, realActor(200, "ren"), ""); !errors.As(err, &conflict) {
		t.Fatalf("a proposal filed on a before b's edit must conflict on relations, got %v", err)
	}

	if _, _, err := e.Revert(testCtx, editing.RevertInput{
		EntityType: editspec.TypeWork, EntityID: b.ID, ToSeq: 0, Actor: realActor(200, "ren"),
	}); err != nil {
		t.Fatalf("revert b: %v", err)
	}
	revs = revisionsOf(t, editspec.TypeWork, a.ID)
	if len(revs) != 2 || revs[1].Action != editing.ActionReverted {
		t.Fatalf("reverting b must give a a reverted revision too, got %d revisions", len(revs))
	}
	sameJSON(t, "a after revert", snapshotField(t, revs[1].Snapshot, editspec.FieldWorkRelations), []any{})
}

func TestWorkRelationEchoLeavesTheOtherWorkAlone(t *testing.T) {
	e := newEngine(t)
	a := createWork(t, "前作")
	b := createWork(t, "続編")
	c := createWork(t, "FD")
	mergeField(t, e, b.ID, editspec.FieldWorkRelations,
		[]any{map[string]any{"work_id": float64(a.ID), "relation": "sequel_of"}})
	before := len(revisionsOf(t, editspec.TypeWork, b.ID))

	mergeField(t, e, a.ID, editspec.FieldWorkRelations, []any{
		map[string]any{"work_id": float64(b.ID), "relation": "sequel_of", "reverse": true},
		map[string]any{"work_id": float64(c.ID), "relation": "fandisc_of", "reverse": true},
	})
	if got := len(revisionsOf(t, editspec.TypeWork, b.ID)); got != before {
		t.Fatalf("b's value did not change, so it needs no revision: %d -> %d", before, got)
	}
	if got := len(revisionsOf(t, editspec.TypeWork, c.ID)); got != 1 {
		t.Fatalf("c gained an edge, so it needs one revision, got %d", got)
	}
}

func TestLabelRelationEditGivesTheOtherCompanyARevision(t *testing.T) {
	e := newLabelEngine(t)
	parent := createLabel(t, "VisualArts")
	brand := createLabel(t, "Key")
	if err := mergeEdit(t, e, editspec.TypeLabel, brand.ID, map[string]any{
		editspec.FieldLabelRelations: []any{map[string]any{
			"label_id": float64(parent.ID), "relation": float64(model.LabelRelationParent),
		}},
	}); err != nil {
		t.Fatal(err)
	}
	revs := revisionsOf(t, editspec.TypeLabel, parent.ID)
	if len(revs) != 1 {
		t.Fatalf("the mirrored company needs one revision, got %d", len(revs))
	}
	sameJSON(t, "changed fields", changedFields(t, revs[0]), []string{editspec.FieldLabelRelations})
	sameJSON(t, "snapshot on parent", snapshotField(t, revs[0].Snapshot, editspec.FieldLabelRelations),
		[]any{map[string]any{"label_id": brand.ID, "relation": int64(model.LabelRelationSubsidiary)}})
}

func TestCounterpartRevisionKeepsTheOriginSite(t *testing.T) {
	e := newEngine(t)
	a := createWork(t, "本編")
	b := createWork(t, "外伝")
	ctx := editorigin.With(testCtx, "moyu")

	prop, _, err := e.CreateProposal(ctx, editing.CreateProposalInput{
		EntityType: editspec.TypeWork, EntityID: b.ID,
		Patch: map[string]any{editspec.FieldWorkRelations: []any{
			map[string]any{"work_id": float64(a.ID), "relation": "side_story_of"},
		}},
		Actor: realActor(100),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.MergeProposal(testCtx, prop.ID, realActor(200, "ren"), ""); err != nil {
		t.Fatal(err)
	}

	revs := revisionsOf(t, editspec.TypeWork, a.ID)
	if len(revs) != 1 || revs[0].OriginSite == nil || *revs[0].OriginSite != "moyu" {
		t.Fatalf("the other end's revision must carry the proposal's origin site: %+v", revs)
	}
}
