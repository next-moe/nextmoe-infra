package egworks

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrimaryOrder(t *testing.T) {
	orig := game{ID: 300, Model: "NS", Sellday: "2024-01-01", Transplant: false}
	port := game{ID: 100, Model: "PC", Sellday: "2000-01-01", Transplant: true}
	assert.True(t, primaryLess(orig, port))
	assert.False(t, primaryLess(port, orig))

	pc := game{ID: 300, Model: "PC", Sellday: "2024-01-01"}
	ns := game{ID: 100, Model: "NS", Sellday: "2000-01-01"}
	assert.True(t, primaryLess(pc, ns))
	assert.False(t, primaryLess(ns, pc))

	early := game{ID: 300, Model: "PC", Sellday: "2000-01-01"}
	late := game{ID: 100, Model: "PC", Sellday: "2024-01-01"}
	empty := game{ID: 200, Model: "PC", Sellday: ""}
	assert.True(t, primaryLess(early, late))
	assert.True(t, primaryLess(late, empty))
	assert.True(t, primaryLess(early, empty))

	low := game{ID: 100, Model: "PC", Sellday: "2000-01-01"}
	high := game{ID: 300, Model: "PC", Sellday: "2000-01-01"}
	assert.True(t, primaryLess(low, high))
	got := choosePrimary([]mintCand{{game: high}, {game: low}})
	assert.Equal(t, int64(100), got.game.ID)
}

func TestGroupsAreTransitive(t *testing.T) {
	always := func(int, int) bool { return true }
	groups := groupByKeyLists([][]string{
		{"a"},
		{"a", "b"},
		{"b"},
		{"c"},
	}, always)
	require.Len(t, groups, 2)
	assert.Equal(t, []int{0, 1, 2}, groups[0])
	assert.Equal(t, []int{3}, groups[1])
}

func TestGroupsJoinOnlyCompatibleMembers(t *testing.T) {
	groups := groupByKeyLists([][]string{{"a"}, {"a"}, {"a"}}, func(i, j int) bool {
		return (i == 2 && j == 0) || (i == 0 && j == 2)
	})
	require.Len(t, groups, 2)
	assert.Equal(t, []int{0, 2}, groups[0])
	assert.Equal(t, []int{1}, groups[1])
}

func TestFoldNeedsBrandOrNearDate(t *testing.T) {
	assert.True(t, foldable(game{BrandID: 7, Sellday: "1992-05-22"}, game{BrandID: 7, Sellday: "2020-01-01"}))
	assert.True(t, foldable(game{BrandID: 1, Sellday: "1994-05-27"}, game{BrandID: 2, Sellday: "1995-10-27"}))
	assert.False(t, foldable(game{BrandID: 1, Sellday: "1992-05-22"}, game{BrandID: 2, Sellday: "1998-07-10"}))
	assert.False(t, foldable(game{BrandID: 1, Sellday: ""}, game{BrandID: 2, Sellday: "1998-07-10"}))
	assert.False(t, foldable(game{BrandID: 0, Sellday: ""}, game{BrandID: 0, Sellday: ""}))
	assert.True(t, foldable(game{BrandID: 1, Sellday: "2020-01-01"}, game{BrandID: 2, Sellday: "2021-12-31"}))
	assert.False(t, foldable(game{BrandID: 1, Sellday: "2020-01-01"}, game{BrandID: 2, Sellday: "2022-01-02"}))
}
