package service

import "testing"

func points(grant map[int]int) int64 {
	var p int64
	for face, n := range grant {
		p += int64(face) * int64(n)
	}
	return p
}

func TestAllocatePlacesTheFirstDLsiteBatchByShare(t *testing.T) {
	// The first real batch (2026-09): ten 5,000-point and twenty-one
	// 1,000-point coupons, over roughly the human clicks of the two kungal
	// sites and one small partner.
	claims := []Claim{{"forum", 3060}, {"patch", 3357}, {"yukihub", 7}}
	pool := map[int]int{5000: 10, 1000: 21}
	got := Allocate(claims, pool)

	var placed int64
	for _, grant := range got {
		placed += points(grant)
	}
	if placed != 71_000 {
		t.Fatalf("placed %d points, want the whole 71,000-point pool", placed)
	}
	ent := Entitlements(claims, 71_000)
	for _, c := range claims {
		diff := points(got[c.ClientID]) - ent[c.ClientID].Points
		if diff < -1000 || diff > 1000 {
			t.Errorf("%s: allocated %d, entitled %d — off by more than the smallest coupon",
				c.ClientID, points(got[c.ClientID]), ent[c.ClientID].Points)
		}
	}
	if len(got["yukihub"]) != 0 {
		t.Errorf("a site entitled to ~77 points must not take a coupon: %v", got["yukihub"])
	}
}

func TestAllocateLeavesTheSmallestFaceForTheSmallestDeficit(t *testing.T) {
	got := Allocate([]Claim{{"a", 90}, {"b", 10}}, map[int]int{5000: 1, 1000: 5})
	if got["a"][5000] != 1 || got["a"][1000] != 4 || got["b"][1000] != 1 {
		t.Fatalf("want a=5000+4×1000, b=1×1000 (entitled 9,000 / 1,000), got %v", got)
	}
}

func TestAllocateBreaksTiesDeterministically(t *testing.T) {
	for range 20 {
		got := Allocate([]Claim{{"b", 5}, {"a", 5}}, map[int]int{1000: 1})
		if got["a"][1000] != 1 {
			t.Fatalf("an exact tie goes to the lower client id, got %v", got)
		}
	}
}

func TestAllocateWithNoClicksPlacesNothing(t *testing.T) {
	got := Allocate([]Claim{{"a", 0}}, map[int]int{1000: 3})
	if len(got) != 0 {
		t.Fatalf("no clicks, no basis for a split: got %v", got)
	}
	if ent := Entitlements([]Claim{{"a", 0}}, 3000); ent["a"].Points != 0 {
		t.Fatalf("entitlement without clicks = %+v", ent["a"])
	}
}

func TestEntitlementsSumToThePool(t *testing.T) {
	ent := Entitlements([]Claim{{"a", 1}, {"b", 1}, {"c", 1}}, 3000)
	for id, e := range ent {
		if e.Points != 1000 || e.SharePPM != 333_333 {
			t.Errorf("%s = %+v, want 1000 points, 333333 ppm", id, e)
		}
	}
}
