package main

import (
	"encoding/json"
	"testing"

	"api/internal/platform/catalog/walkthrough"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sampleText = `綾地 寧々
【START】から開始
もう１回正直に褒める
☆(しつこいからもういいか)
◆セーブ01
綾地さんに頼む
寧々END1
※タイトル画面に【RESTART】が追加
◆セーブ01から
このまま中に(このまま外に)
差分CG回収後終了`

func parseExtracted(t *testing.T, raw string) extracted {
	t.Helper()
	var x extracted
	require.NoError(t, json.Unmarshal([]byte(raw), &x))
	return x
}

func TestChartTextKeepsOneMarkerPerLine(t *testing.T) {
	got := chartText(`<div><script>var a=1</script><p>◆セーブ01</p><table><tr><td>綾地&nbsp;さんに頼む</td></tr></table><br>寧々END1</div>`)
	assert.Equal(t, "◆セーブ01\n綾地 さんに頼む\n寧々END1", got)
}

func TestExtractedChartInlinesEndingsOnePerOccurrence(t *testing.T) {
	x := parseExtracted(t, `{"no_chart":false,
	 "characters":[{"key":"c1","name":"綾地 寧々","aliases":["寧々"]}],
	 "sections":[{"key":"s1","title":"綾地 寧々","characters":["寧々"]}],
	 "entries":[{"key":"start","label":"START","unlocked_by":null},{"key":"restart","label":"RESTART","unlocked_by":"g1"}],
	 "saves":[{"key":"sv01","label":"セーブ01"}],
	 "segments":[
	  {"key":"g1","section":"s1","from":{"node_type":"entry","key":"start"},"condition":null,
	   "steps":[{"step_type":"choice","text":"もう１回正直に褒める","alt":[],"achievement":false,"save":null},
	            {"step_type":"save","text":"◆セーブ01","alt":[],"achievement":false,"save":null},
	            {"step_type":"save","text":null,"alt":[],"achievement":false,"save":null}],
	   "to":{"node_type":"ending","ending":{"name":"寧々END1","ending_type":"heroine","characters":["c1"]}}},
	  {"key":"g2","section":"s1","from":{"node_type":"save","key":"セーブ01"},"condition":null,
	   "steps":[{"step_type":"choice","text":"このまま中に","alt":["このまま外に"],"achievement":false,"save":null}],
	   "to":{"node_type":"collect","ending":null}},
	  {"key":"g3","section":"s1","from":{"node_type":"ending","key":"g1"},"condition":null,
	   "steps":[],"to":{"node_type":"ending","ending":{"name":"END","ending_type":"after","characters":["c1"]}}}]}`)
	c := x.chart()
	walkthrough.Normalize(&c)

	require.Len(t, c.Endings, 2)
	assert.Equal(t, "e1", c.Endings[0].Key)
	assert.Equal(t, "e1", *c.Entries[1].UnlockedBy, "unlocked_by named the segment; it now names that segment's ending")
	assert.Equal(t, "e1", *c.Segments[2].From.Key, "an after-story starts from the ending, not the segment")
	assert.Equal(t, "e2", *c.Segments[2].To.Key)
	assert.Equal(t, "sv01", *c.Segments[1].From.Key, "a resume written as the save label resolves to its key")
	require.Len(t, c.Segments[0].Steps, 2, "a save step with neither key nor text is dropped")
	assert.Equal(t, "sv01", *c.Segments[0].Steps[1].Save)
	assert.Nil(t, c.Segments[0].Steps[1].Text)
	assert.Equal(t, []string{"c1"}, c.Sections[0].Characters, "a character written by alias resolves to its key")
	assert.Empty(t, walkthrough.Validate(c))
}

func TestRepairCreatesCharactersNamedOnlyInHeadings(t *testing.T) {
	x := parseExtracted(t, `{"no_chart":false,"characters":[],
	 "sections":[{"key":"s1","title":"あざみ","characters":["あざみ"]}],
	 "entries":[{"key":"start","label":"START","unlocked_by":null}],"saves":[],
	 "segments":[{"key":"g1","section":"s1","from":{"node_type":"entry","key":"start"},"condition":null,
	   "steps":[{"step_type":"choice","text":"はい","alt":[],"achievement":false,"save":null}],
	   "to":{"node_type":"ending","ending":{"name":"あざみEND","ending_type":"heroine","characters":["あざみ"]}}}]}`)
	c := x.chart()
	require.Len(t, c.Characters, 1)
	assert.Equal(t, "あざみ", c.Characters[0].Name)
	assert.Equal(t, []string{c.Characters[0].Key}, c.Endings[0].Characters)
}

func TestGateRefusesTextThatIsNotOnThePage(t *testing.T) {
	x := parseExtracted(t, `{"no_chart":false,"characters":[{"key":"c1","name":"綾地 寧々","aliases":[]}],
	 "sections":[],"entries":[{"key":"start","label":"START","unlocked_by":null}],"saves":[],
	 "segments":[{"key":"g1","section":null,"from":{"node_type":"entry","key":"start"},"condition":"※全員クリア後",
	   "steps":[{"step_type":"choice","text":"綾地さんに頼む","alt":[],"achievement":false,"save":null},
	            {"step_type":"choice","text":"見殺しにする","alt":[],"achievement":false,"save":null}],
	   "to":{"node_type":"ending","ending":{"name":"寧々END1","ending_type":"heroine","characters":["c1"]}}}]}`)
	var m extractMetrics
	verdict, c := settle(sampleText, x, &m)
	assert.Equal(t, verdictRejected, verdict)
	assert.Equal(t, []string{"見殺しにする"}, m.NotVerbatim)
	assert.Equal(t, []string{"※全員クリア後"}, m.DroppedConditions, "a paraphrased condition is dropped, not fatal")
	assert.Nil(t, c.Segments[0].Condition)
}

func TestGateSendsMissingEndingsToReview(t *testing.T) {
	x := parseExtracted(t, `{"no_chart":false,"characters":[],"sections":[],
	 "entries":[{"key":"start","label":"START","unlocked_by":null}],"saves":[],
	 "segments":[{"key":"g1","section":null,"from":{"node_type":"entry","key":"start"},"condition":null,
	   "steps":[{"step_type":"choice","text":"もう１回正直に褒める","alt":["しつこいからもういいか"],"achievement":true,"save":null},
	            {"step_type":"choice","text":"綾地さんに頼む","alt":[],"achievement":false,"save":null}],
	   "to":{"node_type":"open","ending":null}},
	  {"key":"g2","section":null,"from":{"node_type":"entry","key":"start"},"condition":null,
	   "steps":[{"step_type":"choice","text":"このまま中に","alt":["このまま外に"],"achievement":false,"save":null}],
	   "to":{"node_type":"collect","ending":null}}]}`)
	var m extractMetrics
	verdict, _ := settle(sampleText, x, &m)
	assert.Equal(t, 1, m.EndMarkers)
	assert.Equal(t, 0, m.EndingsReached)
	assert.Equal(t, verdictReview, verdict, "the page has an END marker the chart never reaches")
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

func TestNameMaskRoundTripsKeptNames(t *testing.T) {
	m := newNameMask([]string{"しづる", "春日 しづる", "藍"})
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
