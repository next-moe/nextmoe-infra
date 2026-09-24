package handler

import (
	"path"
	"strings"
	"time"

	"api/internal/platform/apiv2/repr"
	"api/internal/platform/catalog/dto"
)

func workFromListItem(it dto.PublicWorkListItem, include []string, logoURL func(string) string) repr.Work {
	var cover, banner *repr.CoverSlot
	if it.Covers != nil {
		cover = imageFromSlot(it.Covers.Portrait)
		banner = imageFromSlot(it.Covers.Banner)
	}
	if cover == nil {
		cover = slotFromImage(imageFromURL(it.Cover, ""), "cover")
	}
	created := it.Created
	if created == "" {
		created = it.Updated
	}
	w, _ := repr.NewWork(
		it.ID, it.Medium, it.DisplayName, it.OLang, it.ContentRating, releaseStatus(it.ReleaseDate),
		created, it.Updated, optString(it.Latin), localizedFrom(it.Localized),
		releaseDateValue(it.ReleaseDate), releasePrecision(it.ReleaseDate), cover, banner, claimFrom(it.ClaimedBy),
		it.ContentLimit,
	)
	want := map[string]bool{}
	for _, t := range include {
		want[t] = true
	}
	if want["intros"] {
		w.Intros = ptrSlice(introsFrom(it.Intros))
	}
	if want["companies"] {
		w.Companies = ptrSlice(workCompaniesFrom(it.Labels, logoURL))
	}
	if want["ratings"] {
		w.Ratings = ptrSlice(ratingsFrom(it.Ratings))
	}
	if want["refs"] {
		w.Refs = ptrSlice(refsFrom(it.Refs))
	}
	if want["tags"] {
		w.Tags = ptrSlice(workTagsFrom(it.Tags))
	}
	if want["credits"] {
		w.Credits = ptrSlice(creditGroupsFrom(it.Credits))
	}
	if it.ViaLabel != nil {
		w.ViaCompany = &repr.ViaCompany{
			Object: "company", ID: repr.ID(it.ViaLabel.ID),
			DisplayName: it.ViaLabel.DisplayName, Localized: localizedFrom(it.ViaLabel.Localized),
		}
	}
	return w
}

func workFromDetail(rec dto.PublicCatalogWork, include []string, logoURL func(string) string) repr.Work {
	var cover, banner *repr.CoverSlot
	if rec.CoverSlots != nil {
		cover = imageFromSlot(rec.CoverSlots.Portrait)
		banner = imageFromSlot(rec.CoverSlots.Banner)
	}
	created := rec.Created
	if created == "" {
		created = rec.Updated
	}
	w, _ := repr.NewWork(
		rec.ID, rec.Medium, rec.DisplayName, rec.OLang, rec.ContentRating, releaseStatus(rec.ReleaseDate),
		created, rec.Updated, optString(rec.Latin), localizedFrom(rec.Localized),
		releaseDateValue(rec.ReleaseDate), releasePrecision(rec.ReleaseDate), cover, banner, claimFrom(rec.ClaimedBy),
		rec.ContentLimit,
	)
	attachWorkIncludes(&w, rec, include, logoURL)
	return w
}

func attachWorkIncludes(w *repr.Work, rec dto.PublicCatalogWork, include []string, logoURL func(string) string) {
	want := map[string]bool{}
	for _, t := range include {
		want[t] = true
	}
	if want["titles"] {
		w.Titles = ptrSlice(titlesFrom(rec.Titles))
	}
	if want["refs"] {
		w.Refs = ptrSlice(refsFrom(rec.Refs))
	}
	if want["relations"] {
		w.Relations = ptrSlice(relationsFrom(rec.Relations))
	}
	if want["credits"] {
		w.Credits = ptrSlice(creditGroupsFrom(rec.Credits))
	}
	if want["releases"] {
		w.Releases = ptrSlice(releasesFrom(rec.Releases))
	}
	if want["popularity"] {
		w.Popularity = ptrSlice(popularityFrom(rec.Popularity))
	}
	if want["ratings"] {
		w.Ratings = ptrSlice(ratingsFrom(rec.Ratings))
	}
	if want["tags"] {
		w.Tags = ptrSlice(workTagsFrom(rec.Tags))
	}
	if want["playtimes"] {
		w.Playtimes = ptrSlice(playtimesFrom(rec.Playtimes))
	}
	if want["series"] {
		w.Series = ptrSlice(workSeriesFrom(rec.Series))
	}
	if want["platforms"] {
		w.Platforms = ptrSlice(platformsFrom(rec.Platforms))
	}
	if want["intros"] {
		w.Intros = ptrSlice(introsFrom(rec.Intros))
	}
	if want["covers"] {
		w.Covers = ptrSlice(coversFrom(rec.Covers))
	}
	if want["screenshots"] {
		w.Screenshots = ptrSlice(screenshotsFrom(rec.Screenshots))
	}
	if want["characters"] {
		w.Characters = ptrSlice(workCharactersFrom(rec.Characters))
	}
	if want["companies"] {
		w.Companies = ptrSlice(workCompaniesFrom(rec.Labels, logoURL))
	}
	if want["engines"] {
		w.Engines = ptrSlice(workEnginesFrom(rec.Engines))
	}
	if want["links"] {
		w.Links = ptrSlice(workLinksFrom(rec.Links))
	}
}

func claimFrom(c *dto.PublicClaimedBy) *repr.Claim {
	if c == nil {
		return nil
	}
	return &repr.Claim{
		Site: c.Site, SiteWorkID: repr.ID(c.WorkID), State: c.State, ContentLimit: c.ContentLimit,
	}
}

func localizedFrom(in map[string]dto.PublicLocalizedName) map[string]repr.LocalizedText {
	out := map[string]repr.LocalizedText{}
	for k, v := range in {
		out[k] = repr.LocalizedText{Value: v.Value, IsMachine: v.Machine}
	}
	return out
}

func optString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func optID(id int64) *string {
	if id <= 0 {
		return nil
	}
	s := repr.ID(id)
	return &s
}

// The catalog stores a fuzzy release date: partialISOFromOrdinal yields "2024",
// "2024-06" or "2024-06-15". v2 published that verbatim under `format: date`
// with `release_date_precision` hard-coded to "day", so a year-only row shipped
// "2024" — which no date parser accepts — while claiming day precision, and a
// work released next year read `released`. The schema already says what to do:
// month dates sit on the 1st, year dates on January 1.
func releasePrecision(date *string) *string {
	p := ""
	switch partialDateLen(date) {
	case 4:
		p = "year"
	case 7:
		p = "month"
	case 10:
		p = "day"
	default:
		return nil
	}
	return &p
}

func releaseDateValue(date *string) *string {
	switch partialDateLen(date) {
	case 4:
		s := *date + "-01-01"
		return &s
	case 7:
		s := *date + "-01"
		return &s
	case 10:
		return date
	default:
		return nil
	}
}

func releaseStatus(date *string) string {
	full := releaseDateValue(date)
	if full == nil {
		return "unknown"
	}
	if *full > time.Now().UTC().Format("2006-01-02") {
		return "dated"
	}
	return "released"
}

func partialDateLen(date *string) int {
	if date == nil {
		return 0
	}
	switch len(*date) {
	case 4, 7, 10:
		return len(*date)
	}
	return 0
}

func imageFromSlot(slot *dto.PublicCoverSlot) *repr.CoverSlot {
	if slot == nil {
		return nil
	}
	return slotFromImage(
		imageFromURLMeta(slot.URL, slot.Source, slot.Width, slot.Height, slot.Thumbhash, slot.Sexual),
		slot.Origin,
	)
}

func slotFromImage(img *repr.Image, origin string) *repr.CoverSlot {
	if img == nil {
		return nil
	}
	return &repr.CoverSlot{
		URL: img.URL, Hash: img.Hash, Width: img.Width, Height: img.Height,
		Thumbhash: img.Thumbhash, Sexual: img.Sexual, Violence: img.Violence,
		Source: img.Source, Origin: origin,
	}
}

func imageFromURL(url, source string) *repr.Image {
	return imageFromURLMeta(url, source, 0, 0, "", 0)
}

func imageFromURLMeta(url, source string, width, height int, thumbhash string, sexual int16) *repr.Image {
	h := hashFromURL(url)
	if url == "" || h == "" {
		return nil
	}
	img := &repr.Image{URL: url, Hash: h, Source: source}
	if width > 0 {
		w := width
		img.Width = &w
	}
	if height > 0 {
		ht := height
		img.Height = &ht
	}
	if thumbhash != "" {
		th := thumbhash
		img.Thumbhash = &th
	}
	if width > 0 || height > 0 || thumbhash != "" || sexual != 0 {
		if sx, ok := repr.Sexual(&sexual); ok {
			img.Sexual = sx
		}
	}
	return img
}

func hashFromURL(url string) string {
	base := path.Base(url)
	if i := strings.LastIndex(base, "."); i > 0 {
		base = base[:i]
	}
	if len(base) == 64 {
		return base
	}
	return ""
}
