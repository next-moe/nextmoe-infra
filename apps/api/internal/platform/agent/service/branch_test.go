package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"api/internal/platform/agent/model"
)

func TestRegenerateEditSwitchStarAndFeedback(t *testing.T) {
	resetAgent(t)
	ctx := context.Background()
	s := NewStore(testDB)
	c, err := s.CreateConversation(ctx, 7, 14, model.ModeAssistant, nil)
	if err != nil {
		t.Fatal(err)
	}
	finish := func(run *model.Run, text string) {
		t.Helper()
		if err := s.FinishRun(ctx, run, Finish{Status: model.RunCompleted, Parts: []Part{{Type: "text", Text: text}}}); err != nil {
			t.Fatal(err)
		}
	}
	first, _, err := s.StartRun(ctx, StartRequest{Conversation: c, Parts: []Part{{Type: "text", Text: "推荐纯爱作"}}, Mode: model.ModeAssistant, IdemKey: "a"})
	if err != nil {
		t.Fatal(err)
	}
	finish(first, "第一版回答")

	user := first.UserMessageID
	again, _, err := s.StartRun(ctx, StartRequest{Conversation: c, Regenerate: &user, Mode: model.ModeAssistant, IdemKey: "b"})
	if err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	if again.UserMessageID != user || again.AssistantMessageID == first.AssistantMessageID {
		t.Fatalf("a regenerated run answers the same user message with a new reply: %+v", again)
	}
	finish(again, "第二版回答")
	siblings, _ := s.SiblingIDs(ctx, c.ID)
	if got := siblings[user]; len(got) != 2 || got[0] != first.AssistantMessageID || got[1] != again.AssistantMessageID {
		t.Fatalf("two replies under the user message, oldest first: %v", got)
	}
	var users int64
	testDB.Model(&model.Message{}).Where("conversation_id = ? AND role = ?", c.ID, model.RoleUser).Count(&users)
	if users != 1 {
		t.Fatalf("regenerating must not add a user message, have %d", users)
	}

	if _, _, err := s.StartRun(ctx, StartRequest{Conversation: c, Regenerate: &first.AssistantMessageID, Mode: model.ModeAssistant, IdemKey: "c"}); !errors.Is(err, ErrNotRegenerable) {
		t.Fatalf("regenerating an assistant message: err = %v", err)
	}

	root := int64(0)
	edited, _, err := s.StartRun(ctx, StartRequest{Conversation: c, ParentID: &root, Parts: []Part{{Type: "text", Text: "推荐狐娘纯爱作"}}, Mode: model.ModeAssistant, IdemKey: "d"})
	if err != nil {
		t.Fatal(err)
	}
	finish(edited, "狐娘回答")
	c, _ = s.Conversation(ctx, 7, 14, c.ID)
	if *c.HeadMessageID != edited.AssistantMessageID {
		t.Fatalf("the edit becomes the current branch, head=%d", *c.HeadMessageID)
	}

	leaf, err := s.SetHead(ctx, c, first.UserMessageID)
	if err != nil || leaf != again.AssistantMessageID {
		t.Fatalf("switching to the first user message lands on its newest reply: leaf=%d err=%v", leaf, err)
	}
	c, _ = s.Conversation(ctx, 7, 14, c.ID)
	branch, _ := s.Branch(ctx, c.HeadMessageID)
	if len(branch) != 2 || branch[1].ID != again.AssistantMessageID {
		t.Fatalf("branch after switching: %+v", branch)
	}

	if leaf, err := s.SetHead(ctx, c, first.AssistantMessageID); err != nil || leaf != first.AssistantMessageID {
		t.Fatalf("pick the first reply: leaf=%d err=%v", leaf, err)
	}
	c, _ = s.Conversation(ctx, 7, 14, c.ID)
	later, _, err := s.StartRun(ctx, StartRequest{Conversation: c, ParentID: &root, Parts: []Part{{Type: "text", Text: "推荐兽耳纯爱作"}}, Mode: model.ModeAssistant, IdemKey: "d2"})
	if err != nil {
		t.Fatal(err)
	}
	finish(later, "兽耳回答")
	if leaf, _ := s.SetHead(ctx, c, user); leaf != first.AssistantMessageID {
		t.Fatalf("returning to a question restores the reply last picked under it: leaf=%d want %d", leaf, first.AssistantMessageID)
	}

	if err := s.StarConversation(ctx, c, true); err != nil {
		t.Fatal(err)
	}
	other, _ := s.CreateConversation(ctx, 7, 14, model.ModeAssistant, nil)
	yes, no := true, false
	starred, _ := s.ListConversations(ctx, 7, 14, &yes, time.Time{}, 0, 10)
	plain, _ := s.ListConversations(ctx, 7, 14, &no, time.Time{}, 0, 10)
	if len(starred) != 1 || starred[0].ID != c.ID || len(plain) != 1 || plain[0].ID != other.ID {
		t.Fatalf("starred filter: starred=%v plain=%v", starred, plain)
	}

	if err := s.SetFeedback(ctx, 7, 14, again.AssistantMessageID, model.FeedbackDown); err != nil {
		t.Fatal(err)
	}
	if err := s.SetFeedback(ctx, 7, 14, user, model.FeedbackUp); !errors.Is(err, ErrNotFound) {
		t.Fatalf("feedback on a user message: err = %v", err)
	}
	if err := s.SetFeedback(ctx, 8, 14, again.AssistantMessageID, model.FeedbackUp); !errors.Is(err, ErrNotFound) {
		t.Fatalf("feedback on another user's message: err = %v", err)
	}
	branch, _ = s.Branch(ctx, &again.AssistantMessageID)
	if branch[1].Feedback != model.FeedbackDown {
		t.Fatalf("feedback is read back with the branch: %+v", branch[1])
	}

	running, _, err := s.StartRun(ctx, StartRequest{Conversation: c, Parts: []Part{{Type: "text", Text: "再来"}}, Mode: model.ModeAssistant, IdemKey: "e"})
	if err != nil {
		t.Fatal(err)
	}
	active, _ := s.ActiveRuns(ctx, []int64{c.ID, other.ID})
	if active[c.ID] != running.ID || active[other.ID] != "" {
		t.Fatalf("active runs: %v", active)
	}
}
