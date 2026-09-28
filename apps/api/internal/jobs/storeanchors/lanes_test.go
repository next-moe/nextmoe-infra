package storeanchors

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizers(t *testing.T) {
	cases := []struct {
		name string
		lane string
		in   string
		want string
	}{
		{"steam appid", LaneSteam, "2258770", "2258770"},
		{"steam appid padded", LaneSteam, "  3987860 ", "3987860"},
		{"steam url rejected", LaneSteam, "store.steampowered.com/app/2258770/", ""},
		{"steam empty", LaneSteam, "", ""},

		{"dmm mono pcgame", LaneDmm, "www.dmm.co.jp/mono/pcgame/-/detail/=/cid=2212apc13900/", "2212apc13900"},
		{"dmm dc doujin", LaneDmm, "www.dmm.co.jp/dc/doujin/-/detail/=/cid=d_022265/", "d_022265"},
		{"dmm digital shop param", LaneDmm, "www.dmm.co.jp/digital/doujin/-/detail/=/shop=doujin/cid=d_123/", "d_123"},
		{"dmm en prefix", LaneDmm, "www.dmm.co.jp/en/dc/doujin/-/detail/=/cid=d_999/", "d_999"},
		{"dmm dlsoft", LaneDmm, "dlsoft.dmm.co.jp/detail/vsat_0145/", "vsat_0145"},
		{"dmm dlsoft en", LaneDmm, "dlsoft.dmm.co.jp/en/detail/awc_0006/", "awc_0006"},
		{"dmm dlsoft com", LaneDmm, "dlsoft.dmm.com/detail/next_0031/", "next_0031"},
		{"dmm original landing", LaneDmm, "dlsoft.dmm.co.jp/original/yuho/", ""},
		{"dmm feature", LaneDmm, "dlsoft.dmm.com/feature/dmmgames/iwaihime/", ""},
		{"dmm serial", LaneDmm, "dlsoft.dmm.com/serial/views_0721/", ""},
		{"dmm maker list", LaneDmm, "www.dmm.co.jp/dc/doujin/-/list/=/article=maker/id=25344/", ""},
		{"dmm index page", LaneDmm, "www.dmm.com/dc/pcgame/galaxy_angel/index_html/=/ch_navi=/", ""},

		{"dlsite RJ", LaneDlsite, "RJ264149", "RJ264149"},
		{"dlsite VJ", LaneDlsite, "VJ010897", "VJ010897"},
		{"dlsite RE not in JP lane", LaneDlsite, "RE123456", ""},
		{"dlsite url rejected", LaneDlsite, "www.dlsite.com/maniax/work/=/product_id/RJ264149.html", ""},

		{"dlsite EN RE", LaneDlsiteEN, "RE245678", "RE245678"},
		{"dlsite EN rejects RJ", LaneDlsiteEN, "RJ264149", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, laneByName(t, tc.lane).normalize(tc.in))
		})
	}
}

func laneByName(t *testing.T, name string) lane {
	t.Helper()
	ls, err := selectedLanes(name)
	require.NoError(t, err)
	require.Len(t, ls, 1)
	return ls[0]
}

func TestSelectedLanes(t *testing.T) {
	all, err := selectedLanes("")
	require.NoError(t, err)
	assert.Len(t, all, 4, "every lane runs when --only is empty")

	_, err = selectedLanes("getchu")
	assert.Error(t, err, "getchu belongs to the wave-167 importer, not to this one")
}

func TestDecideFilters(t *testing.T) {
	l := laneByName(t, LaneSteam)
	cands := []candidate{
		{ReleaseID: 1, WorkID: 10, RawValue: "111"},
		{ReleaseID: 2, WorkID: 20, RawValue: "store"},
		{ReleaseID: 3, WorkID: 30, RawValue: "333"},
		{ReleaseID: 4, WorkID: 40, RawValue: "444"},
		{ReleaseID: 5, WorkID: 50, RawValue: "555"},
		{ReleaseID: 6, WorkID: 60, RawValue: "555"},
		{ReleaseID: 7, WorkID: 70, RawValue: "777"},
		{ReleaseID: 7, WorkID: 70, RawValue: "777"},
	}
	taken := takenExact{release: map[string]struct{}{"444": {}}, work: map[string]int64{}}
	rejected := map[string]struct{}{rejKey(model.EntityTypeRelease, 3, "333"): {}}

	ls := &LaneStats{Candidates: len(cands)}
	plan := decide(cands, l, taken, rejected, ls)

	assert.Equal(t, 1, ls.SkippedMalformed)
	assert.Equal(t, 1, ls.SkippedRejection)
	assert.Equal(t, 1, ls.SkippedValueTaken)
	assert.Equal(t, 2, ls.SkippedAmbiguous, "BOTH holders are skipped — no arbitrary winner")
	assert.Equal(t, 1, ls.SkippedDedup)
	require.Len(t, plan, 2)
	assert.Equal(t, []plannedRef{
		{entityType: model.EntityTypeRelease, entityID: 1, workID: 10, externalID: "111"},
		{entityType: model.EntityTypeRelease, entityID: 7, workID: 70, externalID: "777"},
	}, plan)

	accounted := len(plan) + ls.SkippedMalformed + ls.SkippedRejection +
		ls.SkippedValueTaken + ls.SkippedAmbiguous + ls.SkippedDedup
	assert.Equal(t, len(cands), accounted, "every candidate is accounted for exactly once")
}

func TestAmbiguityBeatsOrdering(t *testing.T) {
	l := laneByName(t, LaneDmm)
	cands := []candidate{
		{ReleaseID: 9, WorkID: 90, RawValue: "dlsoft.dmm.co.jp/detail/next_0352/"},
		{ReleaseID: 8, WorkID: 80, RawValue: "www.dmm.co.jp/mono/pcgame/-/detail/=/cid=next_0352/"},
	}
	ls := &LaneStats{}
	plan := decide(cands, l, noneTaken(), map[string]struct{}{}, ls)
	assert.Empty(t, plan)
	assert.Equal(t, 2, ls.SkippedAmbiguous)
	assert.Contains(t, ls.AmbiguousSamples, "next_0352",
		"both URL shapes normalize to the same cid, which is what makes them ambiguous")
}

func noneTaken() takenExact {
	return takenExact{release: map[string]struct{}{}, work: map[string]int64{}}
}

func TestSameWorkGroupGoesToTheWork(t *testing.T) {
	l := laneByName(t, LaneSteam)
	cands := []candidate{
		{ReleaseID: 11, WorkID: 100, RawValue: "900"},
		{ReleaseID: 12, WorkID: 100, RawValue: "900"},
		{ReleaseID: 13, WorkID: 100, RawValue: "900"},
		{ReleaseID: 21, WorkID: 200, RawValue: "901"},
		{ReleaseID: 22, WorkID: 200, RawValue: "901"},
		{ReleaseID: 31, WorkID: 300, RawValue: "902"},
		{ReleaseID: 32, WorkID: 300, RawValue: "902"},
		{ReleaseID: 41, WorkID: 400, RawValue: "903"},
		{ReleaseID: 42, WorkID: 400, RawValue: "903"},
	}
	taken := takenExact{release: map[string]struct{}{}, work: map[string]int64{"901": 200, "902": 999}}
	rejected := map[string]struct{}{rejKey(model.EntityTypeWork, 400, "903"): {}}

	ls := &LaneStats{Candidates: len(cands)}
	plan := decide(cands, l, taken, rejected, ls)

	assert.Equal(t, []plannedRef{
		{entityType: model.EntityTypeWork, entityID: 100, workID: 100, externalID: "900"},
	}, plan)
	assert.Equal(t, 1, ls.PlannedWorkGrain)
	assert.Equal(t, 1, ls.SkippedWorkHeld, "the work already holds the id")
	assert.Equal(t, 1, ls.SkippedValueTaken, "another work holds the id at work grain")
	assert.Equal(t, 1, ls.SkippedRejection, "a human rejected the pair at work grain")
	assert.Equal(t, 5, ls.SkippedSibling)
	assert.Zero(t, ls.SkippedAmbiguous)

	accounted := len(plan) + ls.SkippedMalformed + ls.SkippedRejection + ls.SkippedValueTaken +
		ls.SkippedAmbiguous + ls.SkippedDedup + ls.SkippedSibling + ls.SkippedWorkHeld
	assert.Equal(t, len(cands), accounted, "every candidate is accounted for exactly once")
}
