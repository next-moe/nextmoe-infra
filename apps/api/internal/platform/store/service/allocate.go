package service

import (
	"cmp"
	"slices"
)

// Claim is one settlement-eligible site and its de-duplicated clicks over a
// batch's period.
type Claim struct {
	ClientID string
	Uniques  int64
}

// Entitlement is a site's proportional slice of a pool, in points and in parts
// per million of the claimants' clicks.
type Entitlement struct {
	SharePPM int64
	Points   int64
}

// Entitlements splits totalPoints among the claims in proportion to their
// uniques. A claim with no clicks is entitled to nothing.
func Entitlements(claims []Claim, totalPoints int64) map[string]Entitlement {
	var sum int64
	for _, c := range claims {
		sum += max(c.Uniques, 0)
	}
	out := make(map[string]Entitlement, len(claims))
	for _, c := range claims {
		if sum == 0 || c.Uniques <= 0 {
			out[c.ClientID] = Entitlement{}
			continue
		}
		out[c.ClientID] = Entitlement{
			SharePPM: divRound(c.Uniques*1_000_000, sum),
			Points:   divRound(totalPoints*c.Uniques, sum),
		}
	}
	return out
}

// Allocate hands out a pool of coupons (face value → count) to the claims:
// largest face value first, each coupon to the site furthest below its
// proportional entitlement. Every coupon is placed as long as some claim has
// clicks; the result maps client ID → face value → count.
func Allocate(claims []Claim, pool map[int]int) map[string]map[int]int {
	out := make(map[string]map[int]int, len(claims))
	var sum, total int64
	live := make([]Claim, 0, len(claims))
	for _, c := range claims {
		if c.Uniques > 0 {
			live = append(live, c)
			sum += c.Uniques
		}
	}
	faces := make([]int, 0, len(pool))
	for face, n := range pool {
		if n > 0 {
			faces = append(faces, face)
			total += int64(face) * int64(n)
		}
	}
	if sum == 0 {
		return out
	}
	slices.SortFunc(faces, func(a, b int) int { return cmp.Compare(b, a) })

	// deficit is scaled by sum so the comparison stays in integers: a site's
	// entitlement is total·uniques/sum, so sum·(entitlement − allocated) is
	// total·uniques − allocated·sum.
	allocated := make(map[string]int64, len(live))
	deficit := func(c Claim) int64 { return total*c.Uniques - allocated[c.ClientID]*sum }
	for _, face := range faces {
		for range pool[face] {
			best := live[0]
			for _, c := range live[1:] {
				if d, bd := deficit(c), deficit(best); d > bd ||
					(d == bd && (c.Uniques > best.Uniques || (c.Uniques == best.Uniques && c.ClientID < best.ClientID))) {
					best = c
				}
			}
			if out[best.ClientID] == nil {
				out[best.ClientID] = map[int]int{}
			}
			out[best.ClientID][face]++
			allocated[best.ClientID] += int64(face)
		}
	}
	return out
}

func divRound(num, den int64) int64 { return (num + den/2) / den }
