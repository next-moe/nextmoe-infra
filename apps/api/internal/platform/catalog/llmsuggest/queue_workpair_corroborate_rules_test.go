package llmsuggest

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStrippedAcceptsSameOnly(t *testing.T) {
	s := workPairSides{AID: 1, BID: 2}
	ev := pairEvidence{StrippedName: "abcde", StrippedHolders: exclusiveNameHolders}
	p := planWorkPair(VerdictSame, 1, 0.7, s, ev)
	assert.Equal(t, applyAccept, p.Action)
	assert.Equal(t, `sole holders of stripped "abcde"`, p.Reason)

	p = planWorkPair(VerdictUnsure, 0, 0.7, s, ev)
	assert.Equal(t, applyDefer, p.Action)
	assert.Equal(t, stampKeptApartUncorroborated, p.stamp())
}

func TestAcceptReasonPreferenceOrder(t *testing.T) {
	s := workPairSides{AID: 1, BID: 2}
	ev := pairEvidence{
		Name: "folded", Holders: exclusiveNameHolders,
		SharedSourceKey: "vndb", SharedExternalID: "v1", SharedHolders: exclusiveNameHolders,
		DeclaredSubject: "100", DeclaredWorkno: "RJ012345",
		LooseName: "abcdefgh", LooseHolders: exclusiveNameHolders,
		StrippedName: "strippedkey", StrippedHolders: exclusiveNameHolders,
	}
	assert.Equal(t, `sole holders of "folded"`, planWorkPair(VerdictSame, 1, 0.7, s, ev).Reason)
	ev.Name, ev.Holders = "", 0
	assert.Equal(t, "shares vndb:v1", planWorkPair(VerdictSame, 1, 0.7, s, ev).Reason)
	ev.SharedSourceKey, ev.SharedExternalID, ev.SharedHolders = "", "", 0
	assert.Equal(t, "bangumi 100 declares dlsite RJ012345", planWorkPair(VerdictSame, 1, 0.7, s, ev).Reason)
	ev.DeclaredSubject, ev.DeclaredWorkno = "", ""
	assert.Equal(t, `sole holders of loose "abcdefgh"`, planWorkPair(VerdictSame, 1, 0.7, s, ev).Reason)
	ev.LooseName, ev.LooseHolders = "", 0
	assert.Equal(t, `sole holders of stripped "strippedkey"`, planWorkPair(VerdictSame, 1, 0.7, s, ev).Reason)
	ev.StrippedName, ev.StrippedHolders = "", 0
	assert.Equal(t, stampKeptApartUncorroborated, planWorkPair(VerdictSame, 1, 0.7, s, ev).stamp())
}

func TestStrippedKeyIgnoresRemakeAndVolumePhrases(t *testing.T) {
	holders := map[string]map[int64]struct{}{
		"abcde": {1: {}, 2: {}},
	}
	assert.Empty(t, StrippedKey("ABCDE リマスター", "ABCDE", holders))
	assert.Empty(t, StrippedKey("ABCDE 前編", "ABCDE 後編", holders))
	assert.Equal(t, "abcde", StrippedKey("【スマホ版】ABCDE", "ABCDE", holders))
}
