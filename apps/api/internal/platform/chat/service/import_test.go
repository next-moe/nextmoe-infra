package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"api/internal/platform/chat/content"
	"api/internal/platform/chat/dto"
	"api/internal/platform/chat/model"
)

func legacy(key string, sender int64, text string, at time.Time) ImportMessage {
	return ImportMessage{SourceKey: key, SenderID: sender, Text: text, CreatedAt: at, OriginSite: "kungal"}
}

func TestImportBuildsHistoryWithoutUpdates(t *testing.T) {
	r := newRig(t, 1, 2)
	ctx := context.Background()
	t0 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	hash := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	in := ImportConversation{
		UserA: 2, UserB: 1, OriginSite: "kungal", CreatedAt: t0,
		Messages: []ImportMessage{
			legacy("kungal:1", 1, "hello", t0.Add(time.Minute)),
			{SourceKey: "moyu:9", SenderID: 2, Text: "**from moyu**", Entities: []content.Entity{bold(0, 4)}, CreatedAt: t0.Add(2 * time.Minute), OriginSite: "moyu",
				ReplyToKey: "kungal:1", Reactions: []ImportReaction{{UserID: 1, Reaction: "heart", CreatedAt: t0}, {UserID: 1, Reaction: "nonsense"}}},
			{SourceKey: "kungal:2", SenderID: 1, Media: &dto.Media{Type: "photo", ImageHash: hash, Width: 1, Height: 1}, CreatedAt: t0.Add(3 * time.Minute), OriginSite: "kungal"},
			{SourceKey: "kungal:3", SenderID: 1, Deleted: true, CreatedAt: t0.Add(4 * time.Minute), OriginSite: "kungal"},
			legacy("kungal:4", 1, "unread", t0.Add(5*time.Minute)),
		},
	}
	in.Messages[2].ReadThroughFor = []int64{2}
	res, err := r.svc.Import(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Created || res.Imported != 5 || res.Skipped != 0 {
		t.Fatalf("result: %+v", res)
	}
	conv := res.ConversationID
	if n := len(updatesOf(t, 1)) + len(updatesOf(t, 2)); n != 0 {
		t.Fatalf("history nobody has seen writes no updates, got %d", n)
	}
	page, _ := r.svc.Messages(ctx, actor(2), conv, MessageQuery{})
	if len(page.Messages) != 4 {
		t.Fatalf("the tombstone is not listed: %d", len(page.Messages))
	}
	moyu := page.Messages[1]
	if moyu.Seq != 2 || moyu.ReplyTo == nil || moyu.ReplyTo.Seq != 1 || len(moyu.Reactions) != 1 || moyu.Reactions[0].Reaction != "heart" {
		t.Fatalf("moyu message: %+v", moyu)
	}
	if !moyu.CreatedAt.Equal(t0.Add(2 * time.Minute)) {
		t.Fatal("imported messages keep their time")
	}
	two := member(t, conv, 2)
	if two.LastReadSeq != 3 || two.UnreadCount != 1 || two.AcceptedAt == nil {
		t.Fatalf("member 2: read %d unread %d", two.LastReadSeq, two.UnreadCount)
	}
	one := member(t, conv, 1)
	if one.LastReadSeq != 5 || one.UnreadCount != 0 {
		t.Fatalf("member 1 read through its own last message: read %d unread %d", one.LastReadSeq, one.UnreadCount)
	}
	if one.LastMessageAt == nil || !one.LastMessageAt.Equal(t0.Add(5*time.Minute)) {
		t.Fatalf("last_message_at: %v", one.LastMessageAt)
	}
}

func TestImportRerunAppendsOnlyWhatIsNewAndTellsClients(t *testing.T) {
	r := newRig(t, 1, 2)
	ctx := context.Background()
	t0 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	base := ImportConversation{UserA: 1, UserB: 2, OriginSite: "kungal", CreatedAt: t0, AllReadFor: []int64{1, 2},
		Messages: []ImportMessage{legacy("kungal:1", 1, "a", t0), legacy("kungal:2", 2, "b", t0.Add(time.Minute))}}
	first, err := r.svc.Import(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	again, err := r.svc.Import(ctx, base)
	if err != nil || again.Imported != 0 || again.Skipped != 2 {
		t.Fatalf("re-run: %+v %v", again, err)
	}
	base.Messages = append(base.Messages, legacy("kungal:3", 1, "sent on the old site after the import", t0.Add(time.Hour)))
	base.AllReadFor = nil
	sweep, err := r.svc.Import(ctx, base)
	if err != nil || sweep.Imported != 1 || sweep.ConversationID != first.ConversationID {
		t.Fatalf("sweep: %+v %v", sweep, err)
	}
	var cnt int64
	testDB.Model(&model.ChatMessage{}).Where("conversation_id = ?", first.ConversationID).Count(&cnt)
	if cnt != 3 {
		t.Fatalf("messages %d", cnt)
	}
	ups := updatesOf(t, 2)
	if len(ups) != 1 || ups[0].Kind != model.UpdateNewMessage {
		t.Fatalf("an append to a conversation in use is announced: %v", kinds(ups))
	}
	if u := member(t, first.ConversationID, 2).UnreadCount; u != 1 {
		t.Fatalf("the swept message is unread for the recipient: %d", u)
	}
}

func TestImportNeverWritesUnderNativeHistory(t *testing.T) {
	r := newRig(t, 1, 2)
	ctx := context.Background()
	conv := r.direct(t, 1, 2)
	r.send(t, 1, conv, "already talking in the new chat")
	_, err := r.svc.Import(ctx, ImportConversation{UserA: 1, UserB: 2, OriginSite: "moyu", CreatedAt: time.Now(),
		Messages: []ImportMessage{legacy("moyu:1", 1, "old", time.Now())}})
	if !errors.Is(err, ErrNativeHistory) {
		t.Fatalf("want ErrNativeHistory, got %v", err)
	}
}

func TestImportFillsAnOpenedEmptyConversation(t *testing.T) {
	r := newRig(t, 1, 2)
	ctx := context.Background()
	opened, err := r.svc.EnsureDirect(ctx, actor(1), 2)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := dto.ParseID(opened.Conversation.ID)
	if acceptedFor(t, id, 2) {
		t.Fatal("setup: a request")
	}
	res, err := r.svc.Import(ctx, ImportConversation{UserA: 1, UserB: 2, OriginSite: "kungal", CreatedAt: time.Now(),
		Messages: []ImportMessage{legacy("kungal:1", 2, "we talked before", time.Now())}})
	if err != nil || res.Created || res.ConversationID != id {
		t.Fatalf("import into the opened conversation: %+v %v", res, err)
	}
	if !acceptedFor(t, id, 2) {
		t.Fatal("a pair with history is past the request stage")
	}
}
