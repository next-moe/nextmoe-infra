package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/require"
)

func listedFolder(t *testing.T, svc *UserFolderService, owner int64, f FolderCreate, works ...int64) *model.CatalogUserFolder {
	t.Helper()
	f.OwnerUID = owner
	row, err := svc.Create(context.Background(), f)
	require.NoError(t, err)
	for _, w := range works {
		_, err := svc.PutItem(context.Background(), owner, row.ID, w)
		require.NoError(t, err)
	}
	return row
}

func sfwWorks(t *testing.T, prefix string, n int) []int64 {
	t.Helper()
	out := make([]int64, n)
	for i := range out {
		out[i] = createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, prefix+string(rune('A'+i))).ID
	}
	return out
}

func TestFolderTagsAndSearch(t *testing.T) {
	svc := newFolderSvc(t)
	ctx := context.Background()
	w := sfwWorks(t, "标签作品", 3)

	tags, err := NormalizeFolderTags([]string{" #催泪 ", "催泪", "VN", "vn", "", "  入坑  向 "})
	require.NoError(t, err)
	require.Equal(t, []string{"催泪", "VN", "入坑 向"}, tags, "trimmed, # dropped, spaces collapsed, duplicates ignoring case dropped")
	_, err = NormalizeFolderTags([]string{"a", "b", "c", "d", "e", "f"})
	require.ErrorIs(t, err, ErrFolderTooManyTags)
	_, err = NormalizeFolderTags([]string{strings.Repeat("长", model.FolderTagMaxRunes+1)})
	require.ErrorIs(t, err, ErrFolderTagTooLong)

	cry := listedFolder(t, svc, 1, FolderCreate{Name: "哭着玩", Description: "100% 催泪", Tags: []string{"催泪", "剧情"}, Visibility: model.FolderVisibilityPublic}, w...)
	plot := listedFolder(t, svc, 2, FolderCreate{Name: "剧情神作", Tags: []string{"剧情"}, Visibility: model.FolderVisibilityPublic}, w...)
	listedFolder(t, svc, 3, FolderCreate{Name: "无标签", Visibility: model.FolderVisibilityPublic}, w...)
	hidden := listedFolder(t, svc, 4, FolderCreate{Name: "不公开的剧情", Tags: []string{"剧情"}, Visibility: model.FolderVisibilityUnlisted}, w...)

	ids := func(q PublicFolderQuery) []int64 {
		t.Helper()
		q.Limit, q.Sort = 50, FolderSortCreated
		rows, err := svc.ListPublicFolders(ctx, q)
		require.NoError(t, err)
		out := make([]int64, len(rows))
		for i, r := range rows {
			out[i] = r.ID
		}
		return out
	}
	require.Equal(t, []int64{plot.ID, cry.ID}, ids(PublicFolderQuery{Tag: "剧情"}), "an unlisted folder stays out of the tag lane too")
	require.Equal(t, []int64{cry.ID}, ids(PublicFolderQuery{Search: "催泪"}), "q matches the description and the tags")
	require.Equal(t, []int64{plot.ID}, ids(PublicFolderQuery{Search: "神作"}))
	require.Equal(t, []int64{cry.ID}, ids(PublicFolderQuery{Search: "100%"}))
	require.Empty(t, ids(PublicFolderQuery{Search: "_"}), "q is a substring, not a pattern")

	counts, err := svc.DirectoryTags(ctx, false, 10)
	require.NoError(t, err)
	require.Equal(t, []FolderTagCount{{"剧情", 2}, {"催泪", 1}}, counts)

	newTags := []string{"冷门佳作"}
	_, err = svc.Patch(ctx, 1, cry.ID, FolderPatch{Tags: &newTags})
	require.NoError(t, err)
	extras, err := svc.FolderExtras(ctx, []int64{cry.ID, hidden.ID})
	require.NoError(t, err)
	require.Equal(t, []string{"冷门佳作"}, extras[cry.ID].Tags)
	require.Equal(t, []string{"剧情"}, extras[hidden.ID].Tags)
	require.NoError(t, svc.Delete(ctx, 4, hidden.ID))
	var left int64
	require.NoError(t, testDB.Model(&model.CatalogUserFolderTag{}).Where("folder_id = ?", hidden.ID).Count(&left).Error)
	require.Zero(t, left)
}

func TestFolderCoverPick(t *testing.T) {
	svc := newFolderSvc(t)
	ctx := context.Background()
	w := sfwWorks(t, "封面作品", 3)
	f := listedFolder(t, svc, 1, FolderCreate{Name: "封面", Visibility: model.FolderVisibilityPublic}, w...)
	cover := func() int64 {
		t.Helper()
		extras, err := svc.FolderExtras(ctx, []int64{f.ID})
		require.NoError(t, err)
		return extras[f.ID].CoverWorkID
	}
	require.Zero(t, cover())
	pick := func(id int64) error {
		_, err := svc.Patch(ctx, 1, f.ID, FolderPatch{CoverWorkID: &id})
		return err
	}
	require.NoError(t, pick(w[1]))
	require.Equal(t, w[1], cover())
	require.NoError(t, pick(w[2]))
	require.Equal(t, w[2], cover(), "a new pick replaces the old one")
	stranger := sfwWorks(t, "不在夹里", 1)[0]
	require.ErrorIs(t, pick(stranger), ErrFolderCoverNotMember)
	require.NoError(t, pick(0))
	require.Zero(t, cover())
	require.NoError(t, pick(w[0]))
	require.NoError(t, svc.DeleteItem(ctx, 1, f.ID, w[0]))
	require.Zero(t, cover(), "removing the cover work clears the pick")
}

func TestFolderMergeCarriesTheCover(t *testing.T) {
	svc := newFolderSvc(t)
	ctx := context.Background()
	src := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "封面源")
	dst := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "封面靶")
	f := listedFolder(t, svc, 1, FolderCreate{Name: "合并封面", Visibility: model.FolderVisibilityPublic}, dst.ID, src.ID)
	_, err := svc.Patch(ctx, 1, f.ID, FolderPatch{CoverWorkID: &src.ID})
	require.NoError(t, err)

	p, err := testMerge.ProposeMerge(ctx, model.EntityTypeWork, src.ID, dst.ID, 7, "folder cover")
	require.NoError(t, err)
	approveAndForceExecutable(t, p.ID)
	require.NoError(t, testMerge.ExecuteMerge(ctx, p.ID, nil))

	extras, err := svc.FolderExtras(ctx, []int64{f.ID})
	require.NoError(t, err)
	require.Equal(t, dst.ID, extras[f.ID].CoverWorkID, "the folded membership's cover pick moves to the survivor")
}

func TestFolderUnlisted(t *testing.T) {
	svc := newFolderSvc(t)
	ctx := context.Background()
	w := sfwWorks(t, "链接可见", 3)
	f := listedFolder(t, svc, 1, FolderCreate{Name: "只给朋友看", Visibility: model.FolderVisibilityUnlisted}, w...)

	got, err := svc.GetPublic(ctx, f.ID)
	require.NoError(t, err)
	require.Equal(t, model.FolderVisibilityUnlisted, got.Visibility)
	rows, err := svc.ListPublicFolders(ctx, PublicFolderQuery{Limit: 10, NSFW: true})
	require.NoError(t, err)
	require.Empty(t, rows, "unlisted stays out of the directory")
	rows, err = svc.ListPublicFolders(ctx, PublicFolderQuery{OwnerUID: 1, Limit: 10})
	require.NoError(t, err)
	require.Empty(t, rows, "and out of the owner's profile lane")

	_, err = svc.Follow(ctx, 2, f.ID, "c", "s")
	require.NoError(t, err, "anyone with the link can follow it")
	follows, err := svc.ListFollows(ctx, 2, 0, 10)
	require.NoError(t, err)
	require.Len(t, follows, 1)

	private := model.FolderVisibilityPrivate
	_, err = svc.Patch(ctx, 1, f.ID, FolderPatch{Visibility: &private})
	require.NoError(t, err)
	_, err = svc.GetPublic(ctx, f.ID)
	require.ErrorIs(t, err, ErrFolderNotFound)
	bad := int16(9)
	_, err = svc.Patch(ctx, 1, f.ID, FolderPatch{Visibility: &bad})
	require.ErrorIs(t, err, ErrFolderBadVisibility)
}

func TestFolderCopy(t *testing.T) {
	svc := newFolderSvc(t)
	ctx := context.Background()
	w := sfwWorks(t, "复制作品", 4)
	src := listedFolder(t, svc, 1, FolderCreate{Name: "十佳", Description: "我的十佳", Tags: []string{"入坑向"}, Visibility: model.FolderVisibilityPublic}, w...)
	_, _, err := svc.PatchItem(ctx, 1, src.ID, w[3], FolderItemPatch{BeforeWorkID: &w[0]})
	require.NoError(t, err)
	note := "第一推荐"
	_, _, err = svc.PatchItem(ctx, 1, src.ID, w[3], FolderItemPatch{Note: &note})
	require.NoError(t, err)
	_, err = svc.Patch(ctx, 1, src.ID, FolderPatch{CoverWorkID: &w[1]})
	require.NoError(t, err)
	require.NoError(t, testDB.Exec("UPDATE catalog_work SET deleted_at = now() WHERE id = ?", w[2]).Error)

	cp, err := svc.Copy(ctx, 2, src.ID, "")
	require.NoError(t, err)
	require.Equal(t, int64(2), cp.OwnerUID)
	require.Equal(t, "十佳", cp.Name)
	require.Equal(t, "源自「十佳」\n\n我的十佳", cp.Description)
	require.Equal(t, model.FolderVisibilityPrivate, cp.Visibility)
	require.False(t, cp.IsDefault)
	require.Equal(t, 3, cp.ItemCount, "the deleted work is not copied")
	require.Equal(t, []int64{w[3], w[0], w[1]}, folderOrder(t, svc, 2, cp.ID), "the source's order is kept")
	item, err := svc.Item(ctx, 2, cp.ID, w[3])
	require.NoError(t, err)
	require.Equal(t, note, item.Note)
	require.Equal(t, int64(2), item.OwnerUID)
	extras, err := svc.FolderExtras(ctx, []int64{cp.ID})
	require.NoError(t, err)
	require.Equal(t, w[1], extras[cp.ID].CoverWorkID)
	require.Equal(t, []string{"入坑向"}, extras[cp.ID].Tags)

	named, err := svc.Copy(ctx, 2, src.ID, "  我的版本 ")
	require.NoError(t, err)
	require.Equal(t, "我的版本", named.Name)

	mine := listedFolder(t, svc, 3, FolderCreate{Name: "私藏", Visibility: model.FolderVisibilityPrivate}, w[0])
	_, err = svc.Copy(ctx, 2, mine.ID, "")
	require.ErrorIs(t, err, ErrFolderNotFound, "another account's private folder cannot be copied")
	_, err = svc.Copy(ctx, 3, mine.ID, "")
	require.NoError(t, err, "the owner can copy their own private folder")
}

func TestFolderFollowUpdates(t *testing.T) {
	svc := newFolderSvc(t)
	ctx := context.Background()
	w := sfwWorks(t, "更新作品", 5)
	f := listedFolder(t, svc, 1, FolderCreate{Name: "会更新", Visibility: model.FolderVisibilityPublic}, w[:3]...)
	quiet := listedFolder(t, svc, 1, FolderCreate{Name: "不更新", Visibility: model.FolderVisibilityPublic}, w[:3]...)
	_, err := svc.Follow(ctx, 2, f.ID, "c", "s")
	require.NoError(t, err)
	_, err = svc.Follow(ctx, 2, quiet.ID, "c", "s")
	require.NoError(t, err)
	past := time.Now().Add(-time.Hour)
	require.NoError(t, testDB.Exec("UPDATE catalog_user_folder_follow SET seen_at = ? WHERE actor_uid = 2", past).Error)
	require.NoError(t, testDB.Exec("UPDATE catalog_user_folder_item SET created_at = ? WHERE folder_id IN ?",
		past.Add(-time.Hour), []int64{f.ID, quiet.ID}).Error)

	fresh, err := svc.FollowNewItems(ctx, 2, []int64{f.ID, quiet.ID})
	require.NoError(t, err)
	require.Equal(t, 0, fresh[f.ID], "works already there when the follow began are not news")
	n, err := svc.CountUpdatedFollows(ctx, 2)
	require.NoError(t, err)
	require.Zero(t, n)

	for _, id := range w[3:] {
		_, err := svc.PutItem(ctx, 1, f.ID, id)
		require.NoError(t, err)
	}
	fresh, err = svc.FollowNewItems(ctx, 2, []int64{f.ID, quiet.ID})
	require.NoError(t, err)
	require.Equal(t, 2, fresh[f.ID])
	require.Equal(t, 0, fresh[quiet.ID])
	n, err = svc.CountUpdatedFollows(ctx, 2)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	earlier := past.Add(-time.Hour)
	row, err := svc.MarkFollowSeen(ctx, 2, f.ID, &earlier)
	require.NoError(t, err)
	require.WithinDuration(t, past, row.SeenAt, time.Second, "the mark never moves back")
	future := time.Now().Add(time.Hour)
	row, err = svc.MarkFollowSeen(ctx, 2, f.ID, &future)
	require.NoError(t, err)
	require.True(t, row.SeenAt.Before(time.Now().Add(time.Second)), "nor past now")
	n, err = svc.CountUpdatedFollows(ctx, 2)
	require.NoError(t, err)
	require.Zero(t, n)
	_, err = svc.MarkFollowSeen(ctx, 3, f.ID, nil)
	require.ErrorIs(t, err, ErrFolderNotFound)
}

func TestFolderProgress(t *testing.T) {
	svc := newFolderSvc(t)
	ctx := context.Background()
	require.NoError(t, testDB.Exec("DELETE FROM catalog_user_work_state WHERE actor_uid = 2").Error)
	w := sfwWorks(t, "进度作品", 5)
	list := listedFolder(t, svc, 1, FolderCreate{Name: "别人的清单", Visibility: model.FolderVisibilityPublic}, w...)
	mine := listedFolder(t, svc, 2, FolderCreate{Name: "我的收藏", Visibility: model.FolderVisibilityPrivate}, w[0], w[1])
	secret := listedFolder(t, svc, 3, FolderCreate{Name: "别人的私密", Visibility: model.FolderVisibilityPrivate}, w[0])
	for i, st := range []int16{model.WorkStateDone, model.WorkStateDone, model.WorkStateWish} {
		require.NoError(t, testDB.Create(&model.CatalogUserWorkState{ActorUID: 2, WorkID: w[i+1], State: st}).Error)
	}
	require.NoError(t, testDB.Exec("UPDATE catalog_work SET deleted_at = now() WHERE id = ?", w[4]).Error)

	rows, err := svc.Progress(ctx, 2, []int64{list.ID, mine.ID, secret.ID})
	require.NoError(t, err)
	require.Equal(t, []FolderProgress{
		{FolderID: list.ID, ItemCount: 4, Collected: 2, Done: 2, Wish: 1},
		{FolderID: mine.ID, ItemCount: 2, Collected: 0, Done: 1},
	}, rows, "another account's private folder is left out; collected counts the bearer's other folders only")
}
