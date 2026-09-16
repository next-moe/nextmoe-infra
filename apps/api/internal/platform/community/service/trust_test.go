package service

import (
	"context"
	"testing"

	"api/internal/platform/community/model"
)

func TestPromotion_Levels(t *testing.T) {
	cleanTables(t)
	ts := NewTrustService(testDB)
	ctx := context.Background()
	u := int64(1)

	tr, err := ts.RecordActivity(ctx, ActivityReceipt{UserID: u, TopicsEntered: 5, PostsRead: 30, ReadTimeS: 600})
	if err != nil {
		t.Fatalf("receipt 1: %v", err)
	}
	if tr.Level != model.TrustLevelBasic {
		t.Fatalf("TL1 not reached: level=%d", tr.Level)
	}

	setLikes(t, u, 1, 1)
	tr, err = ts.RecordActivity(ctx, ActivityReceipt{UserID: u, PostsRead: 70, DaysVisited: 15})
	if err != nil {
		t.Fatalf("receipt 2: %v", err)
	}
	if tr.Level != model.TrustLevelMember {
		t.Fatalf("TL2 not reached: level=%d", tr.Level)
	}

	win := int32(60)
	tr, _ = ts.RecordActivity(ctx, ActivityReceipt{UserID: u, WindowActiveDays: &win})
	if tr.Level != model.TrustLevelRegular {
		t.Fatalf("TL3 not reached: level=%d", tr.Level)
	}

	low := int32(30)
	tr, _ = ts.RecordActivity(ctx, ActivityReceipt{UserID: u, WindowActiveDays: &low})
	if tr.Level != model.TrustLevelMember {
		t.Fatalf("TL3 should demote to TL2 on a low window: level=%d", tr.Level)
	}

	tr, _ = ts.RecordActivity(ctx, ActivityReceipt{UserID: u})
	if tr.Level != model.TrustLevelMember {
		t.Fatalf("empty receipt changed the level: %d", tr.Level)
	}
}

func TestStarterBoost(t *testing.T) {
	cleanTables(t)
	ts := NewTrustService(testDB)
	ctx := context.Background()

	tr, err := ts.SetBoost(ctx, 10, model.GrantedBoostCreator)
	if err != nil {
		t.Fatalf("boost creator: %v", err)
	}
	if tr.Level != model.TrustLevelBasic || tr.GrantedBoost == nil || *tr.GrantedBoost != model.GrantedBoostCreator {
		t.Fatalf("creator boost: level=%d boost=%v", tr.Level, tr.GrantedBoost)
	}

	tr, _ = ts.SetBoost(ctx, 11, model.GrantedBoostStaff)
	if tr.Level != model.TrustLevelRegular {
		t.Fatalf("staff boost should floor at TL3: level=%d", tr.Level)
	}

	seedTrust(t, 12, model.TrustLevelMember, 0)
	tr, _ = ts.SetBoost(ctx, 12, model.GrantedBoostVeteran)
	if tr.Level != model.TrustLevelMember {
		t.Fatalf("boost must not demote a TL2 user: level=%d", tr.Level)
	}

	ts.SetBoost(ctx, 13, model.GrantedBoostStaff)
	low := int32(5)
	tr, _ = ts.RecordActivity(ctx, ActivityReceipt{UserID: 13, WindowActiveDays: &low})
	if tr.Level != model.TrustLevelRegular {
		t.Fatalf("staff-boosted TL3 must not demote: level=%d", tr.Level)
	}
}

func TestStaffBoost_ClearsFirstPostHolds(t *testing.T) {
	cleanTables(t)
	ts := NewTrustService(testDB)
	ctx := context.Background()

	tr, err := ts.SetBoost(ctx, 20, model.GrantedBoostStaff)
	if err != nil {
		t.Fatalf("staff boost: %v", err)
	}
	if tr.FirstPostsHeldRemaining != 0 {
		t.Fatalf("staff boost must zero the hold budget: got %d", tr.FirstPostsHeldRemaining)
	}

	threads := NewThreadService(testDB, NoopSink{})
	_, opening, err := threads.OpenTopic(ctx, OpenTopicParams{
		Site: "letmoe", AuthorID: 20, BoardID: testBoard(t, "letmoe", "b1"),
		Title: "staff first topic", ContentRating: model.ContentRatingAll, BodyRaw: "hello",
	})
	if err != nil {
		t.Fatalf("staff first topic: %v", err)
	}
	if opening.Status != model.PostStatusVisible {
		t.Fatalf("a boosted staffer's first post must be visible, got status=%d", opening.Status)
	}

	seedTrust(t, 21, model.TrustLevelNew, 2)
	tr, err = ts.SetBoost(ctx, 21, model.GrantedBoostCreator)
	if err != nil {
		t.Fatalf("creator boost: %v", err)
	}
	if tr.FirstPostsHeldRemaining != 2 {
		t.Fatalf("creator boost must keep the hold budget: got %d", tr.FirstPostsHeldRemaining)
	}
}
