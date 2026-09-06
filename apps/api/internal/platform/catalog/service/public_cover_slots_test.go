package service

import (
	"testing"

	"api/internal/platform/catalog/dto"
)

func slotSvc() *PublicService { return &PublicService{cdnBase: testCDNBase} }

func slotRow(seed, kind string, pinned bool) WorkCoverRow {
	return WorkCoverRow{ImageHash: hash64(seed), Kind: kind, PortraitPinned: pinned}
}

func shotRow(seed string, sexual int16) WorkScreenshotRow {
	return WorkScreenshotRow{ImageHash: hash64(seed), Sexual: sexual}
}

// slotsWith mirrors both production call sites: the cover ladder first, the
// screenshot fallback only when the banner came back empty.
func slotsWith(svc *PublicService, covers []WorkCoverRow, shots []WorkScreenshotRow,
	meta map[string]ImageMeta, allowSexual bool) *dto.PublicWorkCoverSlots {
	slots := svc.pickCoverSlots(covers, meta, allowSexual)
	if bannerMissing(slots) {
		slots = svc.fillBannerFromScreenshots(slots, shots, meta, allowSexual)
	}
	return slots
}

func bannerOf(slots *dto.PublicWorkCoverSlots) *dto.PublicCoverSlot {
	if slots == nil {
		return nil
	}
	return slots.Banner
}

func portraitOf(slots *dto.PublicWorkCoverSlots) *dto.PublicCoverSlot {
	if slots == nil {
		return nil
	}
	return slots.Portrait
}

func TestDiscFaceIsNeverABanner(t *testing.T) {
	svc := slotSvc()
	disc, art := slotRow("d15c", "pkgmed", false), slotRow("a217", "dig", true)
	meta := map[string]ImageMeta{
		disc.ImageHash: {Width: 1084, Height: 1080},
		art.ImageHash:  {Width: 720, Height: 1080},
	}

	slots := svc.pickCoverSlots([]WorkCoverRow{disc, art}, meta, false)
	if slots == nil {
		t.Fatal("slots = nil, want the work's covers resolved")
	}
	if slots.Banner != nil {
		t.Fatalf("banner = %+v, want null: the work has no wide artwork at all", slots.Banner)
	}
	if slots.Portrait == nil || slots.Portrait.URL != svc.imageURL(art.ImageHash) {
		t.Fatalf("portrait = %+v, want the pinned digital cover", slots.Portrait)
	}
}

func TestBannerNeedsWidthAndArtwork(t *testing.T) {
	cases := []struct {
		name       string
		kind       string
		w, h       int
		wantBanner bool
	}{
		{"wide artwork", "dig", 1920, 1080, true},
		{"exactly 3:2 qualifies", "main", 1200, 800, true},
		{"exactly 4:3 qualifies", "main", 1024, 768, true},
		{"just short of 4:3 is not hero art", "main", 1000, 768, false},
		{"near-square is not hero art", "main", 1084, 1080, false},
		{"wide but a photo of the box front", "pkgfront", 1920, 1080, false},
		{"wide but a photo of the box back", "pkgback", 1920, 1080, false},
		{"wide but a booklet page", "pkgcontent", 1920, 1080, false},
		{"wide but a spine", "pkgside", 1920, 1080, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc := slotSvc()
			row := slotRow("bb01", c.kind, false)
			slots := svc.pickCoverSlots([]WorkCoverRow{row},
				map[string]ImageMeta{row.ImageHash: {Width: c.w, Height: c.h}}, false)
			if got := bannerOf(slots) != nil; got != c.wantBanner {
				t.Fatalf("banner filled = %v, want %v (%dx%d %q)", got, c.wantBanner, c.w, c.h, c.kind)
			}
		})
	}
}

func TestPackageArtIsNeverElected(t *testing.T) {
	svc := slotSvc()
	front, back := slotRow("0f20", "pkgfront", false), slotRow("0bac", "pkgback", false)
	meta := map[string]ImageMeta{
		front.ImageHash: {Width: 700, Height: 1000},
		back.ImageHash:  {Width: 800, Height: 1200},
	}

	if slots := svc.pickCoverSlots([]WorkCoverRow{front, back}, meta, false); slots != nil {
		t.Fatalf("slots = %+v, want null: a scan of the physical case is not this work's art", slots)
	}

	front.PortraitPinned = true
	if slots := svc.pickCoverSlots([]WorkCoverRow{front, back}, meta, false); slots != nil {
		t.Fatalf("slots = %+v, want null even for a pinned pkgfront: the read face neutralises "+
			"the ~258 package pins the retired repin ladder left behind", slots)
	}

	art := slotRow("a71a", "dig", false)
	meta[art.ImageHash] = ImageMeta{Width: 600, Height: 900}
	slots := svc.pickCoverSlots([]WorkCoverRow{front, back, art}, meta, false)
	if portraitOf(slots) == nil || slots.Portrait.URL != svc.imageURL(art.ImageHash) {
		t.Fatalf("portrait = %+v, want the digital art over a pinned package front", portraitOf(slots))
	}
}

func TestSharpestWinsWithinACategory(t *testing.T) {
	svc := slotSvc()
	small, big := slotRow("5111", "dig", false), slotRow("b166", "dig", false)
	wideSmall, wideBig := slotRow("w511", "main", false), slotRow("w166", "main", false)
	meta := map[string]ImageMeta{
		small.ImageHash:     {Width: 600, Height: 900},
		big.ImageHash:       {Width: 1200, Height: 1800},
		wideSmall.ImageHash: {Width: 960, Height: 540},
		wideBig.ImageHash:   {Width: 1920, Height: 1080},
	}

	slots := svc.pickCoverSlots([]WorkCoverRow{small, big, wideSmall, wideBig}, meta, false)
	if portraitOf(slots) == nil || slots.Portrait.URL != svc.imageURL(big.ImageHash) {
		t.Fatalf("portrait = %+v, want the larger portrait, not the first in sort order", portraitOf(slots))
	}
	if bannerOf(slots) == nil || slots.Banner.URL != svc.imageURL(wideBig.ImageHash) {
		t.Fatalf("banner = %+v, want the larger wide cover", bannerOf(slots))
	}
}

func TestEqualAreaBreaksOnHash(t *testing.T) {
	svc := slotSvc()
	a, b := slotRow("aaa1", "dig", false), slotRow("0001", "dig", false)
	meta := map[string]ImageMeta{
		a.ImageHash: {Width: 600, Height: 900},
		b.ImageHash: {Width: 600, Height: 900},
	}
	slots := svc.pickCoverSlots([]WorkCoverRow{a, b}, meta, false)
	if portraitOf(slots) == nil || slots.Portrait.URL != svc.imageURL(b.ImageHash) {
		t.Fatalf("portrait = %+v, want the lexicographically smaller hash", portraitOf(slots))
	}
}

func TestSafeCoverBeatsALargerExplicitOne(t *testing.T) {
	svc := slotSvc()
	huge, safe := slotRow("5e59", "dig", false), slotRow("5afe", "dig", false)
	huge.Sexual = 2
	meta := map[string]ImageMeta{
		huge.ImageHash: {Width: 2000, Height: 3000},
		safe.ImageHash: {Width: 600, Height: 900},
	}
	slots := svc.pickCoverSlots([]WorkCoverRow{huge, safe}, meta, true)
	if portraitOf(slots) == nil || slots.Portrait.URL != svc.imageURL(safe.ImageHash) {
		t.Fatalf("portrait = %+v, want the display-safe cover: the sexual pass fills only the "+
			"categories the safe pass left empty, so area never crosses the two passes", portraitOf(slots))
	}
}

func TestPortraitWidthTierBeatsArea(t *testing.T) {
	svc := slotSvc()
	narrow, wide := slotRow("2a11", "dig", false), slotRow("f011", "dig", false)
	meta := map[string]ImageMeta{
		narrow.ImageHash: {Width: 400, Height: 4000},
		wide.ImageHash:   {Width: 500, Height: 750},
	}

	slots := svc.pickCoverSlots([]WorkCoverRow{narrow, wide}, meta, false)
	if portraitOf(slots) == nil || slots.Portrait.URL != svc.imageURL(wide.ImageHash) {
		t.Fatalf("portrait = %+v, want the 500px-wide cover: the tier is checked before area, "+
			"so a taller sliver does not win on pixel count", portraitOf(slots))
	}

	slots = svc.pickCoverSlots([]WorkCoverRow{narrow}, meta, false)
	if portraitOf(slots) == nil || slots.Portrait.URL != svc.imageURL(narrow.ImageHash) {
		t.Fatalf("portrait = %+v, want the sub-500px portrait when nothing wider exists: "+
			"500 is a tier, not a gate", portraitOf(slots))
	}
}

func TestBannerPrefersTheWiderCandidate(t *testing.T) {
	svc := slotSvc()
	thumb, real := slotRow("7b17", "", false), slotRow("b16b", "dig", false)
	meta := map[string]ImageMeta{
		thumb.ImageHash: {Width: 256, Height: 144},
		real.ImageHash:  {Width: 1920, Height: 1080},
	}

	slots := svc.pickCoverSlots([]WorkCoverRow{thumb, real}, meta, false)
	if slots.Banner == nil || slots.Banner.URL != svc.imageURL(real.ImageHash) {
		t.Fatalf("banner = %+v, want the full-size art over the 256px thumbnail", slots.Banner)
	}

	slots = svc.pickCoverSlots([]WorkCoverRow{thumb}, meta, false)
	if slots.Banner == nil || slots.Banner.URL != svc.imageURL(thumb.ImageHash) {
		t.Fatalf("banner = %+v, want the thumbnail as the last resort", slots.Banner)
	}
}

func TestSexualCoversNeedTheWorksOwnPermission(t *testing.T) {
	svc := slotSvc()
	sexy, safe := slotRow("5e59", "dig", false), slotRow("5afe", "dig", false)
	sexy.Sexual = 2
	meta := map[string]ImageMeta{
		sexy.ImageHash: {Width: 1920, Height: 740},
		safe.ImageHash: {Width: 1529, Height: 1080},
	}

	slots := svc.pickCoverSlots([]WorkCoverRow{sexy, safe}, meta, false)
	if slots.Banner == nil || slots.Banner.URL != svc.imageURL(safe.ImageHash) {
		t.Fatalf("banner = %+v, want the display-safe art", slots.Banner)
	}

	tall := slotRow("7a11", "dig", false)
	meta[tall.ImageHash] = ImageMeta{Width: 600, Height: 900}
	slots = svc.pickCoverSlots([]WorkCoverRow{sexy, tall}, meta, false)
	if slots.Banner != nil {
		t.Fatalf("banner = %+v, want null for a display-safe work with only sexual wide art", slots.Banner)
	}
	if slots.Portrait == nil || slots.Portrait.URL != svc.imageURL(tall.ImageHash) {
		t.Fatalf("portrait = %+v, want the display-safe portrait", slots.Portrait)
	}

	slots = svc.pickCoverSlots([]WorkCoverRow{sexy}, meta, true)
	if slots.Banner == nil || slots.Banner.URL != svc.imageURL(sexy.ImageHash) {
		t.Fatalf("banner = %+v, want the sexual art once the work permits it", slots.Banner)
	}
	slots = svc.pickCoverSlots([]WorkCoverRow{sexy, safe}, meta, true)
	if slots.Banner == nil || slots.Banner.URL != svc.imageURL(safe.ImageHash) {
		t.Fatalf("banner = %+v, want the display-safe art to still win its tier", slots.Banner)
	}
}

func TestSuggestiveCoversAreDisplaySafe(t *testing.T) {
	svc := slotSvc()
	pinned, safe := slotRow("9d1a", "dig", true), slotRow("5afe", "dig", false)
	pinned.Sexual = 1
	meta := map[string]ImageMeta{
		pinned.ImageHash: {Width: 720, Height: 1080},
		safe.ImageHash:   {Width: 600, Height: 900},
	}

	slots := svc.pickCoverSlots([]WorkCoverRow{pinned, safe}, meta, false)
	if slots.Portrait == nil || slots.Portrait.URL != svc.imageURL(pinned.ImageHash) {
		t.Fatalf("portrait = %+v, want the suggestive pin honoured for every viewer: "+
			"the pin job pins sexual=1 covers, so hiding them here blanks the work", slots.Portrait)
	}

	only := slotRow("0a1b", "dig", false)
	only.Sexual = 1
	meta[only.ImageHash] = ImageMeta{Width: 600, Height: 900}
	slots = svc.pickCoverSlots([]WorkCoverRow{only}, meta, false)
	if slots == nil || slots.Portrait == nil || slots.Portrait.URL != svc.imageURL(only.ImageHash) {
		t.Fatalf("slots = %+v, want a work with only suggestive covers to still resolve one", slots)
	}
}

func TestPortraitPrefersDisplaySafeOverAnExplicitPin(t *testing.T) {
	svc := slotSvc()
	pinned, safe := slotRow("9d1a", "dig", true), slotRow("5afe", "dig", false)
	pinned.Sexual = 2
	meta := map[string]ImageMeta{
		pinned.ImageHash: {Width: 720, Height: 1080},
		safe.ImageHash:   {Width: 600, Height: 900},
	}

	slots := svc.pickCoverSlots([]WorkCoverRow{pinned, safe}, meta, false)
	if slots.Portrait == nil || slots.Portrait.URL != svc.imageURL(safe.ImageHash) {
		t.Fatalf("portrait = %+v, want the display-safe cover over an explicit pin", slots.Portrait)
	}
	slots = svc.pickCoverSlots([]WorkCoverRow{pinned, safe}, meta, true)
	if slots.Portrait == nil || slots.Portrait.URL != svc.imageURL(pinned.ImageHash) {
		t.Fatalf("portrait = %+v, want the pin honoured once the work permits it", slots.Portrait)
	}
}

func TestLandscapePinDoesNotTakeThePortraitSlot(t *testing.T) {
	svc := slotSvc()
	promo, tall := slotRow("9d1a", "dig", true), slotRow("7a11", "dig", false)
	meta := map[string]ImageMeta{
		promo.ImageHash: {Width: 1920, Height: 1080},
		tall.ImageHash:  {Width: 600, Height: 900},
	}

	slots := svc.pickCoverSlots([]WorkCoverRow{promo, tall}, meta, false)
	if portraitOf(slots) == nil || slots.Portrait.URL != svc.imageURL(tall.ImageHash) {
		t.Fatalf("portrait = %+v, want the portrait sibling: the pin write path has no shape "+
			"gate, so a 1920x1080 promo banner can carry portrait_pinned", portraitOf(slots))
	}
	if bannerOf(slots) == nil || slots.Banner.URL != svc.imageURL(promo.ImageHash) {
		t.Fatalf("banner = %+v, want the pinned landscape row: it is still a candidate "+
			"everywhere its shape fits", bannerOf(slots))
	}

	slots = svc.pickCoverSlots([]WorkCoverRow{promo}, meta, false)
	if portraitOf(slots) == nil || slots.Portrait.URL != svc.imageURL(promo.ImageHash) {
		t.Fatalf("portrait = %+v, want the last-resort first row when nothing portrait exists",
			portraitOf(slots))
	}
}

func TestUnmeasuredPinIsStillHonoured(t *testing.T) {
	svc := slotSvc()
	blind, tall := slotRow("9d1a", "dig", true), slotRow("7a11", "dig", false)
	meta := map[string]ImageMeta{tall.ImageHash: {Width: 600, Height: 900}}

	slots := svc.pickCoverSlots([]WorkCoverRow{blind, tall}, meta, false)
	if portraitOf(slots) == nil || slots.Portrait.URL != svc.imageURL(blind.ImageHash) {
		t.Fatalf("portrait = %+v, want the pin: an unmeasured row cannot fail a shape guard, "+
			"and dropping it would blank every work image_service has not measured yet",
			portraitOf(slots))
	}
}

func TestSlotsCarryTheirOrigin(t *testing.T) {
	svc := slotSvc()
	art := slotRow("0a71", "dig", false)
	shot := shotRow("55ff", 0)
	meta := map[string]ImageMeta{
		art.ImageHash:  {Width: 600, Height: 900},
		shot.ImageHash: {Width: 1920, Height: 1080},
	}

	slots := slotsWith(svc, []WorkCoverRow{art}, []WorkScreenshotRow{shot}, meta, false)
	if portraitOf(slots) == nil || slots.Portrait.Origin != "cover" {
		t.Fatalf("portrait = %+v, want origin \"cover\"", portraitOf(slots))
	}
	if bannerOf(slots) == nil || slots.Banner.Origin != "screenshot" {
		t.Fatalf("banner = %+v, want origin \"screenshot\"", bannerOf(slots))
	}
	if slots.Banner.URL != svc.imageURL(shot.ImageHash) {
		t.Fatalf("banner url = %q, want the screenshot", slots.Banner.URL)
	}
}

func TestScreenshotFallbackOnlyFillsAnEmptyBanner(t *testing.T) {
	svc := slotSvc()
	wide, shot := slotRow("b166", "dig", false), shotRow("55ff", 0)
	meta := map[string]ImageMeta{
		wide.ImageHash: {Width: 1920, Height: 1080},
		shot.ImageHash: {Width: 3840, Height: 2160},
	}
	slots := slotsWith(svc, []WorkCoverRow{wide}, []WorkScreenshotRow{shot}, meta, false)
	if bannerOf(slots) == nil || slots.Banner.Origin != "cover" {
		t.Fatalf("banner = %+v, want the cover: a bigger screenshot never displaces a landscape cover",
			bannerOf(slots))
	}
}

func TestScreenshotFallbackRanksSafeThenWideThenArea(t *testing.T) {
	svc := slotSvc()
	tall := slotRow("7a11", "dig", false)
	bigHot, safe := shotRow("5e59", 1), shotRow("5afe", 0)
	narrowBig, wideSlim, bigWide := shotRow("0011", 0), shotRow("0022", 0), shotRow("0033", 0)
	meta := map[string]ImageMeta{
		tall.ImageHash:      {Width: 600, Height: 900},
		bigHot.ImageHash:    {Width: 3840, Height: 2160},
		safe.ImageHash:      {Width: 1280, Height: 720},
		narrowBig.ImageHash: {Width: 780, Height: 585},
		wideSlim.ImageHash:  {Width: 800, Height: 100},
		bigWide.ImageHash:   {Width: 1920, Height: 1080},
	}

	slots := slotsWith(svc, []WorkCoverRow{tall}, []WorkScreenshotRow{bigHot, safe}, meta, false)
	if bannerOf(slots) == nil || slots.Banner.URL != svc.imageURL(safe.ImageHash) {
		t.Fatalf("banner = %+v, want the sexual=0 screenshot over the far larger sexual=1 one",
			bannerOf(slots))
	}

	slots = slotsWith(svc, []WorkCoverRow{tall}, []WorkScreenshotRow{narrowBig, wideSlim}, meta, false)
	if bannerOf(slots) == nil || slots.Banner.URL != svc.imageURL(wideSlim.ImageHash) {
		t.Fatalf("banner = %+v, want the 800px-wide screenshot: the wide tier is checked before area",
			bannerOf(slots))
	}

	slots = slotsWith(svc, []WorkCoverRow{tall}, []WorkScreenshotRow{safe, bigWide}, meta, false)
	if bannerOf(slots) == nil || slots.Banner.URL != svc.imageURL(bigWide.ImageHash) {
		t.Fatalf("banner = %+v, want the larger screenshot inside the wide tier", bannerOf(slots))
	}
}

func TestScreenshotFallbackHonoursTheWorksPermission(t *testing.T) {
	svc := slotSvc()
	tall := slotRow("7a11", "dig", false)
	hot := shotRow("5e59", 2)
	meta := map[string]ImageMeta{
		tall.ImageHash: {Width: 600, Height: 900},
		hot.ImageHash:  {Width: 1920, Height: 1080},
	}

	slots := slotsWith(svc, []WorkCoverRow{tall}, []WorkScreenshotRow{hot}, meta, false)
	if bannerOf(slots) != nil {
		t.Fatalf("banner = %+v, want null: an explicit screenshot is not a display-safe banner",
			bannerOf(slots))
	}
	slots = slotsWith(svc, []WorkCoverRow{tall}, []WorkScreenshotRow{hot}, meta, true)
	if bannerOf(slots) == nil || slots.Banner.URL != svc.imageURL(hot.ImageHash) {
		t.Fatalf("banner = %+v, want the explicit screenshot once the work permits it", bannerOf(slots))
	}
}

func TestPortraitNeverComesFromAScreenshot(t *testing.T) {
	svc := slotSvc()
	front := slotRow("0f20", "pkgfront", false)
	tallShot, wideShot := shotRow("7a11", 0), shotRow("55ff", 0)
	meta := map[string]ImageMeta{
		front.ImageHash:    {Width: 700, Height: 1000},
		tallShot.ImageHash: {Width: 600, Height: 900},
		wideShot.ImageHash: {Width: 1920, Height: 1080},
	}

	slots := slotsWith(svc, []WorkCoverRow{front}, []WorkScreenshotRow{tallShot, wideShot}, meta, false)
	if portraitOf(slots) != nil {
		t.Fatalf("portrait = %+v, want null: a scene still never stands in for box art", portraitOf(slots))
	}
	if bannerOf(slots) == nil || slots.Banner.Origin != "screenshot" {
		t.Fatalf("banner = %+v, want the landscape screenshot", bannerOf(slots))
	}
}

func TestSlotsAreNilOnlyWhenBothEndEmpty(t *testing.T) {
	svc := slotSvc()
	front := slotRow("0f20", "pkgfront", false)
	tallShot := shotRow("7a11", 0)
	meta := map[string]ImageMeta{
		front.ImageHash:    {Width: 700, Height: 1000},
		tallShot.ImageHash: {Width: 600, Height: 900},
	}

	if slots := slotsWith(svc, []WorkCoverRow{front}, nil, meta, false); slots != nil {
		t.Fatalf("slots = %+v, want null: package art only and no screenshots", slots)
	}
	if slots := slotsWith(svc, []WorkCoverRow{front}, []WorkScreenshotRow{tallShot}, meta, false); slots != nil {
		t.Fatalf("slots = %+v, want null: a portrait screenshot fills neither slot", slots)
	}
}

func TestListCoverSkipsPackageArt(t *testing.T) {
	svc := slotSvc()
	front, back := slotRow("0f20", "pkgfront", true), slotRow("0bac", "pkgback", false)
	art := slotRow("a71a", "dig", false)

	if got := svc.pickListCover([]WorkCoverRow{front, back}, false); got != "" {
		t.Fatalf("cover = %q, want empty: even a pinned pkgfront is not this work's art", got)
	}
	if got := svc.pickListCover([]WorkCoverRow{front, back, art}, false); got != svc.imageURL(art.ImageHash) {
		t.Fatalf("cover = %q, want the digital art", got)
	}
}

const censoredTestSourceID = int16(21)

func censoredSvc() *PublicService {
	return &PublicService{cdnBase: testCDNBase,
		sources: map[int16]string{2: "vndb", 12: "curated", censoredTestSourceID: "censored"}}
}

func TestCensoredGhostFillsOnlyTheEmptySFWPortrait(t *testing.T) {
	svc := censoredSvc()
	explicit, ghost := slotRow("ec57", "main", false), slotRow("9057", "main", false)
	explicit.Sexual, explicit.SourceID = 2, 2
	ghost.SourceID = censoredTestSourceID
	meta := map[string]ImageMeta{
		explicit.ImageHash: {Width: 700, Height: 1000},
		ghost.ImageHash:    {Width: 600, Height: 900},
	}
	rows := []WorkCoverRow{ghost, explicit}

	slots := svc.pickCoverSlots(rows, meta, false)
	if portraitOf(slots) == nil || slots.Portrait.URL != svc.imageURL(ghost.ImageHash) {
		t.Fatalf("SFW portrait = %+v, want the censored ghost", portraitOf(slots))
	}
	if slots.Portrait.Origin != "censored" || slots.Portrait.Source != "censored" {
		t.Fatalf("SFW portrait origin/source = %q/%q, want censored/censored",
			slots.Portrait.Origin, slots.Portrait.Source)
	}
	if bannerOf(slots) != nil {
		t.Fatalf("banner = %+v, want null: a ghost never takes the banner", slots.Banner)
	}

	slots = svc.pickCoverSlots(rows, meta, true)
	if portraitOf(slots) == nil || slots.Portrait.URL != svc.imageURL(explicit.ImageHash) {
		t.Fatalf("R18 portrait = %+v, want the real explicit cover over the ghost", portraitOf(slots))
	}
	if slots.Portrait.Origin != "cover" {
		t.Fatalf("R18 portrait origin = %q, want cover", slots.Portrait.Origin)
	}
}

func TestCensoredGhostNeverDisplacesRealSafeArt(t *testing.T) {
	svc := censoredSvc()
	safe, ghost := slotRow("5afe", "main", false), slotRow("9058", "main", false)
	safe.SourceID = 12
	ghost.SourceID = censoredTestSourceID
	meta := map[string]ImageMeta{
		safe.ImageHash:  {Width: 300, Height: 450},
		ghost.ImageHash: {Width: 900, Height: 1350},
	}

	slots := svc.pickCoverSlots([]WorkCoverRow{ghost, safe}, meta, false)
	if portraitOf(slots) == nil || slots.Portrait.URL != svc.imageURL(safe.ImageHash) {
		t.Fatalf("portrait = %+v, want the smaller REAL safe cover over the sharper ghost", portraitOf(slots))
	}
}

func TestCensoredGhostBehindScreenshotBannerLadder(t *testing.T) {
	svc := censoredSvc()
	explicit, ghost := slotRow("ec58", "main", false), slotRow("9059", "main", false)
	explicit.Sexual, explicit.SourceID = 2, 2
	ghost.SourceID = censoredTestSourceID
	shot := shotRow("50f7", 0)
	meta := map[string]ImageMeta{
		explicit.ImageHash: {Width: 700, Height: 1000},
		ghost.ImageHash:    {Width: 600, Height: 900},
		shot.ImageHash:     {Width: 1280, Height: 720},
	}

	slots := slotsWith(svc, []WorkCoverRow{explicit, ghost}, []WorkScreenshotRow{shot}, meta, false)
	if portraitOf(slots) == nil || slots.Portrait.URL != svc.imageURL(ghost.ImageHash) {
		t.Fatalf("portrait = %+v, want the ghost", portraitOf(slots))
	}
	if bannerOf(slots) == nil || slots.Banner.URL != svc.imageURL(shot.ImageHash) {
		t.Fatalf("banner = %+v, want the safe screenshot", bannerOf(slots))
	}
	if slots.Banner.Origin != "screenshot" {
		t.Fatalf("banner origin = %q, want screenshot", slots.Banner.Origin)
	}
}

func TestListCoverGhostLadder(t *testing.T) {
	svc := censoredSvc()
	explicit, ghost := slotRow("ec59", "main", false), slotRow("905a", "main", false)
	explicit.Sexual, explicit.SourceID = 2, 2
	ghost.SourceID = censoredTestSourceID
	rows := []WorkCoverRow{ghost, explicit}

	if got := svc.pickListCover(rows, false); got != svc.imageURL(ghost.ImageHash) {
		t.Fatalf("SFW list cover = %q, want the ghost", got)
	}
	if got := svc.pickListCover(rows, true); got != svc.imageURL(explicit.ImageHash) {
		t.Fatalf("R18 list cover = %q, want the real explicit cover", got)
	}

	safe := slotRow("5aff", "main", false)
	safe.SourceID = 12
	if got := svc.pickListCover([]WorkCoverRow{ghost, safe}, false); got != svc.imageURL(safe.ImageHash) {
		t.Fatalf("list cover = %q, want the real safe cover over the ghost", got)
	}
}
