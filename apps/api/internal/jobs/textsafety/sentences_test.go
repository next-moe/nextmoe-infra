package textsafety

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitSentencesRoundTrips(t *testing.T) {
	for _, s := range []string{
		"星空の下で出会った二人。「好きだよ」と彼は言った！\n\n夏が終わる……そして秋。",
		"He found Ver.2 of the game. It cost 1.5 dollars! Really?  Yes.",
		"没有标点的一句话",
		"",
		"「台詞だけ」",
	} {
		assert.Equal(t, s, strings.Join(splitSentences(s), ""), s)
	}
}

func TestSplitSentencesCutsJapaneseAndEnglish(t *testing.T) {
	assert.Equal(t, []string{"一文目。", "「二文目！」", "三文目"}, splitSentences("一文目。「二文目！」三文目"))
	assert.Equal(t, []string{"Ver.2 is out. ", "It costs 1.5 now."}, splitSentences("Ver.2 is out. It costs 1.5 now."))
	assert.Equal(t, []string{"行一\n\n", "行二"}, splitSentences("行一\n\n行二"))
}

func TestRedactDropsNumberedSegments(t *testing.T) {
	segs := splitSentences("学園が舞台。ヒロインとHシーンが10個。主人公は転校生。")
	require.Len(t, segs, 3)
	text, kept := redact(segs, []int{2})
	assert.Equal(t, "学園が舞台。主人公は転校生。", text)
	assert.Equal(t, visibleRunes(text), kept)

	text, kept = redact(segs, []int{1, 2, 3})
	assert.Equal(t, "", text)
	assert.Zero(t, kept)
}

func TestRedactCollapsesBlankRuns(t *testing.T) {
	text, _ := redact(splitSentences("A plot.\n\nAn ero scene.\n\nAn ending."), []int{2})
	assert.Equal(t, "A plot.\n\nAn ending.", text)
}

func TestNumberedTextSkipsBlankSegments(t *testing.T) {
	assert.Equal(t, "[1] 一。\n[2] 二。\n", numberedText([]string{"一。\n\n", "二。"}))
}

func TestPackRespectsBudgets(t *testing.T) {
	size := func(n int) int { return n }
	assert.Equal(t, [][]int{{3, 3}, {3, 3}, {9}, {1}}, pack([]int{3, 3, 3, 3, 9, 1}, size, 6, 10))
	assert.Equal(t, [][]int{{1, 1}, {1}}, pack([]int{1, 1, 1}, size, 100, 2))
	assert.Nil(t, pack([]int{}, size, 10, 10))
}

func TestHashesMatchPostgresMD5(t *testing.T) {
	// select md5('星空' || E'\n' || 'Katawa Shoujo')
	assert.Equal(t, "fe6fa209729fbc53c2529f9ffa303bba", titleSetHash([]string{"星空", "Katawa Shoujo"}))
}
