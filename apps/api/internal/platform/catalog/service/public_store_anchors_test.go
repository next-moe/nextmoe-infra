package service

import (
	"testing"

	"api/internal/platform/catalog/model"
)

func TestStoreAnchorsReadBothGrains(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()

	w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "two grains")
	rel := createRelease(t, w.ID, 2020, 1, 1)
	addExternalRef(t, model.EntityTypeRelease, rel.ID, srcSteam, "100", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeWork, w.ID, srcSteam, "100", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeWork, w.ID, srcSteam, "200", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeWork, w.ID, srcSteam, "300", model.LinkKindProbable)
	addExternalRef(t, model.EntityTypeWork, w.ID, srcSteam, "400", model.LinkKindExact)
	markRefDead(t, model.EntityTypeWork, w.ID, srcSteam, "400")
	addExternalRef(t, model.EntityTypeWork, w.ID, srcVNDB, "v1", model.LinkKindExact)

	hidden := createRelease(t, w.ID, 2021, 1, 1)
	addExternalRef(t, model.EntityTypeRelease, hidden.ID, srcSteam, "500", model.LinkKindExact)
	if err := testDB.Delete(hidden).Error; err != nil {
		t.Fatalf("hide release: %v", err)
	}

	anchors, visible, err := svc.StoreAnchorsFor(t.Context(), []int64{w.ID})
	if err != nil {
		t.Fatalf("StoreAnchorsFor: %v", err)
	}
	if !visible[w.ID] {
		t.Fatalf("work %d must be visible", w.ID)
	}
	want := []StoreAnchor{{Source: "steam", ExternalID: "100"}, {Source: "steam", ExternalID: "200"}}
	got := anchors[w.ID]
	if len(got) != len(want) {
		t.Fatalf("anchors = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("anchors = %+v, want %+v", got, want)
		}
	}
}
