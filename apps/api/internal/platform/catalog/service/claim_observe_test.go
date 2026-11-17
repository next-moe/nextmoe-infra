package service

import (
	"context"
	"slices"
	"testing"

	"api/internal/platform/catalog/model"
)

func TestClaimWritesNotifyWorkObserver(t *testing.T) {
	s := newLifecycle(t)
	var seen []int64
	s.ObserveWorkWrites(func(ids ...int64) { seen = append(seen, ids...) })

	minted, err := s.SubmitWork(t.Context(), SubmitWorkParams{
		Site: submitSite, ActorUID: 7, Fields: submitFieldsNoAnchor("観測される作品"),
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	work := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "認領作品")
	product := int64(5151)
	act(t, s, work.ID, ClaimActionClaim, ClaimActionParams{Site: submitSite, ProductWorkID: &product, ActorUID: 7})

	if _, err := s.Act(context.Background(), ClaimActionParams{WorkID: work.ID, Action: ClaimActionApprove, ActorUID: 99}); err == nil {
		t.Fatal("approving a draft must be refused")
	}

	if err := s.DeleteDraft(t.Context(), work.ID, 7); err != nil {
		t.Fatalf("delete draft: %v", err)
	}

	if want := []int64{minted.WorkID, work.ID, work.ID}; !slices.Equal(seen, want) {
		t.Fatalf("observed %v, want %v (a refused write must not be reported)", seen, want)
	}
}
