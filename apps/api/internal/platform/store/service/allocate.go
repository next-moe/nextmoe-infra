package service

import (
	"cmp"
	"slices"
)

// Claim is one developer account and the de-duplicated clicks its
// settlement-eligible applications drew over a batch's period.
type Claim struct {
	UserID  uint
	Uniques int64
}

// Entitlement is an account's proportional slice of a pool, in points (rounded
// down) and in parts per million of the claimants' clicks.
type Entitlement struct {
	SharePPM int64
	Points   int64
}

// Entitlements splits totalPoints among the claims in proportion to their
// uniques, rounding each slice down. A claim with no clicks is entitled to
// nothing.
func Entitlements(claims []Claim, totalPoints int64) map[uint]Entitlement {
	var sum int64
	for _, c := range claims {
		sum += max(c.Uniques, 0)
	}
	out := make(map[uint]Entitlement, len(claims))
	for _, c := range claims {
		if sum == 0 || c.Uniques <= 0 {
			out[c.UserID] = Entitlement{}
			continue
		}
		out[c.UserID] = Entitlement{
			SharePPM: divRound(c.Uniques*1_000_000, sum),
			Points:   totalPoints * c.Uniques / sum,
		}
	}
	return out
}

// Allocate hands out a pool of coupons (face value → count) without taking any
// account past its entitlement: largest face value first, each coupon to the
// account with the most entitlement left that the coupon still fits in. A
// coupon that fits nobody stays with the platform. The result maps user ID →
// face value → count.
func Allocate(claims []Claim, pool map[int]int) map[uint]map[int]int {
	out := make(map[uint]map[int]int, len(claims))
	faces := make([]int, 0, len(pool))
	var total int64
	for face, n := range pool {
		if n > 0 {
			faces = append(faces, face)
			total += int64(face) * int64(n)
		}
	}
	slices.SortFunc(faces, func(a, b int) int { return cmp.Compare(b, a) })

	ent := Entitlements(claims, total)
	left := make(map[uint]int64, len(claims))
	for id, e := range ent {
		left[id] = e.Points
	}
	for _, face := range faces {
		for range pool[face] {
			best := -1
			for i, c := range claims {
				if left[c.UserID] < int64(face) {
					continue
				}
				if best < 0 {
					best = i
					continue
				}
				b := claims[best]
				if l, bl := left[c.UserID], left[b.UserID]; l > bl ||
					(l == bl && (c.Uniques > b.Uniques || (c.Uniques == b.Uniques && c.UserID < b.UserID))) {
					best = i
				}
			}
			if best < 0 {
				break
			}
			id := claims[best].UserID
			if out[id] == nil {
				out[id] = map[int]int{}
			}
			out[id][face]++
			left[id] -= int64(face)
		}
	}
	return out
}

func divRound(num, den int64) int64 { return (num + den/2) / den }
