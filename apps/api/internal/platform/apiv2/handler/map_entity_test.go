package handler

import (
	"strings"
	"testing"

	"api/internal/platform/catalog/dto"
)

func TestCreditNameFromDetailBlocks(t *testing.T) {
	hash := strings.Repeat("e", 64)
	y, m := int16(1979), int16(4)
	g := int16(2)
	rec := dto.PublicName{
		ID: 3, DisplayName: "水橋かおり", PersonID: 9, Gender: &g, BirthY: &y, BirthM: &m,
		PhotoHash: hash,
		Siblings:  []dto.PublicSiblingName{{ID: 4, DisplayName: "みずはし かおり"}},
		Aliases:   []dto.PublicAlias{{Value: "Kaori Mizuhashi", Lang: "en", Kind: "translation"}},
		Links:     []dto.PublicLink{{Source: "twitter", URL: "https://x.com/kaori"}},
		Refs:      []dto.PublicCatalogRef{{Source: "vndb", ExternalID: "s1"}},
	}
	bare := creditNameFromDetail(rec, nil, testImageURL(hash))
	if bare.Gender == nil || *bare.Gender != "female" {
		t.Fatalf("gender %+v", bare.Gender)
	}
	if bare.BirthYear == nil || *bare.BirthYear != 1979 || bare.BirthMonth == nil || bare.BirthDay != nil {
		t.Fatalf("birth %+v %+v %+v", bare.BirthYear, bare.BirthMonth, bare.BirthDay)
	}
	if bare.Aliases != nil || bare.Photo != nil || bare.Siblings != nil || bare.Links != nil || bare.Refs != nil {
		t.Fatalf("unrequested blocks must be omitted: %+v", bare)
	}
	full := creditNameFromDetail(rec, []string{"aliases", "photo", "siblings", "intros", "links", "refs"}, testImageURL(hash))
	if full.Photo == nil || full.Photo.Hash != hash {
		t.Fatalf("photo %+v", full.Photo)
	}
	if full.Siblings == nil || len(*full.Siblings) != 1 {
		t.Fatalf("siblings %+v", full.Siblings)
	}
	sib := (*full.Siblings)[0]
	if sib.Object != "credit_name" || sib.PersonID == nil || *sib.PersonID != "9" {
		t.Fatalf("a sibling shares the person by definition: %+v", sib)
	}
	if full.Intros == nil || len(*full.Intros) != 0 {
		t.Fatalf("a requested empty block is an empty array, not null: %+v", full.Intros)
	}
	if full.Links == nil || (*full.Links)[0].URL != "https://x.com/kaori" {
		t.Fatalf("links %+v", full.Links)
	}
	if full.Refs == nil || (*full.Refs)[0].ExternalID != "s1" {
		t.Fatalf("refs %+v", full.Refs)
	}
}

func TestTagAndSeriesDetailBlocks(t *testing.T) {
	tag := tagFromDetail(dto.PublicTagDetail{
		ID: 2, Name: "nukige", Tier: "core", Kind: "content",
		Intros: []dto.PublicIntro{{Lang: "zh-Hans", Intro: "描述", Source: "curated"}},
	}, []string{"intros"})
	if tag.Intros == nil || len(*tag.Intros) != 1 || (*tag.Intros)[0].IsMachine {
		t.Fatalf("tag intros %+v", tag.Intros)
	}
	if tagFromDetail(dto.PublicTagDetail{ID: 2, Name: "nukige"}, nil).Intros != nil {
		t.Fatal("tag intros must be omitted without include=intros")
	}
	ser := seriesFromDetail(dto.PublicSeriesDetail{
		ID: 5, DisplayName: "シリーズ", WorkCount: 2, HasNSFW: true,
		Intros: []dto.PublicSeriesIntro{{Lang: "ja", Intro: "説明", Source: "vndb"}},
		Refs:   []dto.PublicCatalogRef{{Source: "vndb", ExternalID: "s-1"}},
	}, []string{"intros", "refs"})
	if ser.Intros == nil || (*ser.Intros)[0].Value != "説明" || (*ser.Intros)[0].IsMachine {
		t.Fatalf("series intros %+v", ser.Intros)
	}
	if ser.Refs == nil || (*ser.Refs)[0].ExternalID != "s-1" {
		t.Fatalf("series refs %+v", ser.Refs)
	}
	list := seriesFromListItem(dto.PublicSeriesListItem{ID: 6, DisplayName: "x", WorkCount: 0, HasNSFW: true})
	if !list.HasNSFW || list.Intros != nil || list.Refs != nil {
		t.Fatalf("list row %+v", list)
	}
}
