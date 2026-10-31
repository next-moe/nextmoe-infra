package extract

import (
	"encoding/json"
	"fmt"
	"strings"
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

func parseExtracted(t *testing.T, raw string) Extracted {
	t.Helper()
	var x Extracted
	require.NoError(t, json.Unmarshal([]byte(raw), &x))
	return x
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
	c := x.Chart()
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
	c := x.Chart()
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
	var m Metrics
	verdict, c := Settle(sampleText, x, &m)
	assert.Equal(t, VerdictRejected, verdict)
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
	var m Metrics
	verdict, _ := Settle(sampleText, x, &m)
	assert.Equal(t, 1, m.EndMarkers)
	assert.Equal(t, 0, m.EndingsReached)
	assert.Equal(t, VerdictReview, verdict, "the page has an END marker the chart never reaches")
}

func TestCoverageCountsEveryMissedLine(t *testing.T) {
	var page strings.Builder
	page.WriteString("START\n選ぶ\n")
	for i := range 20 {
		fmt.Fprintf(&page, "⇒デート%02d\n", i)
	}
	page.WriteString("寧々に声をかける\n寧々END\n")
	text := func(s string) *string { return &s }
	c := walkthrough.Chart{
		Characters: []walkthrough.Character{{Key: "c1", Name: "寧々"}},
		Segments: []walkthrough.Segment{{Steps: []walkthrough.Step{
			{StepType: walkthrough.StepChoice, Text: text("選ぶ")},
		}}},
	}
	ratio, missed := coverage(page.String(), c)
	assert.InDelta(t, 1.0/22, ratio, 1e-9, "all 21 missed lines count, not the 8 that are listed")
	assert.Len(t, missed, 8)
	assert.NotContains(t, missed, "選ぶ")
}

func TestGateRejectsARemarkWithoutText(t *testing.T) {
	remark := "推理正解"
	c := walkthrough.Chart{Segments: []walkthrough.Segment{{Steps: []walkthrough.Step{
		{StepType: walkthrough.StepChoice, Remark: &remark},
	}}}}
	var m Metrics
	assert.Equal(t, VerdictRejected, Gate(sampleText, c, &m))
}
