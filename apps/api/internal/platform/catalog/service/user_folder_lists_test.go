package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/require"
)

func folderOrder(t *testing.T, svc *UserFolderService, uid, folderID int64) []int64 {
	t.Helper()
	rows, err := svc.ListItems(context.Background(), uid, folderID, FolderItemPage{Order: FolderItemsByPosition, Limit: 100})
	require.NoError(t, err)
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.WorkID
	}
	return out
}

func TestFolderItemOrderAndNotes(t *testing.T) {
	svc := newFolderSvc(t)
	ctx := context.Background()
	f, err := svc.Create(ctx, FolderCreate{OwnerUID: 1, Name: "入坑顺序"})
	require.NoError(t, err)
	var w []int64
	for i := 0; i < 4; i++ {
		work := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "排序作品"+string(rune('A'+i)))
		row, err := svc.PutItem(ctx, 1, f.ID, work.ID)
		require.NoError(t, err)
		require.Equal(t, int64(i+1)*model.FolderItemPositionGap, row.Position, "new works are appended at the end")
		w = append(w, work.ID)
	}
	require.Equal(t, w, folderOrder(t, svc, 1, f.ID))

	moved, changed, err := svc.PatchItem(ctx, 1, f.ID, w[3], FolderItemPatch{BeforeWorkID: &w[0]})
	require.NoError(t, err)
	require.False(t, changed)
	require.Greater(t, moved.Position, int64(0), "positions stay positive: 0 means never placed")
	require.Equal(t, []int64{w[3], w[0], w[1], w[2]}, folderOrder(t, svc, 1, f.ID))

	_, _, err = svc.PatchItem(ctx, 1, f.ID, w[3], FolderItemPatch{AfterWorkID: &w[2]})
	require.NoError(t, err)
	require.Equal(t, []int64{w[0], w[1], w[2], w[3]}, folderOrder(t, svc, 1, f.ID))

	for i := 0; i < 15; i++ {
		mover, anchor := w[2], w[0]
		if i%2 == 1 {
			mover = w[1]
		}
		_, _, err = svc.PatchItem(ctx, 1, f.ID, mover, FolderItemPatch{AfterWorkID: &anchor})
		require.NoError(t, err)
		require.Equal(t, anchor, folderOrder(t, svc, 1, f.ID)[0])
		require.Equal(t, mover, folderOrder(t, svc, 1, f.ID)[1])
	}

	_, _, err = svc.PatchItem(ctx, 1, f.ID, w[0], FolderItemPatch{AfterWorkID: &w[0]})
	require.ErrorIs(t, err, ErrFolderItemAnchor)
	stranger := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "不在夹里")
	_, _, err = svc.PatchItem(ctx, 1, f.ID, w[0], FolderItemPatch{AfterWorkID: &stranger.ID})
	require.ErrorIs(t, err, ErrFolderItemAnchor)
	_, _, err = svc.PatchItem(ctx, 1, f.ID, stranger.ID, FolderItemPatch{AfterWorkID: &w[0]})
	require.ErrorIs(t, err, ErrFolderItemNotFound)
	_, _, err = svc.PatchItem(ctx, 1, f.ID, w[0], FolderItemPatch{AfterWorkID: &w[1], BeforeWorkID: &w[2]})
	require.ErrorIs(t, err, ErrFolderItemAnchorBoth)
	_, _, err = svc.PatchItem(ctx, 2, f.ID, w[0], FolderItemPatch{AfterWorkID: &w[1]})
	require.ErrorIs(t, err, ErrFolderNotFound)

	note := "  共通线就很好哭  "
	row, changed, err := svc.PatchItem(ctx, 1, f.ID, w[0], FolderItemPatch{Note: &note})
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "共通线就很好哭", row.Note)
	_, changed, err = svc.PatchItem(ctx, 1, f.ID, w[0], FolderItemPatch{Note: &note})
	require.NoError(t, err)
	require.False(t, changed, "an unchanged note writes nothing")
	long := strings.Repeat("字", model.FolderItemNoteMax+1)
	_, _, err = svc.PatchItem(ctx, 1, f.ID, w[0], FolderItemPatch{Note: &long})
	require.ErrorIs(t, err, ErrFolderItemNoteTooLong)

	noted, err := svc.NotedItems(ctx, f.ID)
	require.NoError(t, err)
	require.Len(t, noted, 1)

	cleared, err := svc.ModerationSetItemNote(ctx, f.ID, w[0], "")
	require.NoError(t, err)
	require.Empty(t, cleared.Note)
	require.True(t, cleared.UpdatedAt.After(row.UpdatedAt), "a moderated note must reach the owner's sync")
}

func TestFolderItemPositionCursor(t *testing.T) {
	svc := newFolderSvc(t)
	ctx := context.Background()
	f, err := svc.Create(ctx, FolderCreate{OwnerUID: 1, Name: "翻页"})
	require.NoError(t, err)
	var want []int64
	for i := 0; i < 5; i++ {
		work := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "翻页作品"+string(rune('a'+i)))
		_, err := svc.PutItem(ctx, 1, f.ID, work.ID)
		require.NoError(t, err)
		want = append(want, work.ID)
	}
	var got []int64
	p := FolderItemPage{Order: FolderItemsByPosition, Limit: 2}
	for {
		rows, err := svc.ListItems(ctx, 1, f.ID, p)
		require.NoError(t, err)
		if len(rows) == 0 {
			break
		}
		for _, r := range rows {
			got = append(got, r.WorkID)
		}
		last := rows[len(rows)-1]
		p.After, p.SincePosition, p.SinceWorkID = true, last.Position, last.WorkID
	}
	require.Equal(t, want, got)
}

func TestFolderFollows(t *testing.T) {
	svc := newFolderSvc(t)
	ctx := context.Background()
	pub, err := svc.Create(ctx, FolderCreate{OwnerUID: 1, Name: "公开目录", Visibility: model.FolderVisibilityPublic})
	require.NoError(t, err)
	priv, err := svc.Create(ctx, FolderCreate{OwnerUID: 1, Name: "私密"})
	require.NoError(t, err)

	_, err = svc.Follow(ctx, 1, pub.ID, "c", "s")
	require.ErrorIs(t, err, ErrFolderFollowOwn)
	_, err = svc.Follow(ctx, 2, priv.ID, "c", "s")
	require.ErrorIs(t, err, ErrFolderNotFound)

	first, err := svc.Follow(ctx, 2, pub.ID, "c", "s")
	require.NoError(t, err)
	again, err := svc.Follow(ctx, 2, pub.ID, "c", "s")
	require.NoError(t, err)
	require.Equal(t, first.ID, again.ID)
	_, err = svc.Follow(ctx, 3, pub.ID, "c", "s")
	require.NoError(t, err)
	got, err := svc.GetPublic(ctx, pub.ID)
	require.NoError(t, err)
	require.Equal(t, 2, got.FollowerCount, "an idempotent re-follow must not double-count")
	require.Equal(t, pub.UpdatedAt.UTC().Truncate(time.Microsecond), got.UpdatedAt.UTC().Truncate(time.Microsecond),
		"a follower does not move updated_at")

	rows, err := svc.ListFollows(ctx, 2, 0, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)

	private := model.FolderVisibilityPrivate
	_, err = svc.Patch(ctx, 1, pub.ID, FolderPatch{Visibility: &private})
	require.NoError(t, err)
	rows, err = svc.ListFollows(ctx, 2, 0, 10)
	require.NoError(t, err)
	require.Empty(t, rows, "a folder gone private drops out of its followers' lists")
	n, err := svc.CountFollows(ctx, 2)
	require.NoError(t, err)
	require.Zero(t, n)
	public := model.FolderVisibilityPublic
	_, err = svc.Patch(ctx, 1, pub.ID, FolderPatch{Visibility: &public})
	require.NoError(t, err)
	batch, err := svc.ListFollowsFor(ctx, 2, []int64{pub.ID, priv.ID})
	require.NoError(t, err)
	require.Len(t, batch, 1, "the follow comes back with the folder")

	require.NoError(t, svc.Unfollow(ctx, 2, pub.ID))
	require.NoError(t, svc.Unfollow(ctx, 2, pub.ID))
	got, err = svc.GetPublic(ctx, pub.ID)
	require.NoError(t, err)
	require.Equal(t, 1, got.FollowerCount)

	_, err = svc.Follow(ctx, 2, pub.ID, "c", "s")
	require.NoError(t, err)
	_, err = PurgeAccount(ctx, testDB, 3)
	require.NoError(t, err)
	got, err = svc.GetPublic(ctx, pub.ID)
	require.NoError(t, err)
	require.Equal(t, 1, got.FollowerCount, "an erased follower is recounted out")

	_, err = PurgeAccount(ctx, testDB, 1)
	require.NoError(t, err)
	var left int64
	require.NoError(t, testDB.Model(&model.CatalogUserFolderFollow{}).Where("folder_id = ?", pub.ID).Count(&left).Error)
	require.Zero(t, left, "erasing the owner removes the follows on its folders")
}

func TestFolderDirectory(t *testing.T) {
	svc := newFolderSvc(t)
	ctx := context.Background()
	sfw := []int64{}
	for i := 0; i < 3; i++ {
		sfw = append(sfw, createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "全年龄"+string(rune('A'+i))).ID)
	}
	sensitive := createWorkX(t, galgameMediumID, model.ContentRatingSensitive, model.WorkStatusLive, "擦边").ID
	adult := createWorkX(t, galgameMediumID, model.ContentRatingR18, model.WorkStatusLive, "成人").ID

	mk := func(owner int64, name string, isDefault bool, works ...int64) *model.CatalogUserFolder {
		f, err := svc.Create(ctx, FolderCreate{OwnerUID: owner, Name: name, Visibility: model.FolderVisibilityPublic, IsDefault: isDefault})
		require.NoError(t, err)
		for _, w := range works {
			_, err := svc.PutItem(ctx, owner, f.ID, w)
			require.NoError(t, err)
		}
		return f
	}
	allAges := mk(1, "全年龄推荐", false, sfw...)
	mixed := mk(2, "混合", false, sfw[0], sfw[1], sensitive)
	r18 := mk(3, "成人向", false, sfw[0], sfw[1], adult)
	mk(4, "默认收藏", true, sfw...)
	mk(5, "太少", false, sfw[0], sfw[1])
	blank, err := svc.Create(ctx, FolderCreate{OwnerUID: 6, Name: "x", Visibility: model.FolderVisibilityPublic})
	require.NoError(t, err)
	for _, w := range sfw {
		_, err := svc.PutItem(ctx, 6, blank.ID, w)
		require.NoError(t, err)
	}
	empty := ""
	_, err = svc.ModerationPatch(ctx, blank.ID, FolderPatch{Name: &empty})
	require.NoError(t, err)
	private, err := svc.Create(ctx, FolderCreate{OwnerUID: 7, Name: "私密的"})
	require.NoError(t, err)
	for _, w := range sfw {
		_, err := svc.PutItem(ctx, 7, private.ID, w)
		require.NoError(t, err)
	}

	_, err = svc.Follow(ctx, 9, mixed.ID, "c", "s")
	require.NoError(t, err)
	_, err = svc.Follow(ctx, 8, mixed.ID, "c", "s")
	require.NoError(t, err)
	_, err = svc.Follow(ctx, 9, r18.ID, "c", "s")
	require.NoError(t, err)

	ids := func(q PublicFolderQuery) []int64 {
		t.Helper()
		q.Limit = 50
		rows, err := svc.ListPublicFolders(ctx, q)
		require.NoError(t, err)
		out := make([]int64, len(rows))
		for i, r := range rows {
			out[i] = r.ID
		}
		n, err := svc.CountPublicFolders(ctx, q)
		require.NoError(t, err)
		require.Equal(t, int64(len(out)), n)
		return out
	}
	require.Equal(t, []int64{mixed.ID, allAges.ID}, ids(PublicFolderQuery{Sort: FolderSortFollowers}),
		"nsfw=false leaves r18 folders out; default, blank-named, short and private folders never list")
	require.Equal(t, []int64{mixed.ID, r18.ID, allAges.ID}, ids(PublicFolderQuery{Sort: FolderSortFollowers, NSFW: true}))
	require.Equal(t, []int64{r18.ID, mixed.ID, allAges.ID}, ids(PublicFolderQuery{Sort: FolderSortCreated, NSFW: true}))
	require.Equal(t, []int64{r18.ID}, ids(PublicFolderQuery{NSFW: true, ContainsWorkID: adult}))
	require.Equal(t, []int64{mixed.ID}, ids(PublicFolderQuery{Sort: FolderSortFollowers, ContainsWorkID: sensitive}))

	last, err := svc.PutItem(ctx, 1, allAges.ID, sensitive)
	require.NoError(t, err)
	require.NotZero(t, last.Position)
	require.Equal(t, []int64{allAges.ID, r18.ID, mixed.ID}, ids(PublicFolderQuery{Sort: FolderSortUpdated, NSFW: true}))

	require.Len(t, ids(PublicFolderQuery{OwnerUID: 4}), 1, "the owner lane lists every public folder, default included")

	rows, err := svc.ListPublicFolders(ctx, PublicFolderQuery{Sort: FolderSortFollowers, NSFW: true, Limit: 1})
	require.NoError(t, err)
	require.Equal(t, mixed.ID, rows[0].ID)
	rows, err = svc.ListPublicFolders(ctx, PublicFolderQuery{Sort: FolderSortFollowers, NSFW: true, Limit: 5,
		Cursor: FolderCursor{Set: true, ID: rows[0].ID, Followers: rows[0].FollowerCount}})
	require.NoError(t, err)
	require.Equal(t, r18.ID, rows[0].ID)

	ratings, err := svc.FolderContentRatings(ctx, []int64{allAges.ID, mixed.ID, r18.ID, private.ID})
	require.NoError(t, err)
	require.Equal(t, map[int64]int16{
		allAges.ID: model.ContentRatingSensitive, mixed.ID: model.ContentRatingSensitive,
		r18.ID: model.ContentRatingR18, private.ID: model.ContentRatingAllAges,
	}, ratings)
}

func TestFolderMergeCarriesTheNote(t *testing.T) {
	svc := newFolderSvc(t)
	ctx := context.Background()
	src := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "带备注的源")
	dst := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "没备注的靶")
	f, err := svc.Create(ctx, FolderCreate{OwnerUID: 1, Name: "备注"})
	require.NoError(t, err)
	_, err = svc.PutItem(ctx, 1, f.ID, dst.ID)
	require.NoError(t, err)
	_, err = svc.PutItem(ctx, 1, f.ID, src.ID)
	require.NoError(t, err)
	note := "这条备注写在源作品上"
	_, _, err = svc.PatchItem(ctx, 1, f.ID, src.ID, FolderItemPatch{Note: &note})
	require.NoError(t, err)

	p, err := testMerge.ProposeMerge(ctx, model.EntityTypeWork, src.ID, dst.ID, 7, "folder note")
	require.NoError(t, err)
	approveAndForceExecutable(t, p.ID)
	require.NoError(t, testMerge.ExecuteMerge(ctx, p.ID, nil))

	rows, err := svc.ListItems(ctx, 1, f.ID, FolderItemPage{Limit: 10})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, dst.ID, rows[0].WorkID)
	require.Equal(t, note, rows[0].Note, "the survivor had no note, so the folded one's note carries over")
}

func TestFolderRecentWorksAndOwnerItem(t *testing.T) {
	svc := newFolderSvc(t)
	ctx := context.Background()
	f, err := svc.Create(ctx, FolderCreate{OwnerUID: 1, Name: "最近收藏"})
	require.NoError(t, err)
	empty, err := svc.Create(ctx, FolderCreate{OwnerUID: 1, Name: "空的"})
	require.NoError(t, err)
	base := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	var w []int64
	for i := 0; i < 6; i++ {
		work := createWorkX(t, galgameMediumID, model.ContentRatingR18, model.WorkStatusLive, "最近作品"+string(rune('A'+i)))
		_, err := svc.PutItem(ctx, 1, f.ID, work.ID)
		require.NoError(t, err)
		require.NoError(t, testDB.Exec("UPDATE catalog_user_folder_item SET created_at = ? WHERE folder_id = ? AND work_id = ?",
			base.Add(time.Duration(i)*time.Hour), f.ID, work.ID).Error)
		w = append(w, work.ID)
	}
	require.NoError(t, testDB.Exec("UPDATE catalog_work SET deleted_at = now() WHERE id = ?", w[4]).Error)

	recent, err := svc.FolderRecentWorks(ctx, []int64{f.ID, empty.ID})
	require.NoError(t, err)
	require.Equal(t, []int64{w[5], w[3], w[2], w[1]}, recent[f.ID], "newest first, capped at four, deleted works skipped")
	require.Empty(t, recent[empty.ID])

	note := "先玩这个"
	_, _, err = svc.PatchItem(ctx, 1, f.ID, w[1], FolderItemPatch{Note: &note})
	require.NoError(t, err)
	item, err := svc.Item(ctx, 1, f.ID, w[1])
	require.NoError(t, err)
	require.Equal(t, note, item.Note)
	_, err = svc.Item(ctx, 2, f.ID, w[1])
	require.ErrorIs(t, err, ErrFolderNotFound, "another account's folder reads as absent")
	_, err = svc.Item(ctx, 1, empty.ID, w[1])
	require.ErrorIs(t, err, ErrFolderItemNotFound)
	_, err = svc.Item(ctx, 1, f.ID, w[4])
	require.ErrorIs(t, err, ErrFolderItemNotFound, "a deleted work is not an item")
}
