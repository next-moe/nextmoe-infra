package service

import (
	"context"
	"errors"
	"testing"

	"api/internal/platform/community/model"
)

func TestFollowIsIdempotent(t *testing.T) {
	cleanTables(t)
	s := NewFollowService(testDB)
	ctx := context.Background()

	created, err := s.Follow(ctx, "kungal", 1, 2)
	if err != nil {
		t.Fatalf("first follow: %v", err)
	}
	if !created {
		t.Fatal("first follow must create")
	}
	created, err = s.Follow(ctx, "moyu", 1, 2)
	if err != nil {
		t.Fatalf("second follow: %v", err)
	}
	if created {
		t.Fatal("second follow must not create")
	}

	var n int64
	if err := testDB.Model(&model.CommunityUserFollow{}).Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("want 1 row, got %d", n)
	}
	var row model.CommunityUserFollow
	if err := testDB.Where("follower_id = ? AND followee_id = ?", 1, 2).Take(&row).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if row.OriginSite != "kungal" {
		t.Fatalf("origin_site=%q, want kungal", row.OriginSite)
	}
	if row.CreatedAt == nil {
		t.Fatal("created_at must be set")
	}
	if row.ImportedAt != nil {
		t.Fatal("imported_at must be null")
	}
}

func TestFollowRejectsSelfAndNonPositiveIDs(t *testing.T) {
	cleanTables(t)
	s := NewFollowService(testDB)
	ctx := context.Background()

	_, err := s.Follow(ctx, "kungal", 1, 1)
	wantInvalid(t, err, "cannot follow yourself")

	for _, pair := range [][2]int64{{0, 1}, {1, 0}, {-1, 2}, {2, -1}} {
		_, err := s.Follow(ctx, "kungal", pair[0], pair[1])
		wantInvalid(t, err, "user ids must be positive")
	}
}

func TestFollowCapRejectsNewButNotRepeat(t *testing.T) {
	cleanTables(t)
	s := NewFollowService(testDB)
	ctx := context.Background()

	if err := testDB.Exec(`
		INSERT INTO community_user_follow (follower_id, followee_id, origin_site, created_at)
		SELECT 1, generate_series(2, 5001), 'kungal', now()`).Error; err != nil {
		t.Fatalf("seed 5000 edges: %v", err)
	}

	_, err := s.Follow(ctx, "kungal", 1, 5002)
	wantInvalid(t, err, "following limit reached (max 5000)")
	var n int64
	if err := testDB.Model(&model.CommunityUserFollow{}).Where("follower_id = ?", 1).Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 5000 {
		t.Fatalf("want 5000 rows after rejected follow, got %d", n)
	}

	created, err := s.Follow(ctx, "moyu", 1, 2)
	if err != nil {
		t.Fatalf("repeat at cap: %v", err)
	}
	if created {
		t.Fatal("repeating an existing edge must not create")
	}
}

func TestUnfollowIsIdempotent(t *testing.T) {
	cleanTables(t)
	s := NewFollowService(testDB)
	ctx := context.Background()

	if _, err := s.Follow(ctx, "kungal", 1, 2); err != nil {
		t.Fatalf("follow: %v", err)
	}
	deleted, err := s.Unfollow(ctx, 1, 2)
	if err != nil {
		t.Fatalf("first unfollow: %v", err)
	}
	if !deleted {
		t.Fatal("first unfollow must delete")
	}
	deleted, err = s.Unfollow(ctx, 1, 2)
	if err != nil {
		t.Fatalf("second unfollow: %v", err)
	}
	if deleted {
		t.Fatal("second unfollow must not delete")
	}
	deleted, err = s.Unfollow(ctx, 3, 4)
	if err != nil {
		t.Fatalf("missing unfollow: %v", err)
	}
	if deleted {
		t.Fatal("unfollow of a missing edge must not delete")
	}

	_, err = s.Unfollow(ctx, 0, 1)
	wantInvalid(t, err, "user ids must be positive")
}

func TestFollowListsNewestFirstWithCursor(t *testing.T) {
	cleanTables(t)
	s := NewFollowService(testDB)
	ctx := context.Background()

	if _, err := s.Follow(ctx, "kungal", 1, 2); err != nil {
		t.Fatalf("follow 2: %v", err)
	}
	if _, err := s.Follow(ctx, "kungal", 1, 3); err != nil {
		t.Fatalf("follow 3: %v", err)
	}
	if err := testDB.Exec(`
		INSERT INTO community_user_follow (follower_id, followee_id, origin_site, created_at)
		VALUES (1, 4, 'kungal', NULL)`).Error; err != nil {
		t.Fatalf("null created_at following: %v", err)
	}
	if _, err := s.Follow(ctx, "kungal", 5, 1); err != nil {
		t.Fatalf("follower 5: %v", err)
	}
	if _, err := s.Follow(ctx, "kungal", 6, 1); err != nil {
		t.Fatalf("follower 6: %v", err)
	}
	if err := testDB.Exec(`
		INSERT INTO community_user_follow (follower_id, followee_id, origin_site, created_at)
		VALUES (7, 1, 'kungal', NULL)`).Error; err != nil {
		t.Fatalf("null created_at follower: %v", err)
	}

	following, err := s.ListFollowing(1, 0, 2)
	if err != nil {
		t.Fatalf("list following page 1: %v", err)
	}
	if len(following) != 2 || following[0].FolloweeID != 4 || following[1].FolloweeID != 3 {
		t.Fatalf("following page 1: %+v", following)
	}
	if following[0].CreatedAt != nil {
		t.Fatal("null created_at row must be listed with a nil CreatedAt")
	}
	page2, err := s.ListFollowing(1, following[1].ID, 2)
	if err != nil {
		t.Fatalf("list following page 2: %v", err)
	}
	if len(page2) != 1 || page2[0].FolloweeID != 2 {
		t.Fatalf("following page 2: %+v", page2)
	}
	page3, err := s.ListFollowing(1, page2[0].ID, 2)
	if err != nil {
		t.Fatalf("list following page 3: %v", err)
	}
	if len(page3) != 0 {
		t.Fatalf("last following page must be empty, got %+v", page3)
	}

	followers, err := s.ListFollowers(1, 0, 2)
	if err != nil {
		t.Fatalf("list followers page 1: %v", err)
	}
	if len(followers) != 2 || followers[0].FollowerID != 7 || followers[1].FollowerID != 6 {
		t.Fatalf("followers page 1: %+v", followers)
	}
	if followers[0].CreatedAt != nil {
		t.Fatal("null created_at follower must be listed with a nil CreatedAt")
	}
	fpage2, err := s.ListFollowers(1, followers[1].ID, 2)
	if err != nil {
		t.Fatalf("list followers page 2: %v", err)
	}
	if len(fpage2) != 1 || fpage2[0].FollowerID != 5 {
		t.Fatalf("followers page 2: %+v", fpage2)
	}
	fpage3, err := s.ListFollowers(1, fpage2[0].ID, 2)
	if err != nil {
		t.Fatalf("list followers page 3: %v", err)
	}
	if len(fpage3) != 0 {
		t.Fatalf("last followers page must be empty, got %+v", fpage3)
	}
}

func TestFollowStates(t *testing.T) {
	cleanTables(t)
	s := NewFollowService(testDB)
	ctx := context.Background()
	mustFollow := func(follower, followee int64) {
		t.Helper()
		if _, err := s.Follow(ctx, "kungal", follower, followee); err != nil {
			t.Fatalf("follow %d→%d: %v", follower, followee, err)
		}
	}
	mustFollow(1, 2)
	mustFollow(1, 3)
	mustFollow(2, 1)
	mustFollow(4, 2)
	if err := s.SetNotifyLevel(ctx, 1, 3, model.FollowNotifyFeed); err != nil {
		t.Fatalf("feed-only 1→3: %v", err)
	}

	states, err := s.States(1, []int64{2, 5, 3, 2, 1})
	if err != nil {
		t.Fatalf("states: %v", err)
	}
	if len(states) != 4 {
		t.Fatalf("want 4 distinct states, got %d", len(states))
	}
	want := []FollowState{
		{UserID: 2, FollowersCount: 2, FollowingCount: 1, ViewerFollows: true, FollowsViewer: true},
		{UserID: 5, FollowersCount: 0, FollowingCount: 0},
		{UserID: 3, FollowersCount: 1, FollowingCount: 0, ViewerFollows: true},
		{UserID: 1, FollowersCount: 1, FollowingCount: 2},
	}
	wantNotify := []*int16{new(model.FollowNotifyAll), nil, new(model.FollowNotifyFeed), nil}
	for i, st := range states {
		gotNotify := st.ViewerNotify
		st.ViewerNotify = nil
		if st != want[i] {
			t.Errorf("state[%d]=%+v, want %+v", i, st, want[i])
		}
		if (gotNotify == nil) != (wantNotify[i] == nil) || (gotNotify != nil && *gotNotify != *wantNotify[i]) {
			t.Errorf("state[%d] viewer_notify=%v, want %v", i, gotNotify, wantNotify[i])
		}
	}

	anon, err := s.States(0, []int64{2})
	if err != nil {
		t.Fatalf("anonymous: %v", err)
	}
	if len(anon) != 1 || anon[0].UserID != 2 || anon[0].FollowersCount != 2 || anon[0].FollowingCount != 1 ||
		anon[0].ViewerFollows || anon[0].FollowsViewer {
		t.Fatalf("anonymous state: %+v", anon)
	}

	empty, err := s.States(1, nil)
	if err != nil {
		t.Fatalf("empty: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("empty userIDs must return empty, got %+v", empty)
	}

	_, err = s.States(1, make([]int64, 101))
	wantInvalid(t, err, "too many user_ids (max 100)")
	ids101 := make([]int64, 101)
	for i := range ids101 {
		ids101[i] = int64(i + 1)
	}
	_, err = s.States(1, ids101)
	wantInvalid(t, err, "too many user_ids (max 100)")

	_, err = s.States(-1, []int64{1})
	wantInvalid(t, err, "user ids must be positive")
	_, err = s.States(1, []int64{1, 0})
	wantInvalid(t, err, "user ids must be positive")
}

func wantInvalid(t *testing.T, err error, reason string) {
	t.Helper()
	var inv *InvalidError
	if !errors.As(err, &inv) {
		t.Fatalf("want InvalidError %q, got %v", reason, err)
	}
	if inv.Reason != reason {
		t.Fatalf("want reason %q, got %q", reason, inv.Reason)
	}
}
