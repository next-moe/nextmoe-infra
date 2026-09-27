package service

import (
	"context"
	"errors"
	"time"

	"api/internal/platform/chat/dto"
	"api/internal/platform/chat/model"

	"gorm.io/gorm"
)

const (
	maxSeqsPerCall  = 100
	draftsPerMinute = 30
	pinsPerMinute   = 10
	typingWindow    = 5 * time.Second
)

func recountUnread(tx *gorm.DB, conversationID int64, userIDs []int64, now time.Time) (map[int64]int32, error) {
	out := map[int64]int32{}
	if len(userIDs) == 0 {
		return out, nil
	}
	type row struct {
		UserID      int64 `gorm:"column:user_id"`
		UnreadCount int32 `gorm:"column:unread_count"`
	}
	var rows []row
	if err := tx.Raw(`
		UPDATE chat_member m SET
		       unread_count = (
		           SELECT count(*) FROM chat_message x
		            WHERE x.conversation_id = m.conversation_id
		              AND x.seq > GREATEST(m.last_read_seq, m.cleared_through_seq)
		              AND x.seq >= m.visible_from_seq
		              AND x.sender_id <> m.user_id
		              AND x.deleted_at IS NULL
		              AND NOT EXISTS (SELECT 1 FROM chat_hidden_message h WHERE h.user_id = m.user_id AND h.message_id = x.id)),
		       updated_at = ?
		 WHERE m.conversation_id = ? AND m.user_id IN ?
		RETURNING m.user_id, m.unread_count`, now, conversationID, userIDs).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.UserID] = r.UnreadCount
	}
	return out, nil
}

// stripQuotesTx drops the quotes other messages took from erased ones: a quote

// is a copy of the words, and it outlived the delete until a review caught it.

func stripQuotesTx(tx *gorm.DB, conversationID int64, erased []int64) ([]int64, error) {
	var seqs []int64
	if len(erased) == 0 {
		return nil, nil
	}
	err := tx.Raw(`
		UPDATE chat_message SET reply_quote = NULL
		 WHERE conversation_id = ? AND reply_to_seq IN ? AND reply_quote IS NOT NULL
		RETURNING seq`, conversationID, erased).Scan(&seqs).Error
	return seqs, err
}

func quoteEditUpdates(conversationID int64, members []model.ChatMember, seqs []int64) []pendingUpdate {
	var ups []pendingUpdate
	for _, seq := range seqs {
		for _, m := range members {
			ups = append(ups, pendingUpdate{userID: m.UserID, kind: model.UpdateEditMessage, conversationID: conversationID, data: map[string]any{"seq": seq}})
		}
	}
	return ups
}

func checkSeqs(seqs []int64) error {
	if len(seqs) == 0 || len(seqs) > maxSeqsPerCall {
		return &InvalidError{Field: "seqs", Reason: "must name 1-100 messages"}
	}
	for _, s := range seqs {
		if s <= 0 {
			return &InvalidError{Field: "seqs", Reason: "must be positive"}
		}
	}
	return nil
}

func (s *Service) DeleteMessages(ctx context.Context, a Actor, conversationID int64, seqs []int64, forEveryone bool) ([]int64, error) {
	if err := checkSeqs(seqs); err != nil {
		return nil, err
	}
	var (
		done    []int64
		updates []model.ChatUpdate
	)
	err := s.withRetry(ctx, func(tx *gorm.DB) error {
		conv, members, me, err := lockAsMember(tx, conversationID, a.UserID)
		if err != nil {
			return err
		}
		now := s.now()
		var targets []model.ChatMessage
		if err := tx.Where("conversation_id = ? AND seq IN ? AND deleted_at IS NULL AND seq >= ?", conv.ID, seqs, me.VisibleFromSeq).
			Order("seq").Find(&targets).Error; err != nil {
			return err
		}
		done = done[:0]
		if forEveryone {
			ids := make([]int64, 0, len(targets))
			for _, t := range targets {
				if t.SenderID != a.UserID {
					return ErrNotPermitted
				}
				ids = append(ids, t.ID)
				done = append(done, t.Seq)
			}
			if len(ids) == 0 {
				return nil
			}
			if err := tx.Exec(`
				UPDATE chat_message SET deleted_at = ?, text = '', entities = NULL, media = NULL,
				       reply_quote = NULL, context = NULL, pinned_at = NULL
				 WHERE id IN ?`, now, ids).Error; err != nil {
				return err
			}
			if err := tx.Where("message_id IN ?", ids).Delete(&model.ChatReaction{}).Error; err != nil {
				return err
			}
			stripped, err := stripQuotesTx(tx, conv.ID, done)
			if err != nil {
				return err
			}
			others := make([]int64, 0, len(members))
			for _, m := range members {
				if m.UserID != a.UserID {
					others = append(others, m.UserID)
				}
			}
			if _, err := recountUnread(tx, conv.ID, others, now); err != nil {
				return err
			}
			ups := make([]pendingUpdate, 0, len(members))
			for _, m := range members {
				ups = append(ups, pendingUpdate{userID: m.UserID, kind: model.UpdateDeleteMessages, conversationID: conv.ID, data: map[string]any{"seqs": done}})
			}
			ups = append(ups, quoteEditUpdates(conv.ID, members, stripped)...)
			updates, err = appendUpdates(tx, now, ups)
			return err
		}
		rows := make([]model.ChatHiddenMessage, 0, len(targets))
		for _, t := range targets {
			rows = append(rows, model.ChatHiddenMessage{UserID: a.UserID, MessageID: t.ID, CreatedAt: now})
			done = append(done, t.Seq)
		}
		if len(rows) == 0 {
			return nil
		}
		if err := tx.Clauses(onConflictNothing).Create(&rows).Error; err != nil {
			return err
		}
		counts, err := recountUnread(tx, conv.ID, []int64{a.UserID}, now)
		if err != nil {
			return err
		}
		updates, err = appendUpdates(tx, now, []pendingUpdate{{
			userID: a.UserID, kind: model.UpdateHideMessages, conversationID: conv.ID,
			data: map[string]any{"seqs": done, "unread_count": counts[a.UserID]},
		}})
		return err
	})
	if err != nil {
		return nil, err
	}
	s.publishUpdates(ctx, updates, nil)
	return done, nil
}

func (s *Service) React(ctx context.Context, a Actor, messageID int64, reaction *string) ([]dto.ReactionCount, error) {
	if reaction != nil && !knownReaction(*reaction) {
		return nil, &InvalidError{Field: "reaction", Reason: "not in the reaction vocabulary"}
	}
	conversationID, err := s.conversationOfMessage(ctx, messageID)
	if err != nil {
		return nil, err
	}
	var (
		msg     model.ChatMessage
		updates []model.ChatUpdate
		changed bool
	)
	err = s.withRetry(ctx, func(tx *gorm.DB) error {
		conv, members, me, err := lockAsMember(tx, conversationID, a.UserID)
		if err != nil {
			return err
		}
		if conv.Kind == model.KindDirect {
			if _, err := s.directPeer(ctx, conv, members, me); err != nil {
				return err
			}
		}
		if err := tx.Where("id = ? AND deleted_at IS NULL AND seq >= ?", messageID, me.VisibleFromSeq).Take(&msg).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if msg.Kind != model.MessageKindMessage {
			return &InvalidError{Field: "message", Reason: "service messages take no reactions"}
		}
		var current []string
		if err := tx.Model(&model.ChatReaction{}).Where("message_id = ? AND user_id = ?", msg.ID, a.UserID).
			Pluck("reaction", &current).Error; err != nil {
			return err
		}
		if reaction == nil && len(current) == 0 || reaction != nil && len(current) == 1 && current[0] == *reaction {
			changed = false
			return nil
		}
		changed = true
		now := s.now()
		if err := tx.Where("message_id = ? AND user_id = ?", msg.ID, a.UserID).Delete(&model.ChatReaction{}).Error; err != nil {
			return err
		}
		if reaction != nil {
			if err := tx.Create(&model.ChatReaction{MessageID: msg.ID, UserID: a.UserID, Reaction: *reaction, CreatedAt: now}).Error; err != nil {
				return err
			}
		}
		ups := make([]pendingUpdate, 0, len(members))
		for _, m := range members {
			ups = append(ups, pendingUpdate{userID: m.UserID, kind: model.UpdateMessageReactions, conversationID: conv.ID, data: map[string]any{"seq": msg.Seq}})
		}
		updates, err = appendUpdates(tx, now, ups)
		return err
	})
	if err != nil {
		return nil, err
	}
	if changed {
		s.publishReactionUpdates(ctx, updates, msg.ID)
	}
	reactions, err := loadReactions(s.db.WithContext(ctx), a.UserID, []int64{msg.ID})
	if err != nil {
		return nil, err
	}
	out := reactions[msg.ID]
	if out == nil {
		out = []dto.ReactionCount{}
	}
	return out, nil
}

// Muted until unmuted is a real far-future timestamp, never Postgres'

// infinity, which time.Time and pgx cannot carry.

func (s *Service) SetPinned(ctx context.Context, a Actor, conversationID, seq int64, pinned bool) error {
	if err := s.allow(ctx, "pin:"+itoa(a.UserID), pinsPerMinute, time.Minute); err != nil {
		return err
	}
	var (
		updates    []model.ChatUpdate
		service    *model.ChatMessage
		recipients []int64
	)
	err := s.withRetry(ctx, func(tx *gorm.DB) error {
		conv, members, me, err := lockAsMember(tx, conversationID, a.UserID)
		if err != nil {
			return err
		}
		if conv.Kind == model.KindGroup && me.Role == model.RoleMember {
			return ErrNotPermitted
		}
		if conv.Kind == model.KindDirect {
			peer, err := s.directPeer(ctx, conv, members, me)
			if err != nil {
				return err
			}
			if peer.AcceptedAt == nil || me.AcceptedAt == nil {
				return ErrRequestLimit
			}
		}
		var target model.ChatMessage
		if err := tx.Where("conversation_id = ? AND seq = ? AND deleted_at IS NULL AND kind = ? AND seq >= ?", conv.ID, seq, model.MessageKindMessage, me.VisibleFromSeq).
			Take(&target).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if (target.PinnedAt != nil) == pinned {
			return nil
		}
		now := s.now()
		var at *time.Time
		if pinned {
			at = &now
		}
		if err := tx.Model(&model.ChatMessage{}).Where("id = ?", target.ID).Update("pinned_at", at).Error; err != nil {
			return err
		}
		extra := make([]pendingUpdate, 0, len(members))
		for _, m := range members {
			extra = append(extra, pendingUpdate{userID: m.UserID, kind: model.UpdatePinnedMessages, conversationID: conv.ID,
				data: map[string]any{"seqs": []int64{seq}, "pinned": pinned}})
		}
		recipients = memberIDs(members)
		if !pinned {
			updates, err = appendUpdates(tx, now, extra)
			return err
		}
		pinnedSeq := seq
		row := &model.ChatMessage{
			SenderID: a.UserID, Kind: model.MessageKindService, OriginSite: a.Site,
			ServiceAction: jsonOf(storedAction{Type: "message_pinned", Seq: &pinnedSeq}),
		}
		service, updates, err = s.appendMessage(tx, conv, members, row, extra)
		return err
	})
	if err != nil {
		return err
	}
	if service != nil {
		s.publishMessageUpdates(ctx, updates, *service, recipients)
	} else {
		s.publishUpdates(ctx, updates, nil)
	}
	return nil
}

func (s *Service) Typing(ctx context.Context, a Actor, conversationID int64) error {
	var members []model.ChatMember
	if err := s.db.WithContext(ctx).Where("conversation_id = ? AND left_at IS NULL", conversationID).Find(&members).Error; err != nil {
		return err
	}
	me := findMember(members, a.UserID)
	if me == nil {
		return ErrNotFound
	}
	if err := s.allow(ctx, "typing:"+itoa(a.UserID)+":"+itoa(conversationID), 1, typingWindow); err != nil {
		return nil
	}
	if me.AcceptedAt == nil || s.pub == nil {
		return nil
	}
	var conv model.ChatConversation
	if err := s.db.WithContext(ctx).Take(&conv, conversationID).Error; err != nil {
		return err
	}
	if conv.Kind == model.KindDirect {
		if _, err := s.directPeer(ctx, &conv, members, me); err != nil {
			return nil
		}
	}
	event := map[string]any{"type": "typing", "conversation_id": dto.ID(conversationID), "user_id": dto.ID(a.UserID)}
	var out []Delivery
	for _, m := range members {
		if m.UserID != a.UserID && m.AcceptedAt != nil {
			out = append(out, Delivery{UserID: m.UserID, Data: event})
		}
	}
	s.pub.Publish(ctx, out)
	return nil
}
