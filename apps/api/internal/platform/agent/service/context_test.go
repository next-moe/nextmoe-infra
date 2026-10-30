package service

import (
	"encoding/json"
	"strings"
	"testing"

	"api/internal/platform/agent/model"
)

func msg(id int64, role, text string, extra ...Part) model.Message {
	parts, _ := json.Marshal(append([]Part{{Type: "text", Text: text}}, extra...))
	return model.Message{ID: id, Role: role, Parts: parts, Status: model.MessageComplete}
}

func chat(n int, size int) []model.Message {
	var out []model.Message
	for i := range n {
		role := model.RoleUser
		if i%2 == 1 {
			role = model.RoleAssistant
		}
		out = append(out, msg(int64(i+1), role, strings.Repeat("字", size)))
	}
	return out
}

func msgIDs(rs []rendered) []int64 {
	out := make([]int64, len(rs))
	for i, r := range rs {
		out[i] = r.msg.ID
	}
	return out
}

func TestWindowKeepsTheNewestTurnsWithinBudgetStartingAtAUserMessage(t *testing.T) {
	history := chat(10, 100)
	got, summary := window(history, nil, nil, 350)
	if summary != "" || len(got) != 2 || got[0].msg.ID != 9 || got[0].msg.Role != model.RoleUser {
		t.Fatalf("three newest messages fit, but the window must open on a user message: %v", msgIDs(got))
	}
	if got, _ := window(chat(3, 5000), nil, nil, 100); len(got) != 1 || got[0].msg.ID != 3 {
		t.Fatalf("the question itself always goes, however long: %v", msgIDs(got))
	}
}

func TestWindowStartsAfterTheSummary(t *testing.T) {
	history := chat(10, 10)
	got, summary := window(history, &model.Summary{UptoMessageID: 6, Content: "之前聊了催泪作"}, nil, 100000)
	if summary != "之前聊了催泪作" || got[0].msg.ID != 7 || len(got) != 4 {
		t.Fatalf("summary replaces messages 1-6: %v %q", msgIDs(got), summary)
	}
	if got, summary := window(history, &model.Summary{UptoMessageID: 99, Content: "别的分支"}, nil, 100000); summary != "" || len(got) != 10 {
		t.Fatalf("a summary off this branch is ignored: %v %q", msgIDs(got), summary)
	}
}

func TestOldAttachmentsLeaveTheContext(t *testing.T) {
	uploads := map[string]*model.Upload{"u1": {Kind: model.UploadText, Text: "很早的附件全文"}, "u2": {Kind: model.UploadText, Text: "最近的附件全文"}}
	history := []model.Message{
		msg(1, model.RoleUser, "看看这个", Part{Type: "file", Filename: "old.txt", UploadID: "u1"}),
		msg(2, model.RoleAssistant, "好"),
	}
	for i := int64(3); i < 9; i += 2 {
		history = append(history, msg(i, model.RoleUser, "继续"), msg(i+1, model.RoleAssistant, "嗯"))
	}
	history = append(history, msg(9, model.RoleUser, "还有这个", Part{Type: "file", Filename: "new.txt", UploadID: "u2"}))
	got := render(history, uploads)
	if strings.Contains(got[0].text, "很早的附件全文") || !strings.Contains(got[0].text, "〔附件 old.txt〕") {
		t.Fatalf("an attachment older than %d questions keeps only its name: %q", keepAttachments, got[0].text)
	}
	if !strings.Contains(got[len(got)-1].text, "最近的附件全文") {
		t.Fatalf("a recent attachment stays whole: %q", got[len(got)-1].text)
	}
}

func TestFoldPlanSummarizesAllButARecentTail(t *testing.T) {
	history := chat(10, 100)
	if fold := foldPlan(history, nil, nil, 2000); fold != nil {
		t.Fatalf("under three quarters of the budget nothing folds: %d", len(fold))
	}
	fold := foldPlan(history, nil, nil, 1000)
	if len(fold) == 0 || fold[len(fold)-1].Role != model.RoleAssistant {
		t.Fatalf("fold ends before a user message: %d", len(fold))
	}
	tail := 0
	for _, m := range history[len(fold):] {
		tail += estimateTokens(messageText(m, nil))
	}
	if tail > 500 || len(fold) != 6 {
		t.Fatalf("the tail stays within half the budget, as long as possible: fold %d tail %d", len(fold), tail)
	}
	again := foldPlan(history, &model.Summary{UptoMessageID: fold[len(fold)-1].ID, Content: "摘要"}, nil, 1000)
	if again != nil {
		t.Fatalf("once folded, the rest fits: %d", len(again))
	}
	if fold := foldPlan(chat(2, 5000), nil, nil, 1000); fold != nil {
		t.Fatalf("a single huge exchange has nothing older to fold: %d", len(fold))
	}
}

func TestParseMemoryOps(t *testing.T) {
	ops, err := parseMemoryOps("<think>嗯</think>```json\n{\"add\": [{\"content\": \"用户喜欢猫耳\", \"quote\": \"我超喜欢猫耳！\"}, {\"content\": \"用户喜欢《CLANNAD》\", \"quote\": \"推荐《CLANNAD》\"}]," +
		" \"update\": [{\"id\": 3, \"content\": \"用户讨厌 NTR\", \"quote\": \"NTR 真的不行\"}], \"delete\": [4]}\n```")
	if err != nil || len(ops.Add) != 2 || ops.Update[0].ID != 3 || ops.Delete[0] != 4 {
		t.Fatalf("%+v %v", ops, err)
	}
	g := ops.Grounded("我超喜欢 猫耳。\n另外，NTR真的不行")
	if len(g.Add) != 1 || g.Add[0].Content != "用户喜欢猫耳" || len(g.Update) != 1 || len(g.Delete) != 1 {
		t.Fatalf("only memories quoting the user survive, spacing and punctuation aside: %+v", g)
	}
	if g := (MemoryOps{Add: []MemoryEdit{{Content: "用户喜欢猫耳"}}}).Grounded("我喜欢猫耳"); len(g.Add) != 0 {
		t.Fatal("no quote, no memory")
	}
	if ops, err := parseMemoryOps("{}"); err != nil || len(ops.Add) != 0 {
		t.Fatalf("empty is fine: %+v %v", ops, err)
	}
	if _, err := parseMemoryOps("没有需要记的"); err == nil {
		t.Fatal("prose instead of JSON is a failure, so the conversation is retried")
	}
}
