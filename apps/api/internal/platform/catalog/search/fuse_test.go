package search

import (
	"slices"
	"testing"
)

func TestFuseRRF(t *testing.T) {
	got := FuseRRF(60, RankedList{IDs: []int64{1, 2, 3}, Weight: 1}, RankedList{IDs: []int64{3, 4, 1}, Weight: 1})
	if want := []int64{1, 3, 2, 4}; !slices.Equal(got, want) {
		t.Fatalf("fused = %v, want %v", got, want)
	}
}

func TestFuseRRFTiesKeepFirstSeenOrder(t *testing.T) {
	got := FuseRRF(60, RankedList{IDs: []int64{10}, Weight: 1}, RankedList{IDs: []int64{20}, Weight: 1})
	if want := []int64{10, 20}; !slices.Equal(got, want) {
		t.Fatalf("fused = %v, want %v", got, want)
	}
}

func TestFuseRRFWeightBreaksTheTieTowardTheHeavierList(t *testing.T) {
	got := FuseRRF(60, RankedList{IDs: []int64{10, 20}, Weight: 1}, RankedList{IDs: []int64{20, 10}, Weight: 2})
	if want := []int64{20, 10}; !slices.Equal(got, want) {
		t.Fatalf("fused = %v, want %v", got, want)
	}
}
