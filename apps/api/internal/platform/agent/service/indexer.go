package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"api/internal/platform/agent/model"
	"api/internal/platform/llm"
	"api/internal/platform/settings/keys"
)

const (
	idleBatch        = 20
	embedBatch       = 16
	extractUserRunes = 1500
	extractBotRunes  = 300
	extractRunes     = 12000
)

type Indexer struct {
	store *Store
	gw    *llm.Gateway

	mu     sync.Mutex
	failed map[int64]failure
}

func NewIndexer(store *Store, gw *llm.Gateway) *Indexer {
	return &Indexer{store: store, gw: gw, failed: map[int64]failure{}}
}

type idleConversation struct {
	ID            int64
	UserID        int64
	SiteID        int64
	HeadMessageID *int64
	Prev          *time.Time
}

func (s *Store) claimIdle(ctx context.Context, quietSince time.Time, skip []int64, limit int) ([]idleConversation, error) {
	var out []idleConversation
	err := s.db.WithContext(ctx).Raw(`WITH picked AS (
			SELECT c.id, c.scanned_at AS prev FROM agent_conversation c
			WHERE c.updated_at < ? AND (c.scanned_at IS NULL OR c.scanned_at < c.updated_at) AND c.id NOT IN ?
				AND NOT EXISTS (SELECT 1 FROM agent_run r WHERE r.conversation_id = c.id AND r.status = ?)
			ORDER BY c.updated_at LIMIT ? FOR UPDATE SKIP LOCKED
		) UPDATE agent_conversation c SET scanned_at = ? FROM picked WHERE c.id = picked.id
		RETURNING c.id, c.user_id, c.site_id, c.head_message_id, picked.prev`, quietSince, skip, model.RunRunning, limit, s.now()).Scan(&out).Error
	return out, err
}

func (s *Store) unscan(ctx context.Context, id int64, prev *time.Time) error {
	return s.db.WithContext(ctx).Model(&model.Conversation{}).Where("id = ?", id).UpdateColumn("scanned_at", prev).Error
}

func (x *Indexer) Run(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		for {
			n, err := x.Tick(ctx)
			if err != nil {
				slog.Warn("agent: idle conversations", "err", err)
			}
			if err != nil || n < idleBatch {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (x *Indexer) Tick(ctx context.Context) (int, error) {
	quiet := time.Duration(keys.AgentMemoryIdleMinutes.Get()) * time.Minute
	n := 0
	for ; n < idleBatch; n++ {
		now := x.store.now()
		// A batch claimed up front was marked read before it was read: a
		// restart in the middle lost every conversation still in the batch.
		convs, err := x.store.claimIdle(ctx, now.Add(-quiet), x.waiting(now), 1)
		if err != nil || len(convs) == 0 {
			return n, err
		}
		c := convs[0]
		err = x.process(ctx, c)
		x.mu.Lock()
		if err == nil {
			delete(x.failed, c.ID)
		} else {
			f := x.failed[c.ID]
			f.n++
			f.at = now.Add(min(time.Duration(1<<min(f.n, 6))*5*time.Minute, 6*time.Hour))
			x.failed[c.ID] = f
		}
		x.mu.Unlock()
		if err != nil {
			slog.Warn("agent: read idle conversation", "conversation", c.ID, "err", err)
			if err := x.store.unscan(context.WithoutCancel(ctx), c.ID, c.Prev); err != nil {
				slog.Warn("agent: unmark conversation", "conversation", c.ID, "err", err)
			}
		}
	}
	return n, nil
}

type failure struct {
	n  int
	at time.Time
}

func (x *Indexer) waiting(now time.Time) []int64 {
	x.mu.Lock()
	defer x.mu.Unlock()
	out := []int64{0}
	for id, f := range x.failed {
		if f.at.After(now) {
			out = append(out, id)
		}
	}
	return out
}

func (x *Indexer) process(ctx context.Context, c idleConversation) error {
	site := uint(c.SiteID)
	prefs, err := x.store.Preferences(ctx, c.UserID, c.SiteID)
	if err != nil {
		return err
	}
	branch, err := x.store.Branch(ctx, c.HeadMessageID)
	if err != nil {
		return err
	}
	if prefs.History && keys.AgentHistorySearch.ForSite(site) {
		if err := x.index(ctx, c, branch); err != nil {
			slog.Warn("agent: index conversation for search; retried when it is read again", "conversation", c.ID, "err", err)
		}
	}
	if prefs.Memory && keys.AgentMemoryEnabled.ForSite(site) && keys.AgentMemoryAuto.ForSite(site) {
		if err := x.extract(ctx, c, branch); err != nil {
			return fmt.Errorf("extract: %w", err)
		}
	}
	return nil
}

func (x *Indexer) index(ctx context.Context, c idleConversation, branch []model.Message) error {
	byID := map[int64]model.Message{}
	var answers []int64
	for _, m := range branch {
		byID[m.ID] = m
		if m.Role == model.RoleAssistant && m.Status == model.MessageComplete && m.ParentID != nil {
			answers = append(answers, m.ID)
		}
	}
	missing, err := x.store.Unembedded(ctx, answers)
	if err != nil {
		return err
	}
	var todo []model.Message
	for _, id := range answers {
		if missing[id] {
			todo = append(todo, byID[id])
		}
	}
	for len(todo) > 0 {
		batch := todo[:min(embedBatch, len(todo))]
		todo = todo[len(batch):]
		texts := make([]string, len(batch))
		for i, a := range batch {
			texts[i] = exchangeText(byID[*a.ParentID], a)
		}
		vecs, err := embed(ctx, x.gw, texts)
		if err != nil {
			return err
		}
		rows := make([]model.Embedding, len(batch))
		for i, a := range batch {
			rows[i] = model.Embedding{MessageID: a.ID, ConversationID: c.ID, UserID: c.UserID, SiteID: c.SiteID, Vector: model.Vector(vecs[i])}
		}
		if err := x.store.SaveEmbeddings(ctx, rows); err != nil {
			return err
		}
	}
	return nil
}

func (x *Indexer) extract(ctx context.Context, c idleConversation, branch []model.Message) error {
	var fresh []model.Message
	asked := false
	for _, m := range branch {
		if m.Status != model.MessageComplete || (c.Prev != nil && !m.CreatedAt.After(*c.Prev)) {
			continue
		}
		fresh = append(fresh, m)
		asked = asked || m.Role == model.RoleUser
	}
	if !asked {
		return nil
	}
	mems, err := x.store.Memories(ctx, c.UserID, c.SiteID)
	if err != nil {
		return err
	}
	res, err := x.gw.Stream(ctx, llm.Call{
		Scene: sceneOr(x.gw, llm.SceneMemory, llm.SceneAssistant), Buffered: true, MaxTokens: 1024, Site: "agent", UserID: c.UserID,
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: keys.AgentPromptExtract.Get()},
			{Role: llm.RoleUser, Content: extractInput(mems, fresh)},
		},
	}, nil)
	if err != nil {
		return err
	}
	ops, err := parseMemoryOps(res.Text)
	if err != nil {
		return err
	}
	var said strings.Builder
	for _, m := range fresh {
		if m.Role == model.RoleUser {
			said.WriteString(messageText(m, nil) + "\n")
		}
	}
	ops = ops.Grounded(said.String())
	return x.store.ApplyMemoryOps(ctx, c.UserID, c.SiteID, c.ID, ops, int(keys.AgentMemoryMaxItems.Get()))
}

func extractInput(mems []model.Memory, fresh []model.Message) string {
	var b strings.Builder
	b.WriteString("〔现有记忆〕")
	if len(mems) == 0 {
		b.WriteString("\n（还没有）")
	}
	for _, m := range mems {
		lock := ""
		if m.Origin == model.MemoryExplicit {
			lock = "（用户亲自要求记住的，不能改动或删除）"
		}
		fmt.Fprintf(&b, "\n#%d %s%s", m.ID, m.Content, lock)
	}
	b.WriteString("\n〔现有记忆结束〕\n\n〔最近的对话，只有「用户」说的话可以成为记忆〕")
	var lines []string
	for _, m := range fresh {
		label, limit := "用户", extractUserRunes
		if m.Role == model.RoleAssistant {
			label, limit = "助手（只用来理解上下文，不是用户的意思）", extractBotRunes
		}
		if text := clipRunes(messageText(m, nil), limit); text != "" {
			lines = append(lines, label+"："+text)
		}
	}
	body := strings.Join(lines, "\n")
	if r := []rune(body); len(r) > extractRunes {
		body = string(r[len(r)-extractRunes:])
	}
	b.WriteString("\n" + body + "\n〔对话结束〕")
	return b.String()
}

func parseMemoryOps(text string) (MemoryOps, error) {
	var ops MemoryOps
	text = stripThink(text)
	from, to := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if from < 0 || to < from {
		if strings.TrimSpace(text) == "" {
			return ops, nil
		}
		return ops, errors.New("agent: memory extraction answered without JSON")
	}
	if err := json.Unmarshal([]byte(text[from:to+1]), &ops); err != nil {
		return ops, fmt.Errorf("agent: memory extraction answered %q: %w", clipRunes(text, 300), err)
	}
	return ops, nil
}
