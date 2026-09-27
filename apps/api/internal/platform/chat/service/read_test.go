package service

import (
	"context"
	"testing"
	"time"

	"api/internal/platform/chat/dto"
	"api/internal/platform/chat/model"
)

func TestListConversationsFolders(t *testing.T) {
	r := newRig(t, 1, 2, 3, 4)
	ctx := context.Background()
	inbox := r.direct(t, 1, 2)
	archived := r.direct(t, 1, 3)
	r.send(t, 2, inbox, "hi")
	r.send(t, 3, archived, "hi")
	if _, err := r.svc.UpdateDialog(ctx, actor(1), archived, DialogPatch{Archived: ptr(true), Muted: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	res, _ := r.svc.EnsureDirect(ctx, actor(4), 1)
	request, _ := dto.ParseID(res.Conversation.ID)
	r.send(t, 4, request, "can we talk")

	for folder, want := range map[string]int64{FolderInbox: inbox, FolderArchive: archived, FolderRequests: request} {
		page, err := r.svc.ListConversations(ctx, actor(1), folder, "", 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Conversations) != 1 || page.Conversations[0].ID != dto.ID(want) {
			t.Fatalf("%s: %+v", folder, page.Conversations)
		}
		c := page.Conversations[0]
		if c.LastMessage == nil || c.PeerID == nil {
			t.Fatalf("%s: summary lacks last message or peer", folder)
		}
		if len(page.Users) == 0 {
			t.Fatalf("%s: users must come with the page", folder)
		}
	}
	_, err := r.svc.ListConversations(ctx, actor(1), "spam", "", 10)
	wantInvalid(t, err)
}

func TestListConversationsCursor(t *testing.T) {
	r := newRig(t, 1)
	ctx := context.Background()
	var convs []int64
	for peer := int64(2); peer <= 6; peer++ {
		r.users.m[peer] = Profile{ID: peer, CreatedAt: old}
		c := r.direct(t, 1, peer)
		r.send(t, peer, c, "hi")
		r.advance(time.Second)
		convs = append(convs, c)
	}
	var seen []string
	cursor := ""
	for i := 0; i < 5; i++ {
		page, err := r.svc.ListConversations(ctx, actor(1), FolderInbox, cursor, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range page.Conversations {
			seen = append(seen, c.ID)
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	want := []string{dto.ID(convs[4]), dto.ID(convs[3]), dto.ID(convs[2]), dto.ID(convs[1]), dto.ID(convs[0])}
	if len(seen) != 5 {
		t.Fatalf("pages: %v", seen)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("order: got %v want %v", seen, want)
		}
	}
	_, err := r.svc.ListConversations(ctx, actor(1), FolderInbox, "garbage", 2)
	wantInvalid(t, err)
}

func TestMessagesPaging(t *testing.T) {
	r := newRig(t, 1, 2)
	conv := r.direct(t, 1, 2)
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		r.send(t, 1, conv, "m")
	}
	seqs := func(p *MessagePage) []int64 {
		out := make([]int64, len(p.Messages))
		for i, m := range p.Messages {
			out[i] = m.Seq
		}
		return out
	}
	latest, _ := r.svc.Messages(ctx, actor(2), conv, MessageQuery{Limit: 3})
	if got := seqs(latest); len(got) != 3 || got[0] != 8 || got[2] != 10 || !latest.HasMoreBefore {
		t.Fatalf("latest: %v more_before=%v", got, latest.HasMoreBefore)
	}
	before, _ := r.svc.Messages(ctx, actor(2), conv, MessageQuery{BeforeSeq: ptr(int64(8)), Limit: 3})
	if got := seqs(before); got[0] != 5 || got[2] != 7 {
		t.Fatalf("before: %v", got)
	}
	after, _ := r.svc.Messages(ctx, actor(2), conv, MessageQuery{AfterSeq: ptr(int64(8)), Limit: 5})
	if got := seqs(after); len(got) != 2 || got[0] != 9 || after.HasMoreAfter {
		t.Fatalf("after: %v more_after=%v", got, after.HasMoreAfter)
	}
	around, _ := r.svc.Messages(ctx, actor(2), conv, MessageQuery{AroundSeq: ptr(int64(5)), Limit: 4})
	if got := seqs(around); len(got) != 4 || got[0] != 3 || got[3] != 6 {
		t.Fatalf("around: %v", got)
	}
	_, err := r.svc.Messages(ctx, actor(2), conv, MessageQuery{BeforeSeq: ptr(int64(3)), AfterSeq: ptr(int64(1))})
	wantInvalid(t, err)
}

func TestUpdatesStream(t *testing.T) {
	r := newRig(t, 1, 2)
	conv := r.direct(t, 1, 2)
	ctx := context.Background()
	r.send(t, 1, conv, "a")
	m := r.send(t, 1, conv, "b")
	if _, err := r.svc.React(ctx, actor(2), mustID(t, m.ID), ptr("heart")); err != nil {
		t.Fatal(err)
	}

	page, err := r.svc.Updates(ctx, actor(2), 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if page.TooLong || len(page.Updates) != 2 || page.Updates[0].UpdateSeq != 2 || page.LastUpdateSeq != 3 {
		t.Fatalf("updates after 1: %+v", page)
	}
	if len(page.Messages) != 1 || page.Messages[0].Text != "b" {
		t.Fatalf("the new_message update brings its message: %+v", page.Messages)
	}
	if len(page.Conversations) != 1 || len(page.Users) == 0 {
		t.Fatal("conversations and users come along")
	}
	page, _ = r.svc.Updates(ctx, actor(2), 1, 1)
	if len(page.Updates) != 1 || !page.HasMore {
		t.Fatalf("limit 1: %+v", page)
	}
	page, _ = r.svc.Updates(ctx, actor(2), 3, 10)
	if len(page.Updates) != 0 || page.HasMore || page.TooLong {
		t.Fatalf("caught up: %+v", page)
	}

	if _, err := r.svc.DeleteMessages(ctx, actor(1), conv, []int64{m.Seq}, true); err != nil {
		t.Fatal(err)
	}
	page, _ = r.svc.Updates(ctx, actor(2), 1, 10)
	if len(page.Messages) != 0 {
		t.Fatal("a message deleted since is not hydrated; its delete update follows")
	}

	r.advance(updateRetention + time.Hour)
	r.send(t, 1, conv, "fresh")
	if n, err := r.svc.PruneUpdates(ctx); err != nil || n == 0 {
		t.Fatalf("prune: %d %v", n, err)
	}
	page, _ = r.svc.Updates(ctx, actor(2), 1, 10)
	if !page.TooLong {
		t.Fatalf("a position before the kept stream is too_long: %+v", page)
	}
	last := updatesOf(t, 2)
	page, _ = r.svc.Updates(ctx, actor(2), last[0].UpdateSeq-1, 10)
	if page.TooLong || len(page.Updates) != 1 {
		t.Fatalf("from just before the oldest kept row: %+v", page)
	}
	_, err = r.svc.Updates(ctx, actor(2), -1, 10)
	wantInvalid(t, err)
}

func TestSettingsDefaultsAndPatch(t *testing.T) {
	r := newRig(t, 1)
	ctx := context.Background()
	st, err := r.svc.Settings(ctx, actor(1))
	if err != nil {
		t.Fatal(err)
	}
	if st.AllowIncoming != model.AllowFollowing || !st.AcceptRequests || st.AllowGroupInvites != model.AllowFollowing {
		t.Fatalf("defaults: %+v", st)
	}
	st, err = r.svc.UpdateSettings(ctx, actor(1), SettingsPatch{AcceptRequests: ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	if st.AcceptRequests || st.AllowIncoming != model.AllowFollowing {
		t.Fatalf("a patch touches only what it names: %+v", st)
	}
	_, err = r.svc.UpdateSettings(ctx, actor(1), SettingsPatch{AllowIncoming: ptr("friends")})
	wantInvalid(t, err)
}
