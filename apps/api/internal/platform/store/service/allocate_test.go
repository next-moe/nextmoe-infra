package service

import "testing"

func points(grant map[int]int) int64 {
	var p int64
	for face, n := range grant {
		p += int64(face) * int64(n)
	}
	return p
}

func TestAllocateNeverExceedsTheRoundedDownEntitlement(t *testing.T) {
	// The first real batch (2026-09): ten 5,000-point and twenty-one
	// 1,000-point coupons. The forum and the patch site share an owner, so
	// their human clicks arrive as one claim next to a small partner's.
	claims := []Claim{{UserID: 2, Uniques: 6417}, {UserID: 89136, Uniques: 7}, {UserID: 72394, Uniques: 1}}
	pool := map[int]int{5000: 10, 1000: 21}
	got := Allocate(claims, pool)
	ent := Entitlements(claims, 71_000)

	for _, c := range claims {
		if p := points(got[c.UserID]); p > ent[c.UserID].Points {
			t.Errorf("user %d: allocated %d, entitled to %d — rounded up", c.UserID, p, ent[c.UserID].Points)
		}
	}
	if ent[2].Points != 70_911 {
		t.Fatalf("entitlement = %d, want floor(71000·6417/6425) = 70,911", ent[2].Points)
	}
	if got[2][5000] != 10 || got[2][1000] != 20 {
		t.Errorf("want 10×5000 + 20×1000 for the owner of both kungal sites, got %v", got[2])
	}
	if len(got[89136]) != 0 || len(got[72394]) != 0 {
		t.Errorf("accounts entitled to <1,000 points must not take a coupon: %v", got)
	}
}

func TestAllocateFillsWithSmallerFacesWhenALargeOneDoesNotFit(t *testing.T) {
	got := Allocate([]Claim{{UserID: 1, Uniques: 55}, {UserID: 2, Uniques: 45}}, map[int]int{5000: 2, 1000: 5})
	// 15,000 points: entitled 8,250 and 6,750. One 5,000 each, then the
	// 1,000s go to whoever has the most left: 3 to user 1, 1 to user 2 and
	// one coupon fits neither (250 and 750 left).
	if got[1][5000] != 1 || got[1][1000] != 3 || got[2][5000] != 1 || got[2][1000] != 1 {
		t.Fatalf("want 1=5000+3×1000, 2=5000+1×1000, got %v", got)
	}
}

func TestAllocateBreaksTiesDeterministically(t *testing.T) {
	for range 20 {
		got := Allocate([]Claim{{UserID: 9, Uniques: 5}, {UserID: 3, Uniques: 5}}, map[int]int{1000: 2})
		if got[3][1000] != 1 || got[9][1000] != 1 {
			t.Fatalf("an exact tie places one each, got %v", got)
		}
		got = Allocate([]Claim{{UserID: 9, Uniques: 5}, {UserID: 3, Uniques: 5}}, map[int]int{500: 2, 1000: 1})
		if got[3][1000] != 1 {
			t.Fatalf("the first coupon of a tie goes to the lower user id, got %v", got)
		}
	}
}

func TestAllocateWithNoClicksPlacesNothing(t *testing.T) {
	got := Allocate([]Claim{{UserID: 1, Uniques: 0}}, map[int]int{1000: 3})
	if len(got) != 0 {
		t.Fatalf("no clicks, no basis for a split: got %v", got)
	}
	if ent := Entitlements([]Claim{{UserID: 1, Uniques: 0}}, 3000); ent[1].Points != 0 {
		t.Fatalf("entitlement without clicks = %+v", ent[1])
	}
}

func TestEntitlementsRoundDown(t *testing.T) {
	ent := Entitlements([]Claim{{UserID: 1, Uniques: 2}, {UserID: 2, Uniques: 1}}, 1000)
	if ent[1].Points != 666 || ent[2].Points != 333 {
		t.Errorf("want 666 and 333 (a point is left over), got %+v", ent)
	}
	if ent[1].SharePPM != 666_667 || ent[2].SharePPM != 333_333 {
		t.Errorf("shares = %+v", ent)
	}
}
