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
	// A stamp missing from currentStamps is a row the next run re-judges;
	// the kept-apart stamps are the exception TestKeptApartStampsAreNotCurrent
	// names. One that is in the list but no rule writes is a row no later
	// rule can ever reach. Both of those were the 2026-09-14 failure, in
	// opposite directions.
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
	record(planRef(VerdictChainVerified, 1, 0.9, 0, false, refEvidence{}))
	record(planRef(VerdictChainVerified, 1, 0.9, 0, true, refEvidence{}))
	record(planRef(VerdictDifferent, 0.5, 0.9, 0.8, false, refEvidence{}))
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
	assert.Equal(t, stampKeptApartLowConfidence, planWorkPair(VerdictDifferent, 0.6, 0.7, s, soleName("sole")).stamp())

	// The accept direction has no bar left to be independent of: confidence
	// gates rejects only. An accept at 0.10 lands, and an accept at 1.00 whose
	// name a third work also answers to is kept apart rather than merged.
	assert.Equal(t, applyAccept, planWorkPair(VerdictSame, 0.1, 0.7, s, soleName("sole")).Action)
	assert.Equal(t, stampKeptApartUncorroborated,
		planWorkPair(VerdictSame, 1, 0.7, s, pairEvidence{Name: "memoria", Holders: 3}).stamp())

	assert.Equal(t, applyReject, planCreditName(VerdictDifferent, 0.8, 0.9, 0.7).Action)
	assert.Equal(t, skipBelowConfidence, planCreditName(VerdictSame, 0.8, 0.9, 0.7).Skip)
}

func TestRefWithTakenSlotIsVerifiedAsRelated(t *testing.T) {
	ev := refEvidence{Corroborator: "vndb v1"}
	for _, v := range []string{VerdictChainVerified, VerdictSame} {
		assert.Equal(t, applyConfirmRelated, planRef(v, 1, 0.9, 0, true, ev).Action, v)
		assert.Equal(t, applyConfirm, planRef(v, 1, 0.9, 0, false, ev).Action, v)
	}
	// a taken slot does not lower the bar, and does not rescue a held verdict
	assert.Equal(t, skipBelowConfidence, planRef(VerdictChainVerified, 0.5, 0.9, 0, true, ev).Skip)
	assert.Equal(t, skipChainUnproven, planRef(VerdictChainUnproven, 1, 0.9, 0, true, ev).Skip)
	assert.Equal(t, stampHeldProbableDisputed, planRef(VerdictDifferent, 1, 0.9, 0, true, ev).stamp())
}

// The gate is structural on purpose: by ref-v2 the confidence number had
// stopped separating anything, 650 of 796 same verdicts landing on exactly
// 1.00. A confirm at 1.00 with nothing corroborating it is held; a chain
// verdict carries its own upstream join and is not held.
func TestSameRefNeedsACorroborator(t *testing.T) {
	assert.Equal(t, skipUncorroborated, planRef(VerdictSame, 1, 0.9, 0, false, refEvidence{}).Skip)
	assert.Equal(t, skipNoCorroborator, planRef(VerdictSame, 1, 0.9, 0, false, refEvidence{Unavailable: true}).Skip)
	assert.Equal(t, applyConfirm, planRef(VerdictSame, 1, 0.9, 0, false, refEvidence{Corroborator: "vndb v1"}).Action)
	assert.Equal(t, applyConfirm, planRef(VerdictChainVerified, 1, 0.9, 0, false, refEvidence{}).Action)
	assert.Equal(t, applyConfirmRelated, planRef(VerdictRelated, 0, 0.9, 0, false, refEvidence{}).Action)
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

	// Different below the reject bar is stamped held, so the ref lane loads
	// every verdict the rules can decide, including those under the confirm bar.
	opts.Queue = QueueRef
	minConf, _ = applySelection(opts)
	assert.Zero(t, minConf)
}

func exclusiveLoose(s string) pairEvidence {
	return pairEvidence{LooseName: s, LooseHolders: exclusiveNameHolders}
}

func exclusiveShared(key, ext string) pairEvidence {
	return pairEvidence{SharedSourceKey: key, SharedExternalID: ext, SharedHolders: exclusiveNameHolders}
}

func TestSameAcceptsOnLooseName(t *testing.T) {
	s := workPairSides{AID: 1, BID: 2}
	p := planWorkPair(VerdictSame, 1, 0.7, s, exclusiveLoose("abcdefgh"))
	assert.Equal(t, applyAccept, p.Action)
	assert.Equal(t, `sole holders of loose "abcdefgh"`, p.Reason)

	all := pairEvidence{
		Name: "folded", Holders: exclusiveNameHolders,
		LooseName: "abcdefgh", LooseHolders: exclusiveNameHolders,
		SharedSourceKey: "vndb", SharedExternalID: "v1", SharedHolders: exclusiveNameHolders,
	}
	assert.Equal(t, `sole holders of "folded"`, planWorkPair(VerdictSame, 1, 0.7, s, all).Reason)
	foldedAndLoose := pairEvidence{Name: "folded", Holders: exclusiveNameHolders, LooseName: "abcdefgh", LooseHolders: exclusiveNameHolders}
	sharedAndLoose := pairEvidence{
		LooseName: "abcdefgh", LooseHolders: exclusiveNameHolders,
		SharedSourceKey: "vndb", SharedExternalID: "v1", SharedHolders: exclusiveNameHolders,
	}
	assert.Equal(t, `sole holders of "folded"`, planWorkPair(VerdictSame, 1, 0.7, s, foldedAndLoose).Reason)
	assert.Equal(t, "shares vndb:v1", planWorkPair(VerdictSame, 1, 0.7, s, sharedAndLoose).Reason)
}

func TestUnsureDoesNotAcceptOnLooseName(t *testing.T) {
	s := workPairSides{AID: 1, BID: 2}
	p := planWorkPair(VerdictUnsure, 0, 0.7, s, exclusiveLoose("abcdefgh"))
	assert.Equal(t, applyDefer, p.Action)
	assert.Equal(t, stampKeptApartUncorroborated, p.stamp())
	assert.Equal(t, applyAccept, planWorkPair(VerdictUnsure, 0, 0.7, s, soleName("folded")).Action)
}

func TestLooseNameNeedsFiveRunes(t *testing.T) {
	s := workPairSides{AID: 1, BID: 2}
	p := planWorkPair(VerdictSame, 1, 0.7, s, exclusiveLoose("mine"))
	assert.Equal(t, applyDefer, p.Action)
	assert.Equal(t, stampKeptApartUncorroborated, p.stamp())
	assert.Equal(t, applyAccept, planWorkPair(VerdictSame, 1, 0.7, s, exclusiveLoose("puppy")).Action)
}

func TestLooseNameNeedsExactlyTwoHolders(t *testing.T) {
	s := workPairSides{AID: 1, BID: 2}
	p := planWorkPair(VerdictSame, 1, 0.7, s, pairEvidence{LooseName: "abcdefgh", LooseHolders: 3})
	assert.Equal(t, applyDefer, p.Action)
	assert.Equal(t, stampKeptApartUncorroborated, p.stamp())
}

func TestSameAndUnsureAcceptOnSharedRecord(t *testing.T) {
	s := workPairSides{AID: 1, BID: 2}
	ev := exclusiveShared("vndb", "v1")
	assert.Equal(t, applyAccept, planWorkPair(VerdictSame, 1, 0.7, s, ev).Action)
	assert.Equal(t, "shares vndb:v1", planWorkPair(VerdictSame, 1, 0.7, s, ev).Reason)
	assert.Equal(t, applyAccept, planWorkPair(VerdictUnsure, 0, 0.7, s, ev).Action)
	assert.Equal(t, "shares vndb:v1", planWorkPair(VerdictUnsure, 0, 0.7, s, ev).Reason)
}

func TestBundleRecordNeverCorroborates(t *testing.T) {
	multi := workIdentityRef{SourceID: 5, SourceKey: "erogamescape", ExternalID: "100",
		LinkKind: model.LinkKindRelated, MatchedBy: matchedByEGXlinkMulti}
	holders := map[identityRefKey]int{{SourceID: 5, ExternalID: "100"}: exclusiveNameHolders}
	sk, ext, n := pickSharedRecord([]workIdentityRef{multi}, []workIdentityRef{multi}, holders)
	assert.Empty(t, sk)
	assert.Empty(t, ext)
	assert.Zero(t, n)

	s := workPairSides{AID: 1, BID: 2}
	p := planWorkPair(VerdictSame, 1, 0.7, s, pairEvidence{})
	assert.Equal(t, stampKeptApartUncorroborated, p.stamp())
}

func TestRelatedRefOtherThanTwinNeverCorroborates(t *testing.T) {
	other := workIdentityRef{SourceID: 5, SourceKey: "erogamescape", ExternalID: "100",
		LinkKind: model.LinkKindRelated, MatchedBy: "rule:eg-dmm"}
	holders := map[identityRefKey]int{{SourceID: 5, ExternalID: "100"}: exclusiveNameHolders}
	sk, _, _ := pickSharedRecord([]workIdentityRef{other}, []workIdentityRef{other}, holders)
	assert.Empty(t, sk)
}

func TestSharedRecordNeedsExactlyTwoHolders(t *testing.T) {
	twin := workIdentityRef{SourceID: 5, SourceKey: "erogamescape", ExternalID: "100",
		LinkKind: model.LinkKindRelated, MatchedBy: matchedByEGXlinkTwin}
	exact := workIdentityRef{SourceID: 5, SourceKey: "erogamescape", ExternalID: "100",
		LinkKind: model.LinkKindExact, MatchedBy: "test"}
	holders := map[identityRefKey]int{{SourceID: 5, ExternalID: "100"}: 3}
	sk, _, _ := pickSharedRecord([]workIdentityRef{exact}, []workIdentityRef{twin}, holders)
	assert.Empty(t, sk)

	s := workPairSides{AID: 1, BID: 2}
	p := planWorkPair(VerdictSame, 1, 0.7, s, pairEvidence{
		SharedSourceKey: "erogamescape", SharedExternalID: "100", SharedHolders: 3,
	})
	assert.Equal(t, stampKeptApartUncorroborated, p.stamp())
}

func TestRefConflictStillWinsOverNewCorroborators(t *testing.T) {
	s := workPairSides{
		AID: 1, BID: 2,
		RefsA: []exactRef{ref(2, "vndb", 1, "v1")},
		RefsB: []exactRef{ref(2, "vndb", 1, "v2")},
	}
	ev := pairEvidence{
		Name: "folded", Holders: exclusiveNameHolders,
		LooseName: "abcdefgh", LooseHolders: exclusiveNameHolders,
		SharedSourceKey: "erogamescape", SharedExternalID: "100", SharedHolders: exclusiveNameHolders,
	}
	p := planWorkPair(VerdictSame, 1, 0.7, s, ev)
	assert.Equal(t, applyReject, p.Action)
	assert.Equal(t, stampRefConflict, p.stamp())
}

func TestUncorroboratedIsKeptApart(t *testing.T) {
	s := workPairSides{AID: 1, BID: 2}
	p := planWorkPair(VerdictSame, 1, 0.7, s, pairEvidence{})
	assert.Equal(t, applyDefer, p.Action)
	assert.Equal(t, stampKeptApartUncorroborated, p.stamp())
	assert.False(t, p.recordOnly())
}

func TestBothClaimedIsKeptApart(t *testing.T) {
	s := workPairSides{AID: 1, BID: 2, ClaimedA: true, ClaimedB: true}
	p := planWorkPair(VerdictSame, 1, 0.7, s, soleName("folded"))
	assert.Equal(t, applyDefer, p.Action)
	assert.Equal(t, stampKeptApartBothClaimed, p.stamp())
	assert.NotEqual(t, applyAccept, p.Action)
}

func TestLowConfidenceDifferentIsKeptApart(t *testing.T) {
	s := workPairSides{AID: 1, BID: 2}
	p := planWorkPair(VerdictDifferent, 0.6, 0.7, s, soleName("folded"))
	assert.Equal(t, applyDefer, p.Action)
	assert.Equal(t, stampKeptApartLowConfidence, p.stamp())
}

func TestKeptApartStampsAreNotCurrent(t *testing.T) {
	for _, stamp := range []string{
		stampKeptApartBothClaimed, stampKeptApartUncorroborated, stampKeptApartLowConfidence,
	} {
		assert.NotContains(t, currentStamps, stamp)
	}
}

func TestHeldProbableDisputedIsCurrent(t *testing.T) {
	assert.Contains(t, currentStamps, stampHeldProbableDisputed)
}

func TestRefDifferentAboveBarRejects(t *testing.T) {
	p := planRef(VerdictDifferent, 0.85, 0.9, 0.8, false, refEvidence{})
	assert.Equal(t, applyReject, p.Action)
	assert.Equal(t, applyReject, p.stamp())
}

func TestRefDifferentBelowBarIsHeld(t *testing.T) {
	p := planRef(VerdictDifferent, 0.5, 0.9, 0.8, false, refEvidence{})
	assert.Equal(t, stampHeldProbableDisputed, p.stamp())
	assert.True(t, p.recordOnly())
	assert.NotEqual(t, applyReject, p.Action)
}

func TestRefApplyWithoutRejectFlagNeverRejects(t *testing.T) {
	p := planFor(QueueRef, QueueVerdict{Verdict: VerdictDifferent, Confidence: 1},
		nil, Options{MinConfidence: 0.9}, false, applyEvidence{})
	assert.NotEqual(t, applyReject, p.Action)
	assert.Equal(t, stampHeldProbableDisputed, p.stamp())
	assert.True(t, p.recordOnly())
}
