package service

import (
	"testing"

	"api/internal/platform/catalog/dto"
	"api/internal/platform/catalog/model"
)

func TestPublicRelationsCarryCoverAndReleaseDate(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvcCDN()
	ctx := t.Context()

	safeHash, sexualHash := hash64("1122"), hash64("3344")
	svc.WithImageMeta(stubMeta(map[string]ImageMeta{
		safeHash:   {Width: 800, Height: 1200, Thumbhash: "TH-SAFE"},
		sexualHash: {Width: 800, Height: 1200, Thumbhash: "TH-SEX"},
	}))

	w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "base")
	other := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "fandisc")
	bare := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "no media")
	explicit := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "only explicit art")
	createWorkRelation(t, w.ID, other.ID)
	createWorkRelation(t, w.ID, bare.ID)
	createWorkRelation(t, w.ID, explicit.ID)
	addWorkCover(t, explicit.ID, sexualHash, 0, "main", false, 2, srcVNDB)
	addWorkCover(t, other.ID, sexualHash, 0, "main", false, 2, srcVNDB)
	addWorkCover(t, other.ID, safeHash, 1, "main", false, 0, srcVNDB)
	createRelease(t, other.ID, 2018, 6, 29)
	createRelease(t, other.ID, 2020, 6, 26)

	byID := func(rels []relationView) map[int64]relationView {
		out := map[int64]relationView{}
		for _, r := range rels {
			out[r.id] = r
		}
		return out
	}

	rec, found, err := svc.WorkDetail(ctx, w.ID, PublicInclude{Relations: true}, false, 0, PublicFields{})
	if err != nil || !found {
		t.Fatalf("detail: found=%v err=%v", found, err)
	}
	got := byID(viewRelations(rec.Relations))
	if g := got[other.ID]; g.date != "2018-06-29" || g.coverURL != testCDNBase+"/11/22/"+safeHash+".webp" || g.thumbhash != "TH-SAFE" {
		t.Fatalf("related work = %+v, want the earliest release date and the display-safe cover", g)
	}
	if g := got[bare.ID]; g.date != "" || g.coverURL != "" {
		t.Fatalf("work without releases or covers = %+v, want both empty", g)
	}

	sub, found, err := svc.WorkRelations(ctx, w.ID, false, 50, 0)
	if err != nil || !found {
		t.Fatalf("relations sub-face: found=%v err=%v", found, err)
	}
	if g := byID(viewRelations(sub.Items))[other.ID]; g != got[other.ID] {
		t.Fatalf("sub-face %+v differs from include=relations %+v", g, got[other.ID])
	}

	if g := got[explicit.ID]; g.coverURL != "" {
		t.Fatalf("sfw caller = %+v, must never receive a sexual cover", g)
	}

	setDisplayNSFW(t, explicit.ID, true)
	rec, _, err = svc.WorkDetail(ctx, w.ID, PublicInclude{Relations: true}, true, 0, PublicFields{})
	if err != nil {
		t.Fatal(err)
	}
	nsfwView := byID(viewRelations(rec.Relations))
	if g := nsfwView[explicit.ID]; g.coverURL != testCDNBase+"/33/44/"+sexualHash+".webp" {
		t.Fatalf("nsfw caller on a display_nsfw work = %+v, want its sexual cover", g)
	}
	if g := nsfwView[other.ID]; g.coverURL != testCDNBase+"/11/22/"+safeHash+".webp" {
		t.Fatalf("nsfw caller = %+v, the display-safe cover keeps winning its tier as on the works list", g)
	}
	rec, _, err = svc.WorkDetail(ctx, w.ID, PublicInclude{Relations: true}, false, 0, PublicFields{})
	if err != nil {
		t.Fatal(err)
	}
	if g := byID(viewRelations(rec.Relations))[explicit.ID]; g.coverURL != "" {
		t.Fatalf("sfw caller on a display_nsfw work = %+v, must never receive a sexual cover", g)
	}
}

type relationView struct {
	id                        int64
	date, coverURL, thumbhash string
}

func viewRelations(in []dto.PublicRelation) []relationView {
	out := make([]relationView, 0, len(in))
	for _, r := range in {
		v := relationView{id: r.Work.ID}
		if r.Work.ReleaseDate != nil {
			v.date = *r.Work.ReleaseDate
		}
		if r.Work.Cover != nil {
			v.coverURL, v.thumbhash = r.Work.Cover.URL, r.Work.Cover.Thumbhash
		}
		out = append(out, v)
	}
	return out
}
