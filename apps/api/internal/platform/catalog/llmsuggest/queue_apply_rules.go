package llmsuggest

import (
	"fmt"
	"slices"
	"sort"

	"api/internal/platform/catalog/model"
)

const (
	applyAccept         = "accept"
	applyReject         = "reject"
	applyDefer          = "defer"
	applyConfirm        = "confirm"
	applyConfirmRelated = "confirm-related"

	stampRefConflict = "reject_ref_conflict"
	stampTargetGone  = "obsolete_target_missing"

	// The stamp is renamed because what it records changed: it used to mean
	// "noted, candidate untouched", and it now means "candidate deferred".
	// Renaming it is what reaches the 158 rows the old spelling already stamped
	// — a stamp outside currentStamps is re-judged, so they pick up the
	// disposition they should have had. Rename the stamp whenever the rule
	// behind it starts doing something else.
	stampObsoletePair = "obsolete_endpoint_deferred"

	skipUnsure            = "skipped_unsure"
	skipBelowConfidence   = "skipped_below_confidence"
	skipFrozenBothClaimed = "frozen_both_claimed"
	skipRefDifferent      = "skipped_different_held_for_human"
	skipChainUnproven     = "skipped_chain_unproven"
	skipGoldQueue         = "skipped_gold_queue"
	skipUnknownVerdict    = "skipped_unknown_verdict"

	errExactTaken = "error_exact_taken"
	errState      = "error_state"
	errNotFound   = "error_not_found"
	errOther      = "error_other"
)

// currentStamps is every value this package can write to applied_action. It is
// the idempotency key of the whole apply step, which is why it is a list and
// not an empty-string test: on 2026-09-14 a hand-written SQL pass stamped 325
// rows with excluded_conflicting_refs and held_ref_conflict, words that appear
// nowhere in this repo, and selecting on an empty applied_action then hid those
// rows from every rule written afterwards — including the ref-conflict screen
// that would have decided 319 of them that same night. A stamp outside this
// list means the row was parked by something that is no longer the rule, so
// the row is judged again rather than treated as done.
var currentStamps = []string{
	applyAccept, applyReject, applyConfirm, applyConfirmRelated,
	stampRefConflict, stampObsoletePair, stampTargetGone,
}

type exactRef struct {
	SourceID   int16
	SourceKey  string
	TrustTier  int16
	ExternalID string
}

type workPairSides struct {
	AID, BID           int64
	ClaimedA, ClaimedB bool
	ExactA, ExactB     int
	DeletedA, DeletedB bool
	RefsA, RefsB       []exactRef
}

func bothClaimed(s workPairSides) bool { return s.ClaimedA && s.ClaimedB }

// contradictingExactRef names one exact-tier disagreement between the two
// sides, or "" when they do not contradict. Two entities can never corroborate
// through exact refs — the partial unique uq_catalog_external_ref_exact makes
// sharing one impossible — so disagreement is the only signal this tier
// carries. IdentityVetoExemptSourceIDs says which sources carry no signal and
// why; the screen drops them itself rather than trusting its caller to, because
// a rule whose safety lives in a distant loader is one refactor from being a
// rule that vetoes on a howlongtobeat id.
//
// This runs ahead of the verdict, and overrules it. On 2026-09-14 the model
// judged works 8460 ⇔ 8501 ("Kill or Love" ⇔ "The Smoke Room") same at
// confidence 1.00, giving as its reason "both records share the same vndb id
// v26356" — the dossier it was handed lists v26356 against v28983 and an empty
// shared_refs. The registries disagreeing is a fact; the verdict is an opinion.
func contradictingExactRef(s workPairSides) string {
	byKey := map[int16][]exactRef{}
	for _, r := range s.RefsB {
		if slices.Contains(model.IdentityVetoExemptSourceIDs, r.SourceID) {
			continue
		}
		byKey[r.SourceID] = append(byKey[r.SourceID], r)
	}
	var hits []struct {
		ref   exactRef
		other string
	}
	for _, a := range s.RefsA {
		for _, b := range byKey[a.SourceID] {
			if a.ExternalID == b.ExternalID {
				continue
			}
			hits = append(hits, struct {
				ref   exactRef
				other string
			}{a, b.ExternalID})
		}
	}
	if len(hits) == 0 {
		return ""
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].ref.TrustTier != hits[j].ref.TrustTier {
			return hits[i].ref.TrustTier < hits[j].ref.TrustTier
		}
		if hits[i].ref.SourceID != hits[j].ref.SourceID {
			return hits[i].ref.SourceID < hits[j].ref.SourceID
		}
		return hits[i].ref.ExternalID < hits[j].ref.ExternalID
	})
	return fmt.Sprintf("%s %s vs %s", hits[0].ref.SourceKey, hits[0].ref.ExternalID, hits[0].other)
}

func survivorTarget(s workPairSides) (source, target int64) {
	switch {
	case s.ClaimedA && !s.ClaimedB:
		return s.BID, s.AID
	case s.ClaimedB && !s.ClaimedA:
		return s.AID, s.BID
	case s.ExactA > s.ExactB:
		return s.BID, s.AID
	case s.ExactB > s.ExactA:
		return s.AID, s.BID
	case s.AID < s.BID:
		return s.BID, s.AID
	default:
		return s.AID, s.BID
	}
}

type applyPlan struct {
	Action string
	Stamp  string
	Skip   string
	Source int64
	Target int64
	Reason string
}

// stamp is what lands in applied_action. It defaults to the action so the
// existing counters keep their names, and differs only where the row has to
// stay greppable after the fact: a mechanical reject, or a row retired without
// calling the service at all.
func (p applyPlan) stamp() string {
	if p.Stamp != "" {
		return p.Stamp
	}
	return p.Action
}

// recordOnly is a row whose decision needs no service call — nothing is left to
// decide. Stamping it is the point: the apply loop does not stamp a row that
// errored, so before this the 22 verdicts naming a work that a merge had
// already retired failed and retried every single night.
func (p applyPlan) recordOnly() bool { return p.Action == "" && p.Stamp != "" }

// confidence thresholds are split by direction because the two directions are
// not each other's mirror. An accept files a merge and MergeService.Unmerge has
// no route and no CLI in production; a reject only parks a pair, and the pair
// stays in catalog_match_candidate where it can be read back. Holding both to
// the accept bar is what left 811 judged-different pairs sitting in
// needs_manual on 2026-09-14, none of which the nightly lane could ever clear.
func planCreditName(verdict string, conf, minAccept, minReject float64) applyPlan {
	switch verdict {
	case VerdictSame:
		if conf < minAccept {
			return applyPlan{Skip: skipBelowConfidence}
		}
		return applyPlan{Action: applyAccept}
	case VerdictDifferent:
		if conf < minReject {
			return applyPlan{Skip: skipBelowConfidence}
		}
		return applyPlan{Action: applyReject}
	case VerdictUnsure:
		return applyPlan{Skip: skipUnsure}
	default:
		return applyPlan{Skip: skipUnknownVerdict}
	}
}

// A retired endpoint is deferred, not rejected: reject writes a
// catalog_match_rejection saying the two works are not the same, and a pair
// whose one side has been merged away supports no such claim. Stamping the
// verdict row without touching the candidate is what left 158 of these sitting
// in needs_manual after the 2026-09-14 sweep, counted as human work forever.
func planWorkPair(verdict string, conf, minAccept, minReject float64, s workPairSides) applyPlan {
	if s.DeletedA || s.DeletedB {
		return applyPlan{Action: applyDefer, Stamp: stampObsoletePair}
	}
	if c := contradictingExactRef(s); c != "" {
		return applyPlan{Action: applyReject, Stamp: stampRefConflict, Reason: "ref-conflict: " + c}
	}
	switch verdict {
	case VerdictDifferent:
		if conf < minReject {
			return applyPlan{Skip: skipBelowConfidence}
		}
		return applyPlan{Action: applyReject}
	case VerdictSame:
		if conf < minAccept {
			return applyPlan{Skip: skipBelowConfidence}
		}
		if bothClaimed(s) {
			return applyPlan{Skip: skipFrozenBothClaimed}
		}
		src, tgt := survivorTarget(s)
		return applyPlan{Action: applyAccept, Source: src, Target: tgt}
	case VerdictUnsure:
		return applyPlan{Skip: skipUnsure}
	default:
		return applyPlan{Skip: skipUnknownVerdict}
	}
}

// planRef splits confirm by whether another entity already holds the exact
// slot. A bundle release legitimately spans up to 16 works and only one of them
// can hold exact, so the other 15 are correct data with a correct verdict and
// no exact action — 10,684 of the 16,654 rows in the queue on 2026-09-14, every
// one chain-verified at confidence >= 0.90. Verifying them as related records
// the judgement without claiming the slot.
func planRef(verdict string, conf, min float64, slotTaken bool) applyPlan {
	switch verdict {
	case VerdictChainVerified, VerdictSame:
		if conf < min {
			return applyPlan{Skip: skipBelowConfidence}
		}
		if slotTaken {
			return applyPlan{Action: applyConfirmRelated}
		}
		return applyPlan{Action: applyConfirm}
	case VerdictDifferent:
		return applyPlan{Skip: skipRefDifferent}
	case VerdictChainUnproven:
		return applyPlan{Skip: skipChainUnproven}
	case VerdictUnsure:
		return applyPlan{Skip: skipUnsure}
	default:
		return applyPlan{Skip: skipUnknownVerdict}
	}
}
