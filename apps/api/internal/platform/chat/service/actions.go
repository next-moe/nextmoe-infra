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

const (
	maxSeqsPerCall  = 100
	draftsPerMinute = 30
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
		conv, _, me, err := lockAsMember(tx, conversationID, a.UserID)
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

// Muted until unmuted is a real far-future timestamp, never Postgres'
// infinity, which time.Time and pgx cannot carry.
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

func (s *Service) SetPinned(ctx context.Context, a Actor, conversationID, seq int64, pinned bool) error {
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
	event := map[string]any{"type": "typing", "conversation_id": conversationID, "user_id": a.UserID}
	var out []Delivery
	for _, m := range members {
		if m.UserID != a.UserID && m.AcceptedAt != nil {
			out = append(out, Delivery{UserID: m.UserID, Data: event})
		}
	}
	s.pub.Publish(ctx, out)
	return nil
}
