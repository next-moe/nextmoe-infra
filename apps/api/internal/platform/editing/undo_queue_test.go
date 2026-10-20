package editing_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"api/internal/platform/editing"
)

func mergePatch(t *testing.T, eng *editing.Engine, actor editing.PolicyContext, patch map[string]any) {
	t.Helper()
	prop, _, err := eng.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: "test.widget", EntityID: 1, Patch: patch, Actor: actor,
	})
	if err != nil {
		t.Fatalf("propose %v: %v", patch, err)
	}
	if _, err := eng.MergeProposal(testCtx, prop.ID, actor, ""); err != nil {
		t.Fatalf("merge %v: %v", patch, err)
	}
}

func openProposal(t *testing.T, eng *editing.Engine, entityID int64, patch map[string]any) *editing.Proposal {
	t.Helper()
	prop, _, err := eng.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: "test.widget", EntityID: entityID, Patch: patch, Actor: actorWith(50, 0, permPropose),
	})
	if err != nil {
		t.Fatalf("propose %v: %v", patch, err)
	}
	return prop
}

func TestUndoPutsBackOnlyTheFieldsTheRevisionChanged(t *testing.T) {
	eng := newEngine(t)
	createWidget(t, 1)
	reviewer := reviewerActor(10)
	mergePatch(t, eng, reviewer, map[string]any{fName: "a", fOLang: "ja"})
	mergePatch(t, eng, reviewer, map[string]any{fName: "b"})
	mergePatch(t, eng, reviewer, map[string]any{fOLang: "en"})

	_, rev, err := eng.Undo(testCtx, editing.UndoInput{EntityType: "test.widget", EntityID: 1, Seq: 2, Actor: reviewer})
	if err != nil {
		t.Fatalf("undo revision 2: %v", err)
	}
	if rev.Seq != 4 || rev.Action != editing.ActionReverted {
		t.Fatalf("undo recorded seq %d action %d, want seq 4 reverted", rev.Seq, rev.Action)
	}
	w := loadWidget(t, 1)
	if w.Name != "a" || w.OLang != "en" {
		t.Fatalf("after undo name=%q olang=%q, want a (put back) and en (left alone)", w.Name, w.OLang)
	}
}

func TestUndoRefusesWhenALaterRevisionChangedTheSameField(t *testing.T) {
	eng := newEngine(t)
	createWidget(t, 1)
	reviewer := reviewerActor(10)
	mergeName(t, eng, reviewer, "a")
	mergeName(t, eng, reviewer, "b")
	mergeName(t, eng, reviewer, "c")
	before := countRows(t, "edit_revision")

	_, _, err := eng.Undo(testCtx, editing.UndoInput{EntityType: "test.widget", EntityID: 1, Seq: 2, Actor: reviewer})
	var conflict *editing.UndoConflictError
	if !errors.As(err, &conflict) || fmt.Sprint(conflict.Keys) != "["+fName+"]" {
		t.Fatalf("undo over a later change: err = %v, want UndoConflictError naming %s", err, fName)
	}
	if countRows(t, "edit_revision") != before || loadWidget(t, 1).Name != "c" {
		t.Fatal("a refused undo must write nothing")
	}

	if _, _, err := eng.Undo(testCtx, editing.UndoInput{
		EntityType: "test.widget", EntityID: 1, Seq: 2, Force: true, Actor: reviewer,
	}); err != nil {
		t.Fatalf("forced undo: %v", err)
	}
	if got := loadWidget(t, 1).Name; got != "a" {
		t.Fatalf("name after forced undo = %q, want a", got)
	}
}

func TestUndoOfTheFirstRevisionPutsBackTheBaseline(t *testing.T) {
	eng := newEngine(t)
	createWidget(t, 1)
	if err := testDB.Exec("UPDATE edit_test_widget SET name = 'imported' WHERE id = 1").Error; err != nil {
		t.Fatalf("seed importer value: %v", err)
	}
	reviewer := reviewerActor(10)
	mergeName(t, eng, reviewer, "first")

	if _, _, err := eng.Undo(testCtx, editing.UndoInput{EntityType: "test.widget", EntityID: 1, Seq: 1, Actor: reviewer}); err != nil {
		t.Fatalf("undo the first revision: %v", err)
	}
	if got := loadWidget(t, 1).Name; got != "imported" {
		t.Fatalf("name after undoing the first edit = %q, want imported", got)
	}
}

func TestUndoOfTheFirstRevisionNeedsARecordedBaseline(t *testing.T) {
	eng := newEngine(t)
	createWidget(t, 1)
	reviewer := reviewerActor(10)
	mergeName(t, eng, reviewer, "first")
	if err := testDB.Exec("UPDATE edit_revision SET baseline_snapshot = NULL").Error; err != nil {
		t.Fatalf("drop baseline: %v", err)
	}
	_, _, err := eng.Undo(testCtx, editing.UndoInput{EntityType: "test.widget", EntityID: 1, Seq: 1, Actor: reviewer})
	if !errors.Is(err, editing.ErrNoBaseline) {
		t.Fatalf("undo without a baseline: err = %v, want ErrNoBaseline", err)
	}
}

func TestUndoNeedsReviewStanding(t *testing.T) {
	eng := newEngine(t)
	createWidget(t, 1)
	mergeName(t, eng, reviewerActor(10), "a")
	mergeName(t, eng, reviewerActor(10), "b")
	_, _, err := eng.Undo(testCtx, editing.UndoInput{
		EntityType: "test.widget", EntityID: 1, Seq: 2, Actor: actorWith(50, 0, permPropose),
	})
	var perm *editing.PermissionError
	if !errors.As(err, &perm) {
		t.Fatalf("undo by a proposer: err = %v, want PermissionError", err)
	}
}

func listIDs(t *testing.T, eng *editing.Engine, f editing.ProposalFilter) []int64 {
	t.Helper()
	rows, _, err := eng.ListProposalsWithTotal(testCtx, f)
	if err != nil {
		t.Fatalf("list %+v: %v", f, err)
	}
	ids := make([]int64, 0, len(rows))
	for i := range rows {
		ids = append(ids, rows[i].ID)
	}
	return ids
}

func TestQueueFieldFilterSeesAmendments(t *testing.T) {
	eng := newEngine(t)
	createWidget(t, 1)
	byName := openProposal(t, eng, 1, map[string]any{fName: "x"})
	byLang := openProposal(t, eng, 1, map[string]any{fOLang: "ja"})
	if _, err := eng.AmendProposal(testCtx, byLang.ID, editing.AmendInput{
		Set: map[string]any{fName: "y"}, Actor: reviewerActor(10),
	}); err != nil {
		t.Fatalf("amend: %v", err)
	}
	openProposal(t, eng, 1, map[string]any{fRating: float64(3)})

	got := listIDs(t, eng, editing.ProposalFilter{Status: -1, FieldKey: fName})
	if fmt.Sprint(got) != fmt.Sprint([]int64{byLang.ID, byName.ID}) {
		t.Fatalf("field=%s matched %v, want the direct patch and the amended one %v", fName, got, []int64{byLang.ID, byName.ID})
	}
}

func TestQueueConflictFilterAndConflictFields(t *testing.T) {
	eng := newEngine(t)
	createWidget(t, 1)
	stale := openProposal(t, eng, 1, map[string]any{fName: "x", fOLang: "ja"})
	other := openProposal(t, eng, 1, map[string]any{fRating: float64(2)})
	mergeName(t, eng, reviewerActor(10), "landed")

	yes, no := true, false
	if got := listIDs(t, eng, editing.ProposalFilter{Status: -1, HasConflict: &yes}); fmt.Sprint(got) != fmt.Sprint([]int64{stale.ID}) {
		t.Fatalf("has_conflict=true = %v, want [%d]", got, stale.ID)
	}
	if got := listIDs(t, eng, editing.ProposalFilter{HasConflict: &no, Status: editing.StatusOpen}); fmt.Sprint(got) != fmt.Sprint([]int64{other.ID}) {
		t.Fatalf("has_conflict=false = %v, want [%d]", got, other.ID)
	}

	rows, _, err := eng.ListProposalsWithTotal(testCtx, editing.ProposalFilter{Status: editing.StatusOpen})
	if err != nil {
		t.Fatal(err)
	}
	effective, err := eng.EffectivePatches(testCtx, rows)
	if err != nil {
		t.Fatal(err)
	}
	fields, err := eng.ConflictFields(testCtx, rows, effective)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(fields[stale.ID]) != "["+fName+"]" || len(fields[other.ID]) != 0 {
		t.Fatalf("conflict fields = %v, want only %s on proposal %d", fields, fName, stale.ID)
	}
}

func TestQueueOrdersPageWithTheirOwnCursors(t *testing.T) {
	eng := newEngine(t)
	createWidget(t, 1)
	a := openProposal(t, eng, 1, map[string]any{fName: "a"})
	b := openProposal(t, eng, 1, map[string]any{fName: "b"})
	c := openProposal(t, eng, 1, map[string]any{fName: "c"})
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for i, id := range []int64{b.ID, c.ID, a.ID} {
		if err := testDB.Exec("UPDATE edit_proposal SET updated_at = ? WHERE id = ?", base.Add(time.Duration(i)*time.Hour), id).Error; err != nil {
			t.Fatal(err)
		}
	}

	if got := listIDs(t, eng, editing.ProposalFilter{Status: -1, Order: editing.ProposalOrderFiledAsc}); fmt.Sprint(got) != fmt.Sprint([]int64{a.ID, b.ID, c.ID}) {
		t.Fatalf("filed_asc = %v", got)
	}
	first := listIDs(t, eng, editing.ProposalFilter{Status: -1, Order: editing.ProposalOrderUpdatedAsc, Limit: 2})
	if fmt.Sprint(first) != fmt.Sprint([]int64{b.ID, c.ID}) {
		t.Fatalf("updated_asc page 1 = %v, want [%d %d]", first, b.ID, c.ID)
	}
	rest := listIDs(t, eng, editing.ProposalFilter{Status: -1, Order: editing.ProposalOrderUpdatedAsc, Limit: 2,
		Cursor: editing.ProposalCursor{ID: c.ID, UpdatedAt: base.Add(time.Hour)}})
	if fmt.Sprint(rest) != fmt.Sprint([]int64{a.ID}) {
		t.Fatalf("updated_asc page 2 = %v, want [%d]", rest, a.ID)
	}
	desc := listIDs(t, eng, editing.ProposalFilter{Status: -1, Order: editing.ProposalOrderUpdatedDesc})
	if fmt.Sprint(desc) != fmt.Sprint([]int64{a.ID, c.ID, b.ID}) {
		t.Fatalf("updated_desc = %v", desc)
	}
	since := base.Add(30 * time.Minute)
	if got := listIDs(t, eng, editing.ProposalFilter{Status: -1, UpdatedSince: &since, Order: editing.ProposalOrderFiledAsc}); fmt.Sprint(got) != fmt.Sprint([]int64{a.ID, c.ID}) {
		t.Fatalf("updated_since = %v, want [%d %d]", got, a.ID, c.ID)
	}
}
