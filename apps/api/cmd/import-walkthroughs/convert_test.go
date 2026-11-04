package main

import (
	"testing"

	"api/internal/platform/catalog/walkthrough"

	"github.com/stretchr/testify/assert"
)

func TestChartTextKeepsOneMarkerPerLine(t *testing.T) {
	got := chartText(`<div><script>var a=1</script><p>◆セーブ01</p><table><tr><td>綾地&nbsp;さんに頼む</td></tr></table><br>寧々END1</div>`)
	assert.Equal(t, "◆セーブ01\n綾地 さんに頼む\n寧々END1", got)
}

func TestKeepsNamesRefusesARenamedName(t *testing.T) {
	names := []string{"仮屋 和奏", "和奏"}
	assert.True(t, keepsNames("和奏END", "和奏END", names))
	assert.False(t, keepsNames("和奏の中に全てを…", "将全部射进和鸣体内…", names))
	assert.True(t, keepsNames("このまま中に", "就这样在里面", names))
}

func TestPinCharactersStaysInsideTheRoster(t *testing.T) {
	c := &walkthrough.Chart{Characters: []walkthrough.Character{
		{Key: "c1", Name: "綾地 寧々", Aliases: []string{"寧々"}},
		{Key: "c2", Name: "めぐる"},
		{Key: "c3", Name: "和奏"},
	}}
	roster := []rosterEntry{
		{CharacterID: 10, Names: []string{nameKey("綾地寧々")}},
		{CharacterID: 11, Names: []string{nameKey("因幡めぐる")}},
		{CharacterID: 12, Names: []string{nameKey("仮屋和奏"), nameKey("仮屋 和奏")}},
		{CharacterID: 13, Names: []string{nameKey("和奏の妹")}},
	}
	got := pinCharacters(c, roster)
	assert.Equal(t, characterPin{CharacterID: 10, Rule: ruleSameName}, got["c1"])
	assert.Equal(t, characterPin{CharacterID: 11, Rule: ruleNamePart}, got["c2"])
	_, pinned := got["c3"]
	assert.False(t, pinned, "和奏 is a part of two roster names, so it pins neither")
}

func TestPinCharactersPrefersTheRosterOverPlaceholders(t *testing.T) {
	c := &walkthrough.Chart{Characters: []walkthrough.Character{
		{Key: "c1", Name: "鏡"},
		{Key: "c2", Name: "澪"},
		{Key: "c3", Name: "由希"},
		{Key: "c4", Name: "空"},
	}}
	roster := []rosterEntry{
		{CharacterID: 1, Names: []string{nameKey("敷島 鏡")}},
		{CharacterID: 2, Names: []string{nameKey("如月 澪")}},
		{CharacterID: 3, Names: []string{nameKey("如月澪")}, HasImage: true},
		{CharacterID: 4, Names: []string{nameKey("御園 由希")}},
		{CharacterID: 5, Names: []string{nameKey("三条由希")}},
		{CharacterID: 6, Names: []string{nameKey("美空")}},
		{CharacterID: 7, Names: []string{nameKey("鏡")}, Placeholder: true},
		{CharacterID: 8, Names: []string{nameKey("由希")}, Placeholder: true},
	}
	got := pinCharacters(c, roster)
	assert.Equal(t, characterPin{CharacterID: 1, Rule: ruleNamePart}, got["c1"], "a real full name beats a placeholder of the same name")
	assert.Equal(t, characterPin{CharacterID: 3, Rule: ruleNamePart}, got["c2"], "one person entered twice pins the copy with an image")
	assert.Equal(t, characterPin{CharacterID: 8, Rule: ruleSameName}, got["c3"], "two different people stay ambiguous, so the placeholder keeps it")
	_, pinned := got["c4"]
	assert.False(t, pinned, "a one-rune name is not read as the end of a two-rune name")
}

func TestParseSourceDate(t *testing.T) {
	assert.Equal(t, "2023-12-07", parseSourceDate("23/12/07").Format("2006-01-02"))
	assert.Equal(t, "2006-02-26", parseSourceDate("最終更新 ２００６/０２/２６").Format("2006-01-02"))
	assert.Nil(t, parseSourceDate("修正なし"))
}

func TestTranslateJobProtectsEveryUntranslatedRosterName(t *testing.T) {
	c := &walkthrough.Chart{
		Characters: []walkthrough.Character{{Key: "c1", Name: "寧々"}, {Key: "c2", Name: "和奏"}},
		Segments: []walkthrough.Segment{{Steps: []walkthrough.Step{
			{StepType: walkthrough.StepChoice, Text: ptr("思わず来栖の横顔に見とれてしまう。")},
		}}},
	}
	roster := []rosterName{
		{CharacterID: 1, Names: []string{"綾地寧々"}, Zh: "绫地宁宁"},
		{CharacterID: 2, Names: []string{"仮屋 和奏"}},
		{CharacterID: 3, Names: []string{"来栖 夏芽"}},
		{CharacterID: 4, Names: []string{"久園寺 美紀"}, Zh: "久園寺 美紀"},
	}
	job := buildTranslateJob(7, c, map[string]characterPin{"c1": {CharacterID: 1}, "c2": {CharacterID: 2}}, roster)
	assert.Contains(t, job.KeepNames, "美紀", "a zh alias copying the Japanese name is no translation")
	assert.NotContains(t, job.Glossary, glossaryTerm{Ja: "久園寺 美紀", Zh: "久園寺 美紀"})
	assert.Contains(t, job.Glossary, glossaryTerm{Ja: "寧々", Zh: "绫地宁宁"}, "a chart short name inherits its pin's sourced zh name")
	assert.Contains(t, job.Glossary, glossaryTerm{Ja: "綾地寧々", Zh: "绫地宁宁"})
	for _, n := range []string{"仮屋 和奏", "和奏", "来栖 夏芽", "来栖", "夏芽"} {
		assert.Contains(t, job.KeepNames, n)
	}
	assert.NotContains(t, job.KeepNames, "寧々")
	assert.False(t, keepsNames("思わず来栖の横顔に見とれてしまう。", "不禁看来了栖的侧脸。", job.KeepNames))
}

func TestShortNamesTakeTheMatchingPartOfTheZhName(t *testing.T) {
	assert.Equal(t, "忧绪", shortZh("憂緒", []string{"鹿ケ谷 憂緒"}, "鹿谷忧绪"))
	assert.Equal(t, "春日", shortZh("春日", []string{"春日 真由美"}, "春日真由美"))
	assert.Equal(t, "鹿谷忧绪", shortZh("鹿ケ谷", []string{"鹿ケ谷 憂緒"}, "鹿谷忧绪"), "a family name of another length keeps the full name")
	assert.Equal(t, "村云春霞", shortZh("スミちゃん", []string{"村雲 春霞"}, "村云春霞"), "a nickname keeps the full name")
	assert.Equal(t, "绫地宁宁", shortZh("寧々", []string{"綾地寧々"}, "绫地宁宁"), "an unspaced full name has no parts to match")
}

func TestNameMaskRoundTripsKeptNames(t *testing.T) {
	m := newNameMask([]string{"しづる", "春日 しづる", "藍"}, 2)
	masked := m.hide("春日 しづるとしづるの部屋で藍を見る")
	assert.Equal(t, "{N1}と{N2}の部屋で藍を見る", masked, "the full name is masked before its part; a one-rune name is not")
	zh, ok := m.restore(masked, "在{N2}的房间里和{N1}一起看藍")
	assert.True(t, ok)
	assert.Equal(t, "在しづる的房间里和春日 しづる一起看藍", zh)
	_, ok = m.restore(masked, "在静流的房间里和{N1}一起看藍")
	assert.False(t, ok, "a dropped token is a renamed name")
	_, ok = m.restore(masked, "{N1}{N2}{N3}")
	assert.False(t, ok, "an invented token is refused")
	assert.True(t, m.onlyNames("■春日 しづる│2"))
	assert.False(t, m.onlyNames("しづるEND"))
	assert.False(t, m.onlyNames("１章"), "a string without a kept name is not a name")
}

func ptr(s string) *string { return &s }
