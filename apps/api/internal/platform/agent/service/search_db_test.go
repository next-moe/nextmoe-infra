package service

import (
	"context"
	"testing"
	"time"

	"api/internal/platform/agent/model"
)

func TestListConversationsSearchesTitlesAndMessageText(t *testing.T) {
	resetAgent(t)
	ctx := context.Background()
	s := NewStore(testDB)
	mk := func(user int64, title, text string) int64 {
		c, err := s.CreateConversation(ctx, user, 14, model.ModeAssistant, nil)
		if err != nil {
			t.Fatal(err)
		}
		if title != "" {
			_ = s.RenameConversation(ctx, c, title)
		}
		if _, _, err := s.StartRun(ctx, StartRequest{Conversation: c, Parts: []Part{{Type: "text", Text: text}}, Mode: model.ModeAssistant, IdemKey: title + text}); err != nil {
			t.Fatal(err)
		}
		return c.ID
	}
	tears := mk(7, "催泪作品推荐", "推荐几部催泪作")
	cats := mk(7, "猫耳角色", "有没有猫耳妹妹的作品")
	pct := mk(7, "折扣", "现在打 50% 折吗")
	other := mk(8, "催泪作品推荐", "别人的对话")

	ids := func(q string) []int64 {
		rows, err := s.ListConversations(ctx, 7, 14, nil, q, time.Time{}, 0, 10)
		if err != nil {
			t.Fatal(err)
		}
		var out []int64
		for _, r := range rows {
			out = append(out, r.ID)
		}
		return out
	}
	if got := ids("催泪"); len(got) != 1 || got[0] != tears {
		t.Fatalf("a title match, only the caller's own: %v (other user's is %d)", got, other)
	}
	if got := ids("妹妹"); len(got) != 1 || got[0] != cats {
		t.Fatalf("a message text match: %v", got)
	}
	if got := ids("50%"); len(got) != 1 || got[0] != pct {
		t.Fatalf("%% is literal, not a wildcard: %v", got)
	}
	if got := ids("text"); len(got) != 0 {
		t.Fatalf("part keys must not match: %v", got)
	}
	if got := ids(""); len(got) != 3 {
		t.Fatalf("an empty search lists everything: %v", got)
	}
}
