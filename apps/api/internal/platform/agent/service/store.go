package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"api/internal/platform/agent/model"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

var (
	ErrNotFound        = errors.New("agent: not found")
	ErrRunActive       = errors.New("agent: a reply is still streaming in this conversation")
	ErrIdempotencyUsed = errors.New("agent: idempotency key reused with a different request")
	ErrBadParent       = errors.New("agent: parent message is not in this conversation")
)

type Part struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	MediaType string          `json:"mediaType,omitempty"`
	Filename  string          `json:"filename,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
}

type Store struct {
	db  *gorm.DB
	now func() time.Time
}

func NewStore(db *gorm.DB) *Store { return &Store{db: db, now: time.Now} }

func (s *Store) CreateConversation(ctx context.Context, userID, siteID int64, mode string, personaID *int64) (*model.Conversation, error) {
	c := &model.Conversation{UserID: userID, SiteID: siteID, Mode: mode, PersonaID: personaID}
	return c, s.db.WithContext(ctx).Create(c).Error
}

func (s *Store) Conversation(ctx context.Context, userID, siteID, id int64) (*model.Conversation, error) {
	var c model.Conversation
	err := s.db.WithContext(ctx).Where("id = ? AND user_id = ? AND site_id = ?", id, userID, siteID).Take(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &c, err
}

func (s *Store) ListConversations(ctx context.Context, userID, siteID int64, before time.Time, beforeID int64, limit int) ([]model.Conversation, error) {
	q := s.db.WithContext(ctx).Where("user_id = ? AND site_id = ?", userID, siteID)
	if !before.IsZero() {
		q = q.Where("(updated_at, id) < (?, ?)", before, beforeID)
	}
	var out []model.Conversation
	return out, q.Order("updated_at DESC, id DESC").Limit(limit).Find(&out).Error
}

func (s *Store) RenameConversation(ctx context.Context, c *model.Conversation, title string) error {
	return s.db.WithContext(ctx).Model(c).Updates(map[string]any{"title": title, "updated_at": s.now()}).Error
}

func (s *Store) DeleteConversation(ctx context.Context, c *model.Conversation) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, m := range []any{&model.Message{}, &model.Run{}} {
			if err := tx.Where("conversation_id = ?", c.ID).Delete(m).Error; err != nil {
				return err
			}
		}
		return tx.Delete(c).Error
	})
}

func (s *Store) Branch(ctx context.Context, head *int64) ([]model.Message, error) {
	if head == nil {
		return nil, nil
	}
	var out []model.Message
	err := s.db.WithContext(ctx).Raw(`WITH RECURSIVE chain AS (
			SELECT * , 0 AS depth FROM agent_message WHERE id = ?
			UNION ALL
			SELECT m.*, chain.depth + 1 FROM agent_message m JOIN chain ON m.id = chain.parent_id
		) SELECT id, conversation_id, parent_id, role, parts, status, run_id, created_at FROM chain ORDER BY depth DESC`, *head).Scan(&out).Error
	return out, err
}

func (s *Store) Leaf(ctx context.Context, conversationID, messageID int64) (int64, error) {
	var n int64
	if err := s.db.WithContext(ctx).Model(&model.Message{}).Where("id = ? AND conversation_id = ?", messageID, conversationID).Count(&n).Error; err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, ErrNotFound
	}
	cur := messageID
	for range 10000 {
		var next []int64
		if err := s.db.WithContext(ctx).Model(&model.Message{}).Where("parent_id = ?", cur).
			Order("id DESC").Limit(1).Pluck("id", &next).Error; err != nil {
			return 0, err
		}
		if len(next) == 0 {
			return cur, nil
		}
		cur = next[0]
	}
	return cur, nil
}

func (s *Store) RunByIdem(ctx context.Context, userID int64, key string) (*model.Run, error) {
	var r model.Run
	err := s.db.WithContext(ctx).Where("user_id = ? AND idem_key = ?", userID, key).Take(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &r, err
}

func (s *Store) SiblingCounts(ctx context.Context, conversationID int64) (map[int64]int, error) {
	var rows []struct {
		ParentID int64
		N        int
	}
	err := s.db.WithContext(ctx).Raw(`SELECT coalesce(parent_id, 0) AS parent_id, count(*) AS n FROM agent_message
		WHERE conversation_id = ? GROUP BY 1`, conversationID).Scan(&rows).Error
	out := map[int64]int{}
	for _, r := range rows {
		out[r.ParentID] = r.N
	}
	return out, err
}

type StartRequest struct {
	Conversation *model.Conversation
	ParentID     *int64
	Parts        []Part
	Mode         string
	WebSearch    bool
	IdemKey      string
	Price        int
}

func RequestHash(r StartRequest) string {
	raw, _ := json.Marshal(struct {
		C  int64
		P  *int64
		Ps []Part
		M  string
		W  bool
	}{r.Conversation.ID, r.ParentID, r.Parts, r.Mode, r.WebSearch})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (s *Store) StartRun(ctx context.Context, r StartRequest) (*model.Run, bool, error) {
	hash := RequestHash(r)
	var existing model.Run
	err := s.db.WithContext(ctx).Where("user_id = ? AND idem_key = ?", r.Conversation.UserID, r.IdemKey).Take(&existing).Error
	if err == nil {
		if existing.IdemHash != hash {
			return nil, false, ErrIdempotencyUsed
		}
		return &existing, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}
	parent := r.ParentID
	switch {
	case parent == nil:
		parent = r.Conversation.HeadMessageID
	case *parent == 0:
		parent = nil
	}
	run := &model.Run{
		ID: uuid.NewString(), ConversationID: r.Conversation.ID, UserID: r.Conversation.UserID, SiteID: r.Conversation.SiteID,
		Mode: r.Mode, WebSearch: r.WebSearch, Status: model.RunRunning, IdemKey: r.IdemKey, IdemHash: hash,
		Price: r.Price, Settle: model.SettleNone, CreatedAt: s.now(),
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if parent != nil {
			var n int64
			if err := tx.Model(&model.Message{}).Where("id = ? AND conversation_id = ?", *parent, r.Conversation.ID).Count(&n).Error; err != nil {
				return err
			}
			if n == 0 {
				return ErrBadParent
			}
		}
		parts, _ := json.Marshal(r.Parts)
		user := model.Message{ConversationID: r.Conversation.ID, ParentID: parent, Role: model.RoleUser,
			Parts: datatypes.JSON(parts), Status: model.MessageComplete, RunID: run.ID, CreatedAt: s.now()}
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		reply := model.Message{ConversationID: r.Conversation.ID, ParentID: &user.ID, Role: model.RoleAssistant,
			Parts: datatypes.JSON("[]"), Status: model.MessageStreaming, RunID: run.ID, CreatedAt: s.now()}
		if err := tx.Create(&reply).Error; err != nil {
			return err
		}
		run.UserMessageID, run.AssistantMessageID = user.ID, reply.ID
		if err := tx.Create(run).Error; err != nil {
			if isUniqueViolation(err, "uq_agent_run_active") {
				return ErrRunActive
			}
			return err
		}
		return tx.Model(r.Conversation).Updates(map[string]any{"head_message_id": reply.ID, "updated_at": s.now()}).Error
	})
	if err != nil {
		return nil, false, err
	}
	return run, true, nil
}

func isUniqueViolation(err error, constraint string) bool {
	msg := err.Error()
	return strings.Contains(msg, "SQLSTATE 23505") && strings.Contains(msg, constraint)
}

type Finish struct {
	Status       string
	ErrorKind    string
	Parts        []Part
	Steps        int
	InputTokens  int
	OutputTokens int
	CostNano     int64
	FreeUnits    int
	PaidUnits    int
	Settle       string
}

func (s *Store) FinishRun(ctx context.Context, run *model.Run, f Finish) error {
	now := s.now()
	msgStatus := model.MessageComplete
	switch f.Status {
	case model.RunFailed, model.RunInterrupted:
		msgStatus = model.MessageFailed
	case model.RunCanceled:
		msgStatus = model.MessageCanceled
	}
	parts, _ := json.Marshal(f.Parts)
	if f.Parts == nil {
		parts = []byte("[]")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.Message{}).Where("id = ?", run.AssistantMessageID).
			Updates(map[string]any{"parts": datatypes.JSON(parts), "status": msgStatus}).Error; err != nil {
			return err
		}
		return tx.Model(run).Updates(map[string]any{
			"status": f.Status, "error_kind": f.ErrorKind, "steps": f.Steps, "input_tokens": f.InputTokens,
			"output_tokens": f.OutputTokens, "cost_nano": f.CostNano, "free_units": f.FreeUnits,
			"paid_units": f.PaidUnits, "settle": f.Settle, "finished_at": now,
		}).Error
	})
}

func (s *Store) Run(ctx context.Context, userID int64, id string) (*model.Run, error) {
	var r model.Run
	err := s.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Take(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &r, err
}

func (s *Store) InterruptOrphans(ctx context.Context) (int64, error) {
	res := s.db.WithContext(ctx).Model(&model.Run{}).Where("status = ?", model.RunRunning).
		Updates(map[string]any{"status": model.RunInterrupted, "finished_at": s.now()})
	if res.Error != nil {
		return 0, res.Error
	}
	err := s.db.WithContext(ctx).Model(&model.Message{}).Where("status = ?", model.MessageStreaming).
		Update("status", model.MessageFailed).Error
	return res.RowsAffected, err
}

func (s *Store) Persona(ctx context.Context, siteID int64) (*model.Persona, error) {
	var p model.Persona
	err := s.db.WithContext(ctx).Where("active AND kind = ? AND site_id IN (?, 0)", model.PersonaMascot, siteID).
		Order("site_id DESC, id").Take(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &p, err
}
