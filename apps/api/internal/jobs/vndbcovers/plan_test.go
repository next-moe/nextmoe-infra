package vndbcovers

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const canned = `{
  "results": [
    {"id": "v17", "image": {"url": "https://t.vndb.org/cv/50/17.jpg", "dims": [256, 400], "sexual": 0.4, "violence": 0}},
    {"id": "v99", "image": {"url": "https://t.vndb.org/cv/12/99.jpg", "dims": [800, 600], "sexual": 1.5, "violence": 2}},
    {"id": "v250", "image": null}
  ],
  "more": false
}`

func TestParseVNResponse(t *testing.T) {
	resp, err := parseVNResponse(strings.NewReader(canned))
	require.NoError(t, err)
	require.Len(t, resp.Results, 3)
	assert.False(t, resp.More)

	assert.Equal(t, "v17", resp.Results[0].ID)
	require.NotNil(t, resp.Results[0].Image)
	assert.Equal(t, "https://t.vndb.org/cv/50/17.jpg", resp.Results[0].Image.URL)
	assert.Equal(t, []int{256, 400}, resp.Results[0].Image.Dims)
	assert.InDelta(t, 0.4, resp.Results[0].Image.Sexual, 1e-9)

	assert.Nil(t, resp.Results[2].Image)
}

func TestParseVNResponseRejectsGarbage(t *testing.T) {
	_, err := parseVNResponse(strings.NewReader(`{"results": [`))
	assert.Error(t, err)
}

func TestRatingLevel(t *testing.T) {
	cases := []struct {
		in   float64
		want int16
	}{
		{0, 0},
		{0.49, 0},
		{0.5, 1},
		{1.49, 1},
		{1.5, 2},
		{2, 2},
		{2.4, 2},
		{-1, 0},
		{math.NaN(), 0},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, RatingLevel(c.in), "RatingLevel(%v)", c.in)
	}
}

func TestPortrait(t *testing.T) {
	assert.True(t, portrait([]int{256, 400}), "h > w is portrait")
	assert.False(t, portrait([]int{800, 600}), "landscape is not")
	assert.False(t, portrait([]int{500, 500}), "square is not")
	assert.False(t, portrait(nil), "unknown dims are never pinned")
	assert.False(t, portrait([]int{0, 400}), "a zero dimension is never pinned")
	assert.False(t, portrait([]int{1, 2, 3}), "a malformed dims array is never pinned")
}

func TestShapeLabel(t *testing.T) {
	assert.Equal(t, "portrait", shapeLabel([]int{256, 400}))
	assert.Equal(t, "landscape", shapeLabel([]int{800, 600}))
	assert.Equal(t, "landscape", shapeLabel([]int{500, 500}))
	assert.Equal(t, "unknown", shapeLabel(nil))
}

func TestIDFilter(t *testing.T) {
	one, err := json.Marshal(idFilter([]string{"v17"}))
	require.NoError(t, err)
	assert.JSONEq(t, `["id","=","v17"]`, string(one))

	many, err := json.Marshal(idFilter([]string{"v17", "v99"}))
	require.NoError(t, err)
	assert.JSONEq(t, `["or",["id","=","v17"],["id","=","v99"]]`, string(many))
}

func TestBuildPlanClassifiesEveryCandidate(t *testing.T) {
	resp, err := parseVNResponse(strings.NewReader(canned))
	require.NoError(t, err)
	images := map[string]*vnImage{}
	for _, r := range resp.Results {
		images[r.ID] = r.Image
	}

	cands := []candidate{
		{WorkID: 1, VNDBID: "v17"},
		{WorkID: 2, VNDBID: "v99"},
		{WorkID: 3, VNDBID: "v250"},
		{WorkID: 4, VNDBID: "v4444"},
	}
	stats := &Stats{Candidates: len(cands)}
	plan := buildPlan(cands, images, stats)
	require.Len(t, plan, 4)

	assert.Equal(t, 1, stats.Portrait)
	assert.Equal(t, 1, stats.Landscape)
	assert.Equal(t, 2, stats.Planned)
	assert.Equal(t, 2, stats.NoImage)

	assert.True(t, plan[0].actionable())
	assert.Equal(t, "no-image", plan[2].Reason)
	assert.Equal(t, "vn-unknown", plan[3].Reason)

	assert.Len(t, actionable(plan, 0), 2)
	assert.Len(t, actionable(plan, 1), 1)
	assert.Equal(t, int64(1), actionable(plan, 1)[0].WorkID)
}

func TestBuildPlanTreatsAnEmptyURLAsNoImage(t *testing.T) {
	stats := &Stats{}
	plan := buildPlan(
		[]candidate{{WorkID: 7, VNDBID: "v7"}},
		map[string]*vnImage{"v7": {URL: "  ", Dims: []int{256, 400}}},
		stats)
	require.Len(t, plan, 1)
	assert.False(t, plan[0].actionable())
	assert.Equal(t, "no-image", plan[0].Reason)
	assert.Equal(t, 1, stats.NoImage)
	assert.Equal(t, 0, stats.Planned)
}

func TestAnchorIDsDeduplicates(t *testing.T) {
	ids := anchorIDs([]candidate{
		{WorkID: 1, VNDBID: "v17"},
		{WorkID: 2, VNDBID: "v17"},
		{WorkID: 3, VNDBID: " "},
		{WorkID: 4, VNDBID: "v9"},
	})
	assert.Equal(t, []string{"v17", "v9"}, ids)
}

func TestParseIDs(t *testing.T) {
	got, err := ParseIDs("1, 2 ,3,")
	require.NoError(t, err)
	assert.Equal(t, []int64{1, 2, 3}, got)

	got, err = ParseIDs("")
	require.NoError(t, err)
	assert.Empty(t, got)

	_, err = ParseIDs("1,abc")
	assert.Error(t, err)
	_, err = ParseIDs("0")
	assert.Error(t, err)
}

func TestCoverFilename(t *testing.T) {
	assert.Equal(t, "17.jpg", coverFilename("https://t.vndb.org/cv/50/17.jpg"))
	assert.Equal(t, "17.jpg", coverFilename("https://t.vndb.org/cv/50/17.jpg?v=2"))
	assert.Equal(t, "cover.jpg", coverFilename("https://t.vndb.org/"))
}

func TestRetryAfter(t *testing.T) {
	const def = 30 * time.Second
	assert.Equal(t, 12*time.Second, retryAfter("12", def), "delta-seconds is honoured")
	assert.Equal(t, def, retryAfter("", def), "an absent header falls back")
	assert.Equal(t, def, retryAfter("Wed, 21 Oct 2026 07:28:00 GMT", def), "the HTTP-date form falls back")
}
