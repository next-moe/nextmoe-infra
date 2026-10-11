package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	"api/internal/platform/auth/model"
	"api/internal/platform/auth/repository"
)

func TestBriefsDrawTheAvatarFromItsHash(t *testing.T) {
	db := requireDB(t)
	tag := strconv.FormatInt(time.Now().UnixNano(), 36)
	hash := "abcd" + tag
	with := &model.User{Name: "av" + tag, Email: "av-" + tag + "@test.local", AvatarImageHash: &hash}
	without := &model.User{Name: "nv" + tag, Email: "nv-" + tag + "@test.local"}
	for _, u := range []*model.User{with, without} {
		if err := db.Create(u).Error; err != nil {
			t.Fatalf("seed user: %v", err)
		}
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id IN (?, ?)`, with.ID, without.ID) })

	svc := NewUserBatchService(repository.NewUserRepository(db), nil, "https://img.example/")
	got, err := svc.GetBriefs(context.Background(), []uint{with.ID, without.ID}, 0)
	if err != nil {
		t.Fatal(err)
	}
	avatars := map[uint]string{}
	for _, b := range got.Users {
		avatars[b.ID] = b.Avatar
	}
	if want := "https://img.example/ab/cd/" + hash + ".webp"; avatars[with.ID] != want {
		t.Fatalf("avatar = %q, want %q", avatars[with.ID], want)
	}
	if avatars[without.ID] != "" {
		t.Fatalf("no hash, no avatar: %q", avatars[without.ID])
	}
}
