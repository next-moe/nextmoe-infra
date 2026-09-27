package service

import (
	"context"
	"time"

	"api/internal/platform/chat/content"
	"api/internal/platform/chat/dto"
	"api/internal/platform/chat/model"

	"gorm.io/gorm"
)

type ReadResult struct {
	LastReadSeq int64
	UnreadCount int32
}

func (s *Service) Read(ctx context.Context, a Actor, conversationID, maxSeq int64) (*ReadResult, error) {
	var (
		res     ReadResult
		updates []model.ChatUpdate
	)
	err := s.withRetry(ctx, func(tx *gorm.DB) error {
		conv, members, me, err := lockAsMember(tx, conversationID, a.UserID)
		if err != nil {
			return err
		}
		if maxSeq > conv.LastSeq {
			maxSeq = conv.LastSeq
		}
		old := me.LastReadSeq
		next := old
		if maxSeq > next {
			next = maxSeq
		}
		res = ReadResult{LastReadSeq: next, UnreadCount: me.UnreadCount}
		if next == old && !me.MarkedUnread {
			return nil
		}
		now := s.now()
		if err := tx.Model(&model.ChatMember{}).Where("conversation_id = ? AND user_id = ?", conv.ID, a.UserID).
			Updates(map[string]any{"last_read_seq": next, "marked_unread": false, "updated_at": now}).Error; err != nil {
			return err
		}
		counts, err := recountUnread(tx, conv.ID, []int64{a.UserID}, now)
		if err != nil {
			return err
		}
		res.UnreadCount = counts[a.UserID]
		ups := []pendingUpdate{{userID: a.UserID, kind: model.UpdateReadInbox, conversationID: conv.ID,
			data: map[string]any{"max_seq": next, "unread_count": res.UnreadCount}}}
		if next > old && me.AcceptedAt != nil {
			var senders []int64
			if err := tx.Model(&model.ChatMessage{}).Distinct("sender_id").
				Where("conversation_id = ? AND seq > ? AND seq <= ? AND sender_id <> ? AND kind = ?", conv.ID, old, next, a.UserID, model.MessageKindMessage).
				Pluck("sender_id", &senders).Error; err != nil {
				return err
			}
			for _, id := range senders {
				if findMember(members, id) == nil {
					continue
				}
				ups = append(ups, pendingUpdate{userID: id, kind: model.UpdateReadOutbox, conversationID: conv.ID, data: map[string]any{"max_seq": next}})
			}
		}
		updates, err = appendUpdates(tx, now, ups)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.publishUpdates(ctx, updates, nil)
	return &res, nil
}

func (s *Service) Accept(ctx context.Context, a Actor, conversationID int64) error {
	var updates []model.ChatUpdate
	err := s.withRetry(ctx, func(tx *gorm.DB) error {
		conv, members, me, err := lockAsMember(tx, conversationID, a.UserID)
		if err != nil {
			return err
		}
		if me.AcceptedAt != nil {
			return nil
		}
		now := s.now()
		if err := tx.Model(&model.ChatMember{}).Where("conversation_id = ? AND user_id = ?", conv.ID, a.UserID).
			Updates(map[string]any{"accepted_at": now, "updated_at": now}).Error; err != nil {
			return err
		}
		ups := []pendingUpdate{{userID: a.UserID, kind: model.UpdateDialog, conversationID: conv.ID, data: map[string]any{"accepted": true}}}
		if me.LastReadSeq > 0 {
			for _, m := range members {
				if m.UserID != a.UserID {
					ups = append(ups, pendingUpdate{userID: m.UserID, kind: model.UpdateReadOutbox, conversationID: conv.ID, data: map[string]any{"max_seq": me.LastReadSeq}})
				}
			}
		}
		updates, err = appendUpdates(tx, now, ups)
		return err
	})
	if err != nil {
		return err
	}
	s.publishUpdates(ctx, updates, nil)
	return nil
}

type DialogPatch struct {
	Muted        *bool
	MutedUntil   *time.Time
	Archived     *bool
	Pinned       *bool
	MarkedUnread *bool
}

var MutedForever = time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)

func (s *Service) UpdateDialog(ctx context.Context, a Actor, conversationID int64, p DialogPatch) (*dto.DialogState, error) {
	var (
		state   dto.DialogState
		updates []model.ChatUpdate
	)
	err := s.withRetry(ctx, func(tx *gorm.DB) error {
		conv, _, me, err := lockAsMember(tx, conversationID, a.UserID)
		if err != nil {
			return err
		}
		now := s.now()
		set := map[string]any{}
		data := map[string]any{}
		if p.Muted != nil {
			var until *time.Time
			if *p.Muted {
				t := MutedForever
				if p.MutedUntil != nil {
					if !p.MutedUntil.After(now) {
						return &InvalidError{Field: "muted_until", Reason: "must be in the future"}
					}
					t = p.MutedUntil.UTC()
				}
				until = &t
			}
			set["muted_until"] = until
			me.MutedUntil = until
			data["muted_until"] = until
		}
		if p.Archived != nil {
			var at *time.Time
			if *p.Archived {
				at = &now
				if me.ArchivedAt != nil {
					at = me.ArchivedAt
				}
			}
			set["archived_at"] = at
			me.ArchivedAt = at
			data["archived"] = *p.Archived
		}
		if p.Pinned != nil {
			var rank *int16
			if *p.Pinned {
				if me.PinnedRank != nil {
					rank = me.PinnedRank
				} else {
					if err := lockChatUser(tx, a.UserID, now); err != nil {
						return err
					}
					var ranks []int16
					if err := tx.Model(&model.ChatMember{}).
						Where("user_id = ? AND left_at IS NULL AND pinned_rank IS NOT NULL", a.UserID).
						Pluck("pinned_rank", &ranks).Error; err != nil {
						return err
					}
					if len(ranks) >= maxPinnedDialogs {
						return &InvalidError{Field: "pinned", Reason: "at most 5 conversations can be pinned"}
					}
					next := int16(1)
					for _, r := range ranks {
						if r >= next {
							next = r + 1
						}
					}
					rank = &next
				}
			}
			set["pinned_rank"] = rank
			me.PinnedRank = rank
			data["pinned_rank"] = rank
		}
		if p.MarkedUnread != nil {
			set["marked_unread"] = *p.MarkedUnread
			me.MarkedUnread = *p.MarkedUnread
			data["marked_unread"] = *p.MarkedUnread
		}
		state = dialogView(*me)
		if len(set) == 0 {
			return nil
		}
		set["updated_at"] = now
		if err := tx.Model(&model.ChatMember{}).Where("conversation_id = ? AND user_id = ?", conv.ID, a.UserID).Updates(set).Error; err != nil {
			return err
		}
		updates, err = appendUpdates(tx, now, []pendingUpdate{{userID: a.UserID, kind: model.UpdateDialog, conversationID: conv.ID, data: data}})
		return err
	})
	if err != nil {
		return nil, err
	}
	s.publishUpdates(ctx, updates, nil)
	return &state, nil
}

func (s *Service) SaveDraft(ctx context.Context, a Actor, conversationID int64, text string, entities []content.Entity, replyToSeq *int64) (*dto.Draft, error) {
	text, entities, err := content.Normalize(text, entities)
	if err != nil {
		return nil, contentErr(err)
	}
	if err := s.allow(ctx, "draft:"+itoa(a.UserID), draftsPerMinute, time.Minute); err != nil {
		return nil, err
	}
	var (
		draft   *dto.Draft
		updates []model.ChatUpdate
	)
	err = s.withRetry(ctx, func(tx *gorm.DB) error {
		conv, _, _, err := lockAsMember(tx, conversationID, a.UserID)
		if err != nil {
			return err
		}
		now := s.now()
		var stored any
		draft = nil
		if text != "" || replyToSeq != nil {
			d := storedDraft{Text: text, Entities: entities, ReplyToSeq: replyToSeq, UpdatedAt: now}
			stored = d
			draft = draftView(jsonOf(d))
		}
		if err := tx.Model(&model.ChatMember{}).Where("conversation_id = ? AND user_id = ?", conv.ID, a.UserID).
			Updates(map[string]any{"draft": jsonOf(stored), "updated_at": now}).Error; err != nil {
			return err
		}
		updates, err = appendUpdates(tx, now, []pendingUpdate{{userID: a.UserID, kind: model.UpdateDialog, conversationID: conv.ID, data: map[string]any{"draft": draft}}})
		return err
	})
	if err != nil {
		return nil, err
	}
	s.publishUpdates(ctx, updates, nil)
	return draft, nil
}

func (s *Service) ClearHistory(ctx context.Context, a Actor, conversationID int64, remove bool) error {
	var updates []model.ChatUpdate
	err := s.withRetry(ctx, func(tx *gorm.DB) error {
		conv, _, _, err := lockAsMember(tx, conversationID, a.UserID)
		if err != nil {
			return err
		}
		now := s.now()
		set := map[string]any{
			"cleared_through_seq": conv.LastSeq, "last_read_seq": conv.LastSeq,
			"unread_count": 0, "marked_unread": false, "updated_at": now,
		}
		if remove {
			set["last_message_at"] = nil
			set["pinned_rank"] = nil
			set["archived_at"] = nil
			set["draft"] = nil
		}
		if err := tx.Model(&model.ChatMember{}).Where("conversation_id = ? AND user_id = ?", conv.ID, a.UserID).Updates(set).Error; err != nil {
			return err
		}
		updates, err = appendUpdates(tx, now, []pendingUpdate{{userID: a.UserID, kind: model.UpdateClearHistory, conversationID: conv.ID,
			data: map[string]any{"through_seq": conv.LastSeq, "removed": remove}}})
		return err
	})
	if err != nil {
		return err
	}
	s.publishUpdates(ctx, updates, nil)
	return nil
}
