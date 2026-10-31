package extract

import (
	"testing"

	"api/internal/platform/catalog/walkthrough"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const leylinePage = `⇒の選択肢は好きな順番に選択しても問題ありません。 ★の選択肢は推理ランクに影響のある選択肢です。
アーデルハイト、九折坂 二人
鹿ケ谷 憂緒
■１話(EpisodeⅠ)━━━━━━
何度もやってるから
★やっぱり遺品ではない？ 推理正解
⇒どんなヤツだった
◆セーブ02
ルイ、ハイジの気持ちも考えてやれ
アーデルハイトEND
◆セーブ02から
ルイの言う通りだろ、ハイジ
★魔女に作り直してもらう？ 不正解
■４話(EpisodeⅣ)━━━━━━
ああ、良かった
━
抵抗する（抵抗しない）
二人END`

func ptr(s string) *string { return &s }

func leylineExtraction() Extracted {
	step := func(text string, alt ...string) walkthrough.Step {
		return walkthrough.Step{StepType: walkthrough.StepChoice, Text: &text, Alt: alt}
	}
	sv := "sv02"
	seg := func(key string, from walkthrough.Node, ending string, steps ...walkthrough.Step) extractedSegment {
		s := extractedSegment{Key: key, Section: ptr("s1"), From: from, Steps: steps}
		if ending == "" {
			s.To.NodeType = walkthrough.NodeOpen
		} else {
			s.To.NodeType = walkthrough.NodeEnding
			s.To.Ending = &struct {
				Name       string   `json:"name"`
				EndingType string   `json:"ending_type"`
				Characters []string `json:"characters"`
			}{Name: ending, EndingType: "normal", Characters: []string{}}
		}
		return s
	}
	start := walkthrough.Node{NodeType: walkthrough.NodeEntry, Key: ptr("start")}
	achieved := step("★やっぱり遺品ではない？")
	achieved.Achievement = true
	return Extracted{
		Characters: []walkthrough.Character{{Key: "c1", Name: "アーデルハイト", Aliases: []string{"九折坂 二人"}}, {Key: "c2", Name: "鹿ケ谷 憂緒"}},
		Sections:   []walkthrough.Section{{Key: "s1", Title: "アーデルハイト、九折坂 二人", Characters: []string{"c1", "c2"}}},
		Entries:    []walkthrough.Entry{{Key: "start", Label: "START"}},
		Saves:      []walkthrough.Save{{Key: "sv02", Label: "セーブ02"}},
		Segments: []extractedSegment{
			seg("g1", start, "アーデルハイトEND", step("何度もやってるから"), achieved, step("⇒どんなヤツだった"),
				walkthrough.Step{StepType: walkthrough.StepSave, Save: &sv}, step("ルイ、ハイジの気持ちも考えてやれ")),
			seg("g2", walkthrough.Node{NodeType: walkthrough.NodeSave, Key: &sv}, "", step("ルイの言う通りだろ、ハイジ"), step("★魔女に作り直してもらう？")),
			seg("g3", start, "二人END", step("ああ、良かった"), step("━"), step("抵抗する", "抵抗しない")),
		},
	}
}

func TestSettleRepairsAChartFromItsSourceLines(t *testing.T) {
	var m Metrics
	verdict, c := Settle(leylinePage, leylineExtraction(), &m)
	require.NotNil(t, c)
	assert.Equal(t, VerdictOK, verdict, m)

	require.Len(t, c.Segments, 2, "the route cut at ■４話 is one segment again")
	g2 := c.Segments[1]
	assert.Equal(t, walkthrough.NodeSave, g2.From.NodeType)
	assert.Equal(t, walkthrough.NodeEnding, g2.To.NodeType)

	var texts, remarks []string
	for _, st := range append(c.Segments[0].Steps, g2.Steps...) {
		if st.Text != nil {
			texts = append(texts, st.StepType+":"+*st.Text)
		}
		if st.Remark != nil {
			remarks = append(remarks, *st.Remark)
		}
		assert.False(t, st.Achievement, "no legend ties ★ to an achievement")
	}
	assert.Equal(t, []string{
		"heading:■１話(EpisodeⅠ)━━━━━━", "choice:何度もやってるから", "choice:★やっぱり遺品ではない？", "choice:⇒どんなヤツだった",
		"choice:ルイ、ハイジの気持ちも考えてやれ",
		"choice:ルイの言う通りだろ、ハイジ", "choice:★魔女に作り直してもらう？", "heading:■４話(EpisodeⅣ)━━━━━━",
		"choice:ああ、良かった", "choice:抵抗する",
	}, texts, "the ━ rule is dropped")
	assert.Equal(t, []string{"推理正解", "不正解"}, remarks)

	assert.Equal(t, []walkthrough.Legend{
		{Text: "⇒の選択肢は好きな順番に選択しても問題ありません。"}, {Text: "★の選択肢は推理ランクに影響のある選択肢です。"},
	}, c.Legend)

	require.Len(t, c.Characters, 3)
	assert.Equal(t, "九折坂 二人", c.Characters[2].Name)
	assert.Empty(t, c.Characters[0].Aliases)
	assert.Equal(t, []string{"c1", "c3"}, c.Sections[0].Characters)
	assert.Equal(t, []string{"c3"}, c.Endings[1].Characters)
	assert.Equal(t, "heroine", c.Endings[1].EndingType)
}

func TestRemarkLike(t *testing.T) {
	for s, want := range map[string]bool{
		"推理正解": true, "不正解": true, "(○)": true, "恋愛P+1": true, "鞠奈＋１": true, "淫乱+1＆独占欲+1": true, "好感度－１": true,
		"シナリオ1-02": false, "Ｂ８－２": false, "(フラグＡが＋４～＋７)": false, "→★母乳出させる": false, "": false,
	} {
		assert.Equal(t, want, remarkLike(s), s)
	}
}

func TestMarkAchievementsFollowsTheLegend(t *testing.T) {
	text := func(s string) *string { return &s }
	c := walkthrough.Chart{
		Legend: []walkthrough.Legend{{Text: "☆は実績の取得場所です。"}},
		Segments: []walkthrough.Segment{{Steps: []walkthrough.Step{
			{StepType: walkthrough.StepChoice, Text: text("☆手を握る")},
			{StepType: walkthrough.StepChoice, Text: text("★逃げる"), Achievement: true},
		}}},
	}
	markAchievements(&c)
	assert.True(t, c.Segments[0].Steps[0].Achievement)
	assert.False(t, c.Segments[0].Steps[1].Achievement)
}

func TestSplitMergedCharactersMovesAnAliasToItsOwner(t *testing.T) {
	c := walkthrough.Chart{Characters: []walkthrough.Character{
		{Key: "c1", Name: "ことみ", Aliases: []string{"古河 渚"}},
		{Key: "c2", Name: "渚", Aliases: []string{"一ノ瀬 ことみ"}},
	}}
	splitMergedCharacters(&c)
	assert.Equal(t, []string{"一ノ瀬 ことみ"}, c.Characters[0].Aliases)
	assert.Equal(t, []string{"古河 渚"}, c.Characters[1].Aliases)
}

func TestTimeHeadings(t *testing.T) {
	for s, want := range map[string]bool{
		"２４日": true, "残り１０日": true, "ターン１１": true, "Day8": true, "3rd Day": true, "最終話": true,
		"『第十話』": true, "第二週一日目": true, "ＰＭ０４：００": true, "土曜日": true, "FinalChapter": true, "○４週目": true,
		"３回目": true, "２人目": true, "フェイズ６": true, "Phase3": true, "ぶどうの月5日": true, "【期限まで:3日】": true,
		"○10/23": true, "【10月4日土曜日】": true, "8月3日Mon": true,
		"残り物を食べる": false, "３日後に会う": false, "日曜日に出かける": false, "ターンエンド": false,
		"２人目の客に話す": false, "月の光を見る": false, "フェイズシフト": false,
	} {
		s := sourceLines{raw: []string{s}}
		assert.Equal(t, want, s.isHeading(0), s.raw[0])
	}
}

func TestRepeatedHeadingsArePicks(t *testing.T) {
	text := func(s string) *string { return &s }
	heading := func(s string) walkthrough.Step { return walkthrough.Step{StepType: walkthrough.StepHeading, Text: text(s)} }
	c := walkthrough.Chart{Segments: []walkthrough.Segment{{Steps: []walkthrough.Step{
		heading("■リーゼ"), heading("第1話"), heading("■リーゼ"), heading("第1話"), heading("■ステラ"), heading("■リーゼ"), heading("第1話"),
	}}}}
	pickRepeatedHeadings(&c)
	var types []string
	for _, st := range c.Segments[0].Steps {
		types = append(types, st.StepType)
	}
	assert.Equal(t, []string{"choice", "heading", "choice", "heading", "heading", "choice", "heading"}, types)
}
