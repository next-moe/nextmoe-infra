package service

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"api/internal/platform/agent/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const MemoryMaxRunes = 200

var ErrMemoryFull = errors.New("agent: memory is full")

type Prefs struct {
	Memory  bool
	History bool
}

func (s *Store) Preferences(ctx context.Context, userID, siteID int64) (Prefs, error) {
	var p model.Preference
	err := s.db.WithContext(ctx).Where("user_id = ? AND site_id = ?", userID, siteID).Take(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Prefs{Memory: true, History: true}, nil
	}
	return Prefs{Memory: p.Memory, History: p.History}, err
}

func (s *Store) SetPreferences(ctx context.Context, userID, siteID int64, memory, history *bool) (Prefs, error) {
	cur, err := s.Preferences(ctx, userID, siteID)
	if err != nil {
		return cur, err
	}
	if memory != nil {
		cur.Memory = *memory
	}
	if history != nil {
		cur.History = *history
	}
	return cur, s.db.WithContext(ctx).Exec(`INSERT INTO agent_preference (user_id, site_id, memory, history, updated_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (user_id, site_id) DO UPDATE SET memory = excluded.memory, history = excluded.history, updated_at = excluded.updated_at`,
		userID, siteID, cur.Memory, cur.History, s.now()).Error
}

func (s *Store) Memories(ctx context.Context, userID, siteID int64) ([]model.Memory, error) {
	var out []model.Memory
	return out, s.db.WithContext(ctx).Where("user_id = ? AND site_id = ?", userID, siteID).
		Order("updated_at DESC, id DESC").Find(&out).Error
}

func tidyMemory(content string) string {
	return clipRunes(strings.Join(strings.Fields(content), " "), MemoryMaxRunes)
}

func (s *Store) AddMemory(ctx context.Context, userID, siteID int64, content, origin string, conv *int64, max int) (*model.Memory, error) {
	var out *model.Memory
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		m, err := addMemory(tx, s.now, userID, siteID, content, origin, conv, max)
		out = m
		return err
	})
	return out, err
}

func addMemory(tx *gorm.DB, now func() time.Time, userID, siteID int64, content, origin string, conv *int64, max int) (*model.Memory, error) {
	content = tidyMemory(content)
	if content == "" {
		return nil, errors.New("agent: empty memory")
	}
	var same model.Memory
	err := tx.Where("user_id = ? AND site_id = ? AND lower(content) = lower(?)", userID, siteID, content).Take(&same).Error
	if err == nil {
		return &same, tx.Model(&same).Update("updated_at", now()).Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	var n int64
	if err := tx.Model(&model.Memory{}).Where("user_id = ? AND site_id = ?", userID, siteID).Count(&n).Error; err != nil {
		return nil, err
	}
	for ; n >= int64(max); n-- {
		var oldest model.Memory
		err := tx.Where("user_id = ? AND site_id = ? AND origin = ?", userID, siteID, model.MemoryAuto).Order("updated_at, id").Take(&oldest).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrMemoryFull
		}
		if err != nil {
			return nil, err
		}
		if err := tx.Delete(&oldest).Error; err != nil {
			return nil, err
		}
	}
	m := &model.Memory{UserID: userID, SiteID: siteID, Content: content, Origin: origin, ConversationID: conv, CreatedAt: now(), UpdatedAt: now()}
	return m, tx.Create(m).Error
}

func (s *Store) UpdateMemory(ctx context.Context, userID, siteID, id int64, content, origin string) (*model.Memory, error) {
	content = tidyMemory(content)
	if content == "" {
		return nil, errors.New("agent: empty memory")
	}
	var m model.Memory
	res := s.db.WithContext(ctx).Model(&m).Clauses(clause.Returning{}).Where("id = ? AND user_id = ? AND site_id = ?", id, userID, siteID).
		Updates(map[string]any{"content": content, "origin": origin, "updated_at": s.now()})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	return &m, nil
}

func (s *Store) DeleteMemory(ctx context.Context, userID, siteID, id int64) (*model.Memory, error) {
	var m model.Memory
	res := s.db.WithContext(ctx).Clauses(clause.Returning{}).Where("id = ? AND user_id = ? AND site_id = ?", id, userID, siteID).Delete(&m)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	return &m, nil
}

func (s *Store) ClearMemories(ctx context.Context, userID, siteID int64) (int64, error) {
	res := s.db.WithContext(ctx).Where("user_id = ? AND site_id = ?", userID, siteID).Delete(&model.Memory{})
	return res.RowsAffected, res.Error
}

type MemoryEdit struct {
	ID      int64  `json:"id"`
	Content string `json:"content"`
	Quote   string `json:"quote"`
}

type MemoryOps struct {
	Add    []MemoryEdit `json:"add"`
	Update []MemoryEdit `json:"update"`
	Delete []int64      `json:"delete"`
}

var quoteNoise = strings.NewReplacer(" ", "", "\n", "", "\t", "", "　", "", "，", "", ",", "", "。", "", ".", "", "！", "", "!", "", "？", "", "?", "",
	"「", "", "」", "", "“", "", "”", "", "\"", "", "《", "", "》", "", "、", "", "～", "", "~", "")

// On qwen3-30b, extraction saved the assistant's recommendations as
// "用户推荐了《クラナド》" and "用户喜欢《ATRI》" after the user had only
// asked for healing games. Every added or rewritten memory must quote the
// user, and a quote that is not in what the user wrote drops it.
func (o MemoryOps) Grounded(userText string) MemoryOps {
	said := strings.ToLower(quoteNoise.Replace(userText))
	keep := func(edits []MemoryEdit) []MemoryEdit {
		var out []MemoryEdit
		for _, e := range edits {
			q := strings.ToLower(quoteNoise.Replace(e.Quote))
			if utf8.RuneCountInString(q) >= 2 && strings.Contains(said, q) {
				out = append(out, e)
			}
		}
		return out
	}
	return MemoryOps{Add: keep(o.Add), Update: keep(o.Update), Delete: o.Delete}
}

func (s *Store) ApplyMemoryOps(ctx context.Context, userID, siteID, conv int64, ops MemoryOps, max int) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		scope := tx.Where("user_id = ? AND site_id = ?", userID, siteID)
		if len(ops.Delete) > 0 {
			if err := scope.Session(&gorm.Session{}).Where("id IN ? AND origin = ?", ops.Delete, model.MemoryAuto).Delete(&model.Memory{}).Error; err != nil {
				return err
			}
		}
		for _, u := range ops.Update {
			content := tidyMemory(u.Content)
			if content == "" {
				continue
			}
			if err := scope.Session(&gorm.Session{}).Model(&model.Memory{}).Where("id = ? AND origin = ?", u.ID, model.MemoryAuto).
				Updates(map[string]any{"content": content, "updated_at": s.now(), "conversation_id": conv}).Error; err != nil {
				return err
			}
		}
		for _, a := range ops.Add {
			if tidyMemory(a.Content) == "" {
				continue
			}
			if _, err := addMemory(tx, s.now, userID, siteID, a.Content, model.MemoryAuto, &conv, max); err != nil && !errors.Is(err, ErrMemoryFull) {
				return err
			}
		}
		return nil
	})
}
