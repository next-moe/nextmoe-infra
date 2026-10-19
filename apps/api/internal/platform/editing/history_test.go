package editing_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"api/internal/platform/editing"

	"gorm.io/gorm"
)

func mergeName(t *testing.T, eng *editing.Engine, actor editing.PolicyContext, name string) {
	t.Helper()
	prop, _, err := eng.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: "test.widget", EntityID: 1, Patch: map[string]any{fName: name}, Actor: actor,
	})
	if err != nil {
		t.Fatalf("propose %q: %v", name, err)
	}
	if _, err := eng.MergeProposal(testCtx, prop.ID, actor, ""); err != nil {
		t.Fatalf("merge %q: %v", name, err)
	}
}

func countRows(t *testing.T, table string) int64 {
	t.Helper()
	var n int64
	if err := testDB.Raw("SELECT count(*) FROM " + table).Scan(&n).Error; err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func proposalStatus(t *testing.T, id int64) int16 {
	t.Helper()
	var status int16
	if err := testDB.Raw("SELECT status FROM edit_proposal WHERE id = ?", id).Scan(&status).Error; err != nil {
		t.Fatalf("load proposal %d: %v", id, err)
	}
	return status
}

func TestRevertToSeqZeroRestoresTheStateBeforeTheFirstEdit(t *testing.T) {
	eng := newEngine(t)
	createWidget(t, 1)
	if err := testDB.Exec("UPDATE edit_test_widget SET name = 'imported' WHERE id = 1").Error; err != nil {
		t.Fatalf("seed importer value: %v", err)
	}
	reviewer := reviewerActor(10)
	mergeName(t, eng, reviewer, "first")
	mergeName(t, eng, reviewer, "second")

	diffs, err := eng.Diff(testCtx, "test.widget", 1, 0, 1)
	if err != nil {
		t.Fatalf("diff against the baseline: %v", err)
	}
	if len(diffs) != 1 || diffs[0].Key != fName || diffs[0].From != "imported" || diffs[0].To != "first" {
		t.Fatalf("diff 0..1 = %+v, want only %s imported -> first", diffs, fName)
	}

	_, rev, err := eng.Revert(testCtx, editing.RevertInput{
		EntityType: "test.widget", EntityID: 1, ToSeq: 0, Actor: reviewer,
	})
	if err != nil {
		t.Fatalf("revert to the baseline: %v", err)
	}
	if rev.Seq != 3 || rev.Action != editing.ActionReverted {
		t.Fatalf("revert recorded seq %d action %d, want a new seq 3 reverted revision", rev.Seq, rev.Action)
	}
	if got := loadWidget(t, 1).Name; got != "imported" {
		t.Fatalf("name after revert = %q, want the importer's %q", got, "imported")
	}
}

func TestRevertToSeqZeroNeedsARecordedBaseline(t *testing.T) {
	eng := newEngine(t)
	createWidget(t, 1)
	reviewer := reviewerActor(10)
	mergeName(t, eng, reviewer, "first")
	if err := testDB.Exec("UPDATE edit_revision SET baseline_snapshot = NULL").Error; err != nil {
		t.Fatalf("drop baseline: %v", err)
	}

	_, _, err := eng.Revert(testCtx, editing.RevertInput{
		EntityType: "test.widget", EntityID: 1, ToSeq: 0, Actor: reviewer,
	})
	if !errors.Is(err, editing.ErrRevisionNotFound) {
		t.Fatalf("revert without a baseline: err = %v, want ErrRevisionNotFound", err)
	}
}

func TestOnlyTheFirstRevisionCarriesABaseline(t *testing.T) {
	eng := newEngine(t)
	createWidget(t, 1)
	reviewer := reviewerActor(10)
	mergeName(t, eng, reviewer, "first")
	mergeName(t, eng, reviewer, "second")

	var seqs []int
	if err := testDB.Raw("SELECT seq FROM edit_revision WHERE baseline_snapshot IS NOT NULL ORDER BY seq").
		Scan(&seqs).Error; err != nil {
		t.Fatalf("load baselines: %v", err)
	}
	if fmt.Sprint(seqs) != "[1]" {
		t.Fatalf("revisions carrying a baseline = %v, want [1]", seqs)
	}
}

func TestAMergeWhoseCommitFailsLeavesNoAppliedValue(t *testing.T) {
	eng := newEngine(t)
	createWidget(t, 1)
	for _, stmt := range []string{
		`DROP TRIGGER IF EXISTS edit_test_refuse_commit ON edit_revision`,
		`CREATE OR REPLACE FUNCTION edit_test_refuse_commit() RETURNS trigger LANGUAGE plpgsql AS $$
		 BEGIN RAISE EXCEPTION 'commit refused'; END $$`,
		`CREATE CONSTRAINT TRIGGER edit_test_refuse_commit AFTER INSERT ON edit_revision
		 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.site = 'commit-refused')
		 EXECUTE FUNCTION edit_test_refuse_commit()`,
	} {
		if err := testDB.Exec(stmt).Error; err != nil {
			t.Fatalf("install commit trap: %v", err)
		}
	}
	t.Cleanup(func() {
		testDB.Exec(`DROP TRIGGER IF EXISTS edit_test_refuse_commit ON edit_revision`)
		testDB.Exec(`DROP FUNCTION IF EXISTS edit_test_refuse_commit()`)
	})

	actor := reviewerActor(40)
	actor.Site = "commit-refused"
	prop, _, err := eng.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: "test.widget", EntityID: 1, Patch: map[string]any{fName: "never"}, Actor: actor,
	})
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	if _, err := eng.MergeProposal(testCtx, prop.ID, actor, ""); err == nil {
		t.Fatalf("merge succeeded although its commit is refused")
	}

	if got := loadWidget(t, 1).Name; got != "" {
		t.Fatalf("name = %q after a merge that never committed, want it untouched", got)
	}
	if n := countRows(t, "edit_revision"); n != 0 {
		t.Fatalf("%d revisions after a refused commit, want 0", n)
	}
	if got := proposalStatus(t, prop.ID); got != editing.StatusOpen {
		t.Fatalf("proposal status = %d after a refused commit, want open", got)
	}
}

func TestAnApplyFailureLeavesNoRevisionAndAnOpenProposal(t *testing.T) {
	cleanTables(t)
	createWidget(t, 1)
	spec := widgetSpec(testDB)
	for i := range spec.Fields {
		if spec.Fields[i].Key != fName {
			continue
		}
		apply := spec.Fields[i].Apply
		spec.Fields[i].Apply = func(ctx context.Context, tx *gorm.DB, id int64, value any) error {
			if value == "boom" {
				return fmt.Errorf("apply refused")
			}
			return apply(ctx, tx, id, value)
		}
	}
	reg := editing.NewRegistry()
	if err := reg.Register(spec); err != nil {
		t.Fatalf("register: %v", err)
	}
	eng := editing.NewEngine(testDB, reg)
	reviewer := reviewerActor(41)

	prop, _, err := eng.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: "test.widget", EntityID: 1, Patch: map[string]any{fName: "boom", fOLang: "en"}, Actor: reviewer,
	})
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	if _, err := eng.MergeProposal(testCtx, prop.ID, reviewer, ""); err == nil {
		t.Fatalf("merge succeeded although one field's apply failed")
	}
	if w := loadWidget(t, 1); w.Name != "" || w.OLang != "ja" {
		t.Fatalf("widget = %+v after a failed apply, want no field written", w)
	}
	if n := countRows(t, "edit_revision"); n != 0 {
		t.Fatalf("%d revisions after a failed apply, want 0", n)
	}
	if got := proposalStatus(t, prop.ID); got != editing.StatusOpen {
		t.Fatalf("proposal status = %d after a failed apply, want open", got)
	}

	mergeName(t, eng, reviewer, "fine")
	if got := loadWidget(t, 1).Name; got != "fine" {
		t.Fatalf("name = %q after a clean merge, want %q", got, "fine")
	}
	if n := countRows(t, "edit_revision"); n != 1 {
		t.Fatalf("%d revisions after a clean merge, want 1", n)
	}
}

func TestDeclineClosesAProposalWhoseEntityIsGone(t *testing.T) {
	cleanTables(t)
	createWidget(t, 1)
	gone := false
	reg := editing.NewRegistry()
	hook := func(context.Context, int64) (*int64, error) {
		if gone {
			return nil, editing.ErrEntityNotFound
		}
		return nil, nil
	}
	if err := reg.Register(ownUserSpec("test.ownuser", hook)); err != nil {
		t.Fatalf("register: %v", err)
	}
	eng := editing.NewEngine(testDB, reg)
	prop := fileProposal(t, eng, "test.ownuser", 30)

	gone = true
	if err := eng.DeclineProposal(testCtx, prop.ID, reviewerActor(31), "merged away"); err != nil {
		t.Fatalf("decline on a retired entity: %v", err)
	}
	if got := proposalStatus(t, prop.ID); got != editing.StatusDeclined {
		t.Fatalf("proposal status = %d, want declined", got)
	}
}
