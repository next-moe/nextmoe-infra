package service

import (
	"context"
	"testing"

	"api/internal/platform/catalog/discussion"
	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/require"
)

func TestPurgeAccountTakesEveryPersonalRowAndNothingElse(t *testing.T) {
	folders := newFolderSvc(t)
	require.NoError(t, testDB.Exec(
		"TRUNCATE catalog_user_playtime, catalog_user_work_state, catalog_user_folder_import, catalog_user_entity_follow, catalog_user_tag_block, catalog_user_work_rating, catalog_user_work_rating_tombstone, catalog_user_privacy, catalog_user_work_view, edit_discussion_post, edit_discussion_watcher RESTART IDENTITY").Error)
	workID, coverID := seedVoteWork(t)
	playtime := NewUserPlaytimeService(testDB)
	states := NewUserWorkStateService(testDB)
	votes := NewCoverVoteService(testDB)
	ctx := context.Background()
	tag := model.CatalogTag{Name: "purge-blocked-tag", Tier: model.TagTierCore, Kind: model.TagKindContent}
	require.NoError(t, testDB.Where(model.CatalogTag{Name: tag.Name}).FirstOrCreate(&tag).Error)

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
		require.NoError(t, testDB.Create(&model.CatalogUserEntityFollow{
			ActorUID: uid, EntityType: model.EntityTypeLabel, EntityID: 1, ClientID: "kungal", Site: "kungal",
		}).Error)
		require.NoError(t, testDB.Create(&model.CatalogUserTagBlock{
			ActorUID: uid, TagID: tag.ID, Mode: model.TagBlockModeTree, ClientID: "kungal", Site: "kungal",
		}).Error)
		_, err = NewUserRatingService(testDB).Set(ctx, uid, workID, "kungal", "kungal",
			RatingInput{Overall: 8, Recommend: model.RecommendYes, ShortSummary: "purge me"})
		require.NoError(t, err)
		_, err = NewUserWorkRecordService(testDB).SetPrivacy(ctx, uid, true)
		require.NoError(t, err)
		require.NoError(t, NewBrowsingHistoryService(testDB).Record(ctx, uid, "kungal", workID))
		_, err = discussion.New(testDB, discussion.Kind{Name: "probe"}).Create(ctx, discussion.CreateInput{
			Kind: "probe", AnchorID: 1, AuthorUID: uid, Body: "我的留言",
		})
		require.NoError(t, err)
	}

	got, err := PurgeAccount(ctx, testDB, gone)
	require.NoError(t, err)
	require.Equal(t, AccountPurged{Folders: 1, FolderItems: 1, Playtimes: 2, WorkStates: 1, Ratings: 1, CoverVotes: 1, EntityFollows: 1, TagBlocks: 1, Privacy: 1,
		DiscussionPosts: 1, DiscussionWatches: 1, BrowsingHistory: 1}, got)
	var left []discussion.Post
	require.NoError(t, testDB.Where("anchor_kind = 'probe'").Order("author_uid").Find(&left).Error)
	require.Len(t, left, 2, "the erased account's post stays as a deleted placeholder")
	require.Equal(t, discussion.StatusDeleted, left[0].Status)
	require.Empty(t, left[0].Body)
	require.Equal(t, "我的留言", left[1].Body, "the bystander's post is untouched")

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
		{"catalog_user_entity_follow", "actor_uid"},
		{"catalog_user_tag_block", "actor_uid"},
		{"catalog_user_work_rating", "actor_uid"},
		{"catalog_user_privacy", "actor_uid"},
		{"catalog_user_work_view", "actor_uid"},
		{"edit_discussion_watcher", "user_id"},
	} {
		require.Zerof(t, count(tc.table, tc.col, gone), "%s still names the deleted account", tc.table)
		require.NotZerof(t, count(tc.table, tc.col, bystander), "%s lost the bystander's rows", tc.table)
	}

	again, err := PurgeAccount(ctx, testDB, gone)
	require.NoError(t, err)
	require.Equal(t, AccountPurged{}, again)
}
