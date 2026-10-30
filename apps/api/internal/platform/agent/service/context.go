package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"api/internal/platform/agent/model"
	"api/internal/platform/llm"
	"api/internal/platform/settings/keys"
)

const (
	imageTokens     = 1000
	keepAttachments = 3
	foldMessageCap  = 3000
)

func estimateTokens(s string) int {
	ascii, wide := 0, 0
	for _, r := range s {
		if r < utf8.RuneSelf {
			ascii++
		} else {
			wide++
		}
	}
	return ascii/4 + wide + 4
}

type rendered struct {
	msg    model.Message
	text   string
	cards  string
	images []string
	tokens int
}

func afterSummary(history []model.Message, sum *model.Summary) ([]model.Message, string) {
	if sum == nil {
		return history, ""
	}
	for i, m := range history {
		if m.ID == sum.UptoMessageID {
			return history[i+1:], sum.Content
		}
	}
	return history, ""
}

func render(history []model.Message, uploads map[string]*model.Upload) []rendered {
	out := make([]rendered, len(history))
	users := 0
	for i := len(history) - 1; i >= 0; i-- {
		m := history[i]
		ups := uploads
		if m.Role == model.RoleUser {
			users++
			if users > keepAttachments {
				ups = nil
			}
		}
		parts := decodeParts(m)
		text, images := hydrate(parts, ups)
		cards := cardList(parts)
		out[i] = rendered{msg: m, text: text, cards: cards, images: images, tokens: estimateTokens(text+cards) + imageTokens*len(images)}
	}
	return out
}

func window(history []model.Message, sum *model.Summary, uploads map[string]*model.Upload, budget int) ([]rendered, string) {
	rest, summary := afterSummary(history, sum)
	all := render(rest, uploads)
	left := budget - estimateTokens(summary)
	start := len(all)
	for start > 0 && (start == len(all) || left-all[start-1].tokens >= 0) {
		left -= all[start-1].tokens
		start--
	}
	for start < len(all)-1 && all[start].msg.Role != model.RoleUser {
		start++
	}
	return all[start:], summary
}

func foldPlan(history []model.Message, sum *model.Summary, uploads map[string]*model.Upload, budget int) []model.Message {
	rest, summary := afterSummary(history, sum)
	all := render(rest, uploads)
	total := estimateTokens(summary)
	for _, r := range all {
		total += r.tokens
	}
	if total <= budget*3/4 {
		return nil
	}
	cut, lastUser, tail := -1, -1, 0
	for i := len(all) - 1; i >= 0; i-- {
		tail += all[i].tokens
		if all[i].msg.Role != model.RoleUser {
			continue
		}
		if lastUser < 0 {
			lastUser = i
		}
		if tail > budget/2 {
			break
		}
		cut = i
	}
	if cut < 0 {
		cut = lastUser
	}
	if cut <= 0 {
		return nil
	}
	return rest[:cut]
}

func foldInput(previous string, fold []model.Message) string {
	var b strings.Builder
	if previous != "" {
		fmt.Fprintf(&b, "〔已有摘要〕\n%s\n〔已有摘要结束〕\n\n", previous)
	}
	b.WriteString("〔新的对话〕")
	for _, m := range fold {
		text := clipRunes(messageText(m, nil), foldMessageCap)
		if text == "" {
			continue
		}
		fmt.Fprintf(&b, "\n%s：%s", speaker(m.Role), text)
	}
	b.WriteString("\n〔新的对话结束〕")
	return b.String()
}

func speaker(role string) string {
	if role == model.RoleAssistant {
		return "助手"
	}
	return "用户"
}

func sceneOr(gw *llm.Gateway, preferred, fallback string) string {
	if _, ok := gw.Ready(preferred); ok {
		return preferred
	}
	return fallback
}

func (r *Runner) compact(t Turn, reply []Part) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	parts, _ := json.Marshal(reply)
	branch := append(append([]model.Message{}, t.History...), model.Message{ID: t.Run.AssistantMessageID, ConversationID: t.Run.ConversationID,
		Role: model.RoleAssistant, Parts: parts, Status: model.MessageComplete})
	fold := foldPlan(branch, t.Summary, t.Uploads, int(keys.AgentContextMaxTokens.Get()))
	if len(fold) == 0 {
		return
	}
	previous := ""
	if t.Summary != nil {
		previous = t.Summary.Content
	}
	res, err := r.gw.Stream(ctx, llm.Call{
		Scene: sceneOr(r.gw, llm.SceneSummary, llm.SceneAssistant), Buffered: true, MaxTokens: 2048, Site: t.SiteName, UserID: t.Run.UserID,
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: keys.AgentPromptSummary.Get()},
			{Role: llm.RoleUser, Content: foldInput(previous, fold)},
		},
	}, nil)
	content := strings.TrimSpace(stripThink(res.Text))
	if err != nil || content == "" {
		slog.Warn("agent: summarize", "conversation", t.Run.ConversationID, "err", err)
		return
	}
	if err := r.store.SaveSummary(ctx, &model.Summary{ConversationID: t.Run.ConversationID, UptoMessageID: fold[len(fold)-1].ID, Content: content}); err != nil {
		slog.Warn("agent: save summary", "conversation", t.Run.ConversationID, "err", err)
	}
}

func stripThink(s string) string {
	if i := strings.Index(s, "</think>"); i >= 0 {
		return s[i+len("</think>"):]
	}
	return s
}
