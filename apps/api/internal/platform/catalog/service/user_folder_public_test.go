package service

import (
	"context"
	"testing"
	"time"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/require"
)

func TestPublicLaneShowsOnlyPublicFolders(t *testing.T) {
	svc := newFolderSvc(t)
	ctx := context.Background()

	pub, err := svc.Create(ctx, FolderCreate{OwnerUID: 1, Name: "public one", Visibility: 1})
	require.NoError(t, err)
	priv, err := svc.Create(ctx, FolderCreate{OwnerUID: 1, Name: "private one"})
	require.NoError(t, err)
	_, err = svc.Create(ctx, FolderCreate{OwnerUID: 2, Name: "someone else", Visibility: 1})
	require.NoError(t, err)

	rows, err := svc.ListPublic(ctx, 1, 0, 50)
	require.NoError(t, err)
	require.Len(t, rows, 1, "uid 1 has one public folder and one private one")
	require.Equal(t, pub.ID, rows[0].ID)

	n, err := svc.CountPublic(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)

	// The lane does not widen for the owner: there is no uid on it to widen for.
	_, err = svc.GetPublic(ctx, priv.ID)
	require.ErrorIs(t, err, ErrFolderNotFound)
	got, err := svc.GetPublic(ctx, pub.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), got.OwnerUID)
}

func TestPublicItemsRefuseAPrivateFolder(t *testing.T) {
	svc := newFolderSvc(t)
	ctx := context.Background()
	w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "公開棚の作品")

	priv, err := svc.Create(ctx, FolderCreate{OwnerUID: 1, Name: "private"})
	require.NoError(t, err)
	_, err = svc.PutItem(ctx, 1, priv.ID, w.ID)
	require.NoError(t, err)

	_, err = svc.ListPublicItems(ctx, priv.ID, time.Time{}, 0, 50)
	require.ErrorIs(t, err, ErrFolderNotFound)
	_, err = svc.CountPublicItems(ctx, priv.ID)
	require.ErrorIs(t, err, ErrFolderNotFound)

	vis := int16(1)
	_, err = svc.Patch(ctx, 1, priv.ID, FolderPatch{Visibility: &vis})
	require.NoError(t, err)

	// Positive control: the same folder answers once its owner publishes it,
	// so the refusal above is the visibility gate and not an empty fixture.
	items, err := svc.ListPublicItems(ctx, priv.ID, time.Time{}, 0, 50)
	require.NoError(t, err)
	require.Len(t, items, 1)
}

func TestContainsWorkKeepsOnlyTheFoldersHoldingIt(t *testing.T) {
	svc := newFolderSvc(t)
	ctx := context.Background()
	w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "検索対象")
	other := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "別の作品")

	holds, err := svc.Create(ctx, FolderCreate{OwnerUID: 1, Name: "holds it"})
	require.NoError(t, err)
	alsoHolds, err := svc.Create(ctx, FolderCreate{OwnerUID: 1, Name: "holds it too"})
	require.NoError(t, err)
	_, err = svc.Create(ctx, FolderCreate{OwnerUID: 1, Name: "holds something else"})
	require.NoError(t, err)
	otherFolder, err := svc.Create(ctx, FolderCreate{OwnerUID: 2, Name: "another user's"})
	require.NoError(t, err)

	for _, id := range []int64{holds.ID, alsoHolds.ID} {
		_, err = svc.PutItem(ctx, 1, id, w.ID)
		require.NoError(t, err)
	}
	_, err = svc.PutItem(ctx, 2, otherFolder.ID, w.ID)
	require.NoError(t, err)

	rows, err := svc.ListMineContaining(ctx, 1, w.ID, 0, 50)
	require.NoError(t, err)
	require.Len(t, rows, 2, "the third folder holds a different work and the fourth is someone else's")
	require.Equal(t, holds.ID, rows[0].ID)
	require.Equal(t, alsoHolds.ID, rows[1].ID)

	n, err := svc.CountMineContaining(ctx, 1, w.ID)
	require.NoError(t, err)
	require.Equal(t, int64(2), n)

	none, err := svc.ListMineContaining(ctx, 1, other.ID, 0, 50)
	require.NoError(t, err)
	require.Empty(t, none, "control: a work nobody filed answers empty, not everything")
}

// The owner's own delete refuses the default folder. Honouring that here would
// let anyone make a folder undeletable by marking it default, so moderation
// deliberately does not.
func TestModerationDeleteAcceptsTheDefaultFolder(t *testing.T) {
	svc := newFolderSvc(t)
	ctx := context.Background()
	w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "既定棚の作品")

	def, err := svc.Create(ctx, FolderCreate{OwnerUID: 1, Name: "spam", IsDefault: true})
	require.NoError(t, err)
	_, err = svc.PutItem(ctx, 1, def.ID, w.ID)
	require.NoError(t, err)

	require.ErrorIs(t, svc.Delete(ctx, 1, def.ID), ErrFolderDefaultDeletion)
	require.NoError(t, svc.ModerationDelete(ctx, def.ID))

	_, err = svc.ModerationGet(ctx, def.ID)
	require.ErrorIs(t, err, ErrFolderNotFound)
	var items int64
	require.NoError(t, testDB.Table("catalog_user_folder_item").
		Where("folder_id = ?", def.ID).Count(&items).Error)
	require.Zero(t, items, "the folder's items go with it")
}

func TestModerationPatchBlanksTextAndHidesButKeepsItems(t *testing.T) {
	svc := newFolderSvc(t)
	ctx := context.Background()
	w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "通報された棚の作品")

	f, err := svc.Create(ctx, FolderCreate{
		OwnerUID: 1, Name: "abusive name", Description: "abusive note", Visibility: 1,
	})
	require.NoError(t, err)
	_, err = svc.PutItem(ctx, 1, f.ID, w.ID)
	require.NoError(t, err)

	blank, priv := "", int16(0)
	got, err := svc.ModerationPatch(ctx, f.ID, FolderPatch{
		Name: &blank, Description: &blank, Visibility: &priv,
	})
	require.NoError(t, err)
	require.Empty(t, got.Name, "the owner path refuses an empty name; moderation is where blanking lives")
	require.Empty(t, got.Description)
	require.Equal(t, int16(0), got.Visibility)

	_, err = svc.GetPublic(ctx, f.ID)
	require.ErrorIs(t, err, ErrFolderNotFound, "forcing it private takes it off the public lane")

	var items int64
	require.NoError(t, testDB.Table("catalog_user_folder_item").
		Where("folder_id = ?", f.ID).Count(&items).Error)
	require.Equal(t, int64(1), items, "moderation acts on the text, never on what the folder holds")

	_, err = svc.ModerationPatch(ctx, f.ID, FolderPatch{})
	require.ErrorIs(t, err, ErrFolderNothingToUpdate)
}

// The importer trusts catalog_user_folder_import to say which folder a source
// collection already became, and does not check that the folder still exists.
// A row that outlives its folder therefore makes the next re-run write
// memberships into an id nothing owns, so every delete path clears it.
func TestDeletingAFolderClearsItsImportProvenance(t *testing.T) {
	svc := newFolderSvc(t)
	ctx := context.Background()
	require.NoError(t, testDB.Exec("TRUNCATE catalog_user_folder_import").Error)

	mine, err := svc.Create(ctx, FolderCreate{OwnerUID: 1, Name: "imported"})
	require.NoError(t, err)
	moderated, err := svc.Create(ctx, FolderCreate{OwnerUID: 1, Name: "imported too"})
	require.NoError(t, err)
	purged, err := svc.Create(ctx, FolderCreate{OwnerUID: 2, Name: "imported elsewhere"})
	require.NoError(t, err)
	kept, err := svc.Create(ctx, FolderCreate{OwnerUID: 3, Name: "untouched"})
	require.NoError(t, err)

	for i, f := range []*model.CatalogUserFolder{mine, moderated, purged, kept} {
		require.NoError(t, testDB.Create(&model.CatalogUserFolderImport{
			Site: model.FolderImportSiteForum, SourceID: int64(9000 + i),
			FolderID: f.ID, OwnerUID: f.OwnerUID,
		}).Error)
	}

	require.NoError(t, svc.Delete(ctx, 1, mine.ID))
	require.NoError(t, svc.ModerationDelete(ctx, moderated.ID))
	_, _, err = svc.PurgeOwner(ctx, 2)
	require.NoError(t, err)

	var orphans int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_user_folder_import p
		WHERE NOT EXISTS (SELECT 1 FROM catalog_user_folder f WHERE f.id = p.folder_id)`).
		Scan(&orphans).Error)
	require.Zero(t, orphans, "all three delete paths must take the provenance row with them")

	// Control: the surviving folder keeps its row, so the assertion above is
	// not just an empty table.
	var left int64
	require.NoError(t, testDB.Model(&model.CatalogUserFolderImport{}).
		Where("folder_id = ?", kept.ID).Count(&left).Error)
	require.Equal(t, int64(1), left)
}

func TestPurgeOwnerRemovesEverythingTheAccountHolds(t *testing.T) {
	svc := newFolderSvc(t)
	ctx := context.Background()
	w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "退会する人の作品")

	a, err := svc.Create(ctx, FolderCreate{OwnerUID: 1, Name: "one", IsDefault: true})
	require.NoError(t, err)
	b, err := svc.Create(ctx, FolderCreate{OwnerUID: 1, Name: "two", Visibility: 1})
	require.NoError(t, err)
	other, err := svc.Create(ctx, FolderCreate{OwnerUID: 2, Name: "a bystander"})
	require.NoError(t, err)
	for _, f := range []*model.CatalogUserFolder{a, b, other} {
		_, err = svc.PutItem(ctx, f.OwnerUID, f.ID, w.ID)
		require.NoError(t, err)
	}

	folders, items, err := svc.PurgeOwner(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, int64(2), folders, "the default folder goes too — nobody is left to keep it for")
	require.Equal(t, int64(2), items)

	rows, err := svc.ListMine(ctx, 1, 0, 50)
	require.NoError(t, err)
	require.Empty(t, rows)

	// Control: the bystander is untouched, so the purge was scoped by owner.
	left, err := svc.ListMine(ctx, 2, 0, 50)
	require.NoError(t, err)
	require.Len(t, left, 1)
	require.Equal(t, other.ID, left[0].ID)
	n, err := svc.CountItems(ctx, 2, other.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)

	folders, items, err = svc.PurgeOwner(ctx, 1)
	require.NoError(t, err)
	require.Zero(t, folders, "an account holding nothing purges to zeros, not an error")
	require.Zero(t, items)
}
