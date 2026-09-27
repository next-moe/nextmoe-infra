package service

import (
	"context"
	"log/slog"
	"sort"
	"time"

	"api/internal/platform/chat/dto"
	"api/internal/platform/chat/model"
)

type Event struct {
	Type      string              `json:"type"`
	Update    *dto.Update         `json:"update,omitempty"`
	Message   *dto.Message        `json:"message,omitempty"`
	Reactions []dto.ReactionCount `json:"reactions,omitempty"`
}

func (s *Service) publishUpdates(ctx context.Context, updates []model.ChatUpdate, attach func(userID int64, e *Event)) {
	if s.pub == nil || len(updates) == 0 {
		return
	}
	out := make([]Delivery, 0, len(updates))
	for _, u := range updates {
		view := updateView(u)
		e := &Event{Type: "update", Update: &view}
		if attach != nil {
			attach(u.UserID, e)
		}
		out = append(out, Delivery{UserID: u.UserID, Data: e})
	}
	s.pub.Publish(context.WithoutCancel(ctx), out)
}

func (s *Service) publishMessageUpdates(ctx context.Context, updates []model.ChatUpdate, msg model.ChatMessage, recipients []int64) {
	if s.pub == nil || len(updates) == 0 {
		return
	}
	views, err := viewsFor(s, ctx, msg, recipients)
	if err != nil {
		slog.Warn("chat push: hydrate message", "message_id", msg.ID, "err", err)
		s.publishUpdates(ctx, updates, nil)
		return
	}
	s.publishUpdates(ctx, updates, func(uid int64, e *Event) {
		if e.Update.Kind != model.UpdateNewMessage && e.Update.Kind != model.UpdateEditMessage {
			return
		}
		if e.Update.ConversationID != dto.ID(msg.ConversationID) {
			return
		}
		if v, ok := views[uid]; ok {
			e.Message = &v
		}
	})
}

func (s *Service) publishReactionUpdates(ctx context.Context, updates []model.ChatUpdate, messageID int64) {
	if s.pub == nil || len(updates) == 0 {
		return
	}
	per, err := reactionsPerViewer(s, ctx, messageID, updateUsers(updates))
	if err != nil {
		slog.Warn("chat push: reactions", "message_id", messageID, "err", err)
		s.publishUpdates(ctx, updates, nil)
		return
	}
	s.publishUpdates(ctx, updates, func(uid int64, e *Event) {
		if e.Update.Kind == model.UpdateMessageReactions {
			r := per[uid]
			if r == nil {
				r = []dto.ReactionCount{}
			}
			e.Reactions = r
		}
	})
}

func updateUsers(updates []model.ChatUpdate) []int64 {
	seen := map[int64]bool{}
	var ids []int64
	for _, u := range updates {
		if !seen[u.UserID] {
			seen[u.UserID] = true
			ids = append(ids, u.UserID)
		}
	}
	return ids
}

type reactionRow struct {
	UserID    int64     `gorm:"column:user_id"`
	Reaction  string    `gorm:"column:reaction"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func reactionsPerViewer(s *Service, ctx context.Context, messageID int64, viewers []int64) (map[int64][]dto.ReactionCount, error) {
	var rows []reactionRow
	if err := s.db.WithContext(ctx).Model(&model.ChatReaction{}).Select("user_id", "reaction", "created_at").
		Where("message_id = ?", messageID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	type agg struct {
		n     int
		first time.Time
		users map[int64]bool
	}
	byReaction := map[string]*agg{}
	for _, r := range rows {
		a := byReaction[r.Reaction]
		if a == nil {
			a = &agg{first: r.CreatedAt, users: map[int64]bool{}}
			byReaction[r.Reaction] = a
		}
		a.n++
		a.users[r.UserID] = true
		if r.CreatedAt.Before(a.first) {
			a.first = r.CreatedAt
		}
	}
	keys := make([]string, 0, len(byReaction))
	for k := range byReaction {
		keys = append(keys, k)
	}
	sort.SliceStable(keys, func(i, j int) bool {
		a, b := byReaction[keys[i]], byReaction[keys[j]]
		if a.n != b.n {
			return a.n > b.n
		}
		return a.first.Before(b.first)
	})
	out := make(map[int64][]dto.ReactionCount, len(viewers))
	for _, v := range viewers {
		list := make([]dto.ReactionCount, 0, len(keys))
		for _, k := range keys {
			a := byReaction[k]
			list = append(list, dto.ReactionCount{Reaction: k, Count: a.n, Reacted: a.users[v]})
		}
		out[v] = list
	}
	return out, nil
}

func viewsFor(s *Service, ctx context.Context, msg model.ChatMessage, viewers []int64) (map[int64]dto.Message, error) {
	base, err := hydrate(s.db.WithContext(ctx), msg.SenderID, []model.ChatMessage{msg})
	if err != nil {
		return nil, err
	}
	reactions, err := reactionsPerViewer(s, ctx, msg.ID, viewers)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]dto.Message, len(viewers))
	for _, v := range viewers {
		m := base[0]
		m.Reactions = reactions[v]
		if v != msg.SenderID {
			m.ClientMessageID = nil
		}
		out[v] = m
	}
	return out, nil
}
