package service

import (
	"testing"

	"api/internal/platform/catalog/model"
)

func addFolderItem(t *testing.T, ownerUID, workID int64, folderName string) {
	t.Helper()
	f := &model.CatalogUserFolder{OwnerUID: ownerUID, Name: folderName}
	if err := testDB.Create(f).Error; err != nil {
		t.Fatalf("create folder: %v", err)
	}
	if err := testDB.Create(&model.CatalogUserFolderItem{
		FolderID: f.ID, WorkID: workID, OwnerUID: ownerUID,
	}).Error; err != nil {
		t.Fatalf("create folder item: %v", err)
	}
}

func workFavorites(t *testing.T, svc *PublicService, workID int64) int64 {
	t.Helper()
	rec, found, err := svc.WorkDetail(t.Context(), workID, PublicInclude{}, true, 0, PublicFields{})
	if err != nil || !found {
		t.Fatalf("work detail: found=%v err=%v", found, err)
	}
	for _, p := range rec.Popularity {
		if p.Metric != "favorites" {
			continue
		}
		if p.Source != sourceKeyNextMoe {
			t.Fatalf("favorites source = %q, want %q", p.Source, sourceKeyNextMoe)
		}
		return p.Value
	}
	t.Fatalf("no favorites row in %+v", rec.Popularity)
	return 0
}

func TestFavoritesPopularityCountsUsersNotRows(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()

	work := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "収藏计数")
	other := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "无人收藏")

	if got := workFavorites(t, svc, work.ID); got != 0 {
		t.Fatalf("favorites before any folder = %d, want 0", got)
	}

	// One user holding the same work in two folders is one favorite, not two:
	// the count is over people, and a folder costs nothing to create.
	addFolderItem(t, 1001, work.ID, "主收藏夹")
	addFolderItem(t, 1001, work.ID, "第二个夹")
	addFolderItem(t, 1002, work.ID, "别人的夹")

	if got := workFavorites(t, svc, work.ID); got != 2 {
		t.Fatalf("favorites = %d, want 2 (two users, three rows)", got)
	}
	if got := workFavorites(t, svc, other.ID); got != 0 {
		t.Fatalf("unfavorited work = %d, want an explicit 0", got)
	}
}

func TestFavoritesPopularityRidesAlongsideSourceRows(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()

	work := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "并存")
	if err := testDB.Create(&model.CatalogWorkPopularity{
		WorkID: work.ID, SourceID: 3, Metric: model.PopularityMetricBgmWish, Value: 42,
	}).Error; err != nil {
		t.Fatalf("popularity fixture: %v", err)
	}
	addFolderItem(t, 2001, work.ID, "夹")

	rec, found, err := svc.WorkDetail(t.Context(), work.ID, PublicInclude{}, true, 0, PublicFields{})
	if err != nil || !found {
		t.Fatalf("work detail: found=%v err=%v", found, err)
	}
	if len(rec.Popularity) != 2 {
		t.Fatalf("popularity = %+v, want the stored row plus favorites", rec.Popularity)
	}
	if rec.Popularity[0].Source != "bangumi" || rec.Popularity[0].Metric != "bgm_wish" || rec.Popularity[0].Value != 42 {
		t.Fatalf("stored row = %+v", rec.Popularity[0])
	}
	if rec.Popularity[1].Source != sourceKeyNextMoe || rec.Popularity[1].Metric != "favorites" || rec.Popularity[1].Value != 1 {
		t.Fatalf("favorites row = %+v", rec.Popularity[1])
	}
}
