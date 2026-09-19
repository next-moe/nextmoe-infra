package vndbtags

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedEndToEnd(t *testing.T) (mainID, multiID, missingID, orphanID, mergedID int64) {
	t.Helper()
	vndb := sourceID(t, "vndb")
	curated := sourceID(t, "curated")
	bangumi := sourceID(t, "bangumi")

	mkSrcTag(t, "g-keep", "cont", "zz-keep", "", 0)
	mkSrcTag(t, "g-upd", "cont", "zz-upd", "", 0)
	mkSrcTag(t, "g-ins", "ero", "zz-ins", "", 0)
	mkSrcTag(t, "g-multi", "cont", "zz-multi", "", 0)

	mkVN(t, "v-main")
	mkVN(t, "v-a")
	mkVN(t, "v-b")
	mkVN(t, "v-merged")

	mainID = mkWork(t, "main")
	mkAnchor(t, mainID, "v-main", model.LinkKindExact)
	mkWorkTag(t, mainID, "zz-keep", vndb, 2, 0, false)
	mkWorkTag(t, mainID, "zz-upd", vndb, 1, 0, true)
	mkWorkTag(t, mainID, "zz-gone", vndb, 5, 0, false)
	mkWorkTag(t, mainID, "curated-stay", curated, 1, 0, false)
	mkWorkTag(t, mainID, "bangumi-stay", bangumi, 1, 0, false)
	mkKeptVotes(t, "g-keep", "v-main", 2, 0)
	mkKeptVotes(t, "g-upd", "v-main", 3, 0)
	mkKeptVotes(t, "g-ins", "v-main", 2, 0)

	multiID = mkWork(t, "multi")
	mkAnchor(t, multiID, "v-a", model.LinkKindExact)
	mkAnchor(t, multiID, "v-b", model.LinkKindExact)
	mkWorkTag(t, multiID, "zz-multi", vndb, 9, 2, false)
	mkWorkTag(t, multiID, "curated-stay", curated, 1, 0, false)
	mkKeptVotes(t, "g-multi", "v-a", 2, 0)
	mkKeptVotes(t, "g-multi", "v-b", 2, 0)

	missingID = mkWork(t, "missing")
	mkAnchor(t, missingID, "v-absent", model.LinkKindExact)
	mkWorkTag(t, missingID, "zz-missing", vndb, 4, 1, true)

	orphanID = mkWork(t, "orphan")
	mkDeadAnchor(t, orphanID, "v-dead")
	mkWorkTag(t, orphanID, "zz-orphan", vndb, 3, 0, false)
	mkWorkTag(t, orphanID, "curated-stay", curated, 1, 0, false)
	mkWorkTag(t, orphanID, "bangumi-stay", bangumi, 1, 0, false)

	mergedID = mkWorkStatus(t, "merged", model.WorkStatusMerged)
	mkAnchor(t, mergedID, "v-merged", model.LinkKindExact)
	mkWorkTag(t, mergedID, "zz-merged", vndb, 8, 0, false)
	mkKeptVotes(t, "g-ins", "v-merged", 2, 0)
	return mainID, multiID, missingID, orphanID, mergedID
}

func TestApplyEndToEnd(t *testing.T) {
	requireDB(t)
	vndb := sourceID(t, "vndb")
	curated := sourceID(t, "curated")
	bangumi := sourceID(t, "bangumi")
	mainID, multiID, missingID, orphanID, mergedID := seedEndToEnd(t)
	require.NoError(t, testDB.Exec(`UPDATE catalog_work SET updated_at = now() - interval '1 day'`).Error)

	st := runJob(t, true, "")
	assert.Equal(t, int64(2), touchedToday(t, mainID, orphanID), "a work whose tags changed is touched for the search index")
	assert.Zero(t, touchedToday(t, multiID, missingID, mergedID))
	assert.Equal(t, 1, st.WorksPopulation)
	assert.Equal(t, 1, st.WorksMultiAnchor)
	assert.Equal(t, 1, st.WorksVNMissing)
	assert.Equal(t, 1, st.WorksChanged)
	assert.Equal(t, 1, st.OrphanWorks)
	assert.Equal(t, 3, st.TagsDesired)
	assert.Equal(t, 1, st.TagsSame)
	assert.Equal(t, 1, st.TagsInserted)
	assert.Equal(t, 1, st.TagsUpdated)
	assert.Equal(t, 1, st.TagsDeleted)
	assert.Zero(t, st.TagsLost)
	assert.Equal(t, 1, st.OrphanRowsRemoved)
	assert.Zero(t, st.Errors)

	main := loadWorkTags(t, mainID, vndb)
	require.Len(t, main, 3)
	byName := map[string]model.CatalogWorkTag{}
	for _, row := range main {
		byName[row.Name] = row
	}
	assert.Equal(t, 2, byName["zz-keep"].Count)
	assert.EqualValues(t, 0, byName["zz-keep"].Spoiler)
	assert.Equal(t, 3, byName["zz-upd"].Count)
	assert.EqualValues(t, 0, byName["zz-upd"].Spoiler)
	assert.True(t, byName["zz-upd"].Sexual, "an update must not rewrite sexual")
	assert.Equal(t, 2, byName["zz-ins"].Count)
	assert.True(t, byName["zz-ins"].Sexual, "new ero name takes ero")
	_, gone := byName["zz-gone"]
	assert.False(t, gone)

	assert.Len(t, loadWorkTags(t, mainID, curated), 1)
	assert.Len(t, loadWorkTags(t, mainID, bangumi), 1)

	multi := loadWorkTags(t, multiID, vndb)
	require.Len(t, multi, 1)
	assert.Equal(t, "zz-multi", multi[0].Name)
	assert.Equal(t, 9, multi[0].Count)

	missing := loadWorkTags(t, missingID, vndb)
	require.Len(t, missing, 1)
	assert.Equal(t, "zz-missing", missing[0].Name)
	assert.True(t, missing[0].Sexual)

	assert.Empty(t, loadWorkTags(t, orphanID, vndb))
	assert.Len(t, loadWorkTags(t, orphanID, curated), 1)
	assert.Len(t, loadWorkTags(t, orphanID, bangumi), 1)

	merged := loadWorkTags(t, mergedID, vndb)
	require.Len(t, merged, 1)
	assert.Equal(t, "zz-merged", merged[0].Name)
	assert.Equal(t, 8, merged[0].Count)
}

func TestSecondApplyWritesNothing(t *testing.T) {
	requireDB(t)
	seedEndToEnd(t)
	_ = runJob(t, true, "")
	st := runJob(t, true, "")
	assert.Equal(t, 1, st.WorksPopulation)
	assert.Equal(t, 3, st.TagsSame)
	assert.Zero(t, st.WorksChanged)
	assert.Zero(t, st.TagsInserted)
	assert.Zero(t, st.TagsUpdated)
	assert.Zero(t, st.TagsDeleted)
	assert.Zero(t, st.TagsLost)
	assert.Zero(t, st.OrphanWorks)
	assert.Zero(t, st.OrphanRowsRemoved)
	assert.Zero(t, st.SexualInherited)
	assert.Zero(t, st.Errors)
}

func TestDryRunMatchesApply(t *testing.T) {
	requireDB(t)
	seedEndToEnd(t)
	before := tagCount(t)
	dry := runJob(t, false, "")
	assert.Equal(t, before, tagCount(t), "dry run must write nothing")
	apply := runJob(t, true, "")
	assert.Equal(t, dry.WorksPopulation, apply.WorksPopulation)
	assert.Equal(t, dry.WorksMultiAnchor, apply.WorksMultiAnchor)
	assert.Equal(t, dry.WorksVNMissing, apply.WorksVNMissing)
	assert.Equal(t, dry.WorksChanged, apply.WorksChanged)
	assert.Equal(t, dry.OrphanWorks, apply.OrphanWorks)
	assert.Equal(t, dry.TagsDesired, apply.TagsDesired)
	assert.Equal(t, dry.TagsSame, apply.TagsSame)
	assert.Equal(t, dry.TagsInserted, apply.TagsInserted)
	assert.Equal(t, dry.TagsUpdated, apply.TagsUpdated)
	assert.Equal(t, dry.TagsDeleted, apply.TagsDeleted)
	assert.Equal(t, dry.OrphanRowsRemoved, apply.OrphanRowsRemoved)
	assert.Zero(t, dry.TagsLost)
	assert.Zero(t, apply.TagsLost)
}

func TestMirrorGuardRefuses(t *testing.T) {
	requireDB(t)
	vndb := sourceID(t, "vndb")
	w := mkWork(t, "guard")
	mkDeadAnchor(t, w, "v-none")
	mkWorkTag(t, w, "zz-guard", vndb, 1, 0, false)
	before := tagCount(t)

	for _, apply := range []bool{false, true} {
		_, err := Run(context.Background(), Opts{
			Apply: apply, DSN: testDSN, MinMirrorRows: 1_000_000,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "min-mirror-rows")
		assert.Equal(t, before, tagCount(t), "guard must write nothing (apply=%v)", apply)
	}
}

func TestGuardedUpdateAndDeleteLost(t *testing.T) {
	requireDB(t)
	vndb := sourceID(t, "vndb")
	w := mkWork(t, "lost")
	updID := mkWorkTag(t, w, "zz-upd", vndb, 3, 0, true)
	delID := mkWorkTag(t, w, "zz-del", vndb, 4, 1, false)

	n, _, err := applyUpdate(testDB, w, vndb, Update{
		ID: updID, Name: "zz-upd",
		OldSpoiler: 0, NewSpoiler: 1, OldCount: 1, NewCount: 3, Sexual: false,
	})
	require.NoError(t, err)
	assert.Zero(t, n)
	row := loadWorkTags(t, w, vndb)
	var upd model.CatalogWorkTag
	for _, r := range row {
		if r.ID == updID {
			upd = r
		}
	}
	assert.Equal(t, 3, upd.Count)
	assert.EqualValues(t, 0, upd.Spoiler)
	assert.True(t, upd.Sexual)

	n, _, err = applyDelete(testDB, w, vndb, Delete{
		ID: delID, Name: "zz-del", Spoiler: 1, Count: 9,
	}, "delete")
	require.NoError(t, err)
	assert.Zero(t, n)
	assert.Len(t, loadWorkTags(t, w, vndb), 2)
}

func TestReceipts(t *testing.T) {
	requireDB(t)
	dir := t.TempDir()
	dryPath := filepath.Join(dir, "dry.jsonl")
	seedEndToEnd(t)
	_ = runJob(t, false, dryPath)
	_, err := os.Stat(dryPath)
	assert.ErrorIs(t, err, os.ErrNotExist)

	applyPath := filepath.Join(dir, "apply.jsonl")
	st := runJob(t, true, applyPath)
	f, err := os.Open(applyPath)
	require.NoError(t, err)
	defer f.Close()
	var recs []receipt
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var rec receipt
		require.NoError(t, json.Unmarshal(sc.Bytes(), &rec))
		recs = append(recs, rec)
	}
	require.NoError(t, sc.Err())
	assert.Equal(t, st.TagsInserted+st.TagsUpdated+st.TagsDeleted+st.OrphanRowsRemoved, len(recs))
	ops := map[string]int{}
	for _, rec := range recs {
		ops[rec.Op]++
		assert.NotZero(t, rec.WorkID)
		assert.NotEmpty(t, rec.Name)
	}
	assert.Equal(t, 1, ops["insert"])
	assert.Equal(t, 1, ops["update"])
	assert.Equal(t, 1, ops["delete"])
	assert.Equal(t, 1, ops["orphan_delete"])

	emptyPath := filepath.Join(dir, "empty.jsonl")
	st2 := runJob(t, true, emptyPath)
	assert.Zero(t, st2.TagsInserted+st2.TagsUpdated+st2.TagsDeleted+st2.OrphanRowsRemoved)
	_, err = os.Stat(emptyPath)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestMergedWorkIsNeverAnOrphan(t *testing.T) {
	requireDB(t)
	vndb := sourceID(t, "vndb")
	merged := mkWorkStatus(t, "merged-orphan", model.WorkStatusMerged)
	mkWorkTag(t, merged, "zz-merged-stays", vndb, 2, 0, false)
	mkSrcTag(t, "g-other", "cont", "zz-other", "", 0)
	mkKeptVotes(t, "g-other", "v-unanchored", 1, 0)

	st := runJob(t, true, "")
	assert.Zero(t, st.OrphanWorks)
	assert.Zero(t, st.OrphanRowsRemoved)
	assert.Len(t, loadWorkTags(t, merged, vndb), 1)
}

func TestWritePlansCountsLostInsert(t *testing.T) {
	requireDB(t)
	vndb := sourceID(t, "vndb")
	w := mkWork(t, "raced")
	mkWorkTag(t, w, "zz-raced", vndb, 2, 0, false)

	out, err := writePlans(testDB, vndb, []workPlan{{
		workID: w,
		plan:   Plan{Inserts: []Insert{{Name: "zz-raced", Spoiler: 0, Count: 5}}},
	}})
	require.NoError(t, err)
	assert.Equal(t, 1, out.lost, "the row was written by someone else after the read")
	assert.Zero(t, out.inserted)
	assert.Empty(t, out.touched)
	assert.Empty(t, out.recs)
}

func TestApplyInheritsSexualFromAnotherWork(t *testing.T) {
	requireDB(t)
	vndb := sourceID(t, "vndb")
	mkSrcTag(t, "g-x", "cont", "zz-x", "", 0)
	mkVN(t, "v-x")
	mkVN(t, "v-y1")
	mkVN(t, "v-y2")

	plain := mkWork(t, "plain")
	mkAnchor(t, plain, "v-x", model.LinkKindExact)
	mkKeptVotes(t, "g-x", "v-x", 2, 0)
	mkWorkTag(t, plain, "zz-x", vndb, 2, 0, false)

	flagged := mkWork(t, "flagged")
	mkAnchor(t, flagged, "v-y1", model.LinkKindExact)
	mkAnchor(t, flagged, "v-y2", model.LinkKindExact)
	mkWorkTag(t, flagged, "zz-x", vndb, 2, 0, true)

	st := runJob(t, true, "")
	assert.Equal(t, 1, st.SexualInherited)
	rows := loadWorkTags(t, plain, vndb)
	require.Len(t, rows, 1)
	assert.True(t, rows[0].Sexual, "a name flagged sexual on any work flags it everywhere")
}

func touchedToday(t *testing.T, ids ...int64) int64 {
	t.Helper()
	var n int64
	require.NoError(t, testDB.Raw(
		`SELECT count(*) FROM catalog_work WHERE id IN ? AND updated_at > now() - interval '1 hour'`, ids).Scan(&n).Error)
	return n
}
