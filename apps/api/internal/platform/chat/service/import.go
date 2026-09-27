package service

import (
	"context"
	"errors"
	"time"

	"api/internal/platform/chat/content"
	"api/internal/platform/chat/dto"
	"api/internal/platform/chat/model"

	"gorm.io/gorm"
)

type ImportReaction struct {
	UserID    int64
	Reaction  string
	CreatedAt time.Time
}

type ImportMessage struct {
	SourceKey      string
	ReplyToKey     string
	SenderID       int64
	Text           string
	Entities       []content.Entity
	Media          *dto.Media
	MediaGroupID   *int64
	Deleted        bool
	EditedAt       *time.Time
	CreatedAt      time.Time
	OriginSite     string
	Reactions      []ImportReaction
	ReadThroughFor []int64
}

type ImportConversation struct {
	UserA, UserB int64
	OriginSite   string
	CreatedAt    time.Time
	// Messages in the order they are to be numbered.
	Messages []ImportMessage
	// Users whose read position the old site never recorded: everything
	// imported counts as read for them.
	AllReadFor []int64
}

type ImportResult struct {
	ConversationID int64
	Created        bool
	Imported       int
	Skipped        int
}

var ErrNativeHistory = errors.New("chat: the pair already talks in chat; history is not imported under it")

// Import writes old messages into the pair's direct conversation. Messages
// whose SourceKey is already in the ledger are skipped, so a re-run appends
// only what the old site gained since. Appending to a conversation that was
// already in the ledger writes update rows as a live send would; the first
// import of a conversation writes none, since no client has seen it yet.
func (s *Service) Import(ctx context.Context, in ImportConversation) (*ImportResult, error) {
	if in.UserA <= 0 || in.UserB <= 0 || in.UserA == in.UserB {
		return nil, &InvalidError{Field: "users", Reason: "two distinct positive ids"}
	}
	low, high := min(in.UserA, in.UserB), max(in.UserA, in.UserB)
	res := &ImportResult{}
	err := s.withRetry(ctx, func(tx *gorm.DB) error {
		*res = ImportResult{}
		now := s.now()
		convID, err := s.findDirect(tx, low, high)
		if err != nil {
			return err
		}
		if convID == 0 {
			if err := tx.Raw(`
				INSERT INTO chat_conversation
				       (kind, direct_user_low_id, direct_user_high_id, creator_id, origin_site, last_seq, member_count, created_at, updated_at)
				VALUES ('direct', ?, ?, ?, ?, 0, 2, ?, ?)
				RETURNING id`, low, high, low, in.OriginSite, in.CreatedAt, now).Scan(&convID).Error; err != nil {
				return err
			}
			at := in.CreatedAt
			members := []model.ChatMember{
				newMember(convID, low, model.RoleMember, in.CreatedAt, &at),
				newMember(convID, high, model.RoleMember, in.CreatedAt, &at),
			}
			if err := tx.Create(&members).Error; err != nil {
				return err
			}
			if err := ensureChatUsers(tx, []int64{low, high}, now); err != nil {
				return err
			}
			res.Created = true
		}
		res.ConversationID = convID
		conv, members, err := lockConversationMembers(tx, convID)
		if err != nil {
			return err
		}
		var ledgered int64
		if err := tx.Model(&model.ChatImportMessage{}).Where("conversation_id = ?", convID).Count(&ledgered).Error; err != nil {
			return err
		}
		if conv.LastSeq > 0 && ledgered == 0 {
			return ErrNativeHistory
		}
		live := ledgered > 0

		keys := make([]string, len(in.Messages))
		for i, m := range in.Messages {
			keys[i] = m.SourceKey
		}
		known := map[string]int64{}
		var rows []model.ChatImportMessage
		if err := tx.Where("source_key IN ?", keys).Find(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			known[r.SourceKey] = r.MessageID
		}
		seqOf := map[int64]int64{}
		if len(rows) > 0 {
			ids := make([]int64, 0, len(rows))
			for _, r := range rows {
				ids = append(ids, r.MessageID)
			}
			var ms []model.ChatMessage
			if err := tx.Select("id", "seq").Where("id IN ?", ids).Find(&ms).Error; err != nil {
				return err
			}
			for _, m := range ms {
				seqOf[m.ID] = m.Seq
			}
		}

		seq := conv.LastSeq
		readThrough := map[int64]int64{}
		var ups []pendingUpdate
		var lastAt time.Time
		for _, m := range in.Messages {
			if _, ok := known[m.SourceKey]; ok {
				res.Skipped++
				continue
			}
			seq++
			row := model.ChatMessage{
				ConversationID: convID, Seq: seq, SenderID: m.SenderID, Kind: model.MessageKindMessage,
				OriginSite: m.OriginSite, EditedAt: m.EditedAt, CreatedAt: m.CreatedAt, MediaGroupID: m.MediaGroupID,
			}
			if m.Deleted {
				at := m.CreatedAt
				row.DeletedAt = &at
			} else {
				row.Text = m.Text
				if len(m.Entities) > 0 {
					row.Entities = jsonOf(m.Entities)
				}
				if m.Media != nil {
					row.Media = jsonOf(m.Media)
				}
				if id, ok := known[m.ReplyToKey]; ok && m.ReplyToKey != "" {
					if rs, ok := seqOf[id]; ok {
						row.ReplyToSeq = &rs
					}
				}
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			known[m.SourceKey] = row.ID
			seqOf[row.ID] = seq
			if err := tx.Create(&model.ChatImportMessage{SourceKey: m.SourceKey, MessageID: row.ID, ConversationID: convID, ImportedAt: now}).Error; err != nil {
				return err
			}
			if !m.Deleted {
				for _, rx := range m.Reactions {
					if !knownReaction(rx.Reaction) || findMember(members, rx.UserID) == nil {
						continue
					}
					if err := tx.Clauses(onConflictNothing).Create(&model.ChatReaction{
						MessageID: row.ID, UserID: rx.UserID, Reaction: rx.Reaction, CreatedAt: rx.CreatedAt,
					}).Error; err != nil {
						return err
					}
				}
			}
			readThrough[m.SenderID] = seq
			for _, uid := range m.ReadThroughFor {
				readThrough[uid] = seq
			}
			lastAt = m.CreatedAt
			res.Imported++
			if live {
				for _, mem := range members {
					ups = append(ups, pendingUpdate{userID: mem.UserID, kind: model.UpdateNewMessage, conversationID: convID, data: map[string]any{"seq": seq}})
				}
			}
		}
		if res.Imported == 0 {
			return nil
		}
		for _, uid := range in.AllReadFor {
			readThrough[uid] = seq
		}
		if err := tx.Model(&model.ChatConversation{}).Where("id = ?", convID).
			Updates(map[string]any{"last_seq": seq, "updated_at": now}).Error; err != nil {
			return err
		}
		for _, mem := range members {
			set := map[string]any{"updated_at": now}
			if mem.AcceptedAt == nil {
				set["accepted_at"] = in.CreatedAt
			}
			if lastAt.After(timeOr(mem.LastMessageAt)) {
				set["last_message_at"] = lastAt
			}
			if r, ok := readThrough[mem.UserID]; ok && r > mem.LastReadSeq {
				set["last_read_seq"] = r
			}
			if err := tx.Model(&model.ChatMember{}).Where("conversation_id = ? AND user_id = ?", convID, mem.UserID).Updates(set).Error; err != nil {
				return err
			}
		}
		if _, err := recountUnread(tx, convID, memberIDs(members), now); err != nil {
			return err
		}
		if len(ups) > 0 {
			if _, err := appendUpdates(tx, now, ups); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

func timeOr(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

func lockConversationMembers(tx *gorm.DB, id int64) (*model.ChatConversation, []model.ChatMember, error) {
	conv, err := lockConversation(tx, id)
	if err != nil {
		return nil, nil, err
	}
	members, err := lockMembers(tx, id)
	return conv, members, err
}
