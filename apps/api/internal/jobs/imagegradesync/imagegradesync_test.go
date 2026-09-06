package imagegradesync

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMapLevel(t *testing.T) {
	cases := []struct {
		level int
		want  int16
		ok    bool
	}{
		{0, 0, true},
		{1, 1, true},
		{2, 2, true},
		{3, 2, true},
		{4, 0, false},
		{-1, 0, false},
	}
	for _, c := range cases {
		got, ok := mapLevel(c.level)
		assert.Equal(t, c.ok, ok, "level %d", c.level)
		assert.Equal(t, c.want, got, "level %d", c.level)
	}
}

func registry() []sourceRow {
	return []sourceRow{
		{ID: 1, Key: "user"},
		{ID: 2, Key: "vndb"},
		{ID: 3, Key: "bangumi"},
		{ID: 4, Key: "dlsite"},
		{ID: 12, Key: "curated"},
		{ID: 13, Key: "upscale"},
		{ID: 17, Key: "getchu"},
		{ID: 21, Key: "censored"},
		{ID: 99, Key: "some_future_source"},
	}
}

func TestBuildScopeExcludesHumanAuthoredAndKeepsFutureSources(t *testing.T) {
	sc, err := buildScope(registry(), "")
	require.NoError(t, err)
	assert.ElementsMatch(t, []int16{3, 4, 13, 17, 99}, sc.ids)
	assert.Equal(t, "some_future_source", sc.names[99])
	for _, id := range []int16{1, 2, 12, 21} {
		assert.NotContains(t, sc.names, id)
	}
}

func TestBuildScopeSourceFilter(t *testing.T) {
	sc, err := buildScope(registry(), "getchu")
	require.NoError(t, err)
	assert.Equal(t, []int16{17}, sc.ids)

	_, err = buildScope(registry(), "vndb")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "human-authored")

	_, err = buildScope(registry(), "nope")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown --source")
}

func TestBuildScopeRefusesWhenAHumanSourceIsNotRegistered(t *testing.T) {
	var rows []sourceRow
	for _, r := range registry() {
		if r.Key == "curated" {
			continue
		}
		rows = append(rows, r)
	}
	_, err := buildScope(rows, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "curated")
}

func TestStatsMatrixIsSortedAndCounted(t *testing.T) {
	st := &Stats{}
	st.note("getchu", "catalog_work_screenshot", 2, 0)
	st.note("getchu", "catalog_work_screenshot", 2, 0)
	st.note("bangumi", "catalog_work_cover", 0, 2)
	changes := st.Changes()
	require.Len(t, changes, 2)
	assert.Equal(t, Change{Source: "bangumi", Table: "catalog_work_cover", From: 0, To: 2, Count: 1}, changes[0])
	assert.Equal(t, Change{Source: "getchu", Table: "catalog_work_screenshot", From: 2, To: 0, Count: 2}, changes[1])
	assert.Contains(t, st.Matrix(), "2 -> 0   2")
	assert.Equal(t, "no sexual value would change", (&Stats{}).Matrix())
}
