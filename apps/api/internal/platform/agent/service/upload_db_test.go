package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"api/internal/platform/agent/model"
)

func TestUploadsJoinTheConversationFeedTheModelAndGoWithIt(t *testing.T) {
	resetAgent(t)
	testDB.Exec("TRUNCATE agent_upload")
	ctx := context.Background()
	s := NewStore(testDB)
	c, _ := s.CreateConversation(ctx, 7, 14, model.ModeAssistant, nil)

	doc := &model.Upload{ID: "u-doc", UserID: 7, SiteID: 14, Kind: model.UploadDocument, MediaType: "application/pdf", Filename: "攻略.pdf", Size: 10, Text: "第三章选择「留下」", Truncated: true, CreatedAt: time.Now()}
	pic := &model.Upload{ID: "u-pic", UserID: 7, SiteID: 14, Kind: model.UploadImage, MediaType: "image/jpeg", Filename: "cg.jpg", Size: 3, Data: []byte{1, 2, 3}, CreatedAt: time.Now()}
	stale := &model.Upload{ID: "u-old", UserID: 7, SiteID: 14, Kind: model.UploadText, MediaType: "text/plain", Text: "x", CreatedAt: time.Now().Add(-48 * time.Hour)}
	for _, u := range []*model.Upload{doc, pic, stale} {
		if err := s.CreateUpload(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	parts := []Part{{Type: "text", Text: "这张图是哪个结局？"}, {Type: "file", UploadID: "u-doc"}, {Type: "file", UploadID: "u-pic"}}
	run, _, err := s.StartRun(ctx, StartRequest{Conversation: c, Parts: parts, Mode: model.ModeAssistant, IdemKey: "up"})
	if err != nil {
		t.Fatal(err)
	}
	if u, _ := s.Upload(ctx, 7, 14, "u-doc"); u.ConversationID == nil || *u.ConversationID != c.ID {
		t.Fatalf("a sent upload belongs to its conversation: %+v", u.ConversationID)
	}

	branch, _ := s.Branch(ctx, &run.UserMessageID)
	uploads, _ := s.UploadsByID(ctx, UploadIDs(decodeParts(branch[0])))
	persona := &model.Persona{Prompt: "p"}
	msgs, vision := buildMessages(Turn{Persona: persona, History: branch, Uploads: uploads}, time.Now())
	last := msgs[len(msgs)-1]
	if !vision || !strings.Contains(string(last.Raw), `"image_url"`) || !strings.Contains(string(last.Raw), "data:image/jpeg;base64,AQID") {
		t.Fatalf("a picture goes to the model as image content and asks for the vision scene: vision=%v raw=%s", vision, last.Raw)
	}
	if !strings.Contains(last.Content, "第三章选择「留下」") || !strings.Contains(last.Content, "只是开头部分") {
		t.Fatalf("a document's text reaches the model, flagged when cut: %q", last.Content)
	}

	if n, err := s.SweepUploads(ctx, time.Now().Add(-24*time.Hour)); err != nil || n != 1 {
		t.Fatalf("the sweep drops only stale unattached uploads: n=%d err=%v", n, err)
	}
	if _, err := s.Upload(ctx, 8, 14, "u-pic"); err == nil {
		t.Fatal("another user must not read the upload")
	}
	if err := s.DeleteConversation(ctx, c); err != nil {
		t.Fatal(err)
	}
	var left int64
	testDB.Model(&model.Upload{}).Count(&left)
	if left != 0 {
		t.Fatalf("deleting the conversation deletes its uploads, %d left", left)
	}
}
