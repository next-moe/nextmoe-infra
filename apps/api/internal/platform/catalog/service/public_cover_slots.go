package service

import (
	"api/internal/platform/catalog/dto"
	"api/internal/platform/catalog/model"
)

func isPortraitDims(w, h int) bool { return int64(h)*20 > int64(w)*21 }

func isBannerDims(w, h int) bool { return int64(w)*3 >= int64(h)*4 }

const (
	bannerMinWidth   = 800
	portraitMinWidth = 500
)

// Stand-in ladder: real safe art first, real explicit art second (R18 opt-in
// only), the blurred 'censored' ghost last — and only for the portrait slot,
// the card face lists render. Ghosts must never compete inside scanCovers:
// they are sexual=0 by construction, so a ghost in the main scan would win the
// SFW portrait over nothing but ALSO shadow the real explicit cover an R18
// viewer should get, because the sexual pass only fills slots the safe pass
// left empty. The banner keeps the real-art + screenshot ladder and may stay
// nil — a ghost hero adds nothing a nil banner does not already handle.
//
// Rows a merge carried over from the merged-away work (merged_in) stand below
// all of the work's own, ghosts included: they fill the portrait only when the
// work's own rows elect none, and the banner only after its own screenshots
// (fillBannerFromScreenshots). Ranked as equals, the loser's sharper art took
// the survivor's portrait and banner.
func (s *PublicService) pickCoverSlots(rows []WorkCoverRow, meta map[string]ImageMeta, allowSexual bool) *dto.PublicWorkCoverSlots {
	chosen := s.chosenPortrait(rows, meta, allowSexual)
	own, merged := splitMergedCovers(s.coverPool(s.yieldRedraws(rows, meta, allowSexual), allowSexual))
	portraitRow, bannerRow := s.electSlotRows(own, meta, allowSexual)
	if portraitRow == nil {
		portraitRow, _ = s.electSlotRows(merged, meta, allowSexual)
	}
	if chosen != nil {
		portraitRow = chosen
	}
	portrait, banner := s.coverSlot(portraitRow, meta), s.coverSlot(bannerRow, meta)
	if portrait == nil && banner == nil {
		return nil
	}
	return &dto.PublicWorkCoverSlots{Portrait: portrait, Banner: banner}
}

func (s *PublicService) electSlotRows(rows []WorkCoverRow, meta map[string]ImageMeta, allowSexual bool) (portrait, banner *WorkCoverRow) {
	real, ghosts := s.splitCensored(rows)
	cand := ownCoversFirst(real, func(pool []WorkCoverRow) coverCandidates {
		return s.electCovers(pool, meta, allowSexual)
	})
	portrait = cand.portrait()
	if portrait == nil && len(ghosts) > 0 {
		ghost := ownCoversFirst(ghosts, func(pool []WorkCoverRow) coverCandidates {
			return s.electCovers(pool, meta, false)
		})
		portrait = ghost.portrait()
	}
	return portrait, cand.banner()
}

func (s *PublicService) chosenPortrait(rows []WorkCoverRow, meta map[string]ImageMeta, allowSexual bool) *WorkCoverRow {
	var top *WorkCoverRow
	tied := false
	for i := range rows {
		c := &rows[i]
		if (!allowSexual && c.Sexual >= model.SexualExplicit) || s.imageURL(c.ImageHash) == "" ||
			s.sourceKey(c.SourceID) == model.CensoredSourceKey {
			continue
		}
		if c.HumanPinned {
			if m, ok := meta[c.ImageHash]; !ok || m.Width <= 0 || m.Height <= 0 || isPortraitDims(m.Width, m.Height) {
				return c
			}
		}
		switch {
		case c.Votes == 0 || c.MergedIn:
		case top == nil || c.Votes > top.Votes:
			top, tied = c, false
		case c.Votes == top.Votes:
			tied = true
		}
	}
	if tied {
		return nil
	}
	return top
}

// The cover_art_all_explicit trigger (catalog migrate, cover_art_grade.go)
// grades exactly the set this returns; change both or the shelf and the
// election disagree about whether a safe cover exists.
func (s *PublicService) officialCoversFirst(rows []WorkCoverRow) []WorkCoverRow {
	official := false
	for _, c := range rows {
		if model.IsCoverArt(c.Kind) && !model.IsUploadedCoverSource(c.SourceID) &&
			s.sourceKey(c.SourceID) != model.CensoredSourceKey && s.imageURL(c.ImageHash) != "" {
			official = true
			break
		}
	}
	if !official {
		return rows
	}
	out := make([]WorkCoverRow, 0, len(rows))
	for _, c := range rows {
		if !model.IsUploadedCoverSource(c.SourceID) || c.HumanPinned {
			out = append(out, c)
		}
	}
	return out
}

// Bangumi serves the covers of adult games blurred or mosaicked, so a reader
// allowed sexual art gets the work's VNDB cover in their place.
func (s *PublicService) coverPool(rows []WorkCoverRow, allowSexual bool) []WorkCoverRow {
	rows = s.officialCoversFirst(rows)
	if !allowSexual {
		return rows
	}
	vndb := false
	for _, c := range rows {
		if c.SourceID == model.SourceVNDB && model.IsCoverArt(c.Kind) && s.imageURL(c.ImageHash) != "" {
			vndb = true
			break
		}
	}
	if !vndb {
		return rows
	}
	out := make([]WorkCoverRow, 0, len(rows))
	for _, c := range rows {
		if c.SourceID != model.SourceBangumi || c.HumanPinned {
			out = append(out, c)
		}
	}
	return out
}

const redrawTargetEdge = 1080

// AI redraws were made only for works without an official portrait at the
// target edge, and they carry a machine pin; one arriving later takes the slot
// back.
func (s *PublicService) yieldRedraws(rows []WorkCoverRow, meta map[string]ImageMeta, allowSexual bool) []WorkCoverRow {
	official := false
	for _, c := range rows {
		if c.SourceID == model.SourceAIRedraw || model.IsUploadedCoverSource(c.SourceID) || !model.IsCoverArt(c.Kind) ||
			s.sourceKey(c.SourceID) == model.CensoredSourceKey || s.imageURL(c.ImageHash) == "" ||
			(!allowSexual && c.Sexual >= model.SexualExplicit) {
			continue
		}
		if m, ok := meta[c.ImageHash]; ok && isPortraitDims(m.Width, m.Height) && max(m.Width, m.Height) >= redrawTargetEdge {
			official = true
			break
		}
	}
	if !official {
		return rows
	}
	out := make([]WorkCoverRow, 0, len(rows))
	for _, c := range rows {
		if c.SourceID != model.SourceAIRedraw || c.HumanPinned {
			out = append(out, c)
		}
	}
	return out
}

func splitMergedCovers(rows []WorkCoverRow) (own, merged []WorkCoverRow) {
	for _, c := range rows {
		if c.MergedIn {
			merged = append(merged, c)
		} else {
			own = append(own, c)
		}
	}
	return own, merged
}

// Per work, not per slot: a work whose own art is all portrait gets the
// screenshot banner, not the bundle box it shares — that box as the banner of a
// game it does not show is the forum todo #231 report.
func ownCoversFirst[T comparable](rows []WorkCoverRow, elect func([]WorkCoverRow) T) T {
	var own, shared []WorkCoverRow
	for _, c := range rows {
		if c.Shared {
			shared = append(shared, c)
		} else {
			own = append(own, c)
		}
	}
	var none T
	if got := elect(own); got != none {
		return got
	}
	return elect(shared)
}

func (s *PublicService) electCovers(rows []WorkCoverRow, meta map[string]ImageMeta, allowSexual bool) coverCandidates {
	cand := s.scanCovers(rows, meta, false)
	if allowSexual && !cand.complete() {
		cand.fillFrom(s.scanCovers(rows, meta, true))
	}
	return cand
}

func (s *PublicService) splitCensored(rows []WorkCoverRow) (real, ghosts []WorkCoverRow) {
	for _, c := range rows {
		if s.sourceKey(c.SourceID) == model.CensoredSourceKey {
			ghosts = append(ghosts, c)
		} else {
			real = append(real, c)
		}
	}
	return real, ghosts
}

type coverCandidates struct {
	pinned                    *WorkCoverRow
	portraitWide, portraitAny *WorkCoverRow
	bannerWide, bannerAny     *WorkCoverRow
	sharpest, first           *WorkCoverRow
}

func (c coverCandidates) portrait() *WorkCoverRow {
	switch {
	case c.pinned != nil:
		return c.pinned
	case c.portraitWide != nil:
		return c.portraitWide
	case c.portraitAny != nil:
		return c.portraitAny
	// Without a portrait the first row by sort order took the slot, and rows
	// share sort_order 0, so the hash decided: on 2026-10-28 work 30923 showed
	// a 256x144 VNDB image over its 1668x940 Bangumi cover.
	case c.sharpest != nil:
		return c.sharpest
	default:
		return c.first
	}
}

func (c coverCandidates) banner() *WorkCoverRow {
	if c.bannerWide != nil {
		return c.bannerWide
	}
	return c.bannerAny
}

func (c coverCandidates) complete() bool {
	return c.pinned != nil && c.portraitWide != nil && c.portraitAny != nil &&
		c.bannerWide != nil && c.bannerAny != nil && c.sharpest != nil && c.first != nil
}

func (c *coverCandidates) fillFrom(other coverCandidates) {
	for _, pair := range []struct{ dst, src **WorkCoverRow }{
		{&c.pinned, &other.pinned}, {&c.portraitWide, &other.portraitWide},
		{&c.portraitAny, &other.portraitAny}, {&c.bannerWide, &other.bannerWide},
		{&c.bannerAny, &other.bannerAny}, {&c.sharpest, &other.sharpest}, {&c.first, &other.first},
	} {
		if *pair.dst == nil {
			*pair.dst = *pair.src
		}
	}
}

func (s *PublicService) scanCovers(rows []WorkCoverRow, meta map[string]ImageMeta, allowSexual bool) coverCandidates {
	var out coverCandidates
	for i := range rows {
		c := &rows[i]
		if !model.IsCoverArt(c.Kind) {
			continue
		}
		if !allowSexual && c.Sexual >= model.SexualExplicit {
			continue
		}
		if s.imageURL(c.ImageHash) == "" {
			continue
		}
		if out.first == nil {
			out.first = c
		}
		m, ok := meta[c.ImageHash]
		dimsKnown := ok && m.Width > 0 && m.Height > 0
		// The forum-side pin write path has no shape gate, so production carries
		// landscape rows flagged portrait_pinned — a 1920x1080 promo banner pinned
		// as the card face. An unmeasured pin is still honoured; a measured one
		// must be portrait to take the slot.
		if c.PortraitPinned && out.pinned == nil && (!dimsKnown || isPortraitDims(m.Width, m.Height)) {
			out.pinned = c
		}
		if !dimsKnown {
			continue
		}
		out.sharpest = sharperCover(out.sharpest, c, meta)
		switch {
		case isPortraitDims(m.Width, m.Height):
			out.portraitAny = sharperCover(out.portraitAny, c, meta)
			if m.Width >= portraitMinWidth {
				out.portraitWide = sharperCover(out.portraitWide, c, meta)
			}
		case isBannerDims(m.Width, m.Height):
			out.bannerAny = sharperCover(out.bannerAny, c, meta)
			if m.Width >= bannerMinWidth {
				out.bannerWide = sharperCover(out.bannerWide, c, meta)
			}
		}
	}
	return out
}

func sharperCover(cur, next *WorkCoverRow, meta map[string]ImageMeta) *WorkCoverRow {
	if cur == nil {
		return next
	}
	a, b := pixelArea(meta[cur.ImageHash]), pixelArea(meta[next.ImageHash])
	if a != b {
		if b > a {
			return next
		}
		return cur
	}
	if next.ImageHash < cur.ImageHash {
		return next
	}
	return cur
}

func pixelArea(m ImageMeta) int64 { return int64(m.Width) * int64(m.Height) }

func (s *PublicService) coverSlot(c *WorkCoverRow, meta map[string]ImageMeta) *dto.PublicCoverSlot {
	if c == nil {
		return nil
	}
	origin := "cover"
	if s.sourceKey(c.SourceID) == model.CensoredSourceKey {
		origin = model.CensoredSourceKey
	}
	slot := &dto.PublicCoverSlot{
		URL: s.imageURL(c.ImageHash), Sexual: c.Sexual, Violence: c.Violence,
		Source: s.sourceKey(c.SourceID), Origin: origin,
	}
	if m, ok := meta[c.ImageHash]; ok {
		slot.Width, slot.Height, slot.Thumbhash = m.Width, m.Height, m.Thumbhash
	}
	return slot
}

func bannerMissing(slots *dto.PublicWorkCoverSlots) bool {
	return slots == nil || slots.Banner == nil
}

func (s *PublicService) fillBannerFromScreenshots(
	slots *dto.PublicWorkCoverSlots, covers []WorkCoverRow, shots []WorkScreenshotRow,
	meta map[string]ImageMeta, allowSexual bool,
) *dto.PublicWorkCoverSlots {
	var ownShots, mergedShots []WorkScreenshotRow
	for _, sc := range shots {
		if sc.MergedIn {
			mergedShots = append(mergedShots, sc)
		} else {
			ownShots = append(ownShots, sc)
		}
	}
	banner := s.bannerFromShots(ownShots, meta, allowSexual)
	if banner == nil {
		_, merged := splitMergedCovers(s.coverPool(covers, allowSexual))
		_, row := s.electSlotRows(merged, meta, allowSexual)
		banner = s.coverSlot(row, meta)
	}
	if banner == nil {
		banner = s.bannerFromShots(mergedShots, meta, allowSexual)
	}
	if banner == nil {
		return slots
	}
	if slots == nil {
		slots = &dto.PublicWorkCoverSlots{}
	}
	slots.Banner = banner
	return slots
}

func (s *PublicService) bannerFromShots(shots []WorkScreenshotRow, meta map[string]ImageMeta, allowSexual bool) *dto.PublicCoverSlot {
	best := s.pickBannerScreenshot(shots, meta, false)
	if best == nil && allowSexual {
		best = s.pickBannerScreenshot(shots, meta, true)
	}
	if best == nil {
		return nil
	}
	return s.screenshotSlot(best, meta)
}

func (s *PublicService) pickBannerScreenshot(rows []WorkScreenshotRow, meta map[string]ImageMeta, allowSexual bool) *WorkScreenshotRow {
	var best *WorkScreenshotRow
	var bestMeta ImageMeta
	for i := range rows {
		sc := &rows[i]
		if !allowSexual && sc.Sexual >= model.SexualExplicit {
			continue
		}
		if s.imageURL(sc.ImageHash) == "" {
			continue
		}
		m, ok := meta[sc.ImageHash]
		if !ok || m.Width <= 0 || m.Height <= 0 || !isBannerDims(m.Width, m.Height) {
			continue
		}
		if best == nil || betterBannerShot(*sc, m, *best, bestMeta) {
			best, bestMeta = sc, m
		}
	}
	return best
}

func betterBannerShot(a WorkScreenshotRow, am ImageMeta, b WorkScreenshotRow, bm ImageMeta) bool {
	if a.Sexual != b.Sexual {
		return a.Sexual < b.Sexual
	}
	if aWide, bWide := am.Width >= bannerMinWidth, bm.Width >= bannerMinWidth; aWide != bWide {
		return aWide
	}
	if aArea, bArea := pixelArea(am), pixelArea(bm); aArea != bArea {
		return aArea > bArea
	}
	return a.ImageHash < b.ImageHash
}

func (s *PublicService) screenshotSlot(sc *WorkScreenshotRow, meta map[string]ImageMeta) *dto.PublicCoverSlot {
	slot := &dto.PublicCoverSlot{
		URL: s.imageURL(sc.ImageHash), Sexual: sc.Sexual, Violence: sc.Violence,
		Source: s.sourceKey(sc.SourceID), Origin: "screenshot",
	}
	if m, ok := meta[sc.ImageHash]; ok {
		slot.Width, slot.Height, slot.Thumbhash = m.Width, m.Height, m.Thumbhash
	}
	return slot
}
