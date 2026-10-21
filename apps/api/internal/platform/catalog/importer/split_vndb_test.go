package importer

import (
	"testing"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVNDBReleasesSkipAReleaseSplitOffItsVN(t *testing.T) {
	clean(t)
	cleanReleases(t)
	require.NoError(t, testDB.Exec(`TRUNCATE catalog_split_request, catalog_match_candidate, catalog_match_rejection RESTART IDENTITY CASCADE`).Error)
	workID := seedVNDBWork(t, "v751")
	for _, r := range []struct{ rid, title string }{{"r1513", "Rewrite"}, {"r45775", "Rewrite+"}} {
		insRelease(t, r.rid, "ja", 20160729, nil, false, false, true)
		insReleaseVN(t, r.rid, "v751", "complete")
		insReleaseTitle(t, r.rid, "ja", false, r.title, "")
	}
	first, err := New(testDB, nil, Options{}).RunReleases()
	require.NoError(t, err)
	require.Equal(t, 2, first.ReleasesWritten)

	var plus int64
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_release WHERE extra->>'vndb_id' = 'r45775'`).Scan(&plus).Error)
	splits := service.NewSplitService(testDB, nil, service.NewMergeService(testDB, nil))
	r, err := splits.Request(t.Context(), service.SplitInput{
		WorkID: workID, ReleaseIDs: []int64{plus}, DisplayName: "Rewrite+", OLang: "ja",
		Titles: []model.SplitTitle{{Lang: "ja", Title: "Rewrite+"}},
	}, 3)
	require.NoError(t, err)
	require.NoError(t, splits.Approve(t.Context(), r.ID, 7))
	require.NoError(t, testDB.Exec(`UPDATE catalog_split_request SET execute_after = now() - interval '1 minute' WHERE id = ?`, r.ID).Error)
	_, err = splits.Execute(t.Context(), r.ID, nil)
	require.NoError(t, err)

	st, err := New(testDB, nil, Options{}).RunReleases()
	require.NoError(t, err)
	assert.Zero(t, st.ReleasesWritten, "the split-off release is not minted again on its VN's work")
	assert.Equal(t, 1, st.SkippedRetired, "the tombstone the split left counts as a retired holder")
	assert.Equal(t, int64(1), countWhere(t, `SELECT count(*) FROM catalog_release WHERE extra->>'vndb_id' = 'r45775' AND deleted_at IS NULL`))
	assert.Equal(t, int64(0), countWhere(t, `SELECT count(*) FROM catalog_release WHERE extra->>'vndb_id' = 'r45775' AND deleted_at IS NULL AND work_id = ?`, workID))
}
