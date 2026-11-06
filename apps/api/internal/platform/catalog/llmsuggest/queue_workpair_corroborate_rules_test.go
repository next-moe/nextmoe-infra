package llmsuggest

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStrippedAcceptsSameOnly(t *testing.T) {
	s := workPairSides{AID: 1, BID: 2}
	ev := pairEvidence{StrippedName: "abcde", StrippedHolders: exclusiveNameHolders}
	p := planWorkPair(VerdictSame, 1, 0.9, 0.7, s, ev)
	assert.Equal(t, applyAccept, p.Action)
	assert.Equal(t, `sole holders of stripped "abcde"`, p.Reason)

	p = planWorkPair(VerdictUnsure, 0, 0.9, 0.7, s, ev)
	assert.Equal(t, applyDefer, p.Action)
	assert.Equal(t, stampKeptApartUncorroborated, p.stamp())
}

func TestOnlyARegistryRecordDecidesAnUnsureVerdict(t *testing.T) {
	s := workPairSides{AID: 1, BID: 2}
	for _, key := range registryRecordKeys {
		ev := pairEvidence{SharedSourceKey: key, SharedExternalID: "1", SharedHolders: exclusiveNameHolders}
		assert.Equal(t, applyAccept, planWorkPair(VerdictUnsure, 0, 0.9, 0.7, s, ev).Action, key)
	}
	for _, ev := range []pairEvidence{
		{SharedSourceKey: "dlsite", SharedExternalID: "RJ1", SharedHolders: exclusiveNameHolders},
		{Name: "dollhouse", Holders: exclusiveNameHolders},
	} {
		p := planWorkPair(VerdictUnsure, 0.6, 0.9, 0.7, s, ev)
		assert.Equal(t, stampKeptApartUncorroborated, p.stamp(), "%+v", ev)
	}
}

// 28775 Doll House (VNDB, 2020) and 263347 Dollhouse (Bangumi, 2026) were the
// sole holders of the folded name "dollhouse"; the judge said unsure at 0.2.
func TestANameNeedsASureVerdictAndAgreeingYears(t *testing.T) {
	s := workPairSides{AID: 28775, BID: 263347}
	ev := pairEvidence{Name: "dollhouse", Holders: exclusiveNameHolders}
	assert.Equal(t, applyAccept, planWorkPair(VerdictSame, 0.9, 0.9, 0.7, s, ev).Action)
	assert.Equal(t, stampHeldSameUncorroborated, planWorkPair(VerdictSame, 0.7, 0.9, 0.7, s, ev).stamp(), "below the accept bar")
	ev.YearsClash = true
	assert.Equal(t, stampHeldSameUncorroborated, planWorkPair(VerdictSame, 1, 0.9, 0.7, s, ev).stamp(), "years disagree")
}

func TestAcceptReasonPreferenceOrder(t *testing.T) {
	s := workPairSides{AID: 1, BID: 2}
	ev := pairEvidence{
		Name: "folded", Holders: exclusiveNameHolders,
		SharedSourceKey: "vndb", SharedExternalID: "v1", SharedHolders: exclusiveNameHolders,
		LooseName: "abcdefgh", LooseHolders: exclusiveNameHolders,
		StrippedName: "strippedkey", StrippedHolders: exclusiveNameHolders,
		DatesNear: true,
		TitleName: "titlekey", TitleHolders: exclusiveNameHolders,
		StoreSourceKey: "dmm", StoreID: "abc_0001", StoreHolders: exclusiveNameHolders,
	}
	assert.Equal(t, "shares vndb:v1", planWorkPair(VerdictSame, 1, 0.9, 0.7, s, ev).Reason)
	ev.SharedSourceKey = "dlsite"
	assert.Equal(t, "shares dlsite:v1", planWorkPair(VerdictSame, 1, 0.9, 0.7, s, ev).Reason)
	ev.SharedSourceKey, ev.SharedExternalID, ev.SharedHolders = "", "", 0
	assert.Equal(t, `sole holders of "folded"`, planWorkPair(VerdictSame, 1, 0.9, 0.7, s, ev).Reason)
	ev.Name, ev.Holders = "", 0
	assert.Equal(t, `sole holders of loose "abcdefgh"`, planWorkPair(VerdictSame, 1, 0.9, 0.7, s, ev).Reason)
	ev.LooseName, ev.LooseHolders = "", 0
	assert.Equal(t, `sole holders of stripped "strippedkey"`, planWorkPair(VerdictSame, 1, 0.9, 0.7, s, ev).Reason)
	ev.StrippedName, ev.StrippedHolders = "", 0
	assert.Equal(t, `sole holders of title "titlekey", release dates agree`, planWorkPair(VerdictSame, 1, 0.9, 0.7, s, ev).Reason)
	ev.TitleName, ev.TitleHolders = "", 0
	assert.Equal(t, "release and work share dmm:abc_0001, release dates agree", planWorkPair(VerdictSame, 1, 0.9, 0.7, s, ev).Reason)
	ev.StoreSourceKey, ev.StoreID, ev.StoreHolders = "", "", 0
	assert.Equal(t, stampHeldSameUncorroborated, planWorkPair(VerdictSame, 1, 0.9, 0.7, s, ev).stamp())
}

func TestStrippedKeyIgnoresRemakeAndVolumePhrases(t *testing.T) {
	holders := map[string]map[int64]struct{}{
		"abcde": {1: {}, 2: {}},
	}
	assert.Empty(t, StrippedKey("ABCDE リマスター", "ABCDE", holders))
	assert.Empty(t, StrippedKey("ABCDE 前編", "ABCDE 後編", holders))
	assert.Equal(t, "abcde", StrippedKey("【スマホ版】ABCDE", "ABCDE", holders))
}
