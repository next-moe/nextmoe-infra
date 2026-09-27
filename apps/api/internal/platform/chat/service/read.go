package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"api/internal/platform/chat/dto"
	"api/internal/platform/chat/model"

	"gorm.io/gorm"
)

const (
	FolderInbox    = "inbox"
	FolderArchive  = "archive"
	FolderRequests = "requests"
)

func (s *Service) State(ctx context.Context, a Actor) (*dto.State, error) {
	db := s.db.WithContext(ctx)
	st := dto.State{Object: "chat_state"}
	if err := db.Raw(`
		SELECT count(*) FILTER (WHERE accepted_at IS NOT NULL AND (unread_count > 0 OR marked_unread)
		                          AND (muted_until IS NULL OR muted_until <= ?)) AS unread_conversation_count,
		       coalesce(sum(unread_count) FILTER (WHERE accepted_at IS NOT NULL
		                          AND (muted_until IS NULL OR muted_until <= ?)), 0) AS unread_message_count,
		       count(*) FILTER (WHERE accepted_at IS NULL) AS request_count
		  FROM chat_member
		 WHERE user_id = ? AND left_at IS NULL AND last_message_at IS NOT NULL`,
		s.now(), s.now(), a.UserID).Scan(&st).Error; err != nil {
		return nil, err
	}
	var seqs []int64
	if err := db.Model(&model.ChatUser{}).Where("user_id = ?", a.UserID).Pluck("last_update_seq", &seqs).Error; err != nil {
		return nil, err
	}
	if len(seqs) > 0 {
		st.LastUpdateSeq = seqs[0]
	}
	return &st, nil
}

type ConversationPage struct {
	Conversations []dto.Conversation
	Users         []dto.User
	NextCursor    string
}

func encodeDialogCursor(m model.ChatMember) string {
	if m.LastMessageAt == nil {
		return ""
	}
	return fmt.Sprintf("%d:%d", m.LastMessageAt.UnixMicro(), m.ConversationID)
}

func decodeDialogCursor(c string) (time.Time, int64, error) {
	at, id, ok := strings.Cut(c, ":")
	micros, err1 := strconv.ParseInt(at, 10, 64)
	conv, err2 := strconv.ParseInt(id, 10, 64)
	if !ok || err1 != nil || err2 != nil || conv <= 0 {
		return time.Time{}, 0, &InvalidError{Field: "cursor", Reason: "not a cursor this face issued"}
	}
	return time.UnixMicro(micros), conv, nil
}

func (s *Service) ListConversations(ctx context.Context, a Actor, folder, cursor string, limit int) (*ConversationPage, error) {
	limit = clampLimit(limit)
	db := s.db.WithContext(ctx)
	q := db.Model(&model.ChatMember{}).
		Where("user_id = ? AND left_at IS NULL AND last_message_at IS NOT NULL", a.UserID)
	switch folder {
	case "", FolderInbox:
		folder = FolderInbox
		q = q.Where("accepted_at IS NOT NULL AND archived_at IS NULL AND pinned_rank IS NULL")
	case FolderArchive:
		q = q.Where("accepted_at IS NOT NULL AND archived_at IS NOT NULL")
	case FolderRequests:
		q = q.Where("accepted_at IS NULL")
	default:
		return nil, &InvalidError{Field: "folder", Reason: "must be inbox, archive or requests"}
	}
	if cursor != "" {
		at, id, err := decodeDialogCursor(cursor)
		if err != nil {
			return nil, err
		}
		q = q.Where("(last_message_at, conversation_id) < (?, ?)", at, id)
	}
	var members []model.ChatMember
	if err := q.Order("last_message_at DESC, conversation_id DESC").Limit(limit + 1).Find(&members).Error; err != nil {
		return nil, err
	}
	next := ""
	if len(members) > limit {
		members = members[:limit]
		next = encodeDialogCursor(members[len(members)-1])
	}
	if folder == FolderInbox && cursor == "" {
		var pinned []model.ChatMember
		if err := db.Where("user_id = ? AND left_at IS NULL AND last_message_at IS NOT NULL AND accepted_at IS NOT NULL AND archived_at IS NULL AND pinned_rank IS NOT NULL", a.UserID).
			Order("pinned_rank DESC").Find(&pinned).Error; err != nil {
			return nil, err
		}
		members = append(pinned, members...)
	}
	rows, err := s.dialogRows(db, members)
	if err != nil {
		return nil, err
	}
	views, userIDs, err := conversationViews(db, a.UserID, rows)
	if err != nil {
		return nil, err
	}
	users, err := s.userViews(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	return &ConversationPage{Conversations: views, Users: users, NextCursor: next}, nil
}

func (s *Service) dialogRows(db *gorm.DB, members []model.ChatMember) ([]dialogRow, error) {
	if len(members) == 0 {
		return nil, nil
	}
	ids := make([]int64, len(members))
	for i, m := range members {
		ids[i] = m.ConversationID
	}
	var convs []model.ChatConversation
	if err := db.Where("id IN ? AND deleted_at IS NULL", ids).Find(&convs).Error; err != nil {
		return nil, err
	}
	byID := make(map[int64]model.ChatConversation, len(convs))
	for _, c := range convs {
		byID[c.ID] = c
	}
	rows := make([]dialogRow, 0, len(members))
	for _, m := range members {
		if c, ok := byID[m.ConversationID]; ok {
			rows = append(rows, dialogRow{Member: m, Conv: c})
		}
	}
	return rows, nil
}

type ConversationResult struct {
	Conversation dto.ConversationDetail
	Users        []dto.User
}

func (s *Service) activeMember(db *gorm.DB, conversationID, uid int64) (*model.ChatMember, error) {
	var m model.ChatMember
	err := db.Where("conversation_id = ? AND user_id = ? AND left_at IS NULL", conversationID, uid).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &m, err
}

func (s *Service) Conversation(ctx context.Context, a Actor, conversationID int64) (*ConversationResult, error) {
	db := s.db.WithContext(ctx)
	me, err := s.activeMember(db, conversationID, a.UserID)
	if err != nil {
		return nil, err
	}
	rows, err := s.dialogRows(db, []model.ChatMember{*me})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	views, userIDs, err := conversationViews(db, a.UserID, rows)
	if err != nil {
		return nil, err
	}
	var members []model.ChatMember
	if err := db.Where("conversation_id = ? AND left_at IS NULL", conversationID).Order("joined_at, user_id").Find(&members).Error; err != nil {
		return nil, err
	}
	detail := dto.ConversationDetail{Conversation: views[0], Members: make([]dto.Member, 0, len(members)), PinnedSeqs: []int64{}}
	for _, m := range members {
		detail.Members = append(detail.Members, dto.Member{UserID: dto.ID(m.UserID), Role: m.Role, JoinedAt: m.JoinedAt})
		userIDs = append(userIDs, m.UserID)
	}
	if err := db.Model(&model.ChatMessage{}).
		Where("conversation_id = ? AND pinned_at IS NOT NULL AND deleted_at IS NULL AND seq >= ?", conversationID, me.VisibleFromSeq).
		Order("pinned_at DESC").Limit(100).Pluck("seq", &detail.PinnedSeqs).Error; err != nil {
		return nil, err
	}
	users, err := s.userViews(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	return &ConversationResult{Conversation: detail, Users: users}, nil
}

type MessagePage struct {
	Messages      []dto.Message
	Users         []dto.User
	HasMoreBefore bool
	HasMoreAfter  bool
}

type MessageQuery struct {
	BeforeSeq *int64
	AfterSeq  *int64
	AroundSeq *int64
	Limit     int
}

func visibleTo(db *gorm.DB, me *model.ChatMember) *gorm.DB {
	return db.Model(&model.ChatMessage{}).
		Where("conversation_id = ? AND seq >= ? AND seq > ? AND deleted_at IS NULL", me.ConversationID, me.VisibleFromSeq, me.ClearedThroughSeq).
		Where("NOT EXISTS (SELECT 1 FROM chat_hidden_message h WHERE h.user_id = ? AND h.message_id = chat_message.id)", me.UserID)
}

func (s *Service) Messages(ctx context.Context, a Actor, conversationID int64, q MessageQuery) (*MessagePage, error) {
	set := 0
	for _, p := range []*int64{q.BeforeSeq, q.AfterSeq, q.AroundSeq} {
		if p != nil {
			set++
		}
	}
	if set > 1 {
		return nil, &InvalidError{Field: "before_seq", Reason: "before_seq, after_seq and around_seq are mutually exclusive"}
	}
	limit := clampLimit(q.Limit)
	db := s.db.WithContext(ctx)
	me, err := s.activeMember(db, conversationID, a.UserID)
	if err != nil {
		return nil, err
	}
	var (
		older, newer []model.ChatMessage
		page         MessagePage
	)
	fetchOlder := func(before int64, n int) error {
		qq := visibleTo(db, me)
		if before > 0 {
			qq = qq.Where("seq < ?", before)
		}
		if err := qq.Order("seq DESC").Limit(n + 1).Find(&older).Error; err != nil {
			return err
		}
		if len(older) > n {
			older = older[:n]
			page.HasMoreBefore = true
		}
		for i, j := 0, len(older)-1; i < j; i, j = i+1, j-1 {
			older[i], older[j] = older[j], older[i]
		}
		return nil
	}
	fetchNewer := func(from int64, n int) error {
		if err := visibleTo(db, me).Where("seq >= ?", from).Order("seq ASC").Limit(n + 1).Find(&newer).Error; err != nil {
			return err
		}
		if len(newer) > n {
			newer = newer[:n]
			page.HasMoreAfter = true
		}
		return nil
	}
	switch {
	case q.AfterSeq != nil:
		err = fetchNewer(*q.AfterSeq+1, limit)
	case q.AroundSeq != nil:
		if err = fetchOlder(*q.AroundSeq, limit/2); err == nil {
			err = fetchNewer(*q.AroundSeq, limit-limit/2)
		}
	case q.BeforeSeq != nil:
		err = fetchOlder(*q.BeforeSeq, limit)
	default:
		err = fetchOlder(0, limit)
	}
	if err != nil {
		return nil, err
	}
	all := append(older, newer...)
	views, err := hydrate(db, a.UserID, all)
	if err != nil {
		return nil, err
	}
	users, err := s.userViews(ctx, messageUserIDs(views))
	if err != nil {
		return nil, err
	}
	page.Messages, page.Users = views, users
	return &page, nil
}

type UpdatesPage struct {
	Updates       []dto.Update
	Messages      []dto.Message
	Conversations []dto.Conversation
	Users         []dto.User
	LastUpdateSeq int64
	HasMore       bool
	TooLong       bool
}

func (s *Service) Updates(ctx context.Context, a Actor, after int64, limit int) (*UpdatesPage, error) {
	if after < 0 {
		return nil, &InvalidError{Field: "after", Reason: "must be >= 0"}
	}
	limit = clampLimit(limit)
	db := s.db.WithContext(ctx)
	st, err := s.State(ctx, a)
	if err != nil {
		return nil, err
	}
	page := &UpdatesPage{Updates: []dto.Update{}, Messages: []dto.Message{}, Conversations: []dto.Conversation{}, Users: []dto.User{}, LastUpdateSeq: st.LastUpdateSeq}
	if after >= st.LastUpdateSeq {
		return page, nil
	}
	var oldest []int64
	if err := db.Model(&model.ChatUpdate{}).Where("user_id = ?", a.UserID).Order("update_seq").Limit(1).Pluck("update_seq", &oldest).Error; err != nil {
		return nil, err
	}
	if len(oldest) == 0 || oldest[0] > after+1 {
		page.TooLong = true
		return page, nil
	}
	var rows []model.ChatUpdate
	if err := db.Where("user_id = ? AND update_seq > ?", a.UserID, after).Order("update_seq").Limit(limit + 1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) > limit {
		rows = rows[:limit]
		page.HasMore = true
	}
	convIDs := map[int64]bool{}
	wanted := map[int64][]int64{}
	for _, u := range rows {
		page.Updates = append(page.Updates, updateView(u))
		convIDs[u.ConversationID] = true
		if u.Kind == model.UpdateNewMessage || u.Kind == model.UpdateEditMessage {
			if d := decodeJSON[struct {
				Seq int64 `json:"seq"`
			}](u.Data); d != nil {
				wanted[u.ConversationID] = append(wanted[u.ConversationID], d.Seq)
			}
		}
	}
	var members []model.ChatMember
	ids := make([]int64, 0, len(convIDs))
	for id := range convIDs {
		ids = append(ids, id)
	}
	if err := db.Where("user_id = ? AND conversation_id IN ? AND left_at IS NULL", a.UserID, ids).Find(&members).Error; err != nil {
		return nil, err
	}
	var msgs []model.ChatMessage
	for _, m := range members {
		seqs := wanted[m.ConversationID]
		if len(seqs) == 0 {
			continue
		}
		var got []model.ChatMessage
		if err := visibleTo(db, &m).Where("seq IN ?", seqs).Order("seq").Find(&got).Error; err != nil {
			return nil, err
		}
		msgs = append(msgs, got...)
	}
	if page.Messages, err = hydrate(db, a.UserID, msgs); err != nil {
		return nil, err
	}
	dialogs, err := s.dialogRows(db, members)
	if err != nil {
		return nil, err
	}
	convViews, userIDs, err := conversationViews(db, a.UserID, dialogs)
	if err != nil {
		return nil, err
	}
	page.Conversations = convViews
	if page.Users, err = s.userViews(ctx, append(userIDs, messageUserIDs(page.Messages)...)); err != nil {
		return nil, err
	}
	return page, nil
}

func settingsView(u model.ChatUser) dto.Settings {
	return dto.Settings{Object: "chat_settings", AllowIncoming: u.AllowIncoming, AcceptRequests: u.AcceptRequests, AllowGroupInvites: u.AllowGroupInvites}
}

func (s *Service) Settings(ctx context.Context, a Actor) (*dto.Settings, error) {
	u, err := s.settingsOf(s.db.WithContext(ctx), a.UserID)
	if err != nil {
		return nil, err
	}
	v := settingsView(u)
	return &v, nil
}

type SettingsPatch struct {
	AllowIncoming     *string
	AcceptRequests    *bool
	AllowGroupInvites *string
}

func validAllow(v string) bool {
	return v == model.AllowAll || v == model.AllowFollowing || v == model.AllowNone
}

func (s *Service) UpdateSettings(ctx context.Context, a Actor, p SettingsPatch) (*dto.Settings, error) {
	if p.AllowIncoming != nil && !validAllow(*p.AllowIncoming) {
		return nil, &InvalidError{Field: "allow_incoming", Reason: "must be all, following or none"}
	}
	if p.AllowGroupInvites != nil && !validAllow(*p.AllowGroupInvites) {
		return nil, &InvalidError{Field: "allow_group_invites", Reason: "must be all, following or none"}
	}
	var out dto.Settings
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := s.now()
		if err := ensureChatUsers(tx, []int64{a.UserID}, now); err != nil {
			return err
		}
		set := map[string]any{"updated_at": now}
		if p.AllowIncoming != nil {
			set["allow_incoming"] = *p.AllowIncoming
		}
		if p.AcceptRequests != nil {
			set["accept_requests"] = *p.AcceptRequests
		}
		if p.AllowGroupInvites != nil {
			set["allow_group_invites"] = *p.AllowGroupInvites
		}
		if err := tx.Model(&model.ChatUser{}).Where("user_id = ?", a.UserID).Updates(set).Error; err != nil {
			return err
		}
		u, err := s.settingsOf(tx, a.UserID)
		out = settingsView(u)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}
