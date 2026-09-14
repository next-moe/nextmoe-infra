package llmsuggest

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
)

func ref(source int16, key string, tier int16, ext string) exactRef {
	return exactRef{SourceID: source, SourceKey: key, TrustTier: tier, ExternalID: ext}
}

func TestRefConflictOverrulesTheVerdict(t *testing.T) {
	// works 8460 and 8501, judged same at 1.00 on a fabricated shared vndb id
	s := workPairSides{
		AID: 8460, BID: 8501,
		ClaimedA: true, ClaimedB: true,
		RefsA: []exactRef{ref(2, "vndb", 1, "v26356")},
		RefsB: []exactRef{ref(2, "vndb", 1, "v28983")},
	}
	p := planWorkPair(VerdictSame, 1, 0.9, 0.7, s)
	assert.Equal(t, applyReject, p.Action)
	assert.Equal(t, stampRefConflict, p.stamp())
	assert.Equal(t, "ref-conflict: vndb v26356 vs v28983", p.Reason)

	// the freeze is what used to hold this pair, and it no longer gets the chance
	assert.NotEqual(t, skipFrozenBothClaimed, p.Skip)

	for _, v := range []string{VerdictUnsure, VerdictDifferent} {
		p := planWorkPair(v, 0.1, 0.9, 0.7, s)
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
			assert.Equal(t, applyAccept, planWorkPair(VerdictSame, 1, 0.9, 0.7, s).Action)
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

func TestDeletedEndpointIsRecordedNotExecuted(t *testing.T) {
	s := workPairSides{AID: 1, BID: 2, DeletedB: true}
	p := planWorkPair(VerdictSame, 1, 0.9, 0.7, s)
	assert.True(t, p.recordOnly())
	assert.Empty(t, p.Action)
	assert.Equal(t, stampObsoletePair, p.stamp())

	// a contradicting pair whose endpoint is gone is still only recorded: there
	// is no candidate left to reject
	s.RefsA = []exactRef{ref(2, "vndb", 1, "v1")}
	s.RefsB = []exactRef{ref(2, "vndb", 1, "v2")}
	assert.True(t, planWorkPair(VerdictSame, 1, 0.9, 0.7, s).recordOnly())

	assert.False(t, planWorkPair(VerdictSame, 1, 0.9, 0.7, workPairSides{AID: 1, BID: 2}).recordOnly())
}

func TestRejectThresholdIsIndependentOfAccept(t *testing.T) {
	s := workPairSides{AID: 1, BID: 2}
	assert.Equal(t, applyReject, planWorkPair(VerdictDifferent, 0.8, 0.9, 0.7, s).Action)
	assert.Equal(t, skipBelowConfidence, planWorkPair(VerdictDifferent, 0.6, 0.9, 0.7, s).Skip)

	// an accept at the same confidence is still held to the accept bar
	assert.Equal(t, skipBelowConfidence, planWorkPair(VerdictSame, 0.8, 0.9, 0.7, s).Skip)

	assert.Equal(t, applyReject, planCreditName(VerdictDifferent, 0.8, 0.9, 0.7).Action)
	assert.Equal(t, skipBelowConfidence, planCreditName(VerdictSame, 0.8, 0.9, 0.7).Skip)
}

func TestRefWithTakenSlotIsVerifiedAsRelated(t *testing.T) {
	for _, v := range []string{VerdictChainVerified, VerdictSame} {
		assert.Equal(t, applyConfirmRelated, planRef(v, 1, 0.9, true).Action, v)
		assert.Equal(t, applyConfirm, planRef(v, 1, 0.9, false).Action, v)
	}
	// a taken slot does not lower the bar, and does not rescue a held verdict
	assert.Equal(t, skipBelowConfidence, planRef(VerdictChainVerified, 0.5, 0.9, true).Skip)
	assert.Equal(t, skipChainUnproven, planRef(VerdictChainUnproven, 1, 0.9, true).Skip)
	assert.Equal(t, skipRefDifferent, planRef(VerdictDifferent, 1, 0.9, true).Skip)
}
