package anchorliveness

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditMarksDeadAndClearsRevived(t *testing.T) {
	requireDB(t)
	clean(t)
	ctx := context.Background()
	ln := withFloor(findLane(t, SourceVNDB, EntityWork), 5)
	src := sourceID(t, SourceVNDB)
	gone := mkEntity(t, model.EntityTypeWork, "deleted upstream")
	revived := mkEntity(t, model.EntityTypeWork, "restored upstream")
	healthy := mkEntity(t, model.EntityTypeWork, "always fine")
	mkRef(t, model.EntityTypeWork, model.LinkKindExact, gone, src, "vgone", false)
	mkRef(t, model.EntityTypeWork, model.LinkKindExact, revived, src, "vback", true)
	mkRef(t, model.EntityTypeWork, model.LinkKindExact, healthy, src, "vlive", false)
	seedMembership(t, ln, "vfill1", "vfill2", "vfill3", "vfill4", "vfill5",
		"vfill6", "vfill7", "vfill8", "vfill9", "vfill10", "vback", "vlive")

	st, err := RunWithDB(ctx, testDB, testEG, runOpts(t, SourceVNDB, true, []Lane{ln}))
	require.NoError(t, err)
	assert.EqualValues(t, 3, st.Per[EntityWork].Refs)
	assert.EqualValues(t, 1, st.Per[EntityWork].Mark)
	assert.EqualValues(t, 1, st.Per[EntityWork].Clear)
	assert.NotNil(t, deadAt(t, model.EntityTypeWork, gone, src, "vgone"))
	assert.Nil(t, deadAt(t, model.EntityTypeWork, revived, src, "vback"))
	assert.Nil(t, deadAt(t, model.EntityTypeWork, healthy, src, "vlive"))

	st, err = RunWithDB(ctx, testDB, testEG, runOpts(t, SourceVNDB, true, []Lane{ln}))
	require.NoError(t, err)
	assert.EqualValues(t, 0, st.Per[EntityWork].Mark)
	assert.EqualValues(t, 0, st.Per[EntityWork].Clear)
	assert.EqualValues(t, 0, st.MarkedTotal)
	assert.EqualValues(t, 0, st.ClearedTotal)
}

func TestAuditDryRunWritesNothing(t *testing.T) {
	requireDB(t)
	clean(t)
	ctx := context.Background()
	ln := withFloor(findLane(t, SourceVNDB, EntityWork), 5)
	src := sourceID(t, SourceVNDB)
	w := mkEntity(t, model.EntityTypeWork, "deleted upstream")
	mkRef(t, model.EntityTypeWork, model.LinkKindExact, w, src, "vgone", false)
	seedMembership(t, ln, "vfill1", "vfill2", "vfill3", "vfill4", "vfill5",
		"vfill6", "vfill7", "vfill8", "vfill9", "vfill10")

	st, err := RunWithDB(ctx, testDB, testEG, runOpts(t, SourceVNDB, false, []Lane{ln}))
	require.NoError(t, err)
	assert.EqualValues(t, 1, st.Per[EntityWork].Mark)
	assert.EqualValues(t, 0, st.Per[EntityWork].Clear)
	assert.Nil(t, deadAt(t, model.EntityTypeWork, w, src, "vgone"))

	st, err = RunWithDB(ctx, testDB, testEG, runOpts(t, SourceVNDB, true, []Lane{ln}))
	require.NoError(t, err)
	assert.EqualValues(t, 1, st.Per[EntityWork].Mark)
	assert.NotNil(t, deadAt(t, model.EntityTypeWork, w, src, "vgone"))
}

func TestAuditRefusesUndersizedMirrorOnDryAndApply(t *testing.T) {
	requireDB(t)
	clean(t)
	ctx := context.Background()
	ln := withFloor(findLane(t, SourceVNDB, EntityWork), 50_000)
	src := sourceID(t, SourceVNDB)
	w := mkEntity(t, model.EntityTypeWork, "would be wrongly killed")
	mkRef(t, model.EntityTypeWork, model.LinkKindExact, w, src, "vgone", false)
	seedMembership(t, ln, "a", "b", "c")

	_, err := RunWithDB(ctx, testDB, testEG, runOpts(t, SourceVNDB, true, []Lane{ln}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing")
	assert.Nil(t, deadAt(t, model.EntityTypeWork, w, src, "vgone"))

	st, err := RunWithDB(ctx, testDB, testEG, runOpts(t, SourceVNDB, false, []Lane{ln}))
	require.Error(t, err, "guards are evaluated in the dry run too")
	assert.Contains(t, err.Error(), "refusing")
	assert.EqualValues(t, 1, st.LanesRefused)
	assert.EqualValues(t, 1, st.Per[EntityWork].Mark)
	assert.Nil(t, deadAt(t, model.EntityTypeWork, w, src, "vgone"))
}

func TestPerSourceMarkClearKindsAndOtherSource(t *testing.T) {
	requireDB(t)
	ctx := context.Background()
	dlsite := sourceID(t, "dlsite")
	for _, ln := range AllLanes() {
		t.Run(ln.Source+"/"+ln.Entity, func(t *testing.T) {
			clean(t)
			ln = withFloor(ln, 1)
			src := sourceID(t, ln.Source)
			gone, back, live := sampleIDs(ln)
			g := mkEntity(t, ln.Type, "gone")
			b := mkEntity(t, ln.Type, "back")
			h := mkEntity(t, ln.Type, "healthy")
			p := mkEntity(t, ln.Type, "probable")
			r := mkEntity(t, ln.Type, "related")
			o := mkEntity(t, ln.Type, "other-source")
			mkRef(t, ln.Type, model.LinkKindExact, g, src, gone, false)
			mkRef(t, ln.Type, model.LinkKindExact, b, src, back, true)
			mkRef(t, ln.Type, model.LinkKindExact, h, src, live, false)
			mkRef(t, ln.Type, model.LinkKindProbable, p, src, gone+"p", false)
			mkRef(t, ln.Type, model.LinkKindRelated, r, src, gone+"r", false)
			mkRef(t, ln.Type, model.LinkKindExact, o, dlsite, gone, false)
			seedMembership(t, ln, back, live)

			st, err := RunWithDB(ctx, testDB, testEG, runOpts(t, ln.Source, true, []Lane{ln}))
			require.NoError(t, err)
			assert.EqualValues(t, 3, st.Per[ln.Entity].Mark, "exact gone + probable + related")
			assert.EqualValues(t, 1, st.Per[ln.Entity].Clear)
			assert.NotNil(t, deadAt(t, ln.Type, g, src, gone))
			assert.Nil(t, deadAt(t, ln.Type, b, src, back))
			assert.Nil(t, deadAt(t, ln.Type, h, src, live))
			assert.NotNil(t, deadAt(t, ln.Type, p, src, gone+"p"))
			assert.NotNil(t, deadAt(t, ln.Type, r, src, gone+"r"))
			assert.Nil(t, deadAt(t, ln.Type, o, dlsite, gone), "other sources must be left alone")
		})
	}
}

func TestVndbReleaseTouchesWork(t *testing.T) {
	requireDB(t)
	clean(t)
	ctx := context.Background()
	ln := withFloor(findLane(t, SourceVNDB, EntityRelease), 1)
	src := sourceID(t, SourceVNDB)
	relID := mkEntity(t, model.EntityTypeRelease, "rel")
	var workID int64
	require.NoError(t, testDB.Raw(`SELECT work_id FROM catalog_release WHERE id = ?`, relID).Scan(&workID).Error)
	require.NoError(t, testDB.Exec(`UPDATE catalog_work SET updated_at = '2000-01-01' WHERE id = ?`, workID).Error)
	mkRef(t, model.EntityTypeRelease, model.LinkKindExact, relID, src, "rgone", false)
	seedMembership(t, ln, "rkeep")

	_, err := RunWithDB(ctx, testDB, testEG, runOpts(t, SourceVNDB, true, []Lane{ln}))
	require.NoError(t, err)
	assert.NotNil(t, deadAt(t, model.EntityTypeRelease, relID, src, "rgone"))
	assert.True(t, workUpdatedAt(t, workID).After(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)))
}

func TestFloorTripWritesNothingWhileOtherLanesRun(t *testing.T) {
	requireDB(t)
	clean(t)
	ctx := context.Background()
	work := withFloor(findLane(t, SourceVNDB, EntityWork), 1)
	char := withFloor(findLane(t, SourceVNDB, EntityCharacter), 100)
	src := sourceID(t, SourceVNDB)
	w := mkEntity(t, model.EntityTypeWork, "work-gone")
	c := mkEntity(t, model.EntityTypeCharacter, "char-gone")
	mkRef(t, model.EntityTypeWork, model.LinkKindExact, w, src, "vgone", false)
	mkRef(t, model.EntityTypeCharacter, model.LinkKindExact, c, src, "cgone", false)
	seedMembership(t, work, "vkeep")

	st, err := RunWithDB(ctx, testDB, testEG, runOpts(t, SourceVNDB, true, []Lane{work, char}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "character")
	assert.EqualValues(t, 1, st.LanesRefused)
	assert.EqualValues(t, 1, st.Per[EntityWork].Mark)
	assert.NotNil(t, deadAt(t, model.EntityTypeWork, w, src, "vgone"))
	assert.Nil(t, deadAt(t, model.EntityTypeCharacter, c, src, "cgone"))
}

func TestEGStaleMarksFreshClears(t *testing.T) {
	requireDB(t)
	clean(t)
	ctx := context.Background()
	ln := withFloor(findLane(t, SourceEG, EntityWork), 1)
	src := sourceID(t, SourceEG)
	max := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	for i := 1; i <= 34; i++ {
		ts := max
		if i == 3 {
			ts = max.Add(-5 * 24 * time.Hour)
		}
		require.NoError(t, testDB.Exec(
			`INSERT INTO anchorliveness_eg.games (id, synced_at) VALUES (?, ?)`, i, ts).Error)
	}
	stale := mkEntity(t, model.EntityTypeWork, "stale")
	freshDead := mkEntity(t, model.EntityTypeWork, "fresh-dead")
	freshLive := mkEntity(t, model.EntityTypeWork, "fresh-live")
	mkRef(t, model.EntityTypeWork, model.LinkKindExact, stale, src, "3", false)
	mkRef(t, model.EntityTypeWork, model.LinkKindExact, freshDead, src, "1", true)
	mkRef(t, model.EntityTypeWork, model.LinkKindExact, freshLive, src, "2", false)

	st, err := RunWithDB(ctx, testDB, testEG, runOpts(t, SourceEG, true, []Lane{ln}))
	require.NoError(t, err)
	assert.EqualValues(t, 1, st.Per[EntityWork].Mark)
	assert.EqualValues(t, 1, st.Per[EntityWork].Clear)
	assert.NotNil(t, deadAt(t, model.EntityTypeWork, stale, src, "3"))
	assert.Nil(t, deadAt(t, model.EntityTypeWork, freshDead, src, "1"))
	assert.Nil(t, deadAt(t, model.EntityTypeWork, freshLive, src, "2"))
}

func TestEGFreshnessGuardRefusesAndWritesNothing(t *testing.T) {
	requireDB(t)
	clean(t)
	ctx := context.Background()
	ln := withFloor(findLane(t, SourceEG, EntityWork), 1)
	src := sourceID(t, SourceEG)
	now := time.Now()
	for i := 1; i <= 100; i++ {
		ts := now
		if i > 96 {
			ts = now.Add(-5 * 24 * time.Hour)
		}
		require.NoError(t, testDB.Exec(
			`INSERT INTO anchorliveness_eg.games (id, synced_at) VALUES (?, ?)`, i, ts).Error)
	}
	w := mkEntity(t, model.EntityTypeWork, "stale")
	mkRef(t, model.EntityTypeWork, model.LinkKindExact, w, src, "100", false)

	st, err := RunWithDB(ctx, testDB, testEG, runOpts(t, SourceEG, true, []Lane{ln}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fresh")
	assert.EqualValues(t, 1, st.LanesRefused)
	assert.Zero(t, st.Per[EntityWork].Mark)
	assert.Nil(t, deadAt(t, model.EntityTypeWork, w, src, "100"))
}

func TestReceiptsOneLinePerWrite(t *testing.T) {
	requireDB(t)
	clean(t)
	ctx := context.Background()
	ln := withFloor(findLane(t, SourceVNDB, EntityWork), 1)
	src := sourceID(t, SourceVNDB)
	gone := mkEntity(t, model.EntityTypeWork, "gone")
	back := mkEntity(t, model.EntityTypeWork, "back")
	mkRef(t, model.EntityTypeWork, model.LinkKindExact, gone, src, "vgone", false)
	mkRef(t, model.EntityTypeWork, model.LinkKindExact, back, src, "vback", true)
	seedMembership(t, ln, "vback")

	dir := t.TempDir()
	path := filepath.Join(dir, "liveness.jsonl")
	opts := runOpts(t, SourceVNDB, false, []Lane{ln})
	opts.Receipts = path
	_, err := RunWithDB(ctx, testDB, testEG, opts)
	require.NoError(t, err)
	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err), "dry run must not create receipts")

	opts.Apply = true
	_, err = RunWithDB(ctx, testDB, testEG, opts)
	require.NoError(t, err)
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	var lines []receipt
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var rec receipt
		require.NoError(t, json.Unmarshal(sc.Bytes(), &rec))
		lines = append(lines, rec)
	}
	require.NoError(t, sc.Err())
	require.Len(t, lines, 2)
	actions := map[string]int{}
	for _, rec := range lines {
		actions[rec.Action]++
		assert.Equal(t, SourceVNDB, rec.Source)
		assert.Equal(t, EntityWork, rec.Entity)
	}
	assert.Equal(t, 1, actions["mark"])
	assert.Equal(t, 1, actions["clear"])
}
