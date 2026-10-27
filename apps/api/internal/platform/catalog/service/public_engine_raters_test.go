package service

import (
	"testing"

	"api/internal/platform/catalog/model"
)

func rateWork(t *testing.T, workID int64, source int16, votes int) {
	t.Helper()
	if err := testDB.Create(&model.CatalogWorkRating{WorkID: workID, SourceID: source, Score: 7, VoteCount: votes}).Error; err != nil {
		t.Fatalf("rate work: %v", err)
	}
}

func TestEnginesRankByRaters(t *testing.T) {
	cleanTables(t)
	cleanTaxonomyTables(t)
	svc := newPublicSvc()
	ctx := t.Context()

	crowded := createEngine(t, "Crowded")
	classic := createEngine(t, "Classic")
	adult := createEngine(t, "Adult")
	empty := createEngine(t, "Empty")

	work := func(name string, rating int16, engine int64, i int) int64 {
		w := createWorkX(t, galgameMediumID, rating, model.WorkStatusLive, name)
		claimLive(t, w.ID, int64(9700+i))
		attachEngine(t, w.ID, engine)
		return w.ID
	}
	for i := range 5 {
		rateWork(t, work("filler", model.ContentRatingAllAges, crowded, i), model.SourceVNDB, 2)
	}
	hit := work("hit", model.ContentRatingAllAges, classic, 10)
	rateWork(t, hit, model.SourceVNDB, 300)
	rateWork(t, hit, model.SourceBangumi, 500)
	rateWork(t, hit, model.SourceErogameScape, 100)
	rateWork(t, hit, 4, 90000)
	r18 := work("r18", model.ContentRatingR18, adult, 11)
	rateWork(t, r18, model.SourceVNDB, 1000)

	list, err := svc.EnginesList(ctx, EnginesListFilter{Sort: engineSortPopularity}, "", 50)
	if err != nil {
		t.Fatalf("EnginesList popularity: %v", err)
	}
	got := map[int64][2]int64{}
	var order []int64
	for _, it := range list.Items {
		order = append(order, it.ID)
		got[it.ID] = [2]int64{it.RaterCount, int64(it.RatedWorkCount)}
	}
	if got[classic] != [2]int64{500, 1} {
		t.Fatalf("classic = %v, want [500 1]: a work counts its largest VNDB/Bangumi/ErogameScape rater count once, DLsite ignored", got[classic])
	}
	if got[crowded] != [2]int64{10, 0} {
		t.Fatalf("crowded = %v, want [10 0]: five works under the floor rate no work", got[crowded])
	}
	if got[adult] != [2]int64{0, 0} {
		t.Fatalf("adult = %v, want [0 0] without nsfw", got[adult])
	}
	if want := []int64{classic, crowded, adult, empty}; len(order) != 4 || order[0] != want[0] || order[1] != want[1] || order[2] != want[2] || order[3] != want[3] {
		t.Fatalf("popularity order = %v, want %v (ties by id)", order, want)
	}
	if list.Total != 4 || list.NextCursor != nil {
		t.Fatalf("total = %d next = %v, want 4 and no cursor", list.Total, list.NextCursor)
	}

	byID, err := svc.EnginesList(ctx, EnginesListFilter{}, "", 50)
	if err != nil {
		t.Fatalf("EnginesList id: %v", err)
	}
	if byID.Items[1].ID != classic || byID.Items[1].RaterCount != 500 {
		t.Fatalf("id-sorted classic = %+v, want rater_count 500 on the default sort too", byID.Items[1])
	}

	nsfw, err := svc.EnginesList(ctx, EnginesListFilter{Sort: engineSortPopularity, NSFW: true}, "", 50)
	if err != nil {
		t.Fatalf("EnginesList nsfw: %v", err)
	}
	if nsfw.Items[0].ID != adult || nsfw.Items[0].RaterCount != 1000 {
		t.Fatalf("nsfw first = %+v, want adult with 1000", nsfw.Items[0])
	}

	var paged []int64
	cursor := ""
	for range 5 {
		page, err := svc.EnginesList(ctx, EnginesListFilter{Sort: engineSortPopularity}, cursor, 1)
		if err != nil {
			t.Fatalf("page: %v", err)
		}
		for _, it := range page.Items {
			paged = append(paged, it.ID)
		}
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
	}
	if len(paged) != 4 || paged[0] != classic || paged[3] != empty {
		t.Fatalf("paged = %v, want the same four in rank order", paged)
	}
	if _, err := svc.EnginesList(ctx, EnginesListFilter{}, cursor, 1); err != ErrBadCursor {
		t.Fatalf("popularity cursor on the id sort: err = %v, want ErrBadCursor", err)
	}

	batch, err := svc.EnginesList(ctx, EnginesListFilter{Sort: engineSortPopularity, IDs: []int64{empty, classic}}, "", 1)
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	if len(batch.Items) != 2 || batch.Items[0].ID != classic || batch.NextCursor != nil {
		t.Fatalf("batch = %+v, want both ids ranked and unpaginated", batch.Items)
	}

	detail, found, err := svc.EngineDetail(ctx, classic, false)
	if err != nil || !found || detail.RaterCount != 500 || detail.RatedWorkCount != 1 {
		t.Fatalf("detail = %+v found=%v err=%v", detail, found, err)
	}
}
