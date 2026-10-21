package service

import (
	"testing"
	"time"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

func cleanDeletionTables(t *testing.T) {
	t.Helper()
	cleanTables(t)
	for _, table := range []string{"catalog_deletion_request", "catalog_user_playtime", "catalog_user_work_state", "edit_proposal"} {
		require.NoError(t, testDB.Exec("TRUNCATE "+table+" RESTART IDENTITY CASCADE").Error)
	}
}

func approvedDeletion(t *testing.T, svc *DeletionService, workID int64) int64 {
	t.Helper()
	r, err := svc.Request(t.Context(), model.EntityTypeWork, workID, 3, model.DeletionReasonNotAWork, "no such game")
	require.NoError(t, err)
	require.NoError(t, svc.Approve(t.Context(), r.ID, 7))
	require.NoError(t, testDB.Exec(`UPDATE catalog_deletion_request SET execute_after = now() - interval '1 minute' WHERE id = ?`, r.ID).Error)
	return r.ID
}

func TestDeletionRequestRefusals(t *testing.T) {
	cleanDeletionTables(t)
	ctx := t.Context()
	svc := NewDeletionService(testDB, testResolve)
	w := createWork(t, "refusals")

	_, err := svc.Request(ctx, model.EntityTypeWork, w.ID, 3, DeletionReasonDuplicate, "same as another")
	require.ErrorIs(t, err, ErrDeletionDuplicate)
	_, err = svc.Request(ctx, model.EntityTypeWork, w.ID, 3, "boring", "meh")
	require.ErrorIs(t, err, ErrDeletionReason)
	_, err = svc.Request(ctx, model.EntityTypeCharacter, w.ID, 3, model.DeletionReasonOther, "x")
	require.ErrorIs(t, err, ErrDeletionFamily)
	_, err = svc.Request(ctx, model.EntityTypeWork, w.ID+999, 3, model.DeletionReasonOther, "x")
	require.ErrorIs(t, err, ErrEntityNotLive)

	r, err := svc.Request(ctx, model.EntityTypeWork, w.ID, 3, model.DeletionReasonSpamOrTest, "test entry")
	require.NoError(t, err)
	_, err = svc.Request(ctx, model.EntityTypeWork, w.ID, 4, model.DeletionReasonOther, "again")
	require.ErrorIs(t, err, ErrDeletionPending)

	require.NoError(t, svc.Approve(ctx, r.ID, 7))
	_, err = svc.Execute(ctx, r.ID, nil)
	require.ErrorIs(t, err, ErrCoolingOff)
	require.NoError(t, svc.Reject(ctx, r.ID, 7, "it is a real game"))
	got, err := svc.Get(ctx, r.ID)
	require.NoError(t, err)
	assert.Equal(t, model.ProposalStatusRejected, got.Status)
	assert.Equal(t, "it is a real game", got.DecisionNote)
}

func TestDeletionHidesAndRestoreReturnsTheWorkAndItsUserData(t *testing.T) {
	cleanDeletionTables(t)
	ctx := t.Context()
	svc := NewDeletionService(testDB, testResolve)
	w := createWork(t, "to be deleted")
	keep := createWork(t, "stays")
	addExternalRef(t, model.EntityTypeWork, w.ID, 2, "v7001", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeWork, w.ID, 5, "8001", model.LinkKindProbable)
	require.NoError(t, testDB.Create(&model.CatalogWorkRelation{AWorkID: keep.ID, BWorkID: w.ID, RelationTypeID: 1}).Error)
	require.NoError(t, testDB.Exec(`UPDATE catalog_work SET updated_at = now() - interval '1 day' WHERE id IN (?, ?)`, w.ID, keep.ID).Error)

	const uid = int64(42)
	ratings := NewUserRatingService(testDB)
	_, err := ratings.Set(ctx, uid, w.ID, "c", "kungal", RatingInput{Overall: 8, Recommend: model.RecommendYes})
	require.NoError(t, err)
	_, err = ratings.Set(ctx, uid, keep.ID, "c", "kungal", RatingInput{Overall: 6, Recommend: model.RecommendNeutral})
	require.NoError(t, err)
	folders := NewUserFolderService(testDB)
	f, err := folders.Create(ctx, FolderCreate{OwnerUID: uid, Name: "box"})
	require.NoError(t, err)
	_, err = folders.PutItem(ctx, uid, f.ID, w.ID)
	require.NoError(t, err)
	_, err = folders.PutItem(ctx, uid, f.ID, keep.ID)
	require.NoError(t, err)
	playtime := NewUserPlaytimeService(testDB)
	_, err = playtime.Report(ctx, PlaytimeReport{ActorUID: uid, WorkID: w.ID, ClientID: "c", Minutes: 90})
	require.NoError(t, err)
	states := NewUserWorkStateService(testDB)
	_, err = states.Set(ctx, uid, w.ID, model.WorkStateDone, nil)
	require.NoError(t, err)
	require.NoError(t, testDB.Create(&editing.Proposal{
		EntityFamily: "catalog", EntityType: editspec.TypeWork, EntityID: w.ID, Patch: datatypes.JSON(`{}`),
		ProposerUID: uid, Site: "kungal", Status: editing.StatusOpen,
	}).Error)

	preview, err := svc.Preview(ctx, w.ID)
	require.NoError(t, err)
	assert.False(t, preview.Deleted)
	assert.Len(t, preview.Anchors, 2)
	assert.Equal(t, int64(1), preview.Ratings)
	assert.Equal(t, int64(1), preview.FolderItems)
	assert.Equal(t, int64(1), preview.Playtimes)
	assert.Equal(t, int64(1), preview.WorkStates)
	assert.Equal(t, int64(1), preview.OpenProposals)

	id := approvedDeletion(t, svc, w.ID)
	closed, err := svc.Execute(ctx, id, nil)
	require.NoError(t, err)
	require.False(t, closed)

	var row struct {
		Status    int16
		DeletedAt *time.Time
		UpdatedAt time.Time
	}
	require.NoError(t, testDB.Raw(`SELECT status, deleted_at, updated_at FROM catalog_work WHERE id = ?`, w.ID).Scan(&row).Error)
	assert.Equal(t, model.WorkStatusDeleted, row.Status)
	require.NotNil(t, row.DeletedAt)
	assert.WithinDuration(t, time.Now(), row.UpdatedAt, time.Minute, "the changes feed reports the work gone")
	var keepUpdated time.Time
	require.NoError(t, testDB.Raw(`SELECT updated_at FROM catalog_work WHERE id = ?`, keep.ID).Scan(&keepUpdated).Error)
	assert.WithinDuration(t, time.Now(), keepUpdated, time.Minute, "a related work's relation list changed")

	var refs int64
	require.NoError(t, testDB.Model(&model.CatalogExternalRef{}).Where("entity_type = ? AND entity_id = ?", model.EntityTypeWork, w.ID).Count(&refs).Error)
	assert.Equal(t, int64(2), refs, "the refs stay as the tombstone")
	var proposalStatus int16
	require.NoError(t, testDB.Raw(`SELECT status FROM edit_proposal WHERE entity_id = ?`, w.ID).Scan(&proposalStatus).Error)
	assert.Equal(t, editing.StatusDeclined, proposalStatus)
	var revAction int16
	require.NoError(t, testDB.Raw(`SELECT action FROM catalog_revision WHERE entity_type = ? AND entity_id = ? ORDER BY revision DESC LIMIT 1`,
		model.EntityTypeWork, w.ID).Scan(&revAction).Error)
	assert.Equal(t, model.RevisionActionDeleted, revAction)
	gone, err := svc.DeletedWorkInfo(ctx, w.ID)
	require.NoError(t, err)
	require.NotNil(t, gone)
	assert.Equal(t, model.DeletionReasonNotAWork, gone.Reason)

	assertVisible := func(t *testing.T, visible bool) {
		t.Helper()
		want := 0
		if visible {
			want = 1
		}
		mine, err := ratings.ListMine(ctx, uid, time.Time{}, 0, 50)
		require.NoError(t, err)
		assert.Len(t, mine, 1+want, "ratings: the other work's rating is always there")
		n, err := ratings.CountMine(ctx, uid)
		require.NoError(t, err)
		assert.Equal(t, int64(1+want), n)
		n, err = ratings.Count(ctx, RatingFilter{WorkID: w.ID})
		require.NoError(t, err)
		assert.Equal(t, int64(want), n)
		forWork, err := ratings.ListMineFor(ctx, uid, []int64{w.ID})
		require.NoError(t, err)
		assert.Len(t, forWork, want)

		items, err := folders.ListItems(ctx, uid, f.ID, time.Time{}, 0, 50)
		require.NoError(t, err)
		assert.Len(t, items, 1+want)
		folder, err := folders.Get(ctx, uid, f.ID)
		require.NoError(t, err)
		assert.Equal(t, 1+want, folder.ItemCount, "the folder's count follows what it shows")
		holdings, err := folders.Holdings(ctx, uid, []int64{w.ID})
		require.NoError(t, err)
		assert.Len(t, holdings, want)
		containing, err := folders.CountMineContaining(ctx, uid, w.ID)
		require.NoError(t, err)
		assert.Equal(t, int64(want), containing)
		holders, err := folders.HoldersOfWork(ctx, w.ID, 0, 10)
		require.NoError(t, err)
		assert.Len(t, holders, want)
		recorded, err := folders.CountRecordedWorks(ctx, uid)
		require.NoError(t, err)
		assert.Equal(t, int64(1+want), recorded)

		plays, err := playtime.ListMine(ctx, uid, time.Time{}, 0, 50)
		require.NoError(t, err)
		assert.Len(t, plays, want)
		st, err := states.GetMine(ctx, uid, w.ID)
		require.NoError(t, err)
		assert.Equal(t, visible, st != nil)
	}
	t.Run("hidden while deleted", func(t *testing.T) { assertVisible(t, false) })

	require.NoError(t, testDB.Exec(`UPDATE catalog_user_work_rating SET changed_at = now() - interval '1 minute'`).Error)
	changes, err := ratings.Changes(ctx, time.Time{}, 0, 50)
	require.NoError(t, err)
	require.Len(t, changes, 2)
	for _, ch := range changes {
		assert.Equalf(t, ch.WorkID == w.ID, ch.Gone, "rating on work %d", ch.WorkID)
	}

	require.NoError(t, svc.Restore(ctx, id, 7, "it exists"))
	require.NoError(t, testDB.Raw(`SELECT status, deleted_at, updated_at FROM catalog_work WHERE id = ?`, w.ID).Scan(&row).Error)
	assert.Equal(t, model.WorkStatusLive, row.Status)
	assert.Nil(t, row.DeletedAt)
	r, err := svc.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, model.DeletionStatusRestored, r.Status)
	gone, err = svc.DeletedWorkInfo(ctx, w.ID)
	require.NoError(t, err)
	assert.Nil(t, gone)
	t.Run("visible again after restore", func(t *testing.T) { assertVisible(t, true) })

	require.ErrorIs(t, svc.Restore(ctx, id, 7, "twice"), ErrProposalState)
}

func TestDeletionOfAWorkMergedMeanwhileClosesTheRequest(t *testing.T) {
	cleanDeletionTables(t)
	ctx := t.Context()
	svc := NewDeletionService(testDB, testResolve)
	w := createWork(t, "merged away")
	id := approvedDeletion(t, svc, w.ID)
	require.NoError(t, testDB.Exec(`UPDATE catalog_work SET status = ?, deleted_at = now() WHERE id = ?`, model.WorkStatusMerged, w.ID).Error)

	closed, err := svc.Execute(ctx, id, nil)
	require.NoError(t, err)
	assert.True(t, closed)
	r, err := svc.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, model.ProposalStatusRejected, r.Status)

	rep, err := svc.ExecuteDue(ctx)
	require.NoError(t, err)
	assert.Zero(t, rep.Due)
}
