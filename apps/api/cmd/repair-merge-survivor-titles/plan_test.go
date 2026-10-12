package main

import (
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	official = model.WorkTitleKindOfficial
	alias    = model.WorkTitleKindAlias
	machine  = model.WorkTitleProvenanceMachine
)

func row(id int64, lang, title string, kind int16) titleRow {
	return titleRow{ID: id, Lang: lang, Title: title, Kind: kind}
}

func lineageOf(loserIDs ...int64) *lineage {
	l := newLineage()
	for _, id := range loserIDs {
		l.loserIDs[id] = struct{}{}
	}
	return l
}

func touched(plan []change) map[int64]string {
	out := map[int64]string{}
	for _, c := range plan {
		out[c.TitleID] = c.Action + "/" + c.Reason
	}
	return out
}

func TestPlanDemotesTheLoserOfficialWhereTheSurvivorHasItsOwn(t *testing.T) {
	w := work{ID: 1, OLang: "ja", DisplayName: "咎狗の血 True Blood", Titles: []titleRow{
		row(10, "ja", "咎狗の血 True Blood NITRO ARCHIVE", official),
		row(20, "ja", "咎狗の血 True Blood", official),
	}}
	plan := planWork(w, lineageOf(10))
	require.Len(t, plan, 1)
	assert.Equal(t, change{
		WorkID: 1, TitleID: 10, Lang: "ja", Title: "咎狗の血 True Blood NITRO ARCHIVE", FromKind: official,
		Action: actionDemote, ToKind: &[]int16{alias}[0], ToLang: &[]string{""}[0], Reason: reasonSnapshot,
	}, plan[0])

	shown, ok := shownTitle(w.Titles, nil, "ja")
	require.True(t, ok)
	assert.Equal(t, "咎狗の血 True Blood NITRO ARCHIVE", shown.Title, "the bug: the older loser row wins the election")
	shown, _ = shownTitle(w.Titles, plan, "ja")
	assert.Equal(t, "咎狗の血 True Blood", shown.Title)
}

func TestPlanPromotesTheSurvivorsOwnAliasWhereEveryOfficialIsALosers(t *testing.T) {
	t.Run("zh-Hans, the 9453 shape", func(t *testing.T) {
		w := work{ID: 1, OLang: "ja", DisplayName: "原題", Titles: []titleRow{
			row(10, "zh-Hans", "败者的中文名", official),
			row(20, "ja", "原題", official),
			row(30, "zh-Hans", "自己的中文名", alias),
		}}
		plan := planWork(w, lineageOf(10))
		assert.Equal(t, map[int64]string{10: "demote/snapshot", 30: "promote/own_alias_promoted"}, touched(plan))
		require.Len(t, plan, 2)
		assert.Equal(t, actionDemote, plan[0].Action, "the demotion runs first and frees (lang, title, 0)")
		assert.Equal(t, int16(official), *plan[1].ToKind)
		assert.Equal(t, "zh-Hans", *plan[1].ToLang)
	})
	t.Run("the original language, the work 60 shape", func(t *testing.T) {
		w := work{ID: 60, OLang: "ja", DisplayName: "STEINS;GATE", Titles: []titleRow{
			row(297163, "ja", "STEINS;GATE Nitro The Best! Vol.5", official),
			row(597618, "ja", "STEINS;GATE", alias),
			row(597619, "en", "STEINS;GATE", official),
		}}
		plan := planWork(w, lineageOf(297163))
		assert.Equal(t, map[int64]string{297163: "demote/snapshot", 597618: "promote/own_alias_promoted"}, touched(plan))
		shown, _ := shownTitle(w.Titles, plan, "ja")
		assert.Equal(t, "STEINS;GATE", shown.Title)
		assert.Equal(t, int16(official), shown.Kind)
	})
}

func TestPlanPromotesTheAliasNamedLikeTheWorkElseTheOldest(t *testing.T) {
	titles := []titleRow{
		row(10, "ja", "敗者の題", official),
		row(20, "ja", "略称", alias),
		row(21, "ja", "原題", alias),
		row(22, "ja", "別名", alias),
	}
	w := work{ID: 1, OLang: "ja", DisplayName: "原題", Titles: titles}
	assert.Equal(t, map[int64]string{10: "demote/snapshot", 21: "promote/own_alias_promoted"}, touched(planWork(w, lineageOf(10))))

	w.DisplayName = "どれでもない"
	assert.Equal(t, map[int64]string{10: "demote/snapshot", 20: "promote/own_alias_promoted"}, touched(planWork(w, lineageOf(10))))
}

func TestPlanPromotesNothingTheSurvivorDoesNotOwn(t *testing.T) {
	loserOfficial := row(10, "ja", "敗者の題", official)
	cases := map[string]work{
		"the alias came from a loser too": {Titles: []titleRow{loserOfficial, row(11, "ja", "原題", alias)}},
		"a machine alias": {Titles: []titleRow{loserOfficial,
			{ID: 20, Lang: "ja", Title: "原題", Kind: alias, Provenance: machine}}},
		"a suppressed alias": {Titles: []titleRow{loserOfficial,
			{ID: 20, Lang: "ja", Title: "原題", Kind: alias, Suppressed: true}}},
		"a lang-less alias": {Titles: []titleRow{loserOfficial, row(20, "", "原題", alias)}},
		"an abbreviation":   {Titles: []titleRow{loserOfficial, row(20, "ja", "原題", model.WorkTitleKindAbbreviation)}},
		"the official it would become was removed by an edit": {
			Titles:     []titleRow{loserOfficial, row(20, "ja", "原題", alias)},
			Suppressed: map[string]struct{}{editspec.WorkTitleIdentity(official, "ja", "原題"): {}},
		},
		"a hidden official already holds that title": {Titles: []titleRow{loserOfficial, row(20, "ja", "原題", alias),
			{ID: 30, Lang: "ja", Title: "原題", Kind: official, Suppressed: true}}},
	}
	for name, w := range cases {
		t.Run(name, func(t *testing.T) {
			w.ID, w.OLang, w.DisplayName = 1, "ja", "Display"
			assert.Empty(t, planWork(w, lineageOf(10, 11)))
		})
	}
}

func TestPlanLeavesTheAliasAloneWhenTheSurvivorHasAnOfficial(t *testing.T) {
	w := work{ID: 1, OLang: "ja", DisplayName: "原題", Titles: []titleRow{
		row(10, "ja", "敗者の題", official),
		row(20, "ja", "原題", official),
		row(21, "ja", "略称", alias),
	}}
	assert.Equal(t, map[int64]string{10: "demote/snapshot"}, touched(planWork(w, lineageOf(10))))
}

func TestPlanLeavesALanguageWhereEveryOfficialCameFromALoser(t *testing.T) {
	w := work{ID: 1, OLang: "ja", DisplayName: "原題", Titles: []titleRow{
		row(10, "en", "Loser One", official),
		row(11, "en", "Loser Two", official),
		row(20, "ja", "原題", official),
	}}
	assert.Empty(t, planWork(w, lineageOf(10, 11)))
}

func TestPlanKeepsALoserTitleTheMergeMadeTheDisplayName(t *testing.T) {
	w := work{ID: 1, OLang: "ja", DisplayName: "敗者の題", Titles: []titleRow{
		row(10, "ja", "敗者の題", official),
		row(20, "ja", "原題", official),
	}}
	assert.Empty(t, planWork(w, lineageOf(10)))
}

func TestPlanDeletesWhereAnIdenticalLanglessAliasExists(t *testing.T) {
	w := work{ID: 1, OLang: "ja", DisplayName: "原題", Titles: []titleRow{
		row(10, "ja", "Same", official),
		row(11, "en", "Same", official),
		row(12, "en", "Other", official),
		row(20, "ja", "原題", official),
		row(21, "en", "Own", official),
		row(30, "", "Other", alias),
	}}
	assert.Equal(t, map[int64]string{
		10: "demote/snapshot", 11: "delete/snapshot", 12: "delete/snapshot",
	}, touched(planWork(w, lineageOf(10, 11, 12))))
}

func TestPlanIgnoresSuppressedAndMachineRows(t *testing.T) {
	suppressedOwn := row(20, "ja", "原題", official)
	suppressedOwn.Suppressed = true
	machineOwn := row(30, "zh-Hans", "机翻", official)
	machineOwn.Provenance = machine
	suppressedLoser := row(11, "en", "Hidden", official)
	suppressedLoser.Suppressed = true
	w := work{ID: 1, OLang: "ja", DisplayName: "原題", Titles: []titleRow{
		row(10, "ja", "敗者の題", official),
		suppressedLoser,
		row(12, "zh-Hans", "败者", official),
		row(13, "en", "Loser", official),
		suppressedOwn,
		machineOwn,
		row(40, "en", "Own", official),
	}}
	assert.Equal(t, map[int64]string{13: "demote/snapshot"}, touched(planWork(w, lineageOf(10, 11, 12, 13))))
}

func TestLoserOriginFallsBackToTheTitleWhenTheRowWasRewritten(t *testing.T) {
	l := newLineage()
	l.loserIDs[10] = struct{}{}
	l.loserPairs[pairKey{"ja", "敗者の題"}] = struct{}{}
	l.loserPairs[pairKey{"ja", "共通"}] = struct{}{}
	l.ownIDs[20] = struct{}{}
	l.ownPairs[pairKey{"ja", "共通"}] = struct{}{}

	assert.True(t, l.loserOrigin(row(10, "ja", "anything", official)), "a moved row id is proof")
	assert.False(t, l.loserOrigin(row(20, "ja", "敗者の題", official)), "an id the survivor held at merge time is its own")
	assert.True(t, l.loserOrigin(row(99, "ja", "敗者の題", official)), "a re-inserted loser title")
	assert.False(t, l.loserOrigin(row(99, "ja", "共通", official)), "both sides had it")
	assert.False(t, l.loserOrigin(row(99, "ja", "新しい", official)))
}

func TestPlanFallsBackToTheDisplayNameWithoutASnapshot(t *testing.T) {
	l := newLineage()
	l.snapshotless = true
	w := work{ID: 1, OLang: "ja", DisplayName: "原題", Titles: []titleRow{
		row(10, "ja", "別題", official),
		row(11, "en", "English", official),
		row(20, "ja", "原題", official),
		row(21, "en", "Own", official),
	}}
	assert.Equal(t, map[int64]string{10: "demote/display_name"}, touched(planWork(w, l)), "olang only")

	w.DisplayName = "どれでもない"
	assert.Empty(t, planWork(w, l), "no official equals display_name: nothing to anchor on")
}
