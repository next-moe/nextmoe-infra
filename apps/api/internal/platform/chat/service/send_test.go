package service

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"api/internal/platform/chat/content"
	"api/internal/platform/chat/dto"
	"api/internal/platform/chat/model"
)

func TestSendNumbersMessagesAndMovesDialogs(t *testing.T) {
	r := newRig(t, 1, 2)
	conv := r.direct(t, 1, 2)
	r.pub.take()

	for i, text := range []string{"one", "two", "three"} {
		m := r.send(t, 1, conv, text)
		if m.Seq != int64(i+1) {
			t.Fatalf("message %q got seq %d", text, m.Seq)
		}
	}
	a, b := member(t, conv, 1), member(t, conv, 2)
	if a.UnreadCount != 0 || a.LastReadSeq != 3 {
		t.Fatalf("sender: unread=%d last_read=%d", a.UnreadCount, a.LastReadSeq)
	}
	if b.UnreadCount != 3 || b.LastReadSeq != 0 {
		t.Fatalf("recipient: unread=%d last_read=%d", b.UnreadCount, b.LastReadSeq)
	}
	if b.LastMessageAt == nil {
		t.Fatal("the recipient's dialog must move")
	}
	for _, uid := range []int64{1, 2} {
		ups := updatesOf(t, uid)
		if len(ups) != 3 {
			t.Fatalf("user %d: want 3 updates, got %v", uid, kinds(ups))
		}
		for i, u := range ups {
			if u.UpdateSeq != int64(i+1) || u.Kind != model.UpdateNewMessage {
				t.Fatalf("user %d update %d: %+v", uid, i, u)
			}
		}
	}
	var cu model.ChatUser
	testDB.Where("user_id = ?", 2).Take(&cu)
	if cu.LastUpdateSeq != 3 {
		t.Fatalf("chat_user.last_update_seq = %d", cu.LastUpdateSeq)
	}
	var conversation model.ChatConversation
	testDB.Take(&conversation, conv)
	if conversation.LastSeq != 3 {
		t.Fatalf("last_seq = %d", conversation.LastSeq)
	}
}

func TestSendPushesEachMemberItsOwnView(t *testing.T) {
	r := newRig(t, 1, 2)
	conv := r.direct(t, 1, 2)
	r.pub.take()
	cid := "0b4a5c52-7c4e-4a3e-9c1c-2c3f4b5a6d7e"
	if _, err := r.svc.Send(context.Background(), actor(1), conv, SendInput{ClientMessageID: &cid, Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	got := r.pub.take()
	if len(got) != 2 {
		t.Fatalf("want one push per member, got %d", len(got))
	}
	for _, d := range got {
		e := d.Data.(*Event)
		if e.Update == nil || e.Update.Kind != model.UpdateNewMessage || e.Message == nil {
			t.Fatalf("push to %d: %+v", d.UserID, e)
		}
		if e.Update.UpdateSeq != 1 {
			t.Fatalf("push to %d carries update_seq %d", d.UserID, e.Update.UpdateSeq)
		}
		own := e.Message.ClientMessageID != nil
		if own != (d.UserID == 1) {
			t.Fatalf("client_message_id must reach the sender only; user %d own=%v", d.UserID, own)
		}
	}
}

func TestSendIsIdempotentOnClientMessageID(t *testing.T) {
	r := newRig(t, 1, 2)
	conv := r.direct(t, 1, 2)
	cid := "0B4A5C52-7C4E-4A3E-9C1C-2C3F4B5A6D7E"
	ctx := context.Background()
	first, err := r.svc.Send(ctx, actor(1), conv, SendInput{ClientMessageID: &cid, Text: "once"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := r.svc.Send(ctx, actor(1), conv, SendInput{ClientMessageID: &cid, Text: "different body"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Message.ID != again.Message.ID || again.Message.Text != "once" {
		t.Fatalf("a resend must return the first message: %+v vs %+v", first.Message, again.Message)
	}
	var n int64
	testDB.Model(&model.ChatMessage{}).Count(&n)
	if n != 1 {
		t.Fatalf("want 1 message, got %d", n)
	}
	if ups := updatesOf(t, 2); len(ups) != 1 {
		t.Fatalf("a resend writes no updates, got %v", kinds(ups))
	}
	r.users.m[3] = Profile{ID: 3, CreatedAt: old}
	other := r.direct(t, 1, 3)
	_, err = r.svc.Send(ctx, actor(1), other, SendInput{ClientMessageID: &cid, Text: "x"})
	wantInvalid(t, err)
	bad := "not-a-uuid"
	_, err = r.svc.Send(ctx, actor(1), conv, SendInput{ClientMessageID: &bad, Text: "x"})
	wantInvalid(t, err)
}

func TestSendValidates(t *testing.T) {
	r := newRig(t, 1, 2)
	conv := r.direct(t, 1, 2)
	ctx := context.Background()
	for name, in := range map[string]SendInput{
		"empty":               {Text: "   "},
		"bad entity":          {Text: "abc", Entities: []content.Entity{bold(1, 9)}},
		"quote without reply": {Text: "x", ReplyQuote: &QuoteInput{Text: "a"}},
		"reply to nothing":    {Text: "x", ReplyToSeq: ptr(int64(42))},
		"mention outsider":    {Text: "@x", Entities: []content.Entity{{Type: content.TypeMention, Offset: 0, Length: 2, UserID: ptr(int64(99))}}},
		"context off site":    {Text: "x", Context: &ContextInput{Kind: "patch", ID: "1", Title: "t", URL: "https://evil.example/p/1"}},
		"group without media": {Text: "x", MediaGroupID: ptr(int64(5))},
	} {
		if _, err := r.svc.Send(ctx, actor(1), conv, in); err == nil {
			t.Errorf("%s: want an error", name)
		} else {
			var ie *InvalidError
			if !errors.As(err, &ie) {
				t.Errorf("%s: want InvalidError, got %v", name, err)
			}
		}
	}
	_, err := r.svc.Send(ctx, actor(1), conv, SendInput{Media: &MediaInput{Type: "photo", ImageHash: "ab"}})
	wantInvalid(t, err)
	hash := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	_, err = r.svc.Send(ctx, actor(1), conv, SendInput{Media: &MediaInput{Type: "photo", ImageHash: hash}})
	wantErr(t, err, ErrImagesDisabled)
	_, err = r.svc.Send(ctx, actor(3), conv, SendInput{Text: "not a member"})
	wantErr(t, err, ErrNotFound)
}

type fakeImages map[string]ImageMeta

func (f fakeImages) Meta(_ context.Context, hashes []string) (map[string]ImageMeta, error) {
	out := map[string]ImageMeta{}
	for _, h := range hashes {
		if m, ok := f[h]; ok {
			out[h] = m
		}
	}
	return out, nil
}

func (f fakeImages) Upload(context.Context, io.Reader, string, string) (*UploadedImage, error) {
	return nil, errors.New("fakeImages does not upload")
}

func (f fakeImages) Ping(context.Context, []string) (int64, int, error) { return 0, 0, nil }

func TestSendPhotoTakesServerDimensions(t *testing.T) {
	r := newRig(t, 1, 2)
	hash := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	r.svc.images = fakeImages{hash: {Width: 800, Height: 600, Thumbhash: "th"}}
	conv := r.direct(t, 1, 2)
	res, err := r.svc.Send(context.Background(), actor(1), conv, SendInput{
		Text: "caption", Media: &MediaInput{Type: "photo", ImageHash: hash}, MediaGroupID: ptr(int64(77)),
	})
	if err != nil {
		t.Fatal(err)
	}
	m := res.Message
	if m.Media == nil || m.Media.Width != 800 || m.Media.Height != 600 || m.Media.Thumbhash != "th" {
		t.Fatalf("media: %+v", m.Media)
	}
	if m.MediaGroupID == nil || *m.MediaGroupID != "77" {
		t.Fatalf("media_group_id: %v", m.MediaGroupID)
	}
	other := "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
	_, err = r.svc.Send(context.Background(), actor(1), conv, SendInput{Media: &MediaInput{Type: "photo", ImageHash: other}})
	wantInvalid(t, err)
}

func TestReplyAndQuote(t *testing.T) {
	r := newRig(t, 1, 2)
	conv := r.direct(t, 1, 2)
	ctx := context.Background()
	orig, err := r.svc.Send(ctx, actor(1), conv, SendInput{Text: "hello world", Entities: []content.Entity{bold(6, 5)}})
	if err != nil {
		t.Fatal(err)
	}
	res, err := r.svc.Send(ctx, actor(2), conv, SendInput{
		Text: "reply", ReplyToSeq: &orig.Message.Seq, ReplyQuote: &QuoteInput{Text: "world", Offset: 6},
	})
	if err != nil {
		t.Fatal(err)
	}
	m := res.Message
	if m.ReplyTo == nil || m.ReplyTo.Seq != 1 || m.ReplyTo.SenderID != "1" || m.ReplyTo.Text != "hello world" {
		t.Fatalf("reply_to: %+v", m.ReplyTo)
	}
	if m.ReplyQuote == nil || m.ReplyQuote.Text != "world" || len(m.ReplyQuote.Entities) != 1 || m.ReplyQuote.Entities[0].Offset != 0 {
		t.Fatalf("reply_quote: %+v", m.ReplyQuote)
	}
	_, err = r.svc.Send(ctx, actor(2), conv, SendInput{Text: "x", ReplyToSeq: &orig.Message.Seq, ReplyQuote: &QuoteInput{Text: "world", Offset: 5}})
	wantInvalid(t, err)
}

func TestRequestLimitsUntilAccepted(t *testing.T) {
	r := newRig(t, 1, 2)
	ctx := context.Background()
	res, err := r.svc.EnsureDirect(ctx, actor(1), 2)
	if err != nil {
		t.Fatal(err)
	}
	conv, _ := dto.ParseID(res.Conversation.ID)

	_, err = r.svc.Send(ctx, actor(1), conv, SendInput{Text: "see https://spam.example"})
	wantErr(t, err, ErrRequestLimit)
	for i := 0; i < pendingMessageLimit; i++ {
		r.send(t, 1, conv, "hello")
	}
	_, err = r.svc.Send(ctx, actor(1), conv, SendInput{Text: "one more"})
	wantErr(t, err, ErrRequestLimit)

	if st, _ := r.svc.State(ctx, actor(2)); st.RequestCount != 1 || st.UnreadMessageCount != 0 {
		t.Fatalf("recipient state: %+v", st)
	}
	r.send(t, 2, conv, "hi back")
	if !acceptedFor(t, conv, 2) {
		t.Fatal("replying accepts the request")
	}
	dialog := updatesOf(t, 2)
	found := false
	for _, u := range dialog {
		if u.Kind == model.UpdateDialog && decodeData(t, u)["accepted"] == true {
			found = true
		}
	}
	if !found {
		t.Fatalf("the recipient's devices must learn of the accept: %v", kinds(dialog))
	}
	if _, err := r.svc.Send(ctx, actor(1), conv, SendInput{Text: "now a link https://ok.example"}); err != nil {
		t.Fatalf("after acceptance links are fine: %v", err)
	}
}

func TestRequestEditCannotSneakALink(t *testing.T) {
	r := newRig(t, 1, 2)
	ctx := context.Background()
	res, _ := r.svc.EnsureDirect(ctx, actor(1), 2)
	conv, _ := dto.ParseID(res.Conversation.ID)
	m := r.send(t, 1, conv, "plain")
	id, _ := dto.ParseID(m.ID)
	_, err := r.svc.Edit(ctx, actor(1), id, "now https://spam.example", nil)
	wantErr(t, err, ErrRequestLimit)
}

func TestSendRefusesAcrossABlock(t *testing.T) {
	r := newRig(t, 1, 2)
	conv := r.direct(t, 1, 2)
	r.send(t, 1, conv, "before")
	r.block(2, 1)
	_, err := r.svc.Send(context.Background(), actor(1), conv, SendInput{Text: "after"})
	wantErr(t, err, ErrBlocked)
	_, err = r.svc.Send(context.Background(), actor(2), conv, SendInput{Text: "the blocker cannot either"})
	wantErr(t, err, ErrBlocked)
}

func TestArchivedConversationComesBackUnlessMuted(t *testing.T) {
	r := newRig(t, 1, 2)
	conv := r.direct(t, 1, 2)
	ctx := context.Background()
	r.send(t, 1, conv, "first")
	if _, err := r.svc.UpdateDialog(ctx, actor(2), conv, DialogPatch{Archived: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	r.send(t, 1, conv, "second")
	if member(t, conv, 2).ArchivedAt != nil {
		t.Fatal("an unmuted archived conversation returns to the inbox on a new message")
	}
	if _, err := r.svc.UpdateDialog(ctx, actor(2), conv, DialogPatch{Archived: ptr(true), Muted: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	r.send(t, 1, conv, "third")
	if member(t, conv, 2).ArchivedAt == nil {
		t.Fatal("a muted archived conversation stays archived")
	}
	if _, err := r.svc.UpdateDialog(ctx, actor(1), conv, DialogPatch{Archived: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	r.send(t, 1, conv, "sender's own message")
	if member(t, conv, 1).ArchivedAt == nil {
		t.Fatal("sending does not unarchive the sender's own dialog")
	}
}

func TestEditRules(t *testing.T) {
	r := newRig(t, 1, 2)
	conv := r.direct(t, 1, 2)
	ctx := context.Background()
	m := r.send(t, 1, conv, "typo")
	id, _ := dto.ParseID(m.ID)
	r.pub.take()

	res, err := r.svc.Edit(ctx, actor(1), id, "fixed", []content.Entity{bold(0, 5)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Message.Text != "fixed" || res.Message.EditedAt == nil || len(res.Message.Entities) != 1 {
		t.Fatalf("edited: %+v", res.Message)
	}
	if got := kinds(updatesOf(t, 2)); got[len(got)-1] != model.UpdateEditMessage {
		t.Fatalf("recipient updates: %v", got)
	}
	for _, d := range r.pub.take() {
		if e := d.Data.(*Event); e.Message == nil || e.Message.Text != "fixed" {
			t.Fatalf("edit push to %d lacks the new text", d.UserID)
		}
	}
	_, err = r.svc.Edit(ctx, actor(2), id, "not mine", nil)
	wantErr(t, err, ErrNotPermitted)
	_, err = r.svc.Edit(ctx, actor(1), id, "  ", nil)
	wantInvalid(t, err)
	r.advance(editWindow + time.Minute)
	_, err = r.svc.Edit(ctx, actor(1), id, "too late", nil)
	wantErr(t, err, ErrEditWindow)
}

func TestSendRateLimit(t *testing.T) {
	r := newRig(t, 1, 2)
	conv := r.direct(t, 1, 2)
	for i := 0; i < messagesPerMinute; i++ {
		r.send(t, 1, conv, "m")
	}
	_, err := r.svc.Send(context.Background(), actor(1), conv, SendInput{Text: "over"})
	var rl *RateLimitError
	if !errors.As(err, &rl) || rl.RetryAfter <= 0 {
		t.Fatalf("want RateLimitError with a retry, got %v", err)
	}
}
