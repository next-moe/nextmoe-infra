package service

import (
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
