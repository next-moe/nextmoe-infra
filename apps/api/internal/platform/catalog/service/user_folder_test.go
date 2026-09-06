package service

import (
	"context"
	"testing"
	"time"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/require"
)

func newFolderSvc(t *testing.T) *UserFolderService {
	t.Helper()
	require.NoError(t, testDB.Exec("TRUNCATE catalog_user_folder_item, catalog_user_folder RESTART IDENTITY").Error)
	return NewUserFolderService(testDB)
}

func TestFolderLifecycle(t *testing.T) {
	svc := newFolderSvc(t)
	ctx := context.Background()

	a, err := svc.Create(ctx, FolderCreate{OwnerUID: 1, Name: " 默认收藏 ", IsDefault: true})
	require.NoError(t, err)
	require.Equal(t, "默认收藏", a.Name)
	require.True(t, a.IsDefault)

	b, err := svc.Create(ctx, FolderCreate{OwnerUID: 1, Name: "二周目", IsDefault: true})
	require.NoError(t, err)
	require.True(t, b.IsDefault)
	a, err = svc.Get(ctx, 1, a.ID)
	require.NoError(t, err)
	require.False(t, a.IsDefault, "creating a second default must demote the first")

	flag := true
	a, err = svc.Patch(ctx, 1, a.ID, FolderPatch{IsDefault: &flag})
	require.NoError(t, err)
	require.True(t, a.IsDefault)
	b, err = svc.Get(ctx, 1, b.ID)
	require.NoError(t, err)
	require.False(t, b.IsDefault, "patching the flag onto a folder must demote the holder")

	off := false
	_, err = svc.Patch(ctx, 1, a.ID, FolderPatch{IsDefault: &off})
	require.ErrorIs(t, err, ErrFolderDefaultOnly)

	blank := "  "
	_, err = svc.Patch(ctx, 1, a.ID, FolderPatch{Name: &blank})
	require.ErrorIs(t, err, ErrFolderNameRequired)

	_, err = svc.Patch(ctx, 1, a.ID, FolderPatch{})
	require.ErrorIs(t, err, ErrFolderNothingToUpdate)

	_, err = svc.Create(ctx, FolderCreate{OwnerUID: 1, Name: "   "})
	require.ErrorIs(t, err, ErrFolderNameRequired)

	_, err = svc.Create(ctx, FolderCreate{OwnerUID: 1, Name: "x", Visibility: 9})
	require.ErrorIs(t, err, ErrFolderBadVisibility)

	got, err := svc.Get(ctx, 2, a.ID)
	require.NoError(t, err)
	require.Nil(t, got, "another user's folder must read as absent")
	_, err = svc.Patch(ctx, 2, a.ID, FolderPatch{IsDefault: &flag})
	require.ErrorIs(t, err, ErrFolderNotFound)
	require.ErrorIs(t, svc.Delete(ctx, 2, a.ID), ErrFolderNotFound)

	require.ErrorIs(t, svc.Delete(ctx, 1, a.ID), ErrFolderDefaultDeletion)
	require.NoError(t, svc.Delete(ctx, 1, b.ID))
	rows, err := svc.ListMine(ctx, 1, 0, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
}

func TestFolderItems(t *testing.T) {
	svc := newFolderSvc(t)
	ctx := context.Background()
	w1 := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "収蔵作品一")
	w2 := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "収蔵作品二")
	dead := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusQuarantine, "隔離作品")

	f, err := svc.Create(ctx, FolderCreate{OwnerUID: 1, Name: "收藏"})
	require.NoError(t, err)

	first, err := svc.PutItem(ctx, 1, f.ID, w1.ID)
	require.NoError(t, err)
	f, err = svc.Get(ctx, 1, f.ID)
	require.NoError(t, err)
	require.Equal(t, 1, f.ItemCount)

	again, err := svc.PutItem(ctx, 1, f.ID, w1.ID)
	require.NoError(t, err)
	// Microseconds, not nanoseconds: the first row carries the Go clock's value
	// and the second was read back from timestamptz, so an exact compare fails on
	// a row nothing touched. A real churn is orders of magnitude larger than this.
	require.Equal(t, first.UpdatedAt.UTC().Truncate(time.Microsecond), again.UpdatedAt.UTC().Truncate(time.Microsecond),
		"re-adding an existing membership must not churn the sync watermark")
	f, err = svc.Get(ctx, 1, f.ID)
	require.NoError(t, err)
	require.Equal(t, 1, f.ItemCount, "re-adding must not double-count")

	_, err = svc.PutItem(ctx, 1, f.ID, dead.ID)
	require.ErrorIs(t, err, ErrFolderWorkUnavailable)
	_, err = svc.PutItem(ctx, 1, f.ID, 99_999_999)
	require.ErrorIs(t, err, ErrFolderWorkUnavailable)
	_, err = svc.PutItem(ctx, 2, f.ID, w1.ID)
	require.ErrorIs(t, err, ErrFolderNotFound)

	_, err = svc.PutItem(ctx, 1, f.ID, w2.ID)
	require.NoError(t, err)
	rows, err := svc.ListItems(ctx, 1, f.ID, time.Time{}, 0, 10)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.LessOrEqual(t, rows[0].UpdatedAt.UnixNano(), rows[1].UpdatedAt.UnixNano())

	tail, err := svc.ListItems(ctx, 1, f.ID, rows[0].UpdatedAt, rows[0].WorkID, 10)
	require.NoError(t, err)
	require.Len(t, tail, 1)
	require.Equal(t, rows[1].WorkID, tail[0].WorkID)

	_, err = svc.ListItems(ctx, 2, f.ID, time.Time{}, 0, 10)
	require.ErrorIs(t, err, ErrFolderNotFound)

	require.NoError(t, svc.DeleteItem(ctx, 1, f.ID, w1.ID))
	f, err = svc.Get(ctx, 1, f.ID)
	require.NoError(t, err)
	require.Equal(t, 1, f.ItemCount)
	require.NoError(t, svc.DeleteItem(ctx, 1, f.ID, w1.ID), "removing an absent membership is a no-op")
	f, err = svc.Get(ctx, 1, f.ID)
	require.NoError(t, err)
	require.Equal(t, 1, f.ItemCount)
}

func TestFolderCap(t *testing.T) {
	svc := newFolderSvc(t)
	ctx := context.Background()
	for i := 0; i < model.FoldersPerUserMax; i++ {
		_, err := svc.Create(ctx, FolderCreate{OwnerUID: 5, Name: "夹"})
		require.NoError(t, err)
	}
	_, err := svc.Create(ctx, FolderCreate{OwnerUID: 5, Name: "超限"})
	require.ErrorIs(t, err, ErrFolderLimit)
	_, err = svc.Create(ctx, FolderCreate{OwnerUID: 6, Name: "别人不受限"})
	require.NoError(t, err)
}

func TestFolderMergeRehang(t *testing.T) {
	svc := newFolderSvc(t)
	ctx := context.Background()
	src := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "合并源")
	dst := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "合并靶")

	both, err := svc.Create(ctx, FolderCreate{OwnerUID: 1, Name: "两边都有"})
	require.NoError(t, err)
	_, err = svc.PutItem(ctx, 1, both.ID, src.ID)
	require.NoError(t, err)
	_, err = svc.PutItem(ctx, 1, both.ID, dst.ID)
	require.NoError(t, err)

	only, err := svc.Create(ctx, FolderCreate{OwnerUID: 2, Name: "只有源"})
	require.NoError(t, err)
	moved, err := svc.PutItem(ctx, 2, only.ID, src.ID)
	require.NoError(t, err)

	p, err := testMerge.ProposeMerge(ctx, model.EntityTypeWork, src.ID, dst.ID, 7, "folder rehang")
	require.NoError(t, err)
	approveAndForceExecutable(t, p.ID)
	require.NoError(t, testMerge.ExecuteMerge(ctx, p.ID, nil))

	both, err = svc.Get(ctx, 1, both.ID)
	require.NoError(t, err)
	require.Equal(t, 1, both.ItemCount, "the duplicate membership must be dropped and recounted")
	rows, err := svc.ListItems(ctx, 1, both.ID, time.Time{}, 0, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, dst.ID, rows[0].WorkID)

	rows, err = svc.ListItems(ctx, 2, only.ID, time.Time{}, 0, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, dst.ID, rows[0].WorkID, "the source-only membership must follow the survivor")
	require.True(t, rows[0].UpdatedAt.After(moved.UpdatedAt),
		"a repointed membership must surface on the sync cursor")
	only, err = svc.Get(ctx, 2, only.ID)
	require.NoError(t, err)
	require.Equal(t, 1, only.ItemCount)
}
