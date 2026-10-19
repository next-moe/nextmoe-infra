package editing_test

import (
	"encoding/json"
	"errors"
	"testing"

	"api/internal/platform/editing"
)

func TestPolicyStorePutAppliesAndAudits(t *testing.T) {
	e := newEngine(t)
	store := editing.NewPolicyStore(testDB, e.Registry())
	spec, _ := e.Registry().Type("test.widget")

	if _, err := store.Put(testCtx, 77, editing.PolicyOverride{
		EntityType: "test.widget", FieldKey: fName, Propose: strp(editing.ProposeOpen),
	}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if spec.EffectivePolicy(fName).Propose != editing.ProposeOpen {
		t.Fatal("a put must apply in-process without waiting for the poll")
	}

	row, err := store.Put(testCtx, 78, editing.PolicyOverride{
		EntityType: "test.widget", FieldKey: fName, Automerge: strp(editing.AutomergeAlways),
	})
	if err != nil {
		t.Fatalf("second put: %v", err)
	}
	if row.Propose != nil || row.Automerge == nil || row.UpdatedBy != 78 {
		t.Fatalf("a put replaces the override whole: %+v", row)
	}
	if got := spec.EffectivePolicy(fName); got.Propose != editing.ProposePerm(permPropose) || got.Automerge != editing.AutomergeAlways {
		t.Fatalf("effective after replace: %+v", got)
	}

	var verr *editing.ValidationError
	if _, err := store.Put(testCtx, 79, editing.PolicyOverride{
		EntityType: "test.widget", Automerge: strp("maybe"),
	}); !errors.As(err, &verr) {
		t.Fatalf("an invalid override must be refused before it is written: %v", err)
	}

	if err := store.Delete(testCtx, 80, "test.widget", fName); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got := spec.EffectivePolicy(fName); got != spec.CodePolicy(fName) {
		t.Fatalf("a delete must restore the code policy: %+v", got)
	}
	if err := store.Delete(testCtx, 80, "test.widget", fName); !errors.Is(err, editing.ErrOverrideNotFound) {
		t.Fatalf("deleting nothing: %v", err)
	}

	audit, err := store.Audit(testCtx, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(audit) != 3 {
		t.Fatalf("audit rows = %d, want 3 (two puts, one delete; the refused put writes none)", len(audit))
	}
	del, replace, first := audit[0], audit[1], audit[2]
	if del.ActorUID != 80 || del.After != nil || len(del.Before) == 0 {
		t.Errorf("delete audit: %+v", del)
	}
	if first.ActorUID != 77 || first.Before != nil {
		t.Errorf("first put audit: %+v", first)
	}
	var before editing.PolicyOverride
	if err := json.Unmarshal(replace.Before, &before); err != nil || before.Propose == nil || *before.Propose != editing.ProposeOpen {
		t.Errorf("replace audit must carry the old override: %+v (%v)", before, err)
	}
}

func TestPolicyStoreReloadPicksUpAnotherWriter(t *testing.T) {
	e := newEngine(t)
	store := editing.NewPolicyStore(testDB, e.Registry())
	spec, _ := e.Registry().Type("test.widget")

	if err := testDB.Exec(`INSERT INTO edit_policy_override (entity_type, field_key, propose, updated_by, updated_at)
		VALUES ('test.widget', ?, 'locked', 1, now()), ('test.gone', '', 'open', 1, now())`, fOLang).Error; err != nil {
		t.Fatal(err)
	}
	if spec.EffectivePolicy(fOLang).Propose == editing.ProposeLocked {
		t.Fatal("fixture: nothing has loaded the row yet")
	}
	if err := store.Reload(testCtx); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if spec.EffectivePolicy(fOLang).Propose != editing.ProposeLocked {
		t.Fatal("a poll must apply a row another process wrote, beside one for a type that no longer exists")
	}
}

func TestOverrideOnAParentReachesItsSuppressionCompanion(t *testing.T) {
	e := widgetSuppressionEngine(t)
	spec, _ := e.Registry().Type("test.widget")
	applyOverrides(t, e, []editing.PolicyOverride{
		{EntityType: "test.widget", FieldKey: fRows, Propose: strp(editing.ProposeLocked)},
	})
	if spec.EffectivePolicy(fRowsSuppr).Propose != editing.ProposeLocked {
		t.Fatal("locking a list field must lock its suppression set too")
	}
	applyOverrides(t, e, []editing.PolicyOverride{
		{EntityType: "test.widget", FieldKey: fRows, Propose: strp(editing.ProposeLocked)},
		{EntityType: "test.widget", FieldKey: fRowsSuppr, Propose: strp(editing.ProposeOpen)},
	})
	if spec.EffectivePolicy(fRowsSuppr).Propose != editing.ProposeOpen {
		t.Fatal("the companion's own override still wins")
	}
}
