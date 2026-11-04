package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"api/internal/platform/agent/model"
)

func TestMemoryDedupesEvictsAutoFirstAndProtectsExplicit(t *testing.T) {
	resetAgent(t)
	ctx := context.Background()
	s := NewStore(testDB)
	add := func(content, origin string) *model.Memory {
		t.Helper()
		m, err := s.AddMemory(ctx, 7, 14, content, origin, nil, 3)
		if err != nil {
			t.Fatalf("add %q: %v", content, err)
		}
		return m
	}
	auto := add("用户喜欢猫耳", model.MemoryAuto)
	again := add("  用户喜欢猫耳 ", model.MemoryExplicit)
	if again.ID != auto.ID {
		t.Fatalf("the same memory twice is one memory: %d %d", auto.ID, again.ID)
	}
	keep := add("用户讨厌 NTR", model.MemoryExplicit)
	add("用户在玩 Summer Pockets", model.MemoryAuto)
	add("用户用 Steam Deck 玩", model.MemoryAuto)
	rows, _ := s.Memories(ctx, 7, 14)
	if len(rows) != 3 {
		t.Fatalf("max 3: %d", len(rows))
	}
	for _, r := range rows {
		if r.ID == auto.ID {
			t.Fatal("the oldest automatic memory makes room")
		}
	}
	add("用户叫助手「小萌」", model.MemoryExplicit)
	add("用户只玩全年龄", model.MemoryExplicit)
	if _, err := s.AddMemory(ctx, 7, 14, "再来一条", model.MemoryExplicit, nil, 3); !errors.Is(err, ErrMemoryFull) {
		t.Fatalf("explicit memories are never evicted: %v", err)
	}

	err := s.ApplyMemoryOps(ctx, 7, 14, 1, MemoryOps{Delete: []int64{keep.ID}, Add: []MemoryEdit{{Content: " "}}}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteMemory(ctx, 8, 14, keep.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another user's memory is not found: %v", err)
	}
	if m, err := s.DeleteMemory(ctx, 7, 14, keep.ID); err != nil || m.Content != "用户讨厌 NTR" {
		t.Fatalf("extraction must not delete an explicit memory: %+v %v", m, err)
	}
}

func TestApplyMemoryOpsAddsUpdatesAndDeletesAutoMemories(t *testing.T) {
	resetAgent(t)
	ctx := context.Background()
	s := NewStore(testDB)
	a, _ := s.AddMemory(ctx, 7, 14, "用户喜欢猫耳", model.MemoryAuto, nil, 10)
	b, _ := s.AddMemory(ctx, 7, 14, "用户在玩 A", model.MemoryAuto, nil, 10)
	other, _ := s.AddMemory(ctx, 8, 14, "别人的", model.MemoryAuto, nil, 10)
	ops := MemoryOps{Add: []MemoryEdit{{Content: "用户用 Steam Deck 玩"}}, Update: []MemoryEdit{{ID: a.ID, Content: "用户喜欢猫耳和兽耳"}},
		Delete: []int64{b.ID, other.ID}}
	if err := s.ApplyMemoryOps(ctx, 7, 14, 42, ops, 10); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.Memories(ctx, 7, 14)
	got := map[string]bool{}
	for _, r := range rows {
		got[r.Content] = true
		if r.ConversationID == nil || *r.ConversationID != 42 {
			t.Fatalf("source conversation recorded: %+v", r)
		}
	}
	if len(rows) != 2 || !got["用户喜欢猫耳和兽耳"] || !got["用户用 Steam Deck 玩"] {
		t.Fatalf("%v", got)
	}
	if theirs, _ := s.Memories(ctx, 8, 14); len(theirs) != 1 {
		t.Fatal("ops are scoped to the user")
	}
}

func TestPreferencesDefaultOnAndPersist(t *testing.T) {
	resetAgent(t)
	ctx := context.Background()
	s := NewStore(testDB)
	if p, _ := s.Preferences(ctx, 7, 14); !p.Memory || !p.History {
		t.Fatalf("both on by default: %+v", p)
	}
	off := false
	if _, err := s.SetPreferences(ctx, 7, 14, &off, nil); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.Preferences(ctx, 7, 14); p.Memory || !p.History {
		t.Fatalf("only memory turned off: %+v", p)
	}
	if p, _ := s.Preferences(ctx, 7, 15); !p.Memory {
		t.Fatal("per site")
	}
}

func TestPastSearchFusesWordsAndMeaning(t *testing.T) {
	resetAgent(t)
	ctx := context.Background()
	s := NewStore(testDB)
	conv := func(title, q, a string, vec []float32) int64 {
		c, _ := s.CreateConversation(ctx, 7, 14, model.ModeAssistant, nil)
		_ = s.RenameConversation(ctx, c, title)
		run, _, err := s.StartRun(ctx, StartRequest{Conversation: c, Parts: []Part{{Type: "text", Text: q}}, Mode: model.ModeAssistant, IdemKey: title})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.FinishRun(ctx, run, Finish{Status: model.RunCompleted, Parts: []Part{{Type: "text", Text: a},
			{Type: "data-entity-card", ID: "c1", Data: json.RawMessage(`{"kind":"work","id":"1","title":"` + strings.Trim(a, "《》") + `"}`)}}}); err != nil {
			t.Fatal(err)
		}
		if vec != nil {
			if err := s.SaveEmbeddings(ctx, []model.Embedding{{MessageID: run.AssistantMessageID, ConversationID: c.ID, UserID: 7, SiteID: 14, Vector: shorten(vec, embedDims)}}); err != nil {
				t.Fatal(err)
			}
		}
		return c.ID
	}
	tears := conv("催泪作品推荐", "推荐几部催泪的", "《Summer Pockets》", []float32{1, 0, 0})
	cats := conv("猫耳角色", "有没有猫耳妹妹", "《NEKOPARA》", []float32{0, 1, 0})
	current := conv("当前对话", "催泪", "嗯", []float32{1, 0, 0})

	words, err := s.keywordHits(ctx, 7, 14, current, queryTerms("催泪 Summer"), 10)
	if err != nil || len(words) != 1 || words[0].ConversationID != tears || words[0].Snippet != "推荐几部催泪的" {
		t.Fatalf("both terms hit the tears chat, never the current one: %+v %v", words, err)
	}
	meaning, err := s.vectorHits(ctx, 7, 14, current, shorten([]float32{0.1, 1, 0}, embedDims), 10)
	if err != nil || len(meaning) != 1 || meaning[0].ConversationID != cats || meaning[0].Snippet != "有没有猫耳妹妹" {
		t.Fatalf("only the close vector passes the score floor: %+v %v", meaning, err)
	}
	if hits, err := s.SearchPast(ctx, nil, 7, 14, current, "催泪", 5); err != nil || len(hits) != 1 || len(hits[0].Shown) != 1 || hits[0].Shown[0] != "Summer Pockets" {
		t.Fatalf("a hit names what that conversation showed: %+v %v", hits, err)
	}
	fused := fuse(5, words, meaning)
	if len(fused) != 2 {
		t.Fatalf("%+v", fused)
	}
	if hits, _ := s.keywordHits(ctx, 8, 14, 0, []string{"催泪"}, 10); len(hits) != 0 {
		t.Fatal("another user's conversations never match")
	}

	past, err := s.ReadPast(ctx, 7, 14, tears)
	if err != nil || len(past.Messages) != 2 || past.Summary != nil {
		t.Fatalf("%+v %v", past, err)
	}
	if err := s.SaveSummary(ctx, &model.Summary{ConversationID: tears, UptoMessageID: past.Messages[0].ID, Content: "问了催泪作"}); err != nil {
		t.Fatal(err)
	}
	if past, _ := s.ReadPast(ctx, 7, 14, tears); past.Summary == nil || len(past.Messages) != 1 {
		t.Fatalf("summary replaces what it covers: %+v", past)
	}

	c, _ := s.Conversation(ctx, 7, 14, tears)
	_, _ = s.AddMemory(ctx, 7, 14, "用户喜欢催泪作", model.MemoryAuto, &tears, 10)
	if err := s.DeleteConversation(ctx, c); err != nil {
		t.Fatal(err)
	}
	var left int64
	testDB.Raw(`SELECT (SELECT count(*) FROM agent_summary WHERE conversation_id = ?) + (SELECT count(*) FROM agent_embedding WHERE conversation_id = ?)
		+ (SELECT count(*) FROM agent_memory WHERE conversation_id = ?)`, tears, tears, tears).Scan(&left)
	if mems, _ := s.Memories(ctx, 7, 14); left != 0 || len(mems) != 1 {
		t.Fatalf("deleting a conversation takes its summaries and vectors, and keeps its memories unlinked: %d left, %d memories", left, len(mems))
	}
}

func TestClaimIdleSkipsBusyRecentAndBackedOffConversations(t *testing.T) {
	resetAgent(t)
	ctx := context.Background()
	s := NewStore(testDB)
	now := time.Now()
	mk := func(updated time.Time, running bool) int64 {
		c, _ := s.CreateConversation(ctx, 7, 14, model.ModeAssistant, nil)
		run, _, err := s.StartRun(ctx, StartRequest{Conversation: c, Parts: []Part{{Type: "text", Text: "hi"}}, Mode: model.ModeAssistant, IdemKey: updated.String() + c.Title + string(rune(c.ID))})
		if err != nil {
			t.Fatal(err)
		}
		if !running {
			_ = s.FinishRun(ctx, run, Finish{Status: model.RunCompleted})
		}
		testDB.Exec("UPDATE agent_conversation SET updated_at = ? WHERE id = ?", updated, c.ID)
		return c.ID
	}
	quiet := mk(now.Add(-time.Hour), false)
	busy := mk(now.Add(-time.Hour), true)
	fresh := mk(now, false)
	waiting := mk(now.Add(-time.Hour), false)

	got, err := s.claimIdle(ctx, now.Add(-30*time.Minute), []int64{0, waiting}, 10)
	if err != nil || len(got) != 1 || got[0].ID != quiet || got[0].Prev != nil {
		t.Fatalf("only the quiet, finished, not backed-off conversation (busy %d, fresh %d): %+v %v", busy, fresh, got, err)
	}
	if again, _ := s.claimIdle(ctx, now.Add(-30*time.Minute), []int64{0, waiting}, 10); len(again) != 0 {
		t.Fatalf("read once until it changes: %+v", again)
	}
	if err := s.unscan(ctx, quiet, nil); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.claimIdle(ctx, now.Add(-30*time.Minute), []int64{0}, 10); len(again) != 2 {
		t.Fatalf("unscanned and no longer backed off: %+v", again)
	}
}
