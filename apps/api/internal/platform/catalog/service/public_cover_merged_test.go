package service

import "testing"

func mergedRow(seed, kind string, pinned bool) WorkCoverRow {
	r := slotRow(seed, kind, pinned)
	r.MergedIn = true
	return r
}

func mergedShot(seed string) WorkScreenshotRow {
	s := shotRow(seed, 0)
	s.MergedIn = true
	return s
}

func TestMergedInCoversRankBelowTheWorksOwn(t *testing.T) {
	svc := slotSvc()
	own := slotRow("0c01", "main", false)
	loserTall, loserWide := mergedRow("0d01", "main", true), mergedRow("0d02", "main", false)
	ownShot, loserShot := shotRow("0e01", 0), mergedShot("0e02")
	meta := map[string]ImageMeta{
		own.ImageHash:       {Width: 256, Height: 362},
		loserTall.ImageHash: {Width: 1200, Height: 1700},
		loserWide.ImageHash: {Width: 1920, Height: 1080},
		ownShot.ImageHash:   {Width: 1280, Height: 720},
		loserShot.ImageHash: {Width: 2560, Height: 1440},
	}
	url := svc.imageURL
	covers := []WorkCoverRow{loserTall, loserWide, own}

	slots := slotsWith(svc, covers, []WorkScreenshotRow{loserShot, ownShot}, meta, false)
	if got := portraitOf(slots); got == nil || got.URL != url(own.ImageHash) {
		t.Fatalf("portrait = %+v, want the work's own over a sharper, pinned merged-in cover", got)
	}
	if got := bannerOf(slots); got == nil || got.URL != url(ownShot.ImageHash) || got.Origin != "screenshot" {
		t.Fatalf("banner = %+v, want the work's own screenshot over merged-in art", got)
	}
	if got := svc.pickListCover(covers, false); got != url(own.ImageHash) {
		t.Fatalf("list cover = %q, want the work's own over a pinned merged-in cover", got)
	}

	slots = slotsWith(svc, covers, []WorkScreenshotRow{loserShot}, meta, false)
	if got := bannerOf(slots); got == nil || got.URL != url(loserWide.ImageHash) || got.Origin != "cover" {
		t.Fatalf("banner = %+v, want the merged-in landscape cover: the work has no landscape art of its own", got)
	}

	slots = slotsWith(svc, []WorkCoverRow{own}, []WorkScreenshotRow{loserShot}, meta, false)
	if got := bannerOf(slots); got == nil || got.URL != url(loserShot.ImageHash) {
		t.Fatalf("banner = %+v, want the merged-in screenshot as the last rung", got)
	}
}

func TestOnlyMergedInCoversStillElect(t *testing.T) {
	svc := slotSvc()
	loserTall, loserWide := mergedRow("0d01", "main", false), mergedRow("0d02", "main", false)
	meta := map[string]ImageMeta{
		loserTall.ImageHash: {Width: 600, Height: 900},
		loserWide.ImageHash: {Width: 1920, Height: 1080},
	}
	slots := slotsWith(svc, []WorkCoverRow{loserTall, loserWide}, nil, meta, false)
	if got := portraitOf(slots); got == nil || got.URL != svc.imageURL(loserTall.ImageHash) {
		t.Fatalf("portrait = %+v, want the merged-in cover: a survivor with no covers takes the loser's", got)
	}
	if got := bannerOf(slots); got == nil || got.URL != svc.imageURL(loserWide.ImageHash) {
		t.Fatalf("banner = %+v, want the merged-in landscape cover", got)
	}
	if got := svc.pickListCover([]WorkCoverRow{loserTall}, false); got != svc.imageURL(loserTall.ImageHash) {
		t.Fatalf("list cover = %q, want the merged-in cover", got)
	}
}

func TestListCoverSlotsForAWorkWithNoScreenshots(t *testing.T) {
	cleanTables(t)
	tall := hash64("0c11")
	svc := newPublicSvcCDN().WithImageMeta(stubMeta(map[string]ImageMeta{tall: {Width: 256, Height: 362}}))
	w := createWorkX(t, galgameMediumID, 0, 0, "portrait only")
	addWorkCover(t, w.ID, tall, 0, "main", false, 0, srcVNDB)

	page, err := svc.WorksList(t.Context(), WorksListFilter{IDs: []int64{w.ID}, Include: ParseWorksListInclude("covers")}, "", 10)
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("WorksList: items=%d err=%v", len(page.Items), err)
	}
	if got := portraitOf(page.Items[0].Covers); got == nil || got.URL != svc.imageURL(tall) {
		t.Fatalf("portrait = %+v, want the work's only cover", got)
	}
	if got := bannerOf(page.Items[0].Covers); got != nil {
		t.Fatalf("banner = %+v, want null: no landscape art and no screenshot", got)
	}
}

func TestTheWorksOwnGhostOutranksMergedInRealArt(t *testing.T) {
	svc := censoredSvc()
	ghost := slotRow("0f01", "main", false)
	ghost.SourceID = censoredTestSourceID
	loser := mergedRow("0d03", "main", false)
	meta := map[string]ImageMeta{
		ghost.ImageHash: {Width: 256, Height: 362},
		loser.ImageHash: {Width: 600, Height: 900},
	}
	slots := svc.pickCoverSlots([]WorkCoverRow{loser, ghost}, meta, false)
	if got := portraitOf(slots); got == nil || got.URL != svc.imageURL(ghost.ImageHash) {
		t.Fatalf("portrait = %+v, want the work's own stand-in: merged-in art ranks below all of its own rows", got)
	}
}
