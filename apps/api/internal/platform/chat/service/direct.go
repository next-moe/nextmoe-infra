package service

import (
	"context"
	"errors"
	"time"

	"api/internal/platform/chat/model"

	"gorm.io/gorm"
)

const (
	requestsPerDay = 20
	newAccountAge  = 72 * time.Hour
)

func (s *Service) settingsOf(db *gorm.DB, uid int64) (model.ChatUser, error) {
	var u model.ChatUser
	err := db.Where("user_id = ?", uid).Take(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return defaultChatUser(uid, s.now()), nil
	}
	return u, err
}

func (s *Service) admitDirect(ctx context.Context, sender, recipient int64, recipientSettings model.ChatUser) (accepted bool, err error) {
	switch recipientSettings.AllowIncoming {
	case model.AllowAll:
		return true, nil
	case model.AllowFollowing:
		if s.rel != nil {
			follows, err := s.rel.Follows(ctx, recipient, sender)
			if err != nil {
				return false, err
			}
			if follows {
				return true, nil
			}
		}
	}
	if !recipientSettings.AcceptRequests {
		return false, &NotAcceptingError{Reason: "they only accept messages from people they follow"}
	}
	if err := s.checkRequestSender(ctx, sender); err != nil {
		return false, err
	}
	if err := s.allow(ctx, "request:"+itoa(sender), requestsPerDay, 24*time.Hour); err != nil {
		return false, err
	}
	return false, nil
}

func (s *Service) checkRequestSender(ctx context.Context, sender int64) error {
	if s.users == nil {
		return nil
	}
	profiles, err := s.users.Profiles(ctx, []int64{sender})
	if err != nil {
		return err
	}
	p, ok := profiles[sender]
	if !ok || s.now().Sub(p.CreatedAt) >= newAccountAge {
		return nil
	}
	level := int16(0)
	if s.rel != nil {
		if level, err = s.rel.TrustLevel(ctx, sender); err != nil {
			return err
		}
	}
	if level < 1 {
		return &NotAcceptingError{Reason: "new accounts can only message people who follow them"}
	}
	return nil
}

func (s *Service) EnsureDirect(ctx context.Context, a Actor, peerID int64) (*ConversationResult, error) {
	if peerID <= 0 {
		return nil, &InvalidError{Field: "user_id", Reason: "must be positive"}
	}
	if peerID == a.UserID {
		return nil, &InvalidError{Field: "user_id", Reason: "cannot message yourself"}
	}
	if id, err := s.findDirect(s.db.WithContext(ctx), a.UserID, peerID); err != nil {
		return nil, err
	} else if id > 0 {
		return s.Conversation(ctx, a, id)
	}
	if s.users != nil {
		profiles, err := s.users.Profiles(ctx, []int64{peerID})
		if err != nil {
			return nil, err
		}
		if p, ok := profiles[peerID]; !ok || p.Deleted {
			return nil, ErrNotFound
		}
	}
	if s.rel != nil {
		blocked, err := s.rel.BlockedEitherWay(ctx, a.UserID, peerID)
		if err != nil {
			return nil, err
		}
		if blocked {
			return nil, ErrBlocked
		}
	}
	peerSettings, err := s.settingsOf(s.db.WithContext(ctx), peerID)
	if err != nil {
		return nil, err
	}
	accepted, err := s.admitDirect(ctx, a.UserID, peerID, peerSettings)
	if err != nil {
		return nil, err
	}

	var id int64
	err = s.withRetry(ctx, func(tx *gorm.DB) error {
		now := s.now()
		low, high := a.UserID, peerID
		if low > high {
			low, high = high, low
		}
		if err := tx.Raw(`
			INSERT INTO chat_conversation
			       (kind, direct_user_low_id, direct_user_high_id, creator_id, origin_site, last_seq, member_count, created_at, updated_at)
			VALUES ('direct', ?, ?, ?, ?, 0, 2, ?, ?)
			ON CONFLICT (direct_user_low_id, direct_user_high_id) WHERE kind = 'direct' DO NOTHING
			RETURNING id`, low, high, a.UserID, a.Site, now, now).Scan(&id).Error; err != nil {
			return err
		}
		if id == 0 {
			found, err := s.findDirect(tx, a.UserID, peerID)
			id = found
			return err
		}
		var peerAccepted *time.Time
		if accepted {
			peerAccepted = &now
		}
		members := []model.ChatMember{
			newMember(id, a.UserID, model.RoleMember, now, &now),
			newMember(id, peerID, model.RoleMember, now, peerAccepted),
		}
		if err := tx.Create(&members).Error; err != nil {
			return err
		}
		return ensureChatUsers(tx, []int64{a.UserID, peerID}, now)
	})
	if err != nil {
		return nil, err
	}
	return s.Conversation(ctx, a, id)
}

func newMember(conversationID, uid int64, role string, now time.Time, acceptedAt *time.Time) model.ChatMember {
	return model.ChatMember{
		ConversationID: conversationID, UserID: uid, Role: role, JoinedAt: now,
		VisibleFromSeq: 1, AcceptedAt: acceptedAt, UpdatedAt: now,
	}
}

func (s *Service) findDirect(db *gorm.DB, a, b int64) (int64, error) {
	low, high := a, b
	if low > high {
		low, high = high, low
	}
	var ids []int64
	err := db.Model(&model.ChatConversation{}).
		Where("kind = ? AND direct_user_low_id = ? AND direct_user_high_id = ? AND deleted_at IS NULL", model.KindDirect, low, high).
		Pluck("id", &ids).Error
	if err != nil || len(ids) == 0 {
		return 0, err
	}
	return ids[0], nil
}
