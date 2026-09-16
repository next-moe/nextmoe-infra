package getchuintros

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPickStoryPrefersTheAnchorThatHasText(t *testing.T) {
	anchors := []anchorRow{
		{WorkID: 1, GetchuID: "100"},
		{WorkID: 1, GetchuID: "200"},
		{WorkID: 2, GetchuID: "300"},
	}
	stories := map[string]string{"200": "  物語です  ", "300": "another"}

	got := pickStory(anchors, stories)
	require.Len(t, got, 2)
	assert.Equal(t, "200", got[0].GetchuID)
	assert.Equal(t, "物語です", got[0].Story, "surrounding whitespace is trimmed")
	assert.Equal(t, "300", got[1].GetchuID)
}

func TestPickStoryIsDeterministic(t *testing.T) {
	anchors := []anchorRow{{WorkID: 1, GetchuID: "100"}, {WorkID: 1, GetchuID: "200"}}
	stories := map[string]string{"100": "first", "200": "second"}

	got := pickStory(anchors, stories)
	require.Len(t, got, 1)
	assert.Equal(t, "first", got[0].Story)
}

func TestPickStoryKeepsUnreachableWorks(t *testing.T) {
	got := pickStory([]anchorRow{{WorkID: 7, GetchuID: "1"}}, map[string]string{"2": "x"})
	require.Len(t, got, 1)
	assert.Equal(t, int64(7), got[0].WorkID)
	assert.Empty(t, got[0].Story)
}

func TestPickStoryIgnoresBlankText(t *testing.T) {
	got := pickStory([]anchorRow{{WorkID: 1, GetchuID: "1"}}, map[string]string{"1": "   \n\t "})
	require.Len(t, got, 1)
	assert.Empty(t, got[0].Story)
}

func TestWindowCountsWorksNotRows(t *testing.T) {
	rows := []anchorRow{
		{WorkID: 1, GetchuID: "a"}, {WorkID: 1, GetchuID: "b"},
		{WorkID: 2, GetchuID: "c"},
		{WorkID: 3, GetchuID: "d"}, {WorkID: 3, GetchuID: "e"},
	}
	assert.Equal(t, rows, window(rows, 0, 0), "no window is a passthrough")

	first := window(rows, 2, 0)
	assert.Equal(t, []anchorRow{rows[0], rows[1], rows[2]}, first, "two works, three rows")

	second := window(rows, 2, 2)
	assert.Equal(t, []anchorRow{rows[3], rows[4]}, second, "resumes at work 3, not row 3")

	assert.Empty(t, window(rows, 2, 9), "an offset past the end yields nothing")
}

func TestPickStoryPassesOverABundleAnchor(t *testing.T) {
	anchors := []anchorRow{
		{WorkID: 1, GetchuID: "100", Bundle: true},
		{WorkID: 1, GetchuID: "200"},
		{WorkID: 2, GetchuID: "300", Bundle: true},
		{WorkID: 3, GetchuID: "400", Bundle: true},
	}
	stories := map[string]string{"100": "the package", "200": "the game", "300": "another package"}

	got := pickStory(anchors, stories)
	require.Len(t, got, 3)
	assert.Equal(t, "200", got[0].GetchuID)
	assert.Equal(t, "the game", got[0].Story, "a single-VN anchor after a bundle still testifies")
	assert.Empty(t, got[1].Story)
	assert.True(t, got[1].Bundle, "a work reachable only through a bundle is marked, not silently storyless")
	assert.Empty(t, got[2].Story)
	assert.False(t, got[2].Bundle, "a bundle with no text is plain no_story")
}
