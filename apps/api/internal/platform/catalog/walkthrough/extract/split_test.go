package extract

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"api/internal/platform/catalog/walkthrough"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitPageCutsAfterEndings(t *testing.T) {
	assert.Equal(t, []string{sampleText}, SplitPage(sampleText))

	var page strings.Builder
	for r := range 12 {
		fmt.Fprintf(&page, "■ルート%d\n【START】から開始\n", r)
		for i := range 60 {
			fmt.Fprintf(&page, "ルート%dの選択肢その%02d\n", r, i)
		}
		fmt.Fprintf(&page, "ルート%dEND\n", r)
	}
	text := strings.TrimSuffix(page.String(), "\n")
	parts := SplitPage(text)
	require.Greater(t, len(parts), 1)
	assert.Equal(t, text, strings.Join(parts, "\n"), "no line is lost or repeated")
	for _, p := range parts[:len(parts)-1] {
		lines := strings.Split(p, "\n")
		assert.Regexp(t, `END$`, lines[len(lines)-1])
		assert.LessOrEqual(t, utf8.RuneCountInString(p), PartRunes*3/2+200)
	}
}

func TestMergeJoinsPartsByNameAndLabel(t *testing.T) {
	var so Extracted
	Merge(&so, parseExtracted(t, `{"no_chart":false,
	 "characters":[{"key":"c1","name":"綾地 寧々","aliases":["寧々"]}],
	 "sections":[{"key":"s1","title":"綾地 寧々","characters":["c1"]}],
	 "entries":[{"key":"start","label":"START","unlocked_by":null}],
	 "saves":[{"key":"sv01","label":"セーブ01"}],
	 "segments":[{"key":"g1","section":"s1","from":{"node_type":"entry","key":"start"},"condition":null,
	   "steps":[{"step_type":"choice","text":"もう１回正直に褒める","alt":[],"remark":null,"achievement":false,"save":null},
	            {"step_type":"save","text":null,"alt":[],"remark":null,"achievement":false,"save":"sv01"}],
	   "to":{"node_type":"ending","ending":{"name":"寧々END1","ending_type":"heroine","characters":["c1"]}}}]}`), 1)
	Merge(&so, parseExtracted(t, `{"no_chart":false,
	 "characters":[{"key":"p2_c1","name":"寧々","aliases":[]}],
	 "sections":[{"key":"p2_s1","title":"綾地 寧々","characters":["p2_c1"]}],
	 "entries":[{"key":"p2_start","label":"【START】","unlocked_by":null}],
	 "saves":[{"key":"p2_sv1","label":"◆セーブ01"}],
	 "segments":[
	  {"key":"p2_g1","section":"s1","from":{"node_type":"save","key":"p2_sv1"},"condition":null,
	   "steps":[{"step_type":"choice","text":"このまま中に","alt":[],"remark":null,"achievement":false,"save":null}],
	   "to":{"node_type":"collect","ending":null}},
	  {"key":"p2_g2","section":"p2_s1","from":{"node_type":"ending","key":"g1"},"condition":null,
	   "steps":[],"to":{"node_type":"ending","ending":{"name":"寧々AFTER END","ending_type":"after","characters":["c1"]}}}]}`), 2)

	require.Len(t, so.Characters, 1)
	assert.Contains(t, so.Characters[0].Aliases, "寧々")
	require.Len(t, so.Sections, 1)
	require.Len(t, so.Entries, 1, "【START】 is the START already there")
	require.Len(t, so.Saves, 1, "◆セーブ01 is the セーブ01 already there")
	require.Len(t, so.Segments, 3)
	c := so.Chart()
	walkthrough.Normalize(&c)
	assert.Empty(t, walkthrough.Validate(c))
	assert.Equal(t, c.Saves[0].Key, *c.Segments[1].From.Key, "the resume reads the first part's save")
	assert.Equal(t, c.Endings[0].Key, *c.Segments[2].From.Key, "the after-story starts from the first part's ending")
	assert.Equal(t, []string{c.Characters[0].Key}, c.Endings[1].Characters)
	assert.Equal(t, c.Sections[0].Key, *c.Segments[1].Section)
}

func TestMergeResolvesAPartsKeysWithOrWithoutItsPrefix(t *testing.T) {
	so := Extracted{Sections: []walkthrough.Section{{Key: "s1", Title: "共通"}}}
	Merge(&so, parseExtracted(t, `{"no_chart":false,
	 "characters":[{"key":"c2","name":"美久","aliases":[]}],
	 "sections":[{"key":"p2_s7","title":"美久","characters":["c2"]}],
	 "entries":[{"key":"p2_start","label":"START","unlocked_by":null}],"saves":[],
	 "segments":[{"key":"p2_g1","section":"s7","from":{"node_type":"entry","key":"p2_start"},"condition":null,
	   "steps":[{"step_type":"choice","text":"はい","alt":[],"remark":null,"achievement":false,"save":null}],
	   "to":{"node_type":"ending","ending":{"name":"美久END","ending_type":"heroine","characters":["p2_c2"]}}}]}`), 2)
	require.Len(t, so.Segments, 1)
	assert.Equal(t, so.Sections[1].Key, *so.Segments[0].Section)
	assert.Equal(t, []string{so.Characters[0].Key}, so.Segments[0].To.Ending.Characters)
}
