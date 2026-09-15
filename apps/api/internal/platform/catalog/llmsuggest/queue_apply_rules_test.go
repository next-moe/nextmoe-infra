package llmsuggest

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
)

func ref(source int16, key string, tier int16, ext string) exactRef {
	return exactRef{SourceID: source, SourceKey: key, TrustTier: tier, ExternalID: ext}
}

func soleName(s string) pairEvidence { return pairEvidence{Name: s, Holders: exclusiveNameHolders} }

func TestRefConflictOverrulesTheVerdict(t *testing.T) {
	// works 8460 and 8501, judged same at 1.00 on a fabricated shared vndb id
	s := workPairSides{
		AID: 8460, BID: 8501,
		ClaimedA: true, ClaimedB: true,
		RefsA: []exactRef{ref(2, "vndb", 1, "v26356")},
		RefsB: []exactRef{ref(2, "vndb", 1, "v28983")},
	}
	p := planWorkPair(VerdictSame, 1, 0.7, s, soleName("sole"))
	assert.Equal(t, applyReject, p.Action)
	assert.Equal(t, stampRefConflict, p.stamp())
	assert.Equal(t, "ref-conflict: vndb v26356 vs v28983", p.Reason)

	// the freeze is what used to hold this pair, and it no longer gets the chance
	assert.NotEqual(t, skipFrozenBothClaimed, p.Skip)

	for _, v := range []string{VerdictUnsure, VerdictDifferent} {
		p := planWorkPair(v, 0.1, 0.7, s, soleName("sole"))
		assert.Equal(t, applyReject, p.Action, v)
	}
}

func TestExemptSourcesDoNotVeto(t *testing.T) {
	for _, c := range []struct {
		name   string
		source int16
		key    string
	}{
		{"first-party curated", model.SourceCurated, "curated"},
		{"does not dedupe itself", model.SourceHowLongToBeat, "howlongtobeat"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := workPairSides{
				AID: 1, BID: 2,
				RefsA: []exactRef{ref(c.source, c.key, 1, "100")},
				RefsB: []exactRef{ref(c.source, c.key, 1, "200")},
			}
			assert.Empty(t, contradictingExactRef(s))
			assert.Equal(t, applyAccept, planWorkPair(VerdictSame, 1, 0.7, s, soleName("sole")).Action)
		})
	}

	// positive control: the same shape under a source nobody exempts does veto,
	// so the two empty results above are the exemption and not a broken compare
	s := workPairSides{
		AID: 1, BID: 2,
		RefsA: []exactRef{ref(3, "bangumi", 1, "100")},
		RefsB: []exactRef{ref(3, "bangumi", 1, "200")},
	}
	assert.Equal(t, "bangumi 100 vs 200", contradictingExactRef(s))
}

func TestContradictionNamesTheMostTrustedSource(t *testing.T) {
	s := workPairSides{
		AID: 1, BID: 2,
		RefsA: []exactRef{ref(15, "dmm", 2, "a1"), ref(2, "vndb", 1, "v1")},
		RefsB: []exactRef{ref(15, "dmm", 2, "a2"), ref(2, "vndb", 1, "v2")},
	}
	assert.Equal(t, "vndb v1 vs v2", contradictingExactRef(s))
}

func TestAgreeingRefsAreNotAContradiction(t *testing.T) {
	s := workPairSides{
		AID: 1, BID: 2,
		RefsA: []exactRef{ref(2, "vndb", 1, "v1")},
		RefsB: []exactRef{ref(3, "bangumi", 1, "b1")},
	}
	assert.Empty(t, contradictingExactRef(s))
	assert.Empty(t, contradictingExactRef(workPairSides{AID: 1, BID: 2}))
}

func TestDeletedEndpointDefersTheCandidate(t *testing.T) {
	s := workPairSides{AID: 1, BID: 2, DeletedB: true}
	p := planWorkPair(VerdictSame, 1, 0.7, s, soleName("sole"))
	assert.Equal(t, applyDefer, p.Action, "the candidate has to leave needs_manual")
	assert.NotEqual(t, applyReject, p.Action, "a retired endpoint is no evidence the works differ")
	assert.Equal(t, stampObsoletePair, p.stamp())
	assert.False(t, p.recordOnly())

	// a contradicting pair whose endpoint is gone is still deferred, not
	// rejected: the pair it names no longer exists to be judged
	s.RefsA = []exactRef{ref(2, "vndb", 1, "v1")}
	s.RefsB = []exactRef{ref(2, "vndb", 1, "v2")}
	assert.Equal(t, applyDefer, planWorkPair(VerdictSame, 1, 0.7, s, soleName("sole")).Action)

	assert.Equal(t, applyAccept, planWorkPair(VerdictSame, 1, 0.7, workPairSides{AID: 1, BID: 2}, soleName("sole")).Action)
}

func TestEveryStampThisPackageWritesIsAKnownStamp(t *testing.T) {
	// A stamp missing from currentStamps is a row the next run re-judges
	// forever; one that is there but no rule writes is a row no later rule can
	// ever reach. Both are the 2026-09-14 failure, in opposite directions.
	written := map[string]bool{}
	record := func(p applyPlan) {
		if p.Skip == "" {
			written[p.stamp()] = true
		}
	}
	s := workPairSides{AID: 1, BID: 2}
	record(planWorkPair(VerdictSame, 1, 0.7, s, soleName("sole")))
	record(planWorkPair(VerdictDifferent, 1, 0.7, s, soleName("sole")))
	record(planWorkPair(VerdictSame, 1, 0.7, workPairSides{AID: 1, BID: 2, DeletedB: true}, soleName("sole")))
	record(planWorkPair(VerdictSame, 1, 0.7, workPairSides{
		AID: 1, BID: 2,
		RefsA: []exactRef{ref(2, "vndb", 1, "v1")},
		RefsB: []exactRef{ref(2, "vndb", 1, "v2")},
	}, soleName("sole")))
	record(planCreditName(VerdictSame, 1, 0.9, 0.7))
	record(planRef(VerdictChainVerified, 1, 0.9, false, refEvidence{}))
	record(planRef(VerdictChainVerified, 1, 0.9, true, refEvidence{}))
	written[stampTargetGone] = true // written by the apply loop, not by a plan

	for stamp := range written {
		assert.Contains(t, currentStamps, stamp)
	}
	for _, stamp := range currentStamps {
		assert.True(t, written[stamp], "no rule writes %q", stamp)
	}
}

func TestRejectThresholdIsIndependentOfAccept(t *testing.T) {
	s := workPairSides{AID: 1, BID: 2}
	assert.Equal(t, applyReject, planWorkPair(VerdictDifferent, 0.8, 0.7, s, soleName("sole")).Action)
	assert.Equal(t, skipBelowConfidence, planWorkPair(VerdictDifferent, 0.6, 0.7, s, soleName("sole")).Skip)

	// The accept direction has no bar left to be independent of: confidence
	// gates rejects only. An accept at 0.10 lands, and an accept at 1.00 whose
	// name a third work also answers to does not.
	assert.Equal(t, applyAccept, planWorkPair(VerdictSame, 0.1, 0.7, s, soleName("sole")).Action)
	assert.Equal(t, skipUncorroborated,
		planWorkPair(VerdictSame, 1, 0.7, s, pairEvidence{Name: "memoria", Holders: 3}).Skip)

	assert.Equal(t, applyReject, planCreditName(VerdictDifferent, 0.8, 0.9, 0.7).Action)
	assert.Equal(t, skipBelowConfidence, planCreditName(VerdictSame, 0.8, 0.9, 0.7).Skip)
}

func TestRefWithTakenSlotIsVerifiedAsRelated(t *testing.T) {
	ev := refEvidence{Corroborator: "vndb v1"}
	for _, v := range []string{VerdictChainVerified, VerdictSame} {
		assert.Equal(t, applyConfirmRelated, planRef(v, 1, 0.9, true, ev).Action, v)
		assert.Equal(t, applyConfirm, planRef(v, 1, 0.9, false, ev).Action, v)
	}
	// a taken slot does not lower the bar, and does not rescue a held verdict
	assert.Equal(t, skipBelowConfidence, planRef(VerdictChainVerified, 0.5, 0.9, true, ev).Skip)
	assert.Equal(t, skipChainUnproven, planRef(VerdictChainUnproven, 1, 0.9, true, ev).Skip)
	assert.Equal(t, skipRefDifferent, planRef(VerdictDifferent, 1, 0.9, true, ev).Skip)
}

// The gate is structural on purpose: by ref-v2 the confidence number had
// stopped separating anything, 650 of 796 same verdicts landing on exactly
// 1.00. A confirm at 1.00 with nothing corroborating it is held; a chain
// verdict carries its own upstream join and is not held.
func TestSameRefNeedsACorroborator(t *testing.T) {
	assert.Equal(t, skipUncorroborated, planRef(VerdictSame, 1, 0.9, false, refEvidence{}).Skip)
	assert.Equal(t, skipNoCorroborator, planRef(VerdictSame, 1, 0.9, false, refEvidence{Unavailable: true}).Skip)
	assert.Equal(t, applyConfirm, planRef(VerdictSame, 1, 0.9, false, refEvidence{Corroborator: "vndb v1"}).Action)
	assert.Equal(t, applyConfirm, planRef(VerdictChainVerified, 1, 0.9, false, refEvidence{}).Action)
	assert.Equal(t, applyConfirmRelated, planRef(VerdictRelated, 0, 0.9, false, refEvidence{}).Action)
}

func TestApplySelectionIsWiderThanEitherBar(t *testing.T) {
	opts := Options{MinConfidence: 0.9, MinConfidenceReject: 0.7}

	opts.Queue = QueueWorkPair
	minConf, verdicts := applySelection(opts)
	assert.Zero(t, minConf, "the contradiction screen does not read confidence")
	assert.Contains(t, verdicts, VerdictUnsure)

	opts.Queue = QueueCreditName
	minConf, verdicts = applySelection(opts)
	assert.Equal(t, 0.7, minConf)
	assert.NotContains(t, verdicts, VerdictUnsure)

	// planRef has no reject path, so the reject bar must not widen the ref lane
	opts.Queue = QueueRef
	minConf, _ = applySelection(opts)
	assert.Equal(t, 0.9, minConf)
}
