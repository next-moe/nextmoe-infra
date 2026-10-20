package service

import (
	"slices"
	"testing"

	"api/internal/platform/catalog/model"
)

func coverHashes(t *testing.T, read *ReadService, workID int64) []string {
	t.Helper()
	out := map[int64][]WorkCoverRow{}
	if err := read.nativeWorkCovers(t.Context(), []int64{workID}, out); err != nil {
		t.Fatal(err)
	}
	hashes := make([]string, 0, len(out[workID]))
	for _, r := range out[workID] {
		hashes = append(hashes, r.ImageHash)
	}
	return hashes
}

func screenshotHashes(t *testing.T, read *ReadService, workID int64) []string {
	t.Helper()
	out := map[int64][]WorkScreenshotRow{}
	if err := read.nativeWorkScreenshots(t.Context(), []int64{workID}, out); err != nil {
		t.Fatal(err)
	}
	hashes := make([]string, 0, len(out[workID]))
	for _, r := range out[workID] {
		hashes = append(hashes, r.ImageHash)
	}
	return hashes
}

func setMediaOrder(t *testing.T, workID int64, kind int16, hashes ...string) {
	t.Helper()
	if err := testDB.Where("work_id = ? AND kind = ?", workID, kind).
		Delete(&model.CatalogWorkMediaOrder{}).Error; err != nil {
		t.Fatal(err)
	}
	for i, h := range hashes {
		if err := testDB.Create(&model.CatalogWorkMediaOrder{
			WorkID: workID, Kind: kind, ImageHash: h, Position: i,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func TestWorkMediaOrderDecidesTheReadOrder(t *testing.T) {
	read := NewReadService(testDB)
	work := createWork(t, "並び順の読み取り")
	other := createWork(t, "別の作品")

	for _, c := range []model.CatalogWorkCover{
		{WorkID: work.ID, ImageHash: "cov-human", SortOrder: 0, SourceID: model.SourceCurated},
		{WorkID: work.ID, ImageHash: "cov-vndb-0", SortOrder: 0, SourceID: model.SourceVNDB},
		{WorkID: work.ID, ImageHash: "cov-vndb-1", SortOrder: 1, SourceID: model.SourceVNDB},
		{WorkID: other.ID, ImageHash: "cov-other", SortOrder: 0, SourceID: model.SourceVNDB},
	} {
		if err := testDB.Create(&c).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range []model.CatalogWorkScreenshot{
		{WorkID: work.ID, ImageHash: "ss-vndb-0", SortOrder: 0, SourceID: model.SourceVNDB},
		{WorkID: work.ID, ImageHash: "ss-vndb-1", SortOrder: 1, SourceID: model.SourceVNDB},
		{WorkID: work.ID, ImageHash: "ss-egs-0", SortOrder: 0, SourceID: model.SourceErogameScape},
		{WorkID: work.ID, ImageHash: "ss-human-0", SortOrder: 0, SourceID: model.SourceCurated},
	} {
		if err := testDB.Create(&s).Error; err != nil {
			t.Fatal(err)
		}
	}

	if got, want := coverHashes(t, read, work.ID), []string{"cov-human", "cov-vndb-0", "cov-vndb-1"}; !slices.Equal(got, want) {
		t.Fatalf("covers without an order = %v, want sort_order then hash %v", got, want)
	}
	if got, want := screenshotHashes(t, read, work.ID), []string{"ss-vndb-0", "ss-vndb-1", "ss-egs-0", "ss-human-0"}; !slices.Equal(got, want) {
		t.Fatalf("screenshots without an order = %v, want source then sort_order %v", got, want)
	}

	setMediaOrder(t, work.ID, model.WorkMediaOrderCover, "cov-vndb-1", "gone-hash", "cov-human")
	if got, want := coverHashes(t, read, work.ID), []string{"cov-vndb-1", "cov-human", "cov-vndb-0"}; !slices.Equal(got, want) {
		t.Fatalf("covers with an order = %v, want listed first, unlisted after, unknown hash ignored %v", got, want)
	}
	if got, want := screenshotHashes(t, read, work.ID), []string{"ss-vndb-0", "ss-vndb-1", "ss-egs-0", "ss-human-0"}; !slices.Equal(got, want) {
		t.Fatalf("a cover order must not reorder screenshots: %v", got)
	}

	setMediaOrder(t, work.ID, model.WorkMediaOrderScreenshot, "ss-human-0", "ss-egs-0")
	if got, want := screenshotHashes(t, read, work.ID), []string{"ss-human-0", "ss-egs-0", "ss-vndb-0", "ss-vndb-1"}; !slices.Equal(got, want) {
		t.Fatalf("screenshots with an order = %v, want %v", got, want)
	}

	if got := coverHashes(t, read, other.ID); !slices.Equal(got, []string{"cov-other"}) {
		t.Fatalf("another work's covers = %v", got)
	}
}

func TestWorkMediaOrderStaysWithItsWorkThroughAMerge(t *testing.T) {
	cleanTables(t)
	read := NewReadService(testDB)
	src, dst := createWork(t, "src"), createWork(t, "dst")
	for _, c := range []model.CatalogWorkCover{
		{WorkID: src.ID, ImageHash: "src-a", SortOrder: 0, SourceID: model.SourceVNDB},
		{WorkID: src.ID, ImageHash: "src-b", SortOrder: 1, SourceID: model.SourceVNDB},
		{WorkID: dst.ID, ImageHash: "dst-a", SortOrder: 0, SourceID: model.SourceBangumi},
		{WorkID: dst.ID, ImageHash: "dst-b", SortOrder: 1, SourceID: model.SourceBangumi},
	} {
		if err := testDB.Create(&c).Error; err != nil {
			t.Fatal(err)
		}
	}
	setMediaOrder(t, src.ID, model.WorkMediaOrderCover, "src-b", "src-a")
	setMediaOrder(t, dst.ID, model.WorkMediaOrderCover, "dst-b", "dst-a")

	mergeNow(t, model.EntityTypeWork, src.ID, dst.ID)

	got := coverHashes(t, read, dst.ID)
	if !slices.Equal(got[:2], []string{"dst-b", "dst-a"}) || len(got) != 4 {
		t.Fatalf("survivor covers = %v, want its own order first and the merged-in covers after", got)
	}
	var kept []string
	if err := testDB.Model(&model.CatalogWorkMediaOrder{}).Where("work_id = ?", src.ID).
		Order("position").Pluck("image_hash", &kept).Error; err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(kept, []string{"src-b", "src-a"}) {
		t.Fatalf("the source's order rows = %v, want them left on the retired id for an unmerge", kept)
	}
}
