package service

import (
	"context"
	"testing"
	"time"

	"api/internal/platform/chat/content"
	"api/internal/platform/chat/dto"
	"api/internal/platform/chat/model"
)

func TestDeleteForEveryoneErasesAndRecounts(t *testing.T) {
	r := newRig(t, 1, 2)
	conv := r.direct(t, 1, 2)
	ctx := context.Background()
	m1 := r.send(t, 1, conv, "keep")
	m2 := r.send(t, 1, conv, "regret")
	id2, _ := dto.ParseID(m2.ID)
	if _, err := r.svc.React(ctx, actor(2), id2, ptr("heart")); err != nil {
		t.Fatal(err)
	}
	if err := r.svc.SetPinned(ctx, actor(1), conv, m2.Seq, true); err != nil {
		t.Fatal(err)
	}
	before := member(t, conv, 2).UnreadCount

	done, err := r.svc.DeleteMessages(ctx, actor(1), conv, []int64{m2.Seq, 999}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(done) != 1 || done[0] != m2.Seq {
		t.Fatalf("deleted %v", done)
	}
	var row model.ChatMessage
	testDB.Take(&row, id2)
	if row.DeletedAt == nil || row.Text != "" || row.PinnedAt != nil {
		t.Fatalf("tombstone: %+v", row)
	}
	var reactions int64
	testDB.Model(&model.ChatReaction{}).Where("message_id = ?", id2).Count(&reactions)
	if reactions != 0 {
		t.Fatal("a deleted message keeps no reactions")
	}
	if after := member(t, conv, 2).UnreadCount; after != before-1 {
		t.Fatalf("recipient unread %d -> %d", before, after)
	}
	for _, uid := range []int64{1, 2} {
		ups := updatesOf(t, uid)
		last := ups[len(ups)-1]
		if last.Kind != model.UpdateDeleteMessages {
			t.Fatalf("user %d: %v", uid, kinds(ups))
		}
	}
	_, err = r.svc.DeleteMessages(ctx, actor(2), conv, []int64{m1.Seq}, true)
	wantErr(t, err, ErrNotPermitted)
	_, err = r.svc.DeleteMessages(ctx, actor(1), conv, nil, true)
	wantInvalid(t, err)

	if err := testDB.Exec(`UPDATE chat_message SET text = 'resurrected' WHERE id = ?`, id2).Error; err == nil {
		t.Fatal("the schema must refuse content on a deleted message")
	}
}

func TestHideIsForTheCallerAlone(t *testing.T) {
	r := newRig(t, 1, 2)
	conv := r.direct(t, 1, 2)
	ctx := context.Background()
	r.send(t, 1, conv, "a")
	b := r.send(t, 1, conv, "b")
	done, err := r.svc.DeleteMessages(ctx, actor(2), conv, []int64{b.Seq}, false)
	if err != nil || len(done) != 1 {
		t.Fatalf("hide: %v %v", done, err)
	}
	if u := member(t, conv, 2).UnreadCount; u != 1 {
		t.Fatalf("hiding an unread message drops it from unread, got %d", u)
	}
	mine, _ := r.svc.Messages(ctx, actor(2), conv, MessageQuery{})
	theirs, _ := r.svc.Messages(ctx, actor(1), conv, MessageQuery{})
	if len(mine.Messages) != 1 || len(theirs.Messages) != 2 {
		t.Fatalf("hidden for 2 only: 2 sees %d, 1 sees %d", len(mine.Messages), len(theirs.Messages))
	}
	if kinds(updatesOf(t, 1))[len(updatesOf(t, 1))-1] == model.UpdateHideMessages {
		t.Fatal("the other side hears nothing of a hide")
	}
}

func TestReactReplacesAndRemoves(t *testing.T) {
	r := newRig(t, 1, 2, 3)
	conv := r.direct(t, 1, 2)
	ctx := context.Background()
	m := r.send(t, 1, conv, "react to me")
	id, _ := dto.ParseID(m.ID)

	if _, err := r.svc.React(ctx, actor(2), id, ptr("heart")); err != nil {
		t.Fatal(err)
	}
	rs, err := r.svc.React(ctx, actor(2), id, ptr("fire"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 1 || rs[0].Reaction != "fire" || rs[0].Count != 1 || !rs[0].Reacted {
		t.Fatalf("one reaction per person, replaced: %+v", rs)
	}
	rs, err = r.svc.React(ctx, actor(1), id, ptr("fire"))
	if err != nil {
		t.Fatal(err)
	}
	if rs[0].Count != 2 {
		t.Fatalf("counts: %+v", rs)
	}
	upsBefore := len(updatesOf(t, 1))
	if _, err := r.svc.React(ctx, actor(1), id, ptr("fire")); err != nil {
		t.Fatal(err)
	}
	if len(updatesOf(t, 1)) != upsBefore {
		t.Fatal("setting the same reaction again writes nothing")
	}
	rs, err = r.svc.React(ctx, actor(2), id, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 1 || rs[0].Count != 1 || rs[0].Reacted {
		t.Fatalf("after removing: %+v", rs)
	}
	_, err = r.svc.React(ctx, actor(2), id, ptr("not-a-reaction"))
	wantInvalid(t, err)
	_, err = r.svc.React(ctx, actor(3), id, ptr("fire"))
	wantErr(t, err, ErrNotFound)
}

func TestReactionPushCarriesEachViewersFlag(t *testing.T) {
	r := newRig(t, 1, 2)
	conv := r.direct(t, 1, 2)
	m := r.send(t, 1, conv, "x")
	id, _ := dto.ParseID(m.ID)
	r.pub.take()
	if _, err := r.svc.React(context.Background(), actor(2), id, ptr("heart")); err != nil {
		t.Fatal(err)
	}
	for _, d := range r.pub.take() {
		e := d.Data.(*Event)
		if len(e.Reactions) != 1 || e.Reactions[0].Reacted != (d.UserID == 2) {
			t.Fatalf("push to %d: %+v", d.UserID, e.Reactions)
		}
	}
}

func TestReadMovesForwardAndTellsTheSender(t *testing.T) {
	r := newRig(t, 1, 2)
	conv := r.direct(t, 1, 2)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		r.send(t, 1, conv, "m")
	}
	res, err := r.svc.Read(ctx, actor(2), conv, 2)
	if err != nil {
		t.Fatal(err)
	}
	if res.LastReadSeq != 2 || res.UnreadCount != 1 {
		t.Fatalf("read: %+v", res)
	}
	senderUps := updatesOf(t, 1)
	last := senderUps[len(senderUps)-1]
	if last.Kind != model.UpdateReadOutbox || decodeData(t, last)["max_seq"] != float64(2) {
		t.Fatalf("the sender must hear read_outbox 2, got %v", kinds(senderUps))
	}
	res, _ = r.svc.Read(ctx, actor(2), conv, 1)
	if res.LastReadSeq != 2 {
		t.Fatal("a read position never moves back")
	}
	res, _ = r.svc.Read(ctx, actor(2), conv, 999)
	if res.LastReadSeq != 3 || res.UnreadCount != 0 {
		t.Fatalf("clamped to the last message: %+v", res)
	}
	view, _ := r.svc.Conversation(ctx, actor(1), conv)
	if view.Conversation.PeerReadSeq != 3 {
		t.Fatalf("peer_read_seq = %d", view.Conversation.PeerReadSeq)
	}
}

func TestReadingARequestTellsTheRequesterNothing(t *testing.T) {
	r := newRig(t, 1, 2)
	ctx := context.Background()
	res, _ := r.svc.EnsureDirect(ctx, actor(1), 2)
	conv, _ := dto.ParseID(res.Conversation.ID)
	r.send(t, 1, conv, "hello stranger")
	before := len(updatesOf(t, 1))
	if _, err := r.svc.Read(ctx, actor(2), conv, 1); err != nil {
		t.Fatal(err)
	}
	if len(updatesOf(t, 1)) != before {
		t.Fatal("no read_outbox while the request is pending")
	}
	view, _ := r.svc.Conversation(ctx, actor(1), conv)
	if view.Conversation.PeerReadSeq != 0 {
		t.Fatalf("the requester sees 0, got %d", view.Conversation.PeerReadSeq)
	}
	if err := r.svc.Accept(ctx, actor(2), conv); err != nil {
		t.Fatal(err)
	}
	ups := updatesOf(t, 1)
	if ups[len(ups)-1].Kind != model.UpdateReadOutbox {
		t.Fatalf("accepting reveals the read position: %v", kinds(ups))
	}
	view, _ = r.svc.Conversation(ctx, actor(1), conv)
	if view.Conversation.PeerReadSeq != 1 {
		t.Fatalf("after accept peer_read_seq = %d", view.Conversation.PeerReadSeq)
	}
}

func TestDialogSettings(t *testing.T) {
	r := newRig(t)
	for i := int64(1); i <= 7; i++ {
		r.users.m[i] = Profile{ID: i, CreatedAt: old}
	}
	ctx := context.Background()
	var convs []int64
	for peer := int64(2); peer <= 7; peer++ {
		c := r.direct(t, 1, peer)
		r.send(t, peer, c, "hi")
		convs = append(convs, c)
	}
	for i := 0; i < maxPinnedDialogs; i++ {
		st, err := r.svc.UpdateDialog(ctx, actor(1), convs[i], DialogPatch{Pinned: ptr(true)})
		if err != nil {
			t.Fatal(err)
		}
		if st.PinnedRank == nil || *st.PinnedRank != int16(i+1) {
			t.Fatalf("pin %d rank %v", i, st.PinnedRank)
		}
	}
	_, err := r.svc.UpdateDialog(ctx, actor(1), convs[5], DialogPatch{Pinned: ptr(true)})
	wantInvalid(t, err)
	page, _ := r.svc.ListConversations(ctx, actor(1), FolderInbox, "", 50)
	if len(page.Conversations) != 6 || page.Conversations[0].ID != dto.ID(convs[4]) {
		t.Fatalf("the newest pin comes first, then the rest: first=%s n=%d", page.Conversations[0].ID, len(page.Conversations))
	}

	st, err := r.svc.UpdateDialog(ctx, actor(1), convs[0], DialogPatch{Muted: ptr(true)})
	if err != nil {
		t.Fatal(err)
	}
	if st.MutedUntil == nil || !st.MutedUntil.Equal(MutedForever) {
		t.Fatalf("muted without a date = forever, got %v", st.MutedUntil)
	}
	until := r.clock.Add(time.Hour)
	st, _ = r.svc.UpdateDialog(ctx, actor(1), convs[0], DialogPatch{Muted: ptr(true), MutedUntil: &until})
	if st.MutedUntil == nil || !st.MutedUntil.Equal(until) {
		t.Fatalf("muted_until = %v", st.MutedUntil)
	}
	_, err = r.svc.UpdateDialog(ctx, actor(1), convs[0], DialogPatch{Muted: ptr(true), MutedUntil: ptr(r.clock.Add(-time.Minute))})
	wantInvalid(t, err)
	st, _ = r.svc.UpdateDialog(ctx, actor(1), convs[0], DialogPatch{Muted: ptr(false), MarkedUnread: ptr(true)})
	if st.MutedUntil != nil || !st.MarkedUnread {
		t.Fatalf("unmute + mark unread: %+v", st)
	}
	state, _ := r.svc.State(ctx, actor(1))
	if state.UnreadConversationCount != 6 {
		t.Fatalf("a manual unread mark counts as unread: %+v", state)
	}
	if _, err := r.svc.Read(ctx, actor(1), convs[0], 0); err != nil {
		t.Fatal(err)
	}
	if member(t, convs[0], 1).MarkedUnread {
		t.Fatal("reading clears the manual mark")
	}
}

func TestMutedConversationsLeaveTheBadge(t *testing.T) {
	r := newRig(t, 1, 2, 3)
	ctx := context.Background()
	c2 := r.direct(t, 1, 2)
	c3 := r.direct(t, 1, 3)
	r.send(t, 2, c2, "a")
	r.send(t, 3, c3, "b")
	r.send(t, 3, c3, "c")
	if _, err := r.svc.UpdateDialog(ctx, actor(1), c3, DialogPatch{Muted: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	st, _ := r.svc.State(ctx, actor(1))
	if st.UnreadConversationCount != 1 || st.UnreadMessageCount != 1 {
		t.Fatalf("state: %+v", st)
	}
}

func TestDraftSyncsAndClears(t *testing.T) {
	r := newRig(t, 1, 2)
	conv := r.direct(t, 1, 2)
	ctx := context.Background()
	d, err := r.svc.SaveDraft(ctx, actor(1), conv, "half a thought", []content.Entity{bold(0, 4)}, nil)
	if err != nil || d == nil || d.Text != "half a thought" || len(d.Entities) != 1 {
		t.Fatalf("draft: %+v %v", d, err)
	}
	view, _ := r.svc.Conversation(ctx, actor(1), conv)
	if view.Conversation.Me.Draft == nil || view.Conversation.Me.Draft.Text != "half a thought" {
		t.Fatal("the draft is part of the caller's dialog")
	}
	d, err = r.svc.SaveDraft(ctx, actor(1), conv, "  ", nil, nil)
	if err != nil || d != nil {
		t.Fatalf("an empty draft clears: %+v %v", d, err)
	}
	if member(t, conv, 1).Draft != nil {
		t.Fatal("cleared draft must be NULL")
	}
	ups := updatesOf(t, 1)
	if ups[len(ups)-1].Kind != model.UpdateDialog {
		t.Fatal("the caller's other devices hear of the draft")
	}
	if len(updatesOf(t, 2)) != 0 {
		t.Fatal("the peer never hears of a draft")
	}
}

func TestClearHistoryAndRemove(t *testing.T) {
	r := newRig(t, 1, 2)
	conv := r.direct(t, 1, 2)
	ctx := context.Background()
	r.send(t, 1, conv, "a")
	r.send(t, 1, conv, "b")
	if err := r.svc.ClearHistory(ctx, actor(2), conv, false); err != nil {
		t.Fatal(err)
	}
	page, _ := r.svc.Messages(ctx, actor(2), conv, MessageQuery{})
	if len(page.Messages) != 0 || member(t, conv, 2).UnreadCount != 0 {
		t.Fatal("cleared history is gone for the caller")
	}
	other, _ := r.svc.Messages(ctx, actor(1), conv, MessageQuery{})
	if len(other.Messages) != 2 {
		t.Fatal("the other side keeps everything")
	}
	if err := r.svc.ClearHistory(ctx, actor(2), conv, true); err != nil {
		t.Fatal(err)
	}
	list, _ := r.svc.ListConversations(ctx, actor(2), FolderInbox, "", 10)
	if len(list.Conversations) != 0 {
		t.Fatal("remove takes the conversation out of the list")
	}
	r.send(t, 1, conv, "c")
	list, _ = r.svc.ListConversations(ctx, actor(2), FolderInbox, "", 10)
	if len(list.Conversations) != 1 || list.Conversations[0].LastMessage == nil || list.Conversations[0].LastMessage.Text != "c" {
		t.Fatalf("the next message brings it back with only the new message: %+v", list.Conversations)
	}
}

func TestPinPostsServiceMessage(t *testing.T) {
	r := newRig(t, 1, 2)
	conv := r.direct(t, 1, 2)
	ctx := context.Background()
	m := r.send(t, 1, conv, "important")
	if err := r.svc.SetPinned(ctx, actor(2), conv, m.Seq, true); err != nil {
		t.Fatal(err)
	}
	if err := r.svc.SetPinned(ctx, actor(2), conv, m.Seq, true); err != nil {
		t.Fatal(err)
	}
	page, _ := r.svc.Messages(ctx, actor(1), conv, MessageQuery{})
	if len(page.Messages) != 2 {
		t.Fatalf("pinning twice posts one service message, got %d messages", len(page.Messages))
	}
	svc := page.Messages[1]
	if svc.Kind != model.MessageKindService || svc.ServiceAction == nil || svc.ServiceAction.Type != "message_pinned" || *svc.ServiceAction.Seq != m.Seq {
		t.Fatalf("service message: %+v", svc)
	}
	later := r.send(t, 2, conv, "also important")
	r.advance(time.Second)
	if err := r.svc.SetPinned(ctx, actor(1), conv, later.Seq, true); err != nil {
		t.Fatal(err)
	}
	view, _ := r.svc.Conversation(ctx, actor(1), conv)
	if len(view.Conversation.PinnedSeqs) != 2 || view.Conversation.PinnedSeqs[0] != later.Seq || view.Conversation.PinnedSeqs[1] != m.Seq {
		t.Fatalf("pinned, most recent first: %v", view.Conversation.PinnedSeqs)
	}
	pm := view.Conversation.PinnedMessages
	if len(pm) != 2 || pm[0].Seq != later.Seq || pm[0].Text != "also important" || pm[1].Text != "important" || pm[0].PinnedAt == nil {
		t.Fatalf("pinned messages follow pinned_seqs: %+v", pm)
	}
	if err := r.svc.SetPinned(ctx, actor(1), conv, later.Seq, false); err != nil {
		t.Fatal(err)
	}
	if err := r.svc.SetPinned(ctx, actor(1), conv, m.Seq, false); err != nil {
		t.Fatal(err)
	}
	view, _ = r.svc.Conversation(ctx, actor(1), conv)
	if len(view.Conversation.PinnedSeqs) != 0 {
		t.Fatal("unpinned")
	}
	_, err := r.svc.React(ctx, actor(1), mustID(t, svc.ID), ptr("heart"))
	wantInvalid(t, err)
}

func mustID(t *testing.T, s string) int64 {
	t.Helper()
	id, ok := dto.ParseID(s)
	if !ok {
		t.Fatalf("bad id %q", s)
	}
	return id
}

func TestTypingReachesOthersOnlyAndIsThrottled(t *testing.T) {
	r := newRig(t, 1, 2)
	conv := r.direct(t, 1, 2)
	ctx := context.Background()
	r.pub.take()
	if err := r.svc.Typing(ctx, actor(1), conv); err != nil {
		t.Fatal(err)
	}
	if err := r.svc.Typing(ctx, actor(1), conv); err != nil {
		t.Fatal(err)
	}
	got := r.pub.take()
	if len(got) != 1 || got[0].UserID != 2 {
		t.Fatalf("one typing push to the peer, got %+v", got)
	}
	if len(updatesOf(t, 2)) != 0 {
		t.Fatal("typing spends no update number")
	}
	wantErr(t, r.svc.Typing(ctx, actor(3), conv), ErrNotFound)
}

func TestTypingIsSilentInARequest(t *testing.T) {
	r := newRig(t, 1, 2)
	ctx := context.Background()
	res, _ := r.svc.EnsureDirect(ctx, actor(1), 2)
	conv, _ := dto.ParseID(res.Conversation.ID)
	r.pub.take()
	if err := r.svc.Typing(ctx, actor(1), conv); err != nil {
		t.Fatal(err)
	}
	if got := r.pub.take(); len(got) != 0 {
		t.Fatalf("no typing into a pending request, got %+v", got)
	}
}
