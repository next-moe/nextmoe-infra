package service

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"api/internal/platform/auth/dto"
	"api/internal/platform/auth/model"
	"api/internal/platform/auth/repository"
	shopModel "api/internal/platform/shop/model"
	"api/pkg/config"
	"api/pkg/errors"
)

type perkTable map[uint][]string

func (p perkTable) PerksFor(_ context.Context, ids []uint) (map[uint][]string, error) {
	out := map[uint][]string{}
	for _, id := range ids {
		if held, ok := p[id]; ok {
			out[id] = held
		}
	}
	return out, nil
}

func TestAboutNeedsThePerkToBeWrittenAndToBeSeen(t *testing.T) {
	db := requireDB(t)
	ctx := context.Background()
	tag := strconv.FormatInt(time.Now().UnixNano(), 36)
	u := &model.User{Name: "ab" + tag, Email: "ab-" + tag + "@test.local"}
	if err := db.Create(u).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = ?`, u.ID) })

	perks := perkTable{}
	repo := repository.NewUserRepository(db)
	svc := NewAuthService(repo, nil, config.JWTConfig{}).WithPerks(perks)
	batch := NewUserBatchService(repo, nil).WithPerks(perks)
	aboutHTML := func() string {
		t.Helper()
		b, err := batch.GetBriefs(ctx, []uint{u.ID}, 0)
		if err != nil || len(b.Users) != 1 {
			t.Fatalf("batch: %+v %v", b, err)
		}
		return b.Users[0].AboutHTML
	}

	text := "**喜欢** 纯爱"
	_, err := svc.UpdateProfile(ctx, u.UUID, &dto.UpdateProfileRequest{About: &text})
	if !errors.Is(err, errors.ErrShopPerkRequired) {
		t.Fatalf("writing an intro without the perk: %v", err)
	}

	perks[u.ID] = []string{shopModel.KindProfileAbout}
	got, err := svc.UpdateProfile(ctx, u.UUID, &dto.UpdateProfileRequest{About: &text})
	if err != nil || got.About != text || !strings.Contains(got.AboutHTML, "<strong>喜欢</strong>") {
		t.Fatalf("writing with the perk: %+v %v", got, err)
	}
	if !strings.Contains(aboutHTML(), "<strong>") {
		t.Fatal("the batch hid an intro whose owner holds the perk")
	}

	delete(perks, u.ID)
	if aboutHTML() != "" {
		t.Fatal("the batch showed an intro after the perk lapsed")
	}
	var kept model.User
	db.First(&kept, u.ID)
	if kept.About != text {
		t.Fatalf("a lapsed perk lost the intro itself: %q", kept.About)
	}

	empty := ""
	got, err = svc.UpdateProfile(ctx, u.UUID, &dto.UpdateProfileRequest{About: &empty})
	if err != nil || got.About != "" || got.AboutHTML != "" {
		t.Fatalf("clearing the intro without the perk: %+v %v", got, err)
	}
}
