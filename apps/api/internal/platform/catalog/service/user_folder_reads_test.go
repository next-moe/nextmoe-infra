package service

import (
	"context"
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/require"
)

func TestFolderHoldingsAndHolders(t *testing.T) {
	svc := newFolderSvc(t)
	ctx := context.Background()

	mine, err := svc.Create(ctx, FolderCreate{OwnerUID: 1, Name: "private", Visibility: model.FolderVisibilityPrivate})
	require.NoError(t, err)
	alsoMine, err := svc.Create(ctx, FolderCreate{OwnerUID: 1, Name: "public", Visibility: model.FolderVisibilityPublic})
	require.NoError(t, err)
	stranger, err := svc.Create(ctx, FolderCreate{OwnerUID: 2, Name: "theirs", Visibility: model.FolderVisibilityPrivate})
	require.NoError(t, err)

	for _, row := range []model.CatalogUserFolderItem{
		{FolderID: mine.ID, WorkID: 10, OwnerUID: 1},
		{FolderID: alsoMine.ID, WorkID: 10, OwnerUID: 1},
		{FolderID: alsoMine.ID, WorkID: 11, OwnerUID: 1},
		{FolderID: stranger.ID, WorkID: 10, OwnerUID: 2},
	} {
		require.NoError(t, testDB.Create(&row).Error)
	}

	held, err := svc.Holdings(ctx, 1, []int64{10, 11, 12})
	require.NoError(t, err)
	require.Equal(t, []FolderHolding{
		{WorkID: 10, FolderID: mine.ID},
		{WorkID: 10, FolderID: alsoMine.ID},
		{WorkID: 11, FolderID: alsoMine.ID},
	}, held, "work-id then folder-id ascending, and never a stranger's folder")

	held, err = svc.Holdings(ctx, 1, nil)
	require.NoError(t, err)
	require.Empty(t, held)

	_, err = svc.Holdings(ctx, 0, []int64{10})
	require.ErrorIs(t, err, ErrFolderActorRequired)

	// Both owners come back although one holds the work only in a private
	// folder: the reverse lookup fans notifications out, it does not display.
	holders, err := svc.HoldersOfWork(ctx, 10, 0, 10)
	require.NoError(t, err)
	require.Equal(t, []int64{1, 2}, holders)

	holders, err = svc.HoldersOfWork(ctx, 10, 1, 10)
	require.NoError(t, err)
	require.Equal(t, []int64{2}, holders, "the cursor is the last owner_uid answered")

	holders, err = svc.HoldersOfWork(ctx, 10, 0, 1)
	require.NoError(t, err)
	require.Equal(t, []int64{1}, holders)

	holders, err = svc.HoldersOfWork(ctx, 999, 0, 10)
	require.NoError(t, err)
	require.Empty(t, holders)
}
