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

func isCoverArt(kind string) bool {
	switch kind {
	case "pkgfront", "pkgback", "pkgmed", "pkgcontent", "pkgside":
		return false
	default:
		return true
	}
}

const censoredSourceKey = "censored"

// Stand-in ladder: real safe art first, real explicit art second (R18 opt-in
// only), the blurred 'censored' ghost last — and only for the portrait slot,
// the card face lists render. Ghosts must never compete inside scanCovers:
// they are sexual=0 by construction, so a ghost in the main scan would win the
// SFW portrait over nothing but ALSO shadow the real explicit cover an R18
// viewer should get, because the sexual pass only fills slots the safe pass
// left empty. The banner keeps the real-art + screenshot ladder and may stay
// nil — a ghost hero adds nothing a nil banner does not already handle.
func (s *PublicService) pickCoverSlots(rows []WorkCoverRow, meta map[string]ImageMeta, allowSexual bool) *dto.PublicWorkCoverSlots {
	real, ghosts := s.splitCensored(rows)
	cand := s.scanCovers(real, meta, false)
	if allowSexual && !cand.complete() {
		cand.fillFrom(s.scanCovers(real, meta, true))
	}
	portraitRow := cand.portrait()
	if portraitRow == nil && len(ghosts) > 0 {
		ghost := s.scanCovers(ghosts, meta, false)
		portraitRow = ghost.portrait()
	}
	portrait, banner := s.coverSlot(portraitRow, meta), s.coverSlot(cand.banner(), meta)
	if portrait == nil && banner == nil {
		return nil
	}
	return &dto.PublicWorkCoverSlots{Portrait: portrait, Banner: banner}
}

func (s *PublicService) splitCensored(rows []WorkCoverRow) (real, ghosts []WorkCoverRow) {
	for _, c := range rows {
		if s.sourceKey(c.SourceID) == censoredSourceKey {
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
	first                     *WorkCoverRow
}

func (c coverCandidates) portrait() *WorkCoverRow {
	switch {
	case c.pinned != nil:
		return c.pinned
	case c.portraitWide != nil:
		return c.portraitWide
	case c.portraitAny != nil:
		return c.portraitAny
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
		c.bannerWide != nil && c.bannerAny != nil && c.first != nil
}

func (c *coverCandidates) fillFrom(other coverCandidates) {
	for _, pair := range []struct{ dst, src **WorkCoverRow }{
		{&c.pinned, &other.pinned}, {&c.portraitWide, &other.portraitWide},
		{&c.portraitAny, &other.portraitAny}, {&c.bannerWide, &other.bannerWide},
		{&c.bannerAny, &other.bannerAny}, {&c.first, &other.first},
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
		if !isCoverArt(c.Kind) {
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
	if s.sourceKey(c.SourceID) == censoredSourceKey {
		origin = censoredSourceKey
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
	slots *dto.PublicWorkCoverSlots, shots []WorkScreenshotRow, meta map[string]ImageMeta, allowSexual bool,
) *dto.PublicWorkCoverSlots {
	best := s.pickBannerScreenshot(shots, meta, false)
	if best == nil && allowSexual {
		best = s.pickBannerScreenshot(shots, meta, true)
	}
	if best == nil {
		return slots
	}
	if slots == nil {
		slots = &dto.PublicWorkCoverSlots{}
	}
	slots.Banner = s.screenshotSlot(best, meta)
	return slots
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
