package walkthrough

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func ptr(s string) *string { return &s }

func TestNormalizeCleansEntryLabelsAndSelfAlternatives(t *testing.T) {
	c := Chart{
		Entries: []Entry{{Key: "start", Label: "【START】から開始"}, {Key: "restart", Label: "【RESTART】"}},
		Segments: []Segment{{Key: "g1", From: Node{NodeType: NodeEntry, Key: ptr("start")}, To: Node{NodeType: NodeOpen},
			Steps: []Step{{StepType: StepChoice, Text: ptr("☆(しつこいからもういいか)"), Alt: []string{"しつこいからもういいか", "別の"}}}}},
	}
	Normalize(&c)
	assert.Equal(t, "START", c.Entries[0].Label)
	assert.Equal(t, "RESTART", c.Entries[1].Label)
	st := c.Segments[0].Steps[0]
	assert.True(t, st.Achievement)
	assert.Equal(t, "(しつこいからもういいか)", *st.Text)
	assert.Equal(t, []string{"別の"}, st.Alt, "an alternative equal to its own choice is dropped")
	assert.Len(t, st.AltZh, 1)
}

func TestNormalizeTurnsCollectionEndingsIntoCollectStops(t *testing.T) {
	c := Chart{
		Endings:  []Ending{{Key: "e1", Name: "差分CG回収後終了", EndingType: "other"}, {Key: "e2", Name: "寧々END", EndingType: "heroine"}},
		Segments: []Segment{{Key: "g1", To: Node{NodeType: NodeEnding, Key: ptr("e1")}}, {Key: "g2", To: Node{NodeType: NodeEnding, Key: ptr("e2")}}},
	}
	Normalize(&c)
	assert.Equal(t, NodeCollect, c.Segments[0].To.NodeType)
	assert.Nil(t, c.Segments[0].To.Key)
	assert.Len(t, c.Endings, 1, "the collection stop's ending is no longer reached and is dropped")
}

func TestNormalizeRetypesEndingsTheModelCalledNormal(t *testing.T) {
	c := Chart{Endings: []Ending{
		{Key: "e1", Name: "寧々END1", EndingType: "normal", Characters: []string{"c1"}},
		{Key: "e2", Name: "ノーマルEND", EndingType: "normal"},
		{Key: "e3", Name: "ＢＡＤ ＥＮＤ", EndingType: "other", Characters: []string{"c1"}},
		{Key: "e4", Name: "ハーレムEND", EndingType: "normal", Characters: []string{"c1", "c2"}},
		{Key: "e5", Name: "END", EndingType: "after", Characters: []string{"c1"}},
	}}
	for i := range c.Endings {
		c.Segments = append(c.Segments, Segment{To: Node{NodeType: NodeEnding, Key: ptr(c.Endings[i].Key)}})
	}
	Normalize(&c)
	var got []string
	for _, e := range c.Endings {
		got = append(got, e.EndingType)
	}
	assert.Equal(t, []string{"heroine", "normal", "bad", "harem", "after"}, got)
}

func TestNormalizeStripsTheAlternativeRepeatedInParentheses(t *testing.T) {
	c := Chart{Segments: []Segment{{To: Node{NodeType: NodeOpen}, Steps: []Step{
		{StepType: StepChoice, Text: ptr("このまま可愛い……(身体を精液で…)"), Alt: []string{"身体を精液で…"}},
	}}}}
	Normalize(&c)
	st := c.Segments[0].Steps[0]
	assert.Equal(t, "このまま可愛い……", *st.Text)
	assert.Equal(t, []string{"身体を精液で…"}, st.Alt)
}
