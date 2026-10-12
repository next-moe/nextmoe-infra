package service

import (
	"testing"

	"api/internal/platform/catalog/dto"
	"api/internal/platform/catalog/model"
)

func sharedRow(seed, kind string) WorkCoverRow {
	r := slotRow(seed, kind, false)
	r.Shared = true
	return r
}

func TestOwnCoverBeatsASharperSharedOne(t *testing.T) {
	svc := slotSvc()
	bundle := sharedRow("b0c5", "main")
	ownTall, ownWide := slotRow("0a01", "main", false), slotRow("0a02", "main", false)
	meta := map[string]ImageMeta{
		bundle.ImageHash:  {Width: 1000, Height: 705},
		ownTall.ImageHash: {Width: 256, Height: 362},
		ownWide.ImageHash: {Width: 640, Height: 360},
	}

	slots := svc.pickCoverSlots([]WorkCoverRow{bundle, ownTall, ownWide}, meta, false)
	if portraitOf(slots) == nil || slots.Portrait.URL != svc.imageURL(ownTall.ImageHash) {
		t.Fatalf("portrait = %+v, want the work's own cover", portraitOf(slots))
	}
	if bannerOf(slots) == nil || slots.Banner.URL != svc.imageURL(ownWide.ImageHash) {
		t.Fatalf("banner = %+v, want the work's own landscape cover over the sharper, wider bundle box",
			bannerOf(slots))
	}
}

func TestSharedCoverTakesNoSlotFromAWorkWithArtOfItsOwn(t *testing.T) {
	svc := slotSvc()
	bundle := sharedRow("b0c5", "main")
	ownTall, ownSquare := slotRow("0a01", "main", false), slotRow("0a03", "main", false)
	shot := shotRow("5507", 0)
	meta := map[string]ImageMeta{
		bundle.ImageHash:    {Width: 1000, Height: 705},
		ownTall.ImageHash:   {Width: 256, Height: 362},
		ownSquare.ImageHash: {Width: 1084, Height: 1080},
		shot.ImageHash:      {Width: 1280, Height: 720},
	}

	slots := slotsWith(svc, []WorkCoverRow{bundle, ownTall}, []WorkScreenshotRow{shot}, meta, false)
	if bannerOf(slots) == nil || slots.Banner.URL != svc.imageURL(shot.ImageHash) || slots.Banner.Origin != "screenshot" {
		t.Fatalf("banner = %+v, want the work's own screenshot: its only landscape cover is the bundle box",
			bannerOf(slots))
	}
	if slots := svc.pickCoverSlots([]WorkCoverRow{bundle, ownTall}, meta, false); bannerOf(slots) != nil {
		t.Fatalf("banner = %+v, want null without a screenshot", bannerOf(slots))
	}

	slots = svc.pickCoverSlots([]WorkCoverRow{bundle, ownSquare}, meta, false)
	if portraitOf(slots) == nil || slots.Portrait.URL != svc.imageURL(ownSquare.ImageHash) {
		t.Fatalf("portrait = %+v, want the own square cover: the any-cover fallback must not reach "+
			"the bundle box, which sorts first", portraitOf(slots))
	}

	if got := svc.pickListCover([]WorkCoverRow{bundle, ownSquare}, false); got != svc.imageURL(ownSquare.ImageHash) {
		t.Fatalf("list cover = %q, want the own cover over the first-sorted bundle box", got)
	}
}

func TestSharedPinLosesToAnUnpinnedOwnCover(t *testing.T) {
	svc := slotSvc()
	pinned := sharedRow("b0c6", "main")
	pinned.PortraitPinned = true
	own := slotRow("0a04", "main", false)
	meta := map[string]ImageMeta{
		pinned.ImageHash: {Width: 1200, Height: 1700},
		own.ImageHash:    {Width: 256, Height: 362},
	}

	slots := svc.pickCoverSlots([]WorkCoverRow{pinned, own}, meta, false)
	if portraitOf(slots) == nil || slots.Portrait.URL != svc.imageURL(own.ImageHash) {
		t.Fatalf("portrait = %+v, want the own cover over the pinned shared one", portraitOf(slots))
	}
	if got := svc.pickListCover([]WorkCoverRow{pinned, own}, false); got != svc.imageURL(own.ImageHash) {
		t.Fatalf("list cover = %q, want the own cover over the pinned shared one", got)
	}
}

func TestOnlySharedCandidatesStillElect(t *testing.T) {
	svc := slotSvc()
	box, tall := sharedRow("b0c5", "main"), sharedRow("b0c7", "main")
	meta := map[string]ImageMeta{
		box.ImageHash:  {Width: 1000, Height: 705},
		tall.ImageHash: {Width: 600, Height: 900},
	}

	slots := svc.pickCoverSlots([]WorkCoverRow{box, tall}, meta, false)
	if portraitOf(slots) == nil || slots.Portrait.URL != svc.imageURL(tall.ImageHash) {
		t.Fatalf("portrait = %+v, want the shared portrait: a bundle cover beats nothing", portraitOf(slots))
	}
	if bannerOf(slots) == nil || slots.Banner.URL != svc.imageURL(box.ImageHash) {
		t.Fatalf("banner = %+v, want the shared box", bannerOf(slots))
	}
	if got := svc.pickListCover([]WorkCoverRow{box}, false); got != svc.imageURL(box.ImageHash) {
		t.Fatalf("list cover = %q, want the shared box", got)
	}

	ownPkg := slotRow("0f20", "pkgfront", false)
	meta[ownPkg.ImageHash] = ImageMeta{Width: 700, Height: 1000}
	slots = svc.pickCoverSlots([]WorkCoverRow{ownPkg, box}, meta, false)
	if portraitOf(slots) == nil || slots.Portrait.URL != svc.imageURL(box.ImageHash) {
		t.Fatalf("portrait = %+v, want the shared box: package art is no candidate, so it cannot "+
			"push the shared cover out", portraitOf(slots))
	}
}

func TestSharedCoverCandidacyFollowsTheViewer(t *testing.T) {
	svc := slotSvc()
	box := sharedRow("b0c5", "main")
	ownHot := slotRow("0a05", "main", false)
	ownHot.Sexual = model.SexualExplicit
	meta := map[string]ImageMeta{
		box.ImageHash:    {Width: 1000, Height: 705},
		ownHot.ImageHash: {Width: 600, Height: 900},
	}
	rows := []WorkCoverRow{box, ownHot}

	slots := svc.pickCoverSlots(rows, meta, false)
	if bannerOf(slots) == nil || slots.Banner.URL != svc.imageURL(box.ImageHash) {
		t.Fatalf("SFW banner = %+v, want the shared box: the own explicit cover is no candidate here",
			bannerOf(slots))
	}
	if got := svc.pickListCover(rows, false); got != svc.imageURL(box.ImageHash) {
		t.Fatalf("SFW list cover = %q, want the shared box", got)
	}

	slots = svc.pickCoverSlots(rows, meta, true)
	if portraitOf(slots) == nil || slots.Portrait.URL != svc.imageURL(ownHot.ImageHash) {
		t.Fatalf("R18 portrait = %+v, want the own explicit cover", portraitOf(slots))
	}
	if bannerOf(slots) != nil {
		t.Fatalf("R18 banner = %+v, want null: the work has art of its own for this viewer", bannerOf(slots))
	}
	if got := svc.pickListCover(rows, true); got != svc.imageURL(ownHot.ImageHash) {
		t.Fatalf("R18 list cover = %q, want the own explicit cover", got)
	}
}

func coversOf(t *testing.T, workID int64) []WorkCoverRow {
	t.Helper()
	byWork, err := NewReadService(testDB).loadWorkCovers(t.Context(), []claimSubject{{WorkID: workID}})
	if err != nil {
		t.Fatalf("loadWorkCovers(%d): %v", workID, err)
	}
	return byWork[workID]
}

func sharedFlagOf(t *testing.T, workID int64, hash string) bool {
	t.Helper()
	for _, c := range coversOf(t, workID) {
		if c.ImageHash == hash {
			return c.Shared
		}
	}
	t.Fatalf("work %d has no cover %s", workID, hash)
	return false
}

func TestSharedMeansThreeLiveWorks(t *testing.T) {
	cleanTables(t)
	h := hash64("5a7e")
	holder := func(name string, status int16) int64 {
		w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, status, name)
		addWorkCover(t, w.ID, h, 0, "main", false, 0, srcVNDB)
		return w.ID
	}

	a := holder("a", model.WorkStatusLive)
	holder("b", model.WorkStatusLive)
	if sharedFlagOf(t, a, h) {
		t.Fatal("an image on 2 works is shared, want not: two editions of one game legitimately share art")
	}

	holder("merged", model.WorkStatusMerged)
	deleted := holder("deleted", model.WorkStatusLive)
	if err := testDB.Delete(&model.CatalogWork{}, deleted).Error; err != nil {
		t.Fatalf("soft-delete work: %v", err)
	}
	if sharedFlagOf(t, a, h) {
		t.Fatal("a merged or deleted holder counted toward shared, want live works only")
	}

	holder("c", model.WorkStatusLive)
	if !sharedFlagOf(t, a, h) {
		t.Fatal("an image on 3 live works is not shared, want shared")
	}
}

// Production shape (forum todo #231): VNDB r139531, the HOOKSOFT 25th Discovery
// Box cover at 1000x705, on works 533, 541 and 5979 among others.
func TestBundleBoxLosesEveryElectionFace(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvcCDN()
	box := "6c47ce998960c451b44ff6d42da8c02c894a2f4e08b87ff658db35b26e4f9353"
	ownTall, ownSquare, ownWide, shot := hash64("0a01"), hash64("0a03"), hash64("0a02"), hash64("5507")
	svc.WithImageMeta(stubMeta(map[string]ImageMeta{
		box:       {Width: 1000, Height: 705},
		ownTall:   {Width: 256, Height: 362},
		ownSquare: {Width: 1084, Height: 1080},
		ownWide:   {Width: 640, Height: 360},
		shot:      {Width: 1280, Height: 720},
	}))

	newWork := func(name string, own ...string) int64 {
		w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, name)
		addWorkCover(t, w.ID, box, 0, "main", false, 0, srcVNDB)
		for i, h := range own {
			addWorkCover(t, w.ID, h, i+1, "main", false, 0, srcVNDB)
		}
		return w.ID
	}
	tallOnly := newWork("v33545", ownTall)
	if err := testDB.Create(&model.CatalogWorkScreenshot{
		WorkID: tallOnly, ImageHash: shot, SourceID: srcVNDB,
	}).Error; err != nil {
		t.Fatalf("create screenshot: %v", err)
	}
	squareOnly := newWork("v33054", ownSquare)
	tallAndWide := newWork("v2992", ownTall, ownWide)
	boxOnly := newWork("bundle-only")

	url := func(h string) string { return svc.imageURL(h) }
	type want struct{ portrait, banner, cover string }
	expect := map[int64]want{
		tallOnly:    {url(ownTall), url(shot), url(ownTall)},
		squareOnly:  {url(ownSquare), "", url(ownSquare)},
		tallAndWide: {url(ownTall), url(ownWide), url(ownTall)},
		boxOnly:     {url(box), url(box), url(box)},
	}
	slotURL := func(s *dto.PublicCoverSlot) string {
		if s == nil {
			return ""
		}
		return s.URL
	}
	check := func(face string, id int64, slots *dto.PublicWorkCoverSlots) {
		t.Helper()
		w := expect[id]
		if got := slotURL(portraitOf(slots)); got != w.portrait {
			t.Errorf("%s work %d portrait = %q, want %q", face, id, got, w.portrait)
		}
		if got := slotURL(bannerOf(slots)); got != w.banner {
			t.Errorf("%s work %d banner = %q, want %q", face, id, got, w.banner)
		}
	}

	page, err := svc.WorksList(t.Context(),
		WorksListFilter{Sort: "id", Include: ParseWorksListInclude("covers")}, "", 50)
	if err != nil {
		t.Fatalf("WorksList include=covers: %v", err)
	}
	if len(page.Items) != len(expect) {
		t.Fatalf("list items = %d, want %d", len(page.Items), len(expect))
	}
	for _, it := range page.Items {
		check("list include=covers", it.ID, it.Covers)
	}

	page, err = svc.WorksList(t.Context(), WorksListFilter{Sort: "id"}, "", 50)
	if err != nil {
		t.Fatalf("WorksList: %v", err)
	}
	for _, it := range page.Items {
		if it.Cover != expect[it.ID].cover {
			t.Errorf("list work %d cover = %q, want %q", it.ID, it.Cover, expect[it.ID].cover)
		}
	}

	for id := range expect {
		rec, found, err := svc.WorkDetail(t.Context(), id, PublicInclude{}, false, 0, PublicFields{})
		if err != nil || !found {
			t.Fatalf("WorkDetail(%d): found=%v err=%v", id, found, err)
		}
		check("detail", id, rec.CoverSlots)
	}
}
