package handler

import (
	"encoding/json"
	"strings"
	"testing"

	"api/internal/platform/apiv2/repr"
	"api/internal/platform/catalog/dto"
	"api/pkg/imageclient"
)

func TestWorkFromListItemBasic(t *testing.T) {
	date := "2011-06-24"
	it := dto.PublicWorkListItem{
		ID:            19658,
		Medium:        "galgame",
		DisplayName:   "紅殻のパンドラ",
		ContentRating: "r18",
		OLang:         "ja",
		ReleaseDate:   &date,
		Updated:       "2026-08-19T02:31:06Z",
		Cover:         "https://img.example/ab/cd/" + strings.Repeat("a", 64) + ".webp",
		ClaimedBy:     &dto.PublicClaimedBy{Site: "touchgal", WorkID: 8812, State: "live", ContentLimit: "sfw"},
	}
	w := workFromListItem(it, nil, testImageURL)
	if w.Object != "work" || w.ID != "19658" || w.ReleaseStatus != "released" {
		t.Fatalf("work %+v", w)
	}
	if w.Claim == nil || w.Claim.SiteWorkID != "8812" {
		t.Fatalf("claim %+v", w.Claim)
	}
	if w.Cover == nil || w.Cover.Hash != strings.Repeat("a", 64) {
		t.Fatalf("cover %+v", w.Cover)
	}
	if w.Cover.Sexual != nil {
		t.Fatalf("url-only cover must not claim sexual: %+v", w.Cover.Sexual)
	}
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"id":19658`) || strings.Contains(string(b), `"kind"`) {
		t.Fatalf("wire: %s", b)
	}
}

func TestWorkFromListItemUnknownRelease(t *testing.T) {
	w := workFromListItem(dto.PublicWorkListItem{
		ID: 1, Medium: "galgame", DisplayName: "x", OLang: "ja", ContentRating: "all_ages",
		Updated: "2026-01-01T00:00:00Z",
	}, nil, testImageURL)
	if w.ReleaseStatus != "unknown" || w.ReleaseDate != nil || w.ReleaseDatePrecision != nil {
		t.Fatalf("unknown release %+v", w)
	}
}

func TestWorkFromDetailCoverSlots(t *testing.T) {
	hash := strings.Repeat("b", 64)
	rec := dto.PublicCatalogWork{
		ID: 2, Medium: "galgame", DisplayName: "y", OLang: "ja", ContentRating: "sensitive",
		Created: "2024-01-01T00:00:00Z", Updated: "2024-02-01T00:00:00Z",
		CoverSlots: &dto.PublicWorkCoverSlots{
			Portrait: &dto.PublicCoverSlot{
				URL: "https://img.example/" + hash + ".webp", Width: 800, Height: 1131, Sexual: 0, Source: "vndb",
			},
		},
	}
	w := workFromDetail(rec, nil, testImageURL)
	if w.Cover == nil || w.Cover.Hash != hash || w.Cover.Sexual == nil || *w.Cover.Sexual != "safe" {
		t.Fatalf("slot cover %+v", w.Cover)
	}
}

func TestHashFromURL(t *testing.T) {
	h := strings.Repeat("c", 64)
	if got := hashFromURL("https://cdn.example/" + h + ".webp"); got != h {
		t.Fatalf("got %q", got)
	}
	if hashFromURL("https://cdn.example/not-a-hash.png") != "" {
		t.Fatal("non-hash")
	}
}

func TestClaimFromNil(t *testing.T) {
	if claimFrom(nil) != nil {
		t.Fatal("nil claim")
	}
	c := claimFrom(&dto.PublicClaimedBy{Site: "s", WorkID: 1, State: "draft", ContentLimit: "nsfw"})
	if c.SiteWorkID != "1" || c.State != "draft" {
		t.Fatalf("%+v", c)
	}
}

func TestReleaseFromFeed(t *testing.T) {
	date := "2011-06-24"
	it := dto.PublicReleaseFeedItem{
		ID: 11, Kind: "digital", Date: &date, Title: "DL", Lang: "ja", Platform: "win",
		Platforms: []string{"win"}, Refs: []dto.PublicCatalogRef{{Source: "dlsite", ExternalID: "RJ1"}},
		Work: dto.PublicWorkListItem{ID: 19658, Medium: "galgame", DisplayName: "x"},
	}
	r := releaseFromFeed(it)
	if r.Object != "release" || r.ID != "11" || r.WorkID == nil || *r.WorkID != "19658" || r.ReleaseKind != "digital" {
		t.Fatalf("%+v", r)
	}
	if r.Title == nil || *r.Title != "DL" || len(r.Refs) != 1 {
		t.Fatalf("title/refs %+v", r)
	}
}

func TestWorkIncludeBlocks(t *testing.T) {
	rec := dto.PublicCatalogWork{
		ID: 1, Medium: "galgame", DisplayName: "x", OLang: "ja", ContentRating: "all_ages",
		Updated: "2026-01-01T00:00:00Z",
		Titles:  []dto.PublicCatalogTitle{{Lang: "ja", Title: "x", Kind: "official"}},
		Refs:    []dto.PublicCatalogRef{{Source: "vndb", ExternalID: "v1"}},
		Labels:  []dto.PublicWorkLabel{{ID: 9, DisplayName: "Brand", LabelKind: "game_brand", Kind: "brand"}},
	}
	w := workFromDetail(rec, []string{"titles", "refs", "companies"}, testImageURL)
	if w.Titles == nil || len(*w.Titles) != 1 || (*w.Titles)[0].TitleKind != "official" {
		t.Fatalf("titles %+v", w.Titles)
	}
	if w.Refs == nil || (*w.Refs)[0].ExternalID != "v1" {
		t.Fatalf("refs %+v", w.Refs)
	}
	if w.Companies == nil || (*w.Companies)[0].AttributionRole != "brand" {
		t.Fatalf("companies %+v", w.Companies)
	}
	if w.Tags != nil || w.Covers != nil {
		t.Fatal("unrequested blocks must be omitted")
	}
}

func testImageURL(hash string) string {
	return imageclient.MainURL("https://img.example.test/image", hash, "webp")
}

func TestWorkCompaniesCarryLogoOnlyWhenHashed(t *testing.T) {
	hash := strings.Repeat("d", 64)
	got := workCompaniesFrom([]dto.PublicWorkLabel{
		{ID: 1, DisplayName: "With", LabelKind: "game_brand", Kind: "brand", LogoHash: hash},
		{ID: 2, DisplayName: "Without", LabelKind: "game_brand", Kind: "brand"},
	}, testImageURL)
	if len(got) != 2 {
		t.Fatalf("companies %+v", got)
	}
	if got[0].Logo == nil || got[0].Logo.Hash != hash {
		t.Fatalf("logo %+v", got[0].Logo)
	}
	if got[1].Logo != nil {
		t.Fatalf("company without a logo hash must have no logo: %+v", got[1].Logo)
	}
}

func TestRatingsCarryDistributionAndStats(t *testing.T) {
	avg, stdev := 63.5, 14.0
	got := ratingsFrom([]dto.PublicRating{
		{
			Source: "bangumi", Score: 7.9, VoteCount: 12,
			Distribution: []dto.RatingBucket{{Score: 8, Count: 5}, {Score: 9, Count: 7}},
			Stats:        &dto.RatingStats{Average: &avg, Stdev: &stdev},
		},
		{Source: "dlsite", Score: 4.5, VoteCount: 2},
	})
	if got[0].Distribution == nil || len(*got[0].Distribution) != 2 {
		t.Fatalf("distribution %+v", got[0].Distribution)
	}
	if (*got[0].Distribution)[0].Score != 8 || (*got[0].Distribution)[1].Count != 7 {
		t.Fatalf("buckets %+v", *got[0].Distribution)
	}
	if got[0].Stats == nil || got[0].Stats.Average == nil || *got[0].Stats.Average != avg || got[0].Stats.Min != nil {
		t.Fatalf("stats %+v", got[0].Stats)
	}
	if got[1].Distribution != nil || got[1].Stats != nil {
		t.Fatalf("a rating with no histogram must omit both keys: %+v", got[1])
	}
}

func TestTaxonomyListItemMappers(t *testing.T) {
	c := companyFromListItem(dto.PublicLabelListItem{
		ID: 7, DisplayName: "Alcot", Kind: "game_brand", WorkCount: 3,
		Localized: map[string]dto.PublicLocalizedName{"zh-Hans": {Value: "品牌"}},
	}, nil, "")
	if c.Object != "company" || c.ID != "7" || c.CompanyKind != "game_brand" || c.WorkCount != 3 {
		t.Fatalf("company %+v", c)
	}
	if c.Localized["zh-Hans"].Value != "品牌" {
		t.Fatalf("localized %+v", c.Localized)
	}
	other := companyFromListItem(dto.PublicLabelListItem{ID: 8, DisplayName: "x", Kind: "other"}, nil, "")
	if other.CompanyKind != "group" {
		t.Fatalf("other kind %s", other.CompanyKind)
	}
	tag := tagFromListItem(dto.PublicTagListItem{ID: 2, Name: "nukige", Tier: "core", Kind: "content", WorkCount: 9})
	if tag.DisplayName != "nukige" || tag.TagKind != "content" || tag.ID != "2" {
		t.Fatalf("tag %+v", tag)
	}
	eng := engineFromListItem(dto.PublicEngineListItem{ID: 4, Name: "KiriKiri", WorkCount: 1})
	if eng.DisplayName != "KiriKiri" || eng.ID != "4" {
		t.Fatalf("engine %+v", eng)
	}
}

func TestLocalizedFromEmpty(t *testing.T) {
	if localizedFrom(nil) == nil {
		t.Fatal("nil must become empty map")
	}
	in := map[string]dto.PublicLocalizedName{"zh-Hans": {Value: "名", Machine: true}}
	out := localizedFrom(in)
	if out["zh-Hans"] != (repr.LocalizedText{Value: "名", IsMachine: true}) {
		t.Fatalf("%+v", out)
	}
}

func TestAnUnassessedImageIsNeverSafe(t *testing.T) {
	hash := strings.Repeat("c", 64)
	url := "https://img.example/" + hash + ".webp"

	art := imageFromPublicMeta(url, &dto.PublicImageMeta{Width: 300, Height: 400}, "")
	if art == nil || art.Sexual != nil {
		t.Fatalf("character art with a size but no assessment must carry sexual null, got %+v", art)
	}

	graded := int16(0)
	cover := imageFromURLMeta(url, "vndb", 0, 0, "", &graded)
	if cover == nil || cover.Sexual == nil || *cover.Sexual != "safe" {
		t.Fatalf("an image graded safe must say so even without a size, got %+v", cover)
	}
}
