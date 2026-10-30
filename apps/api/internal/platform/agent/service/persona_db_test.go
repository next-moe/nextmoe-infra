package service

import (
	"context"
	"testing"

	"api/internal/platform/agent/model"
)

func TestSiteOwnPersonaWinsAndActivationIsExclusive(t *testing.T) {
	ctx := context.Background()
	s := NewStore(testDB)
	const site = 9301
	t.Cleanup(func() { testDB.Where("site_id = ?", site).Delete(&model.Persona{}) })

	def, err := s.Persona(ctx, site)
	if err != nil || def.SiteID != 0 {
		t.Fatalf("a site without its own persona must get the default: %+v, %v", def, err)
	}
	a := &model.Persona{SiteID: site, Kind: model.PersonaMascot, Name: "甲", Prompt: "p", Greetings: []byte("[]"), Suggestions: []byte("[]"), Active: true}
	if err := s.SavePersona(ctx, a); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Persona(ctx, site); got.ID != a.ID {
		t.Fatalf("the site's own persona must win, got %+v", got)
	}
	b := &model.Persona{SiteID: site, Kind: model.PersonaMascot, Name: "乙", Prompt: "p", Greetings: []byte("[]"), Suggestions: []byte("[]"), Active: true}
	if err := s.SavePersona(ctx, b); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.Personas(ctx, &[]int64{site}[0])
	active := 0
	for _, r := range rows {
		if r.Active {
			active++
		}
	}
	if len(rows) != 2 || active != 1 {
		t.Fatalf("activating one must deactivate the other: %+v", rows)
	}
	if got, _ := s.Persona(ctx, site); got.ID != b.ID {
		t.Fatalf("the newly activated persona must answer, got %s", got.Name)
	}
	if err := s.DeletePersona(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Persona(ctx, site); got.SiteID != 0 {
		t.Fatalf("with its active persona gone the site must fall back to the default, got %+v", got)
	}
}
