package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"api/internal/platform/agent/model"
	"api/internal/platform/settings"
	"api/internal/platform/settings/keys"
)

func TestSystemPromptComesFromTheSettingsCenter(t *testing.T) {
	settings.Override(t, keys.AgentPromptRules, "自定义规则：只聊游戏。")
	settings.Override(t, keys.AgentPromptWeb, "")
	turn := Turn{Persona: &model.Persona{Prompt: "你是测试娘。"}, Run: &model.Run{SiteID: 3, WebSearch: true}, Web: true}
	msgs, _ := buildMessages(turn, time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC))
	system := msgs[0].Content
	for _, want := range []string{"你是测试娘。\n\n自定义规则：只聊游戏。", "先用 web_search 查最新信息", "今天是 2026-10-29"} {
		if !strings.Contains(system, want) {
			t.Fatalf("system prompt lacks %q:\n%s", want, system)
		}
	}
	if strings.Contains(system, "资料库里没有的内容") || strings.Contains(system, "\n\n\n") {
		t.Fatalf("an emptied rule must leave nothing behind:\n%s", system)
	}
	if strings.Contains(system, "其他对话") {
		t.Fatalf("no titles, no section:\n%s", system)
	}

	turn.Titles = []PastTitle{{Title: "催泪作品推荐", At: time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)}}
	msgs, _ = buildMessages(turn, time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC))
	if !strings.Contains(msgs[0].Content, "〔用户在本站的其他对话，由近到远，只有标题〕\n- 2026-10-28 催泪作品推荐\n〔其他对话结束〕") {
		t.Fatalf("past titles are fenced as data:\n%s", msgs[0].Content)
	}
}

func TestMemoryAndSummaryReachTheSystemPrompt(t *testing.T) {
	settings.Override(t, keys.AgentPromptMemory, "记忆规则。")
	settings.Override(t, keys.AgentPromptHistory, "翻旧账规则。")
	turn := Turn{Persona: &model.Persona{Prompt: "你是测试娘。"}, Run: &model.Run{SiteID: 3},
		History: chat(5, 10), Summary: &model.Summary{UptoMessageID: 2, Content: "用户想要催泪作"}}
	msgs, _ := buildMessages(turn, time.Now())
	if s := msgs[0].Content; strings.Contains(s, "记忆") || strings.Contains(s, "翻旧账") {
		t.Fatalf("memory off, history off, neither rule nor section:\n%s", s)
	}
	if len(msgs) != 4 || !strings.Contains(msgs[0].Content, "〔本对话较早部分的摘要，之后的消息保持原文〕\n用户想要催泪作\n〔摘要结束〕") {
		t.Fatalf("the summary stands in for messages 1-2: %d messages\n%s", len(msgs), msgs[0].Content)
	}

	turn.Memory, turn.PastChats = true, true
	msgs, _ = buildMessages(turn, time.Now())
	if s := msgs[0].Content; !strings.Contains(s, "记忆规则。\n\n翻旧账规则。") || !strings.Contains(s, "〔关于用户的记忆，编号供 remember / forget 使用〕\n（还没有）") {
		t.Fatalf("an empty memory still tells the model it may remember:\n%s", s)
	}
	turn.Memories = []model.Memory{{ID: 12, Content: "用户讨厌 NTR"}}
	msgs, _ = buildMessages(turn, time.Now())
	if !strings.Contains(msgs[0].Content, "\n- #12 用户讨厌 NTR\n〔记忆结束〕") {
		t.Fatalf("memories carry their ids:\n%s", msgs[0].Content)
	}
}

func TestShownCardsRideOnTheNextQuestion(t *testing.T) {
	reply, _ := json.Marshal([]Part{
		{Type: "text", Text: "推荐《心跳文学部》。"},
		{Type: "data-entity-card", ID: "letmoe:game-148", Data: json.RawMessage(`{"kind":"letmoe:game","id":"148","title":"心跳文学部"}`)},
	})
	history := []model.Message{msg(1, model.RoleUser, "手机上能玩什么"), {ID: 2, Role: model.RoleAssistant, Parts: reply}, msg(3, model.RoleUser, "有汉化吗")}
	msgs, _ := buildMessages(Turn{Persona: &model.Persona{}, Run: &model.Run{}, History: history}, time.Now())
	if len(msgs) != 4 || msgs[2].Content != "推荐《心跳文学部》。" ||
		msgs[3].Content != "〔上一条回答下方给用户展示了这些卡片：心跳文学部(letmoe:game id 148)〕\n有汉化吗" {
		t.Fatalf("the reply stays clean and the next question carries its cards: %+v", msgs[1:])
	}
	if got := messageText(history[1], nil); got != "推荐《心跳文学部》。\n[本轮展示的卡片：心跳文学部(letmoe:game id 148)]" {
		t.Fatalf("summaries and past-conversation reads keep the card note: %q", got)
	}
}

func TestSiteComesFirstAndItsToolsBringTheirNotes(t *testing.T) {
	settings.Override(t, keys.AgentPromptSiteTools, "站点工具规则。")
	turn := Turn{Persona: &model.Persona{Prompt: "你是本站的看板娘。"}, Run: &model.Run{SiteID: 14}, SiteName: "LetMoe·一起萌"}
	msgs, _ := buildMessages(turn, time.Now())
	if s := msgs[0].Content; !strings.HasPrefix(s, "你在「LetMoe·一起萌」为用户服务，「本站」指的就是这个站点。\n\n你是本站的看板娘。") || strings.Contains(s, "站点工具规则") {
		t.Fatalf("the site is named before the persona; no site tools, no rule:\n%s", s)
	}
	turn.SiteTools, turn.SiteNotes = true, []string{"这些工具查的是 LetMoe 的资源。"}
	msgs, _ = buildMessages(turn, time.Now())
	if s := msgs[0].Content; !strings.Contains(s, "站点工具规则。\n\n这些工具查的是 LetMoe 的资源。") {
		t.Fatalf("the rule, then what the server says:\n%s", s)
	}
}
