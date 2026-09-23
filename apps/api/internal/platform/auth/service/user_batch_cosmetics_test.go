package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	"api/internal/platform/auth/model"
	"api/internal/platform/auth/repository"
	shopModel "api/internal/platform/shop/model"
)

type siteCosmetics struct{ asked []uint }

func (s *siteCosmetics) CosmeticsFor(_ context.Context, ids []uint, siteID uint) (map[uint]shopModel.Cosmetics, error) {
	s.asked = append(s.asked, siteID)
	out := map[uint]shopModel.Cosmetics{}
	for _, id := range ids {
		out[id] = shopModel.Cosmetics{shopModel.SlotAvatarFrame: {ItemID: int64(siteID), Name: "框"}}
	}
	return out, nil
}

func TestSearchCarriesTheCallersCosmeticsLikeBatch(t *testing.T) {
	db := requireDB(t)
	tag := strconv.FormatInt(time.Now().UnixNano(), 36)
	u := &model.User{Name: "cs" + tag, Email: "cs-" + tag + "@test.local"}
	if err := db.Create(u).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = ?`, u.ID) })

	src := &siteCosmetics{}
	svc := NewUserBatchService(repository.NewUserRepository(db), nil).WithCosmetics(src)

	found, err := svc.SearchByName(context.Background(), u.Name, 5, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(found.Users) != 1 || found.Users[0].Cosmetics[shopModel.SlotAvatarFrame] == nil {
		t.Fatalf("search answered %+v without the frame", found.Users)
	}
	batch, err := svc.GetBriefs(context.Background(), []uint{u.ID}, 7)
	if err != nil {
		t.Fatal(err)
	}
	if batch.Users[0].Cosmetics[shopModel.SlotAvatarFrame] == nil {
		t.Fatal("batch lost the frame")
	}
	if len(src.asked) != 2 || src.asked[0] != 7 || src.asked[1] != 7 {
		t.Fatalf("resolved for sites %v, want the caller's site both times", src.asked)
	}
}
