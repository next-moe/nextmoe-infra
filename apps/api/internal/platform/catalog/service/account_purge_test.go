package service

import (
	"context"
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/require"
)

func TestPurgeAccountTakesEveryPersonalRowAndNothingElse(t *testing.T) {
	folders := newFolderSvc(t)
	require.NoError(t, testDB.Exec(
		"TRUNCATE catalog_user_playtime, catalog_user_work_state, catalog_user_folder_import RESTART IDENTITY").Error)
	workID, coverID := seedVoteWork(t)
	playtime := NewUserPlaytimeService(testDB)
	states := NewUserWorkStateService(testDB)
	votes := NewCoverVoteService(testDB)
	ctx := context.Background()

	const gone, bystander = int64(31), int64(32)
	for _, uid := range []int64{gone, bystander} {
		f, err := folders.Create(ctx, FolderCreate{OwnerUID: uid, Name: "收藏", IsDefault: true})
		require.NoError(t, err)
		_, err = folders.PutItem(ctx, uid, f.ID, workID)
		require.NoError(t, err)
		require.NoError(t, testDB.Create(&model.CatalogUserFolderImport{
			Site: model.FolderImportSiteForum, SourceID: 8000 + uid, FolderID: f.ID, OwnerUID: uid,
		}).Error)
		_, err = playtime.Report(ctx, PlaytimeReport{ActorUID: uid, WorkID: workID, ClientID: "kungal", Minutes: 60})
		require.NoError(t, err)
		_, err = playtime.Report(ctx, PlaytimeReport{ActorUID: uid, WorkID: workID, ClientID: "moyu", Minutes: 30})
		require.NoError(t, err)
		_, err = states.Set(ctx, uid, workID, model.WorkStateDone, nil)
		require.NoError(t, err)
		_, err = votes.Vote(ctx, CoverVoteParams{WorkID: workID, CoverID: coverID, ActorUID: uid, Site: "kungal"})
		require.NoError(t, err)
	}

	got, err := PurgeAccount(ctx, testDB, gone)
	require.NoError(t, err)
	require.Equal(t, AccountPurged{Folders: 1, FolderItems: 1, Playtimes: 2, WorkStates: 1, CoverVotes: 1}, got)

	count := func(table, col string, uid int64) int64 {
		var n int64
		require.NoError(t, testDB.Table(table).Where(col+" = ?", uid).Count(&n).Error)
		return n
	}
	for _, tc := range []struct{ table, col string }{
		{"catalog_user_folder", "owner_uid"},
		{"catalog_user_folder_item", "owner_uid"},
		{"catalog_user_folder_import", "owner_uid"},
		{"catalog_user_playtime", "actor_uid"},
		{"catalog_user_work_state", "actor_uid"},
		{"catalog_cover_vote", "actor_uid"},
	} {
		require.Zerof(t, count(tc.table, tc.col, gone), "%s still names the deleted account", tc.table)
		require.NotZerof(t, count(tc.table, tc.col, bystander), "%s lost the bystander's rows", tc.table)
	}

	again, err := PurgeAccount(ctx, testDB, gone)
	require.NoError(t, err)
	require.Equal(t, AccountPurged{}, again)
}
