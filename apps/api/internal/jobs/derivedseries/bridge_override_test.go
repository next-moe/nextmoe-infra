package derivedseries

import (
	"context"
	"fmt"
	"testing"

	"api/internal/jobs/seriesorder"

	"github.com/stretchr/testify/require"
)

func runApply(t *testing.T) *Stats {
	t.Helper()
	st, err := RunWithDB(context.Background(), testDB, Opts{Apply: true})
	require.NoError(t, err)
	return st
}

func seriesNames(t *testing.T) map[string]string {
	t.Helper()
	var rows []struct {
		ExternalID  string `gorm:"column:external_id"`
		DisplayName string `gorm:"column:display_name"`
	}
	require.NoError(t, testDB.Raw(`
		SELECT s.external_id, s.display_name FROM catalog_series s
		JOIN catalog_source src ON src.id = s.source_id WHERE src.key = 'derived'`).
		Scan(&rows).Error)
	out := map[string]string{}
	for _, r := range rows {
		out[r.ExternalID] = r.DisplayName
	}
	return out
}

func requireQuiescent(t *testing.T) {
	t.Helper()
	st := runApply(t)
	require.Zero(t, st.SeriesCreated)
	require.Zero(t, st.SeriesRenamed)
	require.Zero(t, st.SeriesDeleted)
	require.Zero(t, st.MembersAdded)
	require.Zero(t, st.MembersStale)
	require.Zero(t, st.OrderChanged)
	require.Zero(t, st.TouchedWorks)
}

func TestBridge_CrossoverIsCutAndPublishedInNeitherLine(t *testing.T) {
	clean(t)
	a1 := mkWork(t, "Alpha First", 2000)
	a2 := mkWork(t, "Alpha Second", 2001)
	b1 := mkWork(t, "Beta First", 2002)
	b2 := mkWork(t, "Beta Second", 2003)
	x := mkWork(t, "Crossover Festival", 2004)
	mkEdge(t, a2, a1, seriesorder.RelSequelOf)
	mkEdge(t, b2, b1, seriesorder.RelSequelOf)
	mkEdge(t, x, a1, seriesorder.RelFandiscOf)
	mkEdge(t, x, b1, seriesorder.RelFandiscOf)

	st := runApply(t)
	require.Equal(t, 1, st.Bridges)
	require.Equal(t, 2, st.BridgeEdgesCut)
	require.Equal(t, 2, st.SeriesCreated)

	var got []int64
	require.NoError(t, testDB.Raw(`SELECT work_id FROM catalog_series_member`).Scan(&got).Error)
	require.ElementsMatch(t, []int64{a1, a2, b1, b2}, got)

	requireQuiescent(t)
}

func TestBridge_HubBaseGameKeepsItsFamily(t *testing.T) {
	clean(t)
	base := mkWork(t, "Wagamama Base", 2000)
	f1 := mkWork(t, "Wagamama OC", 2001)
	f2 := mkWork(t, "Tokusouban Tokuten", 2002)
	mkEdge(t, f1, base, seriesorder.RelFandiscOf)
	mkEdge(t, f2, base, seriesorder.RelFandiscOf)

	st := runApply(t)
	require.Zero(t, st.Bridges)
	require.Equal(t, 1, st.SeriesCreated)
	var n int
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_series_member`).Scan(&n).Error)
	require.Equal(t, 3, n)
}

func TestBridge_SameNameTargetsAreNotSeparateLines(t *testing.T) {
	clean(t)
	t1 := mkWork(t, "DALK", 1992)
	t2 := mkWork(t, "Dalk Renewal", 2004)
	x := mkWork(t, "DALK Gaiden", 1994)
	mkEdge(t, x, t1, seriesorder.RelSideStoryOf)
	mkEdge(t, x, t2, seriesorder.RelSideStoryOf)

	st := runApply(t)
	require.Zero(t, st.Bridges)
	require.Equal(t, 1, st.SeriesCreated)
	var n int
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_series_member`).Scan(&n).Error)
	require.Equal(t, 3, n)
}

func TestOverride_WinsWhileMembershipHolds(t *testing.T) {
	clean(t)
	a := mkWork(t, "Magical Kanan Two Bergamot", 2003)
	b := mkWork(t, "Zenryaku Hakoirimusume", 2005)
	mkEdge(t, b, a, seriesorder.RelSequelOf)
	runApply(t)

	ext := fmt.Sprintf("comp:%d", min64(a, b))
	require.NoError(t, testDB.Exec(`
		INSERT INTO catalog_series_name_override (source_id, external_id, member_hash, display_name, reviewed_by)
		SELECT id, ?, ?, 'Magical Kanan', 'test' FROM catalog_source WHERE key = 'derived'`,
		ext, MemberHash([]int64{a, b})).Error)

	st := runApply(t)
	require.Equal(t, 1, st.NamedByOverride)
	require.Equal(t, 1, st.SeriesRenamed)
	require.Zero(t, st.OverridesStale)
	require.Equal(t, "Magical Kanan", seriesNames(t)[ext])

	requireQuiescent(t)
}

func TestOverride_StaleHashFallsBackAndIsReaped(t *testing.T) {
	clean(t)
	a := mkWork(t, "Line Entry One", 2000)
	b := mkWork(t, "Line Entry Two", 2001)
	mkEdge(t, b, a, seriesorder.RelSequelOf)
	runApply(t)

	ext := fmt.Sprintf("comp:%d", min64(a, b))
	require.NoError(t, testDB.Exec(`
		INSERT INTO catalog_series_name_override (source_id, external_id, member_hash, display_name, reviewed_by)
		SELECT id, ?, 'not-the-current-hash', 'Stale Name', 'test' FROM catalog_source WHERE key = 'derived'`,
		ext).Error)

	st := runApply(t)
	require.Zero(t, st.NamedByOverride)
	require.Equal(t, 1, st.OverridesStale)
	require.Equal(t, "Line Entry", seriesNames(t)[ext])

	var left int
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_series_name_override`).Scan(&left).Error)
	require.Zero(t, left)
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
