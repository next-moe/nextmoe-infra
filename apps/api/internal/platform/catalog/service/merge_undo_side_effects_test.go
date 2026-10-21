package service

import (
	"testing"
	"time"

	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func proposalStatus(t *testing.T, id int64) int16 {
	t.Helper()
	var s int16
	require.NoError(t, testDB.Raw(`SELECT status FROM edit_proposal WHERE id = ?`, id).Scan(&s).Error)
	return s
}

func TestUndoMergeRestoresCollidingUserDataAndReopensProposals(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()
	e := seedEditionPair(t)
	ratings := NewUserRatingService(testDB)
	folders := NewUserFolderService(testDB)
	playtime := NewUserPlaytimeService(testDB)
	states := NewUserWorkStateService(testDB)

	const both, newerOnSource = int64(601), int64(602)
	srcOld, err := ratings.Set(ctx, both, e.src, "c", "kungal", RatingInput{Overall: 6, Recommend: model.RecommendNeutral})
	require.NoError(t, err)
	dstNew, err := ratings.Set(ctx, both, e.dst, "c", "kungal", RatingInput{Overall: 9, Recommend: model.RecommendYes})
	require.NoError(t, err)
	srcNew, err := ratings.Set(ctx, newerOnSource, e.src, "c", "kungal", RatingInput{Overall: 8, Recommend: model.RecommendYes})
	require.NoError(t, err)
	dstOld, err := ratings.Set(ctx, newerOnSource, e.dst, "c", "kungal", RatingInput{Overall: 5, Recommend: model.RecommendNo})
	require.NoError(t, err)
	require.NoError(t, testDB.Exec(`UPDATE catalog_user_work_rating SET updated_at = now() - interval '2 days' WHERE id IN (?, ?)`, srcOld.ID, dstOld.ID).Error)

	f, err := folders.Create(ctx, FolderCreate{OwnerUID: both, Name: "both editions"})
	require.NoError(t, err)
	_, err = folders.PutItem(ctx, both, f.ID, e.src)
	require.NoError(t, err)
	_, err = folders.PutItem(ctx, both, f.ID, e.dst)
	require.NoError(t, err)
	_, err = playtime.Report(ctx, PlaytimeReport{ActorUID: both, WorkID: e.src, ClientID: "c", Minutes: 30})
	require.NoError(t, err)
	_, err = playtime.Report(ctx, PlaytimeReport{ActorUID: both, WorkID: e.dst, ClientID: "c", Minutes: 50})
	require.NoError(t, err)
	_, err = states.Set(ctx, both, e.src, model.WorkStateDone, nil)
	require.NoError(t, err)
	_, err = states.Set(ctx, both, e.dst, model.WorkStateDoing, nil)
	require.NoError(t, err)
	pending := openWorkProposal(t, e.src)

	p, err := testMerge.RequestMerge(ctx, model.EntityTypeWork, e.src, e.dst, 42, "edition duplicate", MergeChoices{})
	require.NoError(t, err)
	approveAndForceExecutable(t, p.ID)
	require.NoError(t, testMerge.ExecuteMerge(ctx, p.ID, nil))
	require.Equal(t, editing.StatusDeclined, proposalStatus(t, pending), "the merge closes proposals on the merged-away work")

	cursor := time.Now().Add(-time.Second)
	_, err = testMerge.UndoMerge(ctx, p.ID, 9, "different editions after all")
	require.NoError(t, err)

	type ratingRow struct {
		ID      int64
		WorkID  int64
		Overall int16
	}
	var rows []ratingRow
	require.NoError(t, testDB.Raw(`SELECT id, work_id, overall FROM catalog_user_work_rating WHERE actor_uid IN (?, ?) ORDER BY id`, both, newerOnSource).Scan(&rows).Error)
	assert.ElementsMatch(t, []ratingRow{
		{srcOld.ID, e.src, 6}, {dstNew.ID, e.dst, 9}, {srcNew.ID, e.src, 8}, {dstOld.ID, e.dst, 5},
	}, rows, "every rating is back on its own work under its own id")
	var tombstones int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_user_work_rating_tombstone WHERE id IN (?, ?, ?, ?)`,
		srcOld.ID, dstNew.ID, srcNew.ID, dstOld.ID).Scan(&tombstones).Error)
	assert.Zero(t, tombstones, "a restored rating leaves no tombstone behind")

	require.NoError(t, testDB.Exec(`UPDATE catalog_user_work_rating SET changed_at = changed_at - interval '10 seconds' WHERE actor_uid IN (?, ?)`, both, newerOnSource).Error)
	changes, err := ratings.Changes(ctx, cursor.Add(-10*time.Second), 0, 50)
	require.NoError(t, err)
	live := map[int64]int64{}
	for _, ch := range changes {
		if !ch.Gone {
			live[ch.ID] = ch.WorkID
		}
	}
	assert.Equal(t, map[int64]int64{srcOld.ID: e.src, dstNew.ID: e.dst, srcNew.ID: e.src, dstOld.ID: e.dst}, live,
		"the feed re-emits every rating on the work it is back on")

	items, err := folders.ListItems(ctx, both, f.ID, time.Time{}, 0, 50)
	require.NoError(t, err)
	assert.Len(t, items, 2, "the folder holds both editions again")
	var playtimes, workStates int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_user_playtime WHERE actor_uid = ?`, both).Scan(&playtimes).Error)
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_user_work_state WHERE actor_uid = ?`, both).Scan(&workStates).Error)
	assert.EqualValues(t, 2, playtimes)
	assert.EqualValues(t, 2, workStates)

	assert.Equal(t, editing.StatusOpen, proposalStatus(t, pending), "undo reopens what the merge closed")
}

func TestRestoreReopensProposalsTheDeletionClosed(t *testing.T) {
	cleanDeletionTables(t)
	ctx := t.Context()
	svc := NewDeletionService(testDB, testResolve)
	w := createWork(t, "deleted then restored")
	pending := openWorkProposal(t, w.ID)
	closedEarlier := openWorkProposal(t, w.ID)
	require.NoError(t, testDB.Exec(`UPDATE edit_proposal SET status = ?, decision_note = 'not this one' WHERE id = ?`, editing.StatusDeclined, closedEarlier).Error)

	id := approvedDeletion(t, svc, w.ID)
	_, err := svc.Execute(ctx, id, nil)
	require.NoError(t, err)
	require.Equal(t, editing.StatusDeclined, proposalStatus(t, pending))

	require.NoError(t, svc.Restore(ctx, id, 7, "it exists"))
	assert.Equal(t, editing.StatusOpen, proposalStatus(t, pending), "restore reopens what the deletion closed")
	assert.Equal(t, editing.StatusDeclined, proposalStatus(t, closedEarlier), "a proposal declined for its own reasons stays declined")
}
