package editing_test

import (
	"testing"

	"api/internal/platform/editing"
)

func driftWidgetName(t *testing.T, name string) {
	t.Helper()
	if err := testDB.Exec("UPDATE edit_test_widget SET name = ? WHERE id = 1", name).Error; err != nil {
		t.Fatalf("drift the name outside the engine: %v", err)
	}
}

func TestDiffShowsTheValueTheEditReplacedNotThePreviousSnapshot(t *testing.T) {
	eng := newEngine(t)
	createWidget(t, 1)
	reviewer := reviewerActor(10)
	mergeName(t, eng, reviewer, "first")
	driftWidgetName(t, "importer")
	mergeName(t, eng, reviewer, "second")

	diffs, err := eng.Diff(testCtx, "test.widget", 1, 1, 2)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if len(diffs) != 1 || diffs[0].Key != fName || diffs[0].From != "importer" || diffs[0].To != "second" {
		t.Fatalf("diff = %+v, want %s importer -> second", diffs, fName)
	}
}

func TestUndoPutsBackTheValueTheEditReplaced(t *testing.T) {
	eng := newEngine(t)
	createWidget(t, 1)
	reviewer := reviewerActor(10)
	mergeName(t, eng, reviewer, "first")
	driftWidgetName(t, "importer")
	mergeName(t, eng, reviewer, "second")

	if _, _, err := eng.Undo(testCtx, editing.UndoInput{EntityType: "test.widget", EntityID: 1, Seq: 2, Actor: reviewer}); err != nil {
		t.Fatalf("undo: %v", err)
	}
	if got := loadWidget(t, 1).Name; got != "importer" {
		t.Fatalf("name after undo = %q, want importer", got)
	}
}

func TestARevisionWithoutPriorValuesStillDiffsAgainstItsPredecessor(t *testing.T) {
	eng := newEngine(t)
	createWidget(t, 1)
	reviewer := reviewerActor(10)
	mergeName(t, eng, reviewer, "first")
	driftWidgetName(t, "importer")
	mergeName(t, eng, reviewer, "second")
	if err := testDB.Exec("UPDATE edit_revision SET prior_values = NULL").Error; err != nil {
		t.Fatalf("clear prior values: %v", err)
	}

	diffs, err := eng.Diff(testCtx, "test.widget", 1, 1, 2)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if len(diffs) != 1 || diffs[0].From != "first" {
		t.Fatalf("diff = %+v, want the predecessor snapshot's value first", diffs)
	}
}
