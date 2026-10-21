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

func TestRestoreResurfacesUserRowsForMirrorsPastThem(t *testing.T) {
	cleanDeletionTables(t)
	ctx := t.Context()
	svc := NewDeletionService(testDB, testResolve)
	folders := NewUserFolderService(testDB)
	playtime := NewUserPlaytimeService(testDB)
	states := NewUserWorkStateService(testDB)
	const uid = int64(88)
	w := createWork(t, "synced, deleted, restored")
	f, err := folders.Create(ctx, FolderCreate{OwnerUID: uid, Name: "box"})
	require.NoError(t, err)
	_, err = folders.PutItem(ctx, uid, f.ID, w.ID)
	require.NoError(t, err)
	_, err = playtime.Report(ctx, PlaytimeReport{ActorUID: uid, WorkID: w.ID, ClientID: "c", Minutes: 40})
	require.NoError(t, err)
	_, err = states.Set(ctx, uid, w.ID, model.WorkStateDone, nil)
	require.NoError(t, err)
	for _, tbl := range []string{"catalog_user_folder_item", "catalog_user_playtime", "catalog_user_work_state"} {
		require.NoError(t, testDB.Exec(`UPDATE `+tbl+` SET updated_at = now() - interval '1 hour' WHERE work_id = ?`, w.ID).Error)
	}
	cursor := time.Now().Add(-30 * time.Minute)

	id := approvedDeletion(t, svc, w.ID)
	_, err = svc.Execute(ctx, id, nil)
	require.NoError(t, err)
	require.NoError(t, svc.Restore(ctx, id, 7, "it exists"))

	items, err := folders.ListItems(ctx, uid, f.ID, cursor, 0, 50)
	require.NoError(t, err)
	assert.Len(t, items, 1, "a folder mirror past the item gets it back")
	plays, err := playtime.ListMine(ctx, uid, cursor, 0, 50)
	require.NoError(t, err)
	assert.Len(t, plays, 1, "a playtime mirror past the row gets it back")
	st, err := states.ListMine(ctx, uid, cursor, 0, 50)
	require.NoError(t, err)
	assert.Len(t, st, 1, "a play-state mirror past the row gets it back")
}

func undoSetup(t *testing.T) (editionPair, func() int64) {
	t.Helper()
	cleanTables(t)
	e := seedEditionPair(t)
	return e, func() int64 {
		p, err := testMerge.RequestMerge(t.Context(), model.EntityTypeWork, e.src, e.dst, 42, "edition duplicate", MergeChoices{})
		require.NoError(t, err)
		approveAndForceExecutable(t, p.ID)
		require.NoError(t, testMerge.ExecuteMerge(t.Context(), p.ID, nil))
		return p.ID
	}
}

func countRows(t *testing.T, sql string, args ...any) int64 {
	t.Helper()
	var n int64
	require.NoError(t, testDB.Raw(sql, args...).Scan(&n).Error)
	return n
}

func TestUndoMergeKeepsWhatTheUserRemovedAfterTheMerge(t *testing.T) {
	e, merge := undoSetup(t)
	ctx := t.Context()
	ratings := NewUserRatingService(testDB)
	folders := NewUserFolderService(testDB)
	states := NewUserWorkStateService(testDB)
	const uid = int64(701)
	old, err := ratings.Set(ctx, uid, e.src, "c", "kungal", RatingInput{Overall: 4, Recommend: model.RecommendNo})
	require.NoError(t, err)
	_, err = ratings.Set(ctx, uid, e.dst, "c", "kungal", RatingInput{Overall: 9, Recommend: model.RecommendYes})
	require.NoError(t, err)
	require.NoError(t, testDB.Exec(`UPDATE catalog_user_work_rating SET updated_at = now() - interval '2 days' WHERE id = ?`, old.ID).Error)
	f, err := folders.Create(ctx, FolderCreate{OwnerUID: uid, Name: "kept"})
	require.NoError(t, err)
	gone, err := folders.Create(ctx, FolderCreate{OwnerUID: uid, Name: "deleted later"})
	require.NoError(t, err)
	for _, folder := range []int64{f.ID, gone.ID} {
		for _, w := range []int64{e.src, e.dst} {
			_, err = folders.PutItem(ctx, uid, folder, w)
			require.NoError(t, err)
		}
	}
	_, err = states.Set(ctx, uid, e.src, model.WorkStateDone, nil)
	require.NoError(t, err)
	_, err = states.Set(ctx, uid, e.dst, model.WorkStateDoing, nil)
	require.NoError(t, err)

	id := merge()
	require.NoError(t, ratings.DeleteMine(ctx, uid, e.dst))
	require.NoError(t, folders.DeleteItem(ctx, uid, f.ID, e.dst))
	require.NoError(t, folders.Delete(ctx, uid, gone.ID))
	require.NoError(t, states.DeleteMine(ctx, uid, e.dst))
	tombstones := countRows(t, `SELECT count(*) FROM catalog_user_work_rating_tombstone`)

	_, err = testMerge.UndoMerge(ctx, id, 9, "undo")
	require.NoError(t, err)

	assert.Zero(t, countRows(t, `SELECT count(*) FROM catalog_user_work_rating WHERE actor_uid = ?`, uid), "the user deleted the rating the merge kept")
	assert.Equal(t, tombstones, countRows(t, `SELECT count(*) FROM catalog_user_work_rating_tombstone`), "the user's own deletion keeps its tombstone")
	assert.Zero(t, countRows(t, `SELECT count(*) FROM catalog_user_folder_item WHERE folder_id = ?`, f.ID), "the user removed the item the merge kept")
	assert.Zero(t, countRows(t, `SELECT count(*) FROM catalog_user_folder_item WHERE folder_id = ?`, gone.ID), "nothing comes back into a deleted folder")
	assert.Zero(t, countRows(t, `SELECT count(*) FROM catalog_user_work_state WHERE actor_uid = ?`, uid), "the user cleared the play state the merge kept")
}

func TestUndoMergeResurrectsNothingForAnErasedAccount(t *testing.T) {
	e, merge := undoSetup(t)
	ctx := t.Context()
	ratings := NewUserRatingService(testDB)
	playtime := NewUserPlaytimeService(testDB)
	const uid = int64(702)
	for _, w := range []int64{e.src, e.dst} {
		_, err := ratings.Set(ctx, uid, w, "c", "kungal", RatingInput{Overall: 7, Recommend: model.RecommendYes})
		require.NoError(t, err)
		_, err = playtime.Report(ctx, PlaytimeReport{ActorUID: uid, WorkID: w, ClientID: "c", Minutes: 10})
		require.NoError(t, err)
	}
	id := merge()
	_, err := PurgeAccount(ctx, testDB, uid)
	require.NoError(t, err)

	_, err = testMerge.UndoMerge(ctx, id, 9, "undo")
	require.NoError(t, err)
	assert.Zero(t, countRows(t, `SELECT count(*) FROM catalog_user_work_rating WHERE actor_uid = ?`, uid))
	assert.Zero(t, countRows(t, `SELECT count(*) FROM catalog_user_playtime WHERE actor_uid = ?`, uid))
}

func TestUndoMergeSurvivesAVoteWhoseCoverWasDeleted(t *testing.T) {
	e, merge := undoSetup(t)
	ctx := t.Context()
	createCover(t, e.src, "srccoverhash")
	createCover(t, e.dst, "dstcoverhash")
	var srcCover, dstCover int64
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_work_cover WHERE work_id = ?`, e.src).Scan(&srcCover).Error)
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_work_cover WHERE work_id = ?`, e.dst).Scan(&dstCover).Error)
	require.NoError(t, testDB.Create(&model.CatalogCoverVote{WorkID: e.src, CoverID: srcCover, ActorUID: 703, Site: "kungal"}).Error)
	require.NoError(t, testDB.Create(&model.CatalogCoverVote{WorkID: e.dst, CoverID: dstCover, ActorUID: 703, Site: "kungal"}).Error)

	id := merge()
	require.NoError(t, testDB.Exec(`DELETE FROM catalog_work_cover WHERE id = ?`, srcCover).Error)

	_, err := testMerge.UndoMerge(ctx, id, 9, "undo")
	require.NoError(t, err, "a vote on a cover deleted after the merge must not abort the undo")
}

func TestUndoLabelMergeRestoresTheSecondFollow(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()
	survivor := createLabel(t, "Brand", model.LabelKindGameBrand)
	loser := createLabel(t, "Brand Inc.", model.LabelKindGameBrand)
	for _, l := range []int64{survivor, loser} {
		require.NoError(t, testDB.Create(&model.CatalogUserEntityFollow{ActorUID: 704, EntityType: model.EntityTypeLabel, EntityID: l, ClientID: "c", Site: "kungal"}).Error)
	}
	p, err := testMerge.ProposeMerge(ctx, model.EntityTypeLabel, loser, survivor, 7, "dup")
	require.NoError(t, err)
	approveAndForceExecutable(t, p.ID)
	require.NoError(t, testMerge.ExecuteMerge(ctx, p.ID, nil))
	require.EqualValues(t, 1, countRows(t, `SELECT count(*) FROM catalog_user_entity_follow WHERE actor_uid = 704`))

	_, err = testMerge.UndoMerge(ctx, p.ID, 9, "undo")
	require.NoError(t, err)
	assert.EqualValues(t, 1, countRows(t, `SELECT count(*) FROM catalog_user_entity_follow WHERE actor_uid = 704 AND entity_id = ?`, loser),
		"the follow the merge folded away comes back")
	assert.EqualValues(t, 1, countRows(t, `SELECT count(*) FROM catalog_user_entity_follow WHERE actor_uid = 704 AND entity_id = ?`, survivor))
}

func TestAPurgedAccountGetsNothingBackFromAMergeArchive(t *testing.T) {
	f := newAbsorbFixture(t)
	seedAbsorbLoser(t, &f)
	const voter = int64(705)
	own := &model.CatalogWorkCover{WorkID: f.loser, ImageHash: "loseronlycoverhash", SourceID: model.SourceBangumi}
	require.NoError(t, testDB.Create(own).Error)
	vote := &model.CatalogCoverVote{WorkID: f.loser, CoverID: own.ID, ActorUID: voter, Site: "kungal"}
	require.NoError(t, testDB.Create(vote).Error)
	pid := absorbNow(t, f.loser, f.survivor)
	require.EqualValues(t, 1, countRows(t, `SELECT count(*) FROM catalog_merge_archive WHERE proposal_id = ? AND table_name = 'catalog_cover_vote' AND (row->>'actor_uid')::bigint = ?`, pid, voter),
		"absorb archives the vote with its imported cover")

	_, err := PurgeAccount(t.Context(), testDB, voter)
	require.NoError(t, err)
	_, err = testMerge.UndoMerge(t.Context(), pid, 9, "undo")
	require.NoError(t, err)
	assert.Zero(t, countRows(t, `SELECT count(*) FROM catalog_cover_vote WHERE actor_uid = ?`, voter), "an erased account's archived vote stays erased")
}
