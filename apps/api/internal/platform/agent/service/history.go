package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"api/internal/platform/agent/model"
	"api/internal/platform/llm"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	embedDims     = 256
	embedText     = 1200
	rrfK          = 60
	minVecScore   = 0.45
	snippetRunes  = 120
	maxQueryTerms = 5
)

const queryInstruct = "Instruct: Given a question about the user's earlier chats, retrieve the chat exchange it refers to\nQuery: "

func (s *Store) SaveSummary(ctx context.Context, sum *model.Summary) error {
	sum.CreatedAt = s.now()
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(sum).Error
}

func (s *Store) BranchSummary(ctx context.Context, branch []model.Message) (*model.Summary, error) {
	if len(branch) == 0 {
		return nil, nil
	}
	ids := make([]int64, len(branch))
	for i, m := range branch {
		ids[i] = m.ID
	}
	var sum model.Summary
	err := s.db.WithContext(ctx).Where("upto_message_id IN ?", ids).Order("upto_message_id DESC").Take(&sum).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &sum, err
}

type PastHit struct {
	ConversationID int64
	Title          string
	UpdatedAt      time.Time
	Snippet        string
	Shown          []string
	score          float64
}

const maxShown = 10

func (s *Store) shownIn(ctx context.Context, hits []PastHit) error {
	if len(hits) == 0 {
		return nil
	}
	ids := make([]int64, len(hits))
	for i, h := range hits {
		ids[i] = h.ConversationID
	}
	var rows []struct {
		ConversationID int64
		Title          string
	}
	if err := s.db.WithContext(ctx).Raw(`SELECT DISTINCT ON (m.conversation_id, p->'data'->>'title') m.conversation_id, p->'data'->>'title' AS title
		FROM agent_message m, jsonb_array_elements(m.parts) p
		WHERE m.conversation_id IN ? AND m.role = ? AND p->>'type' = 'data-entity-card' AND coalesce(p->'data'->>'title', '') <> ''
		ORDER BY m.conversation_id, p->'data'->>'title', m.id`, ids, model.RoleAssistant).Scan(&rows).Error; err != nil {
		return err
	}
	at := map[int64]int{}
	for i, h := range hits {
		at[h.ConversationID] = i
	}
	for _, r := range rows {
		if h := &hits[at[r.ConversationID]]; len(h.Shown) < maxShown {
			h.Shown = append(h.Shown, r.Title)
		}
	}
	return nil
}

func queryTerms(q string) []string {
	var out []string
	for _, t := range strings.Fields(q) {
		if len(out) == maxQueryTerms {
			break
		}
		out = append(out, t)
	}
	return out
}

func (s *Store) keywordHits(ctx context.Context, userID, siteID, except int64, terms []string, limit int) ([]PastHit, error) {
	if len(terms) == 0 {
		return nil, nil
	}
	var hits, ors []string
	var args, anyArgs []any
	for _, t := range terms {
		like := "%" + likeEscaper.Replace(t) + "%"
		hits = append(hits, `(CASE WHEN c.title ILIKE ? OR EXISTS (SELECT 1 FROM agent_message m, jsonb_array_elements(m.parts) p
			WHERE m.conversation_id = c.id AND p->>'type' = 'text' AND p->>'text' ILIKE ?) THEN 1 ELSE 0 END)`)
		ors = append(ors, "p->>'text' ILIKE ?")
		args = append(args, like, like)
		anyArgs = append(anyArgs, like)
	}
	args = append(append(args, anyArgs...), userID, siteID, except, limit)
	var rows []struct {
		ID        int64
		Title     string
		UpdatedAt time.Time
		Hits      int
		Snippet   string
	}
	err := s.db.WithContext(ctx).Raw(`SELECT * FROM (
			SELECT c.id, c.title, c.updated_at, `+strings.Join(hits, " + ")+` AS hits,
				coalesce((SELECT p->>'text' FROM agent_message m, jsonb_array_elements(m.parts) p
					WHERE m.conversation_id = c.id AND p->>'type' = 'text' AND (`+strings.Join(ors, " OR ")+`)
					ORDER BY m.role = 'user' DESC, m.id LIMIT 1), '') AS snippet
			FROM agent_conversation c WHERE c.user_id = ? AND c.site_id = ? AND c.id <> ?
		) x WHERE hits > 0 ORDER BY hits DESC, updated_at DESC LIMIT ?`, args...).Scan(&rows).Error
	out := make([]PastHit, len(rows))
	for i, r := range rows {
		out[i] = PastHit{ConversationID: r.ID, Title: r.Title, UpdatedAt: r.UpdatedAt, Snippet: around(r.Snippet, terms)}
	}
	return out, err
}

func around(text string, terms []string) string {
	runes := []rune(text)
	at := 0
	lower := strings.ToLower(text)
	for _, t := range terms {
		if i := strings.Index(lower, strings.ToLower(t)); i >= 0 {
			at = len([]rune(text[:i]))
			break
		}
	}
	from := max(at-snippetRunes/3, 0)
	to := min(from+snippetRunes, len(runes))
	out := strings.Join(strings.Fields(string(runes[from:to])), " ")
	if from > 0 {
		out = "…" + out
	}
	if to < len(runes) {
		out += "…"
	}
	return out
}

func (s *Store) vectorHits(ctx context.Context, userID, siteID, except int64, vec []float32, limit int) ([]PastHit, error) {
	var rows []struct {
		ConversationID int64
		Title          string
		UpdatedAt      time.Time
		Score          float64
		Question       json.RawMessage
	}
	err := s.db.WithContext(ctx).Raw(`SELECT e.conversation_id, c.title, c.updated_at, e.score, q.parts AS question
		FROM (SELECT message_id, conversation_id, (SELECT sum(a * b) FROM unnest(vector, ?::real[]) AS t(a, b)) AS score
			FROM agent_embedding WHERE user_id = ? AND site_id = ? AND conversation_id <> ?) e
		JOIN agent_conversation c ON c.id = e.conversation_id
		JOIN agent_message a ON a.id = e.message_id
		LEFT JOIN agent_message q ON q.id = a.parent_id
		WHERE e.score >= ? ORDER BY e.score DESC LIMIT ?`, model.Vector(vec), userID, siteID, except, minVecScore, limit*4).Scan(&rows).Error
	var out []PastHit
	seen := map[int64]bool{}
	for _, r := range rows {
		if seen[r.ConversationID] || len(out) == limit {
			continue
		}
		seen[r.ConversationID] = true
		var parts []Part
		_ = json.Unmarshal(r.Question, &parts)
		out = append(out, PastHit{ConversationID: r.ConversationID, Title: r.Title, UpdatedAt: r.UpdatedAt, score: r.Score,
			Snippet: clipRunes(strings.Join(strings.Fields(partsText(parts)), " "), snippetRunes)})
	}
	return out, err
}

func fuse(limit int, lists ...[]PastHit) []PastHit {
	byID := map[int64]*PastHit{}
	for _, list := range lists {
		for rank, h := range list {
			cur := byID[h.ConversationID]
			if cur == nil {
				h := h
				h.score = 0
				byID[h.ConversationID] = &h
				cur = &h
			}
			cur.score += 1 / float64(rrfK+rank+1)
			if cur.Snippet == "" {
				cur.Snippet = h.Snippet
			}
		}
	}
	out := make([]PastHit, 0, len(byID))
	for _, h := range byID {
		out = append(out, *h)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *Store) SearchPast(ctx context.Context, gw *llm.Gateway, userID, siteID, except int64, query string, limit int) ([]PastHit, error) {
	words, err := s.keywordHits(ctx, userID, siteID, except, queryTerms(query), limit*2)
	if err != nil {
		return nil, err
	}
	var meaning []PastHit
	if gw != nil {
		if vecs, err := embed(ctx, gw, []string{queryInstruct + query}); err == nil && len(vecs) == 1 {
			if meaning, err = s.vectorHits(ctx, userID, siteID, except, vecs[0], limit*2); err != nil {
				return nil, err
			}
		}
	}
	hits := fuse(limit, words, meaning)
	return hits, s.shownIn(ctx, hits)
}

func embed(ctx context.Context, gw *llm.Gateway, texts []string) ([][]float32, error) {
	input, _ := json.Marshal(texts)
	res, err := gw.Embed(ctx, llm.Request{Scene: sceneOr(gw, llm.SceneAgentEmbed, llm.SceneCatalogEmbed), Site: "agent",
		Body: map[string]json.RawMessage{"input": input}})
	if err != nil {
		return nil, err
	}
	var body struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Body, &body); err != nil {
		return nil, err
	}
	if len(body.Data) != len(texts) {
		return nil, fmt.Errorf("agent: %d embeddings for %d texts", len(body.Data), len(texts))
	}
	out := make([][]float32, len(texts))
	for i, d := range body.Data {
		idx := d.Index
		if idx < 0 || idx >= len(out) {
			idx = i
		}
		out[idx] = shorten(d.Embedding, embedDims)
	}
	return out, nil
}

func shorten(v []float32, dims int) []float32 {
	v = v[:min(dims, len(v))]
	var norm float64
	for _, x := range v {
		norm += float64(x) * float64(x)
	}
	norm = math.Sqrt(norm)
	out := make([]float32, len(v))
	for i, x := range v {
		if norm > 0 {
			out[i] = float32(float64(x) / norm)
		}
	}
	return out
}

func exchangeText(question, answer model.Message) string {
	return clipRunes("问："+messageText(question, nil)+"\n答："+messageText(answer, nil), embedText)
}

func (s *Store) Unembedded(ctx context.Context, ids []int64) (map[int64]bool, error) {
	out := map[int64]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	var have []int64
	if err := s.db.WithContext(ctx).Model(&model.Embedding{}).Where("message_id IN ?", ids).Pluck("message_id", &have).Error; err != nil {
		return nil, err
	}
	for _, id := range ids {
		out[id] = true
	}
	for _, id := range have {
		delete(out, id)
	}
	return out, nil
}

func (s *Store) SaveEmbeddings(ctx context.Context, rows []model.Embedding) error {
	if len(rows) == 0 {
		return nil
	}
	for i := range rows {
		rows[i].CreatedAt = s.now()
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}

type PastConversation struct {
	Conversation model.Conversation
	Summary      *model.Summary
	Messages     []model.Message
}

func (s *Store) ReadPast(ctx context.Context, userID, siteID, id int64) (*PastConversation, error) {
	c, err := s.Conversation(ctx, userID, siteID, id)
	if err != nil {
		return nil, err
	}
	branch, err := s.Branch(ctx, c.HeadMessageID)
	if err != nil {
		return nil, err
	}
	sum, err := s.BranchSummary(ctx, branch)
	if err != nil {
		return nil, err
	}
	rest, _ := afterSummary(branch, sum)
	return &PastConversation{Conversation: *c, Summary: sum, Messages: rest}, nil
}
