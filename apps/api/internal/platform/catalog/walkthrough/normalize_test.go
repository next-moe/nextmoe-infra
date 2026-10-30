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

func TestNormalizeRetypesLinesThatAreNotChoices(t *testing.T) {
	kinds := map[string]string{
		"５月１４日":        StepHeading,
		"■１２月２０日（火）":   StepHeading,
		"７月１日金曜":       StepHeading,
		"１日目午前":        StepHeading,
		"■４日目━━━━━━━━": StepHeading,
		"第九話":          StepHeading,
		"Ｂ８－２":         StepHeading,
		"※沙耶ＥＮＤを見ている場合は「はい」を選択": StepNote,
		"１→２→３（２は行かなくても可）":      StepAction,
		"■05月07日│寮":   StepChoice,
		"５月１４日の記事を見る": StepChoice,
		"鈴を追う":        StepChoice,
	}
	for text, want := range kinds {
		c := Chart{Segments: []Segment{{Key: "g1", Steps: []Step{{StepType: StepChoice, Text: ptr(text)}}}}}
		Normalize(&c)
		assert.Equal(t, want, c.Segments[0].Steps[0].StepType, text)
	}
	c := Chart{Segments: []Segment{{Key: "g1", Steps: []Step{{StepType: StepChoice, Text: ptr("※中に出す"), Alt: []string{"外に出す"}}}}}}
	Normalize(&c)
	assert.Equal(t, StepChoice, c.Segments[0].Steps[0].StepType, "a line with an alternative is picked")
}

func TestNormalizeReadsRequiredEndingsFromConditions(t *testing.T) {
	ending := func(key, name string) Ending { return Ending{Key: key, Name: name, EndingType: "other"} }
	seg := func(key, cond, to string) Segment {
		return Segment{Key: key, Condition: ptr(cond), To: Node{NodeType: NodeEnding, Key: ptr(to)}}
	}
	c := Chart{
		Endings: []Ending{ending("e1", "ＴＲＵＥ ＥＮＤ"), ending("e2", "沙耶 ＥＮＤ"), ending("e3", "沙耶 ＢＡＤ ＥＮＤ"),
			ending("e4", "END"), ending("e5", "天国END"), ending("e6", "ＴＲＵＥ ＥＮＤ")},
		Segments: []Segment{
			seg("g1", "ＴＲＵＥＥＮＤ後にセーブ１４から", "e2"),
			seg("g2", "沙耶ＢＡＤＥＮＤ後にセーブ１８から", "e1"),
			seg("g3", "※上記END後に", "e3"),
			seg("g4", "※伏姫or天国END後に", "e4"),
			seg("g5", "※小春と天国END後に", "e5"),
			seg("g6", "ＴＲＵＥＥＮＤと沙耶ＥＮＤ後に", "e6"),
			seg("g7", "天国ＥＮＤ後にanother storyを選択して開始", "e1"),
		},
	}
	Normalize(&c)
	got := map[string][]string{}
	for _, s := range c.Segments {
		got[s.Key] = s.RequiresEndings
	}
	assert.Equal(t, []string{"e1"}, got["g1"], "a repeated name resolves to its first ending")
	assert.Equal(t, []string{"e3"}, got["g2"], "the longest name wins over 沙耶ＥＮＤ")
	assert.Empty(t, got["g3"], "a bare END names no ending")
	assert.Empty(t, got["g4"], "either of two is not a list of requirements")
	assert.Empty(t, got["g5"], "小春 is joined to a resolved name but resolves to none")
	assert.ElementsMatch(t, []string{"e1", "e2"}, got["g6"])
	assert.Equal(t, []string{"e5"}, got["g7"], "the OR inside STORY is no either-of")
}

func TestNormalizeGivesABadEndingItsSectionsCharacter(t *testing.T) {
	c := Chart{
		Characters: []Character{{Key: "c1", Name: "鈴"}, {Key: "c2", Name: "小毬"}},
		Sections:   []Section{{Key: "s1", Characters: []string{"c1"}}, {Key: "s2", Characters: []string{"c1", "c2"}}},
		Endings: []Ending{{Key: "e1", Name: "ＢＡＤ ＥＮＤ", EndingType: "bad"}, {Key: "e2", Name: "ＢＡＤ ＥＮＤ", EndingType: "bad"},
			{Key: "e3", Name: "ノーマルEND", EndingType: "normal"}},
		Segments: []Segment{
			{Key: "g1", Section: ptr("s1"), To: Node{NodeType: NodeEnding, Key: ptr("e1")}},
			{Key: "g2", Section: ptr("s2"), To: Node{NodeType: NodeEnding, Key: ptr("e2")}},
			{Key: "g3", Section: ptr("s1"), To: Node{NodeType: NodeEnding, Key: ptr("e3")}},
		},
	}
	Normalize(&c)
	assert.Equal(t, []string{"c1"}, c.Endings[0].Characters)
	assert.Empty(t, c.Endings[1].Characters, "a section about two characters names neither")
	assert.Empty(t, c.Endings[2].Characters, "only bad endings are filled")
	assert.Equal(t, "normal", c.Endings[2].EndingType)
}
