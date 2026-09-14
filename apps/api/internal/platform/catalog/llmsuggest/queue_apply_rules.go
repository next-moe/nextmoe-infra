package llmsuggest

const (
	applyAccept  = "accept"
	applyReject  = "reject"
	applyConfirm = "confirm"

	skipUnsure            = "skipped_unsure"
	skipBelowConfidence   = "skipped_below_confidence"
	skipFrozenBothClaimed = "frozen_both_claimed"
	skipRefDifferent      = "skipped_different_held_for_human"
	skipChainUnproven     = "skipped_chain_unproven"
	skipGoldQueue         = "skipped_gold_queue"
	skipUnknownVerdict    = "skipped_unknown_verdict"
	skipRefExactTaken     = "skipped_exact_slot_taken"

	errExactTaken = "error_exact_taken"
	errState      = "error_state"
	errNotFound   = "error_not_found"
	errOther      = "error_other"
)

type workPairSides struct {
	AID, BID           int64
	ClaimedA, ClaimedB bool
	ExactA, ExactB     int
}

func bothClaimed(s workPairSides) bool { return s.ClaimedA && s.ClaimedB }

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
	Skip   string
	Source int64
	Target int64
}

func planCreditName(verdict string, conf, min float64) applyPlan {
	switch verdict {
	case VerdictSame:
		if conf < min {
			return applyPlan{Skip: skipBelowConfidence}
		}
		return applyPlan{Action: applyAccept}
	case VerdictDifferent:
		if conf < min {
			return applyPlan{Skip: skipBelowConfidence}
		}
		return applyPlan{Action: applyReject}
	case VerdictUnsure:
		return applyPlan{Skip: skipUnsure}
	default:
		return applyPlan{Skip: skipUnknownVerdict}
	}
}

func planWorkPair(verdict string, conf, min float64, s workPairSides) applyPlan {
	switch verdict {
	case VerdictDifferent:
		if conf < min {
			return applyPlan{Skip: skipBelowConfidence}
		}
		return applyPlan{Action: applyReject}
	case VerdictSame:
		if conf < min {
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

func planRef(verdict string, conf, min float64) applyPlan {
	switch verdict {
	case VerdictChainVerified:
		if conf < min {
			return applyPlan{Skip: skipBelowConfidence}
		}
		return applyPlan{Action: applyConfirm}
	case VerdictSame:
		if conf < min {
			return applyPlan{Skip: skipBelowConfidence}
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
