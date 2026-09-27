package service

import (
	"context"
	"testing"

	"api/internal/platform/chat/dto"
)

func TestUnreadNeverCountsYourOwnMessages(t *testing.T) {
	r := newRig(t, 1, 2)
	conv := r.direct(t, 1, 2)
	ctx := context.Background()
	r.send(t, 1, conv, "theirs")
	r.send(t, 2, conv, "mine")
	r.send(t, 1, conv, "theirs again")
	if err := testDB.Exec(`UPDATE chat_member SET last_read_seq = 0 WHERE conversation_id = ? AND user_id = 2`, conv).Error; err != nil {
		t.Fatal(err)
	}
	res, err := r.svc.Read(ctx, actor(2), conv, 1)
	if err != nil {
		t.Fatal(err)
	}
	if res.UnreadCount != 1 {
		t.Fatalf("unread %d: the member's own message counted", res.UnreadCount)
	}
}

func TestLastMessageSkipsHiddenOnes(t *testing.T) {
	r := newRig(t, 1, 2)
	conv := r.direct(t, 1, 2)
	ctx := context.Background()
	r.send(t, 1, conv, "keep")
	last := r.send(t, 1, conv, "hide me")
	if _, err := r.svc.DeleteMessages(ctx, actor(2), conv, []int64{last.Seq}, false); err != nil {
		t.Fatal(err)
	}
	page, _ := r.svc.ListConversations(ctx, actor(2), FolderInbox, "", 10)
	if lm := page.Conversations[0].LastMessage; lm == nil || lm.Text != "keep" {
		t.Fatalf("the list must show the newest message the caller can see: %+v", lm)
	}
	page, _ = r.svc.ListConversations(ctx, actor(1), FolderInbox, "", 10)
	if lm := page.Conversations[0].LastMessage; lm == nil || lm.Text != "hide me" {
		t.Fatalf("the sender still sees it: %+v", lm)
	}
}

func TestTheRecipientOfARequestTypesToNobody(t *testing.T) {
	r := newRig(t, 1, 2)
	ctx := context.Background()
	res, _ := r.svc.EnsureDirect(ctx, actor(1), 2)
	conv, _ := dto.ParseID(res.Conversation.ID)
	r.pub.take()
	if err := r.svc.Typing(ctx, actor(2), conv); err != nil {
		t.Fatal(err)
	}
	if got := r.pub.take(); len(got) != 0 {
		t.Fatalf("typing before accepting reveals the request was opened: %+v", got)
	}
}

func TestContextHostMustBeTheSiteOrASubdomain(t *testing.T) {
	r := newRig(t, 1, 2)
	conv := r.direct(t, 1, 2)
	ctx := context.Background()
	for _, url := range []string{"https://evilletmoe.example/p/1", "http://letmoe.example/p/1"} {
		_, err := r.svc.Send(ctx, actor(1), conv, SendInput{Text: "x", Context: &ContextInput{Kind: "patch", ID: "1", Title: "t", URL: url}})
		wantInvalid(t, err)
	}
	res, err := r.svc.Send(ctx, actor(1), conv, SendInput{Text: "x", Context: &ContextInput{Kind: "patch", ID: "1", Title: "t", URL: "https://www.letmoe.example/p/1"}})
	if err != nil || res.Message.Context == nil || res.Message.Context.Site != "letmoe" {
		t.Fatalf("a subdomain of the client's host is fine: %+v %v", res, err)
	}
}

func TestPendingRequestTakesNoMedia(t *testing.T) {
	r := newRig(t, 1, 2)
	hash := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	r.svc.images = fakeImages{hash: {Width: 1, Height: 1}}
	ctx := context.Background()
	res, _ := r.svc.EnsureDirect(ctx, actor(1), 2)
	conv, _ := dto.ParseID(res.Conversation.ID)
	_, err := r.svc.Send(ctx, actor(1), conv, SendInput{Media: &MediaInput{Type: "photo", ImageHash: hash}})
	wantErr(t, err, ErrRequestLimit)
}

func TestOnlyTheSenderSeesTheirClientMessageID(t *testing.T) {
	r := newRig(t, 1, 2)
	conv := r.direct(t, 1, 2)
	ctx := context.Background()
	cid := "0b4a5c52-7c4e-4a3e-9c1c-2c3f4b5a6d7e"
	if _, err := r.svc.Send(ctx, actor(1), conv, SendInput{ClientMessageID: &cid, Text: "x"}); err != nil {
		t.Fatal(err)
	}
	theirs, _ := r.svc.Messages(ctx, actor(2), conv, MessageQuery{})
	mine, _ := r.svc.Messages(ctx, actor(1), conv, MessageQuery{})
	if theirs.Messages[0].ClientMessageID != nil || mine.Messages[0].ClientMessageID == nil {
		t.Fatalf("recipient %v sender %v", theirs.Messages[0].ClientMessageID, mine.Messages[0].ClientMessageID)
	}
}
