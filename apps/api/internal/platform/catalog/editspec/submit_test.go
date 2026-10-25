package editspec

import (
	"slices"
	"testing"
)

func TestSubmissionFieldsMatchTheMatrix(t *testing.T) {
	matrix := make([]string, 0, len(workFieldSpecs()))
	for _, f := range workFieldSpecs() {
		matrix = append(matrix, f.Key)
	}
	for key := range submissionFields {
		if !slices.Contains(matrix, key) {
			t.Fatalf("%s is submittable but not registered on catalog.work", key)
		}
	}
	var excluded []string
	for _, key := range matrix {
		if _, ok := submissionFields[key]; !ok {
			excluded = append(excluded, key)
		}
	}
	// FieldWorkTitlesSuppr, FieldWorkTagIDsSuppr, FieldWorkCreditsSuppr and
	// FieldWorkRosterSuppr are deliberately not submittable: a work being
	// submitted has no upstream rows yet, so there is nothing to suppress.
	// FieldWorkCredits is excluded for its own reason — a submission form has
	// no way to hand out catalog_credit_name ids, and crediting is an edit made
	// after the work exists, as are the roster, relations, platforms, series
	// placements and tag spoilers: each describes a work against rows the
	// submission form does not show. Route hints are learned by playing the
	// game, not when listing it.
	want := []string{
		FieldWorkTitlesSuppr, FieldWorkRouteHints, FieldWorkTagIDsSuppr, FieldWorkCovers, FieldWorkScreenshots,
		FieldWorkCredits, FieldWorkCreditsSuppr,
		FieldWorkRoster, FieldWorkRosterSuppr,
		FieldWorkRelations, FieldWorkPlatforms, FieldWorkSeriesMembers, FieldWorkTagSpoilers,
	}
	if !slices.Equal(excluded, want) {
		t.Fatalf("excluded from submissions = %v, want %v", excluded, want)
	}
	if got := SubmissionFieldKeys(); len(got) != len(submissionFields) || !slices.Contains(got, FieldWorkDisplayName) {
		t.Fatalf("SubmissionFieldKeys: %v", got)
	}
}
