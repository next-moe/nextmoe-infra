package service

import (
	"context"
	"testing"

	"api/internal/platform/auth/dto"
	"api/internal/platform/auth/federation"
	"api/internal/platform/auth/model"
)

// The second of the two production paths that mint a users row. Registration
// covers the first, and both reach UserRepository.Create, but federated first
// login is the one that builds its model.User far from the registration code
// and would be the easy one to grow its own INSERT.
func TestComplete_federatedFirstLoginIsAdultByConstruction(t *testing.T) {
	h := newFedHarness(t)
	result, err := h.callback(t, &federation.Identity{
		Subject: uniq("s"), Email: uniq("f") + "@gmail.com", EmailVerified: true, Name: "N",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, user, err := h.svc.Complete(context.Background(), &dto.FederationCompleteRequest{
		Token:     result.PendingToken,
		Name:      uniq("n"),
		Password:  "secret12",
		UserAgent: "ua",
		IPAddress: "127.0.0.1",
		BrowserID: "b1",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		h.db.Where("user_id = ?", user.ID).Delete(&model.OAuthAccount{})
		h.db.Where("user_id = ?", user.ID).Delete(&model.Session{})
		h.db.Unscoped().Where("id = ?", user.ID).Delete(&model.User{})
	})

	var stored model.User
	if err := h.db.First(&stored, user.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if stored.AdultConfirmedAt == nil {
		t.Fatal("a federated first login created an account with a null adult_confirmed_at")
	}
	if stored.NSFWDisplay != model.NSFWDisplayShow {
		t.Fatalf("nsfw_display = %q, want %q", stored.NSFWDisplay, model.NSFWDisplayShow)
	}
}
