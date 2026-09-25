package handler

import (
	"testing"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	"api/internal/platform/catalog/dto"
	catsvc "api/internal/platform/catalog/service"
)

func TestListNewCollectionsUnbound(t *testing.T) {
	_, err := (*Catalog)(nil).ListCharacters(t.Context(), collect.Query{}, characterFilter{})
	p, ok := err.(*problem.Problem)
	if !ok || p.Code != problem.CodeServiceUnavailable {
		t.Fatalf("characters %v", err)
	}
	_, err = (*Catalog)(nil).ListCreditNames(t.Context(), collect.Query{}, "")
	if p, ok = err.(*problem.Problem); !ok || p.Code != problem.CodeServiceUnavailable {
		t.Fatalf("names %v", err)
	}
	_, err = (*Catalog)(nil).ListPersons(t.Context(), collect.Query{})
	if p, ok = err.(*problem.Problem); !ok || p.Code != problem.CodeServiceUnavailable {
		t.Fatalf("persons %v", err)
	}
	_, err = (*Catalog)(nil).ListTraits(t.Context(), collect.Query{}, traitFilter{})
	if p, ok = err.(*problem.Problem); !ok || p.Code != problem.CodeServiceUnavailable {
		t.Fatalf("traits %v", err)
	}
}

func TestAttachCharacterAttrs(t *testing.T) {
	g := int16(2)
	m, d := int16(3), int16(9)
	bust := int16(90)
	inst := int64(12)
	out := repr.Character{Object: "character", ID: "1"}
	attachCharacterAttrs(&out, catsvc.CharacterAttributes{
		Gender: &g, BirthdayMonth: &m, BirthdayDay: &d, BustCm: &bust, InstanceOf: &inst,
	})
	if out.Gender == nil || *out.Gender != "female" {
		t.Fatalf("gender %+v", out.Gender)
	}
	if out.Birthday == nil || *out.Birthday != "03-09" {
		t.Fatalf("birthday %+v", out.Birthday)
	}
	if out.Measurements == nil || out.Measurements.BustCm == nil || *out.Measurements.BustCm != 90 {
		t.Fatalf("measurements %+v", out.Measurements)
	}
	if out.InstanceOfID == nil || *out.InstanceOfID != "12" {
		t.Fatalf("instance %+v", out.InstanceOfID)
	}
}

func TestCharacterWantsAttrs(t *testing.T) {
	if characterWantsAttrs(nil) || characterWantsAttrs([]string{"titles"}) {
		t.Fatal("basic")
	}
	if !characterWantsAttrs([]string{"gender"}) {
		t.Fatal("gender")
	}
}

func TestPersonTraitMappers(t *testing.T) {
	pid := int64(8)
	cn := creditNameFromRow(catsvc.EntityListRow{ID: 3, DisplayName: "A", PersonID: &pid})
	if cn.Object != "credit_name" || cn.PersonID == nil || *cn.PersonID != "8" {
		t.Fatalf("%+v", cn)
	}
	p := personFromRow(catsvc.EntityListRow{ID: 8, DisplayName: "P"})
	if p.Object != "person" || p.ID != "8" {
		t.Fatalf("%+v", p)
	}
	tr := traitFromRow(catsvc.EntityListRow{ID: 1, DisplayName: "loli", NameZh: "萝莉", VndbTID: "i1", Sexual: true}, nil)
	if tr.Object != "trait" || !tr.IsSexual || tr.VndbTID != "i1" {
		t.Fatalf("%+v", tr)
	}
	if tr.Parents == nil || tr.Localized == nil {
		t.Fatalf("parents/localized %+v", tr)
	}
}

func TestTraitFromRowIntros(t *testing.T) {
	row := catsvc.EntityListRow{
		ID: 1, DisplayName: "Ahoge",
		Intros: []dto.PublicIntro{
			{Lang: "en", Intro: "A strand of hair.", Source: "vndb"},
			{Lang: "zh-Hans", Intro: "一缕呆毛。", Source: "vndb"},
		},
	}
	got := traitFromRow(row, []string{"intros"})
	if got.Intros == nil || len(*got.Intros) != 2 {
		t.Fatalf("%+v", got.Intros)
	}
	en, zh := (*got.Intros)[0], (*got.Intros)[1]
	if en.Lang != "en" || en.Value != "A strand of hair." || en.IsMachine || en.Source != "vndb" {
		t.Fatalf("en %+v", en)
	}
	if zh.Lang != "zh-Hans" || zh.Value != "一缕呆毛。" || zh.IsMachine || zh.Source != "vndb" {
		t.Fatalf("zh %+v", zh)
	}
	absent := traitFromRow(row, nil)
	if absent.Intros != nil {
		t.Fatalf("absent %+v", absent.Intros)
	}
	empty := traitFromRow(catsvc.EntityListRow{ID: 2, DisplayName: "x", Intros: []dto.PublicIntro{}}, []string{"intros"})
	if empty.Intros == nil || len(*empty.Intros) != 0 {
		t.Fatalf("empty %+v", empty.Intros)
	}
}
