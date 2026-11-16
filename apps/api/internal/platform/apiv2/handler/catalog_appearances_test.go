package handler

import (
	"testing"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	"api/internal/platform/catalog/dto"
	catsvc "api/internal/platform/catalog/service"
)

func TestAppearanceFromPublicRow(t *testing.T) {
	row := dto.PublicCharacterWork{
		Work: dto.PublicWorkBrief{ID: 7, Medium: "galgame", DisplayName: "x", ContentRating: "all_ages"},
		Kind: "main", Spoiler: 1,
		Voices: []dto.PublicVoiceName{{ID: 9, DisplayName: "VA"}},
	}
	role, ok := repr.RosterRoleFromKey(row.Kind)
	if !ok || role != "main" {
		t.Fatalf("role %s %v", role, ok)
	}
	sp, ok := repr.Spoiler(row.Spoiler)
	if !ok || sp != "minor" {
		t.Fatalf("spoiler %s %v", sp, ok)
	}
}

func TestCharacterAppearancesUnbound(t *testing.T) {
	_, err := (*Catalog)(nil).CharacterAppearances(t.Context(), 1, catsvc.EntityWorksGate{}, "", 20)
	p, ok := err.(*problem.Problem)
	if !ok || p.Code != problem.CodeServiceUnavailable {
		t.Fatalf("%v", err)
	}
}
