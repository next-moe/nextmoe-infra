package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"api/internal/platform/chat/dto"
	"api/internal/platform/chat/model"
)

func TestEnsureDirectIsOnePerPair(t *testing.T) {
	r := newRig(t, 1, 2)
	r.follow(2, 1)
	r.follow(1, 2)
	ctx := context.Background()

	first, err := r.svc.EnsureDirect(ctx, actor(1), 2)
	if err != nil {
		t.Fatal(err)
	}
	again, err := r.svc.EnsureDirect(ctx, actor(1), 2)
	if err != nil {
		t.Fatal(err)
	}
	reverse, err := r.svc.EnsureDirect(ctx, actor(2), 1)
	if err != nil {
		t.Fatal(err)
	}
	if first.Conversation.ID != again.Conversation.ID || first.Conversation.ID != reverse.Conversation.ID {
		t.Fatalf("ids differ: %s %s %s", first.Conversation.ID, again.Conversation.ID, reverse.Conversation.ID)
	}
	if *first.Conversation.PeerID != "2" || *reverse.Conversation.PeerID != "1" {
		t.Fatalf("peers: %v %v", *first.Conversation.PeerID, *reverse.Conversation.PeerID)
	}
	var n int64
	testDB.Model(&model.ChatConversation{}).Count(&n)
	if n != 1 {
		t.Fatalf("want one conversation, got %d", n)
	}
	if ups := updatesOf(t, 2); len(ups) != 0 {
		t.Fatalf("opening a conversation sends nothing, got %v", kinds(ups))
	}
	page, err := r.svc.ListConversations(ctx, actor(2), FolderInbox, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Conversations) != 0 {
		t.Fatal("a conversation with no message must stay out of the lists")
	}
}

func TestEnsureDirectRejects(t *testing.T) {
	r := newRig(t, 1, 2)
	ctx := context.Background()

	_, err := r.svc.EnsureDirect(ctx, actor(1), 1)
	wantInvalid(t, err)
	_, err = r.svc.EnsureDirect(ctx, actor(1), 99)
	wantErr(t, err, ErrNotFound)

	r.users.m[3] = Profile{ID: 3, Deleted: true, CreatedAt: old}
	_, err = r.svc.EnsureDirect(ctx, actor(1), 3)
	wantErr(t, err, ErrNotFound)

	r.block(2, 1)
	_, err = r.svc.EnsureDirect(ctx, actor(1), 2)
	wantErr(t, err, ErrBlocked)
}

func setSettings(t *testing.T, r *rig, uid int64, allow string, requests bool) {
	t.Helper()
	if _, err := r.svc.UpdateSettings(context.Background(), actor(uid), SettingsPatch{AllowIncoming: &allow, AcceptRequests: &requests}); err != nil {
		t.Fatal(err)
	}
}

func acceptedFor(t *testing.T, conv, uid int64) bool {
	t.Helper()
	return member(t, conv, uid).AcceptedAt != nil
}

func TestEnsureDirectAdmission(t *testing.T) {
	ctx := context.Background()

	t.Run("following sender goes to the inbox", func(t *testing.T) {
		r := newRig(t, 1, 2)
		conv := r.direct(t, 1, 2)
		if !acceptedFor(t, conv, 2) || !acceptedFor(t, conv, 1) {
			t.Fatal("both sides accepted")
		}
	})
	t.Run("a stranger lands in requests", func(t *testing.T) {
		r := newRig(t, 1, 2)
		res, err := r.svc.EnsureDirect(ctx, actor(1), 2)
		if err != nil {
			t.Fatal(err)
		}
		conv, _ := dto.ParseID(res.Conversation.ID)
		if acceptedFor(t, conv, 2) {
			t.Fatal("the recipient did not follow the sender: must be a request")
		}
		if !acceptedFor(t, conv, 1) {
			t.Fatal("the sender's own side is always accepted")
		}
	})
	t.Run("the sender following the recipient does not count", func(t *testing.T) {
		r := newRig(t, 1, 2)
		r.follow(1, 2)
		res, err := r.svc.EnsureDirect(ctx, actor(1), 2)
		if err != nil {
			t.Fatal(err)
		}
		conv, _ := dto.ParseID(res.Conversation.ID)
		if acceptedFor(t, conv, 2) {
			t.Fatal("only the recipient's follows admit a sender")
		}
	})
	t.Run("allow all", func(t *testing.T) {
		r := newRig(t, 1, 2)
		setSettings(t, r, 2, model.AllowAll, true)
		res, err := r.svc.EnsureDirect(ctx, actor(1), 2)
		if err != nil {
			t.Fatal(err)
		}
		conv, _ := dto.ParseID(res.Conversation.ID)
		if !acceptedFor(t, conv, 2) {
			t.Fatal("allow_incoming=all admits everyone")
		}
	})
	t.Run("no requests refuses strangers", func(t *testing.T) {
		r := newRig(t, 1, 2)
		setSettings(t, r, 2, model.AllowFollowing, false)
		_, err := r.svc.EnsureDirect(ctx, actor(1), 2)
		var na *NotAcceptingError
		if !errors.As(err, &na) {
			t.Fatalf("want NotAcceptingError, got %v", err)
		}
		r.follow(2, 1)
		if _, err := r.svc.EnsureDirect(ctx, actor(1), 2); err != nil {
			t.Fatalf("a followed sender still gets through: %v", err)
		}
	})
	t.Run("none sends everyone to requests", func(t *testing.T) {
		r := newRig(t, 1, 2)
		setSettings(t, r, 2, model.AllowNone, true)
		r.follow(2, 1)
		res, err := r.svc.EnsureDirect(ctx, actor(1), 2)
		if err != nil {
			t.Fatal(err)
		}
		conv, _ := dto.ParseID(res.Conversation.ID)
		if acceptedFor(t, conv, 2) {
			t.Fatal("allow_incoming=none admits nobody straight to the inbox")
		}
	})
	t.Run("a new account without trust cannot send requests", func(t *testing.T) {
		r := newRig(t, 1, 2)
		r.users.m[1] = Profile{ID: 1, Name: "new", CreatedAt: *r.clock}
		_, err := r.svc.EnsureDirect(ctx, actor(1), 2)
		var na *NotAcceptingError
		if !errors.As(err, &na) {
			t.Fatalf("want NotAcceptingError, got %v", err)
		}
		r.rel.levels[1] = 1
		if _, err := r.svc.EnsureDirect(ctx, actor(1), 2); err != nil {
			t.Fatalf("trust level 1 is enough: %v", err)
		}
	})
	t.Run("a new account can still message someone who follows it", func(t *testing.T) {
		r := newRig(t, 1, 2)
		r.users.m[1] = Profile{ID: 1, Name: "new", CreatedAt: *r.clock}
		r.follow(2, 1)
		if _, err := r.svc.EnsureDirect(ctx, actor(1), 2); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("requests per day are capped", func(t *testing.T) {
		r := newRig(t)
		r.users.m[1] = Profile{ID: 1, CreatedAt: old}
		for i := int64(0); i < requestsPerDay+1; i++ {
			r.users.m[100+i] = Profile{ID: 100 + i, CreatedAt: old}
		}
		for i := int64(0); i < requestsPerDay; i++ {
			if _, err := r.svc.EnsureDirect(ctx, actor(1), 100+i); err != nil {
				t.Fatalf("request %d: %v", i, err)
			}
		}
		_, err := r.svc.EnsureDirect(ctx, actor(1), 100+requestsPerDay)
		var rl *RateLimitError
		if !errors.As(err, &rl) {
			t.Fatalf("want RateLimitError, got %v", err)
		}
	})
}

func TestNewAccountAgeBoundary(t *testing.T) {
	r := newRig(t, 1, 2)
	r.users.m[1] = Profile{ID: 1, CreatedAt: r.clock.Add(-newAccountAge + time.Minute)}
	_, err := r.svc.EnsureDirect(context.Background(), actor(1), 2)
	var na *NotAcceptingError
	if !errors.As(err, &na) {
		t.Fatalf("one minute short of the age: want NotAcceptingError, got %v", err)
	}
	r.users.m[1] = Profile{ID: 1, CreatedAt: r.clock.Add(-newAccountAge)}
	if _, err := r.svc.EnsureDirect(context.Background(), actor(1), 2); err != nil {
		t.Fatalf("at the age: %v", err)
	}
}
