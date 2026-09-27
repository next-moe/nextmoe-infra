package service

import (
	"context"
	"errors"
	"time"

	"api/internal/platform/chat/dto"
	"api/internal/platform/chat/model"

	"gorm.io/gorm"
)

// Unlike Telegram, which keeps a deleted account's messages with the people
// it talked to, every message the account sent is deleted for everyone: the
// rule community applies to its posts, and what App Store account deletion
// requires.
func (s *Service) PurgeAccount(ctx context.Context, uid int64) error {
	var convIDs []int64
	if err := s.db.WithContext(ctx).Model(&model.ChatMember{}).
		Where("user_id = ?", uid).Order("conversation_id").Pluck("conversation_id", &convIDs).Error; err != nil {
		return err
	}
	for _, id := range convIDs {
		if err := s.purgeFromConversation(ctx, id, uid); err != nil {
			return err
		}
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", uid).Delete(&model.ChatHiddenMessage{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", uid).Delete(&model.ChatReaction{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", uid).Delete(&model.ChatUpdate{}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.ChatReport{}).Where("reported_user_id = ?", uid).
			Update("snapshot", jsonOf(map[string]any{"erased": true})).Error; err != nil {
			return err
		}
		return tx.Where("user_id = ?", uid).Delete(&model.ChatUser{}).Error
	})
}

func (s *Service) purgeFromConversation(ctx context.Context, conversationID, uid int64) error {
	var (
		updates []model.ChatUpdate
		deleted []int64
	)
	err := s.withRetry(ctx, func(tx *gorm.DB) error {
		conv, err := lockConversation(tx, conversationID)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		members, err := lockMembers(tx, conv.ID)
		if err != nil {
			return err
		}
		now := s.now()
		deleted = deleted[:0]
		if err := tx.Raw(`
			UPDATE chat_message SET deleted_at = ?, text = '', entities = NULL, media = NULL,
			       reply_quote = NULL, context = NULL, pinned_at = NULL
			 WHERE conversation_id = ? AND sender_id = ? AND deleted_at IS NULL
			RETURNING seq`, now, conv.ID, uid).Scan(&deleted).Error; err != nil {
			return err
		}
		if err := tx.Exec(`DELETE FROM chat_reaction r USING chat_message x
			WHERE r.message_id = x.id AND x.conversation_id = ? AND (r.user_id = ? OR x.deleted_at IS NOT NULL)`, conv.ID, uid).Error; err != nil {
			return err
		}
		var others []int64
		for _, m := range members {
			if m.UserID != uid {
				others = append(others, m.UserID)
			}
		}
		if len(others) == 0 {
			return tx.Delete(&model.ChatConversation{}, conv.ID).Error
		}
		if err := tx.Model(&model.ChatMember{}).Where("conversation_id = ? AND user_id = ?", conv.ID, uid).
			Updates(map[string]any{"left_at": gorm.Expr("coalesce(left_at, ?)", now), "draft": nil, "pinned_rank": nil, "updated_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.ChatConversation{}).Where("id = ?", conv.ID).
			Updates(map[string]any{"member_count": len(others), "updated_at": now}).Error; err != nil {
			return err
		}
		if _, err := recountUnread(tx, conv.ID, others, now); err != nil {
			return err
		}
		var ups []pendingUpdate
		for _, id := range others {
			if len(deleted) > 0 {
				ups = append(ups, pendingUpdate{userID: id, kind: model.UpdateDeleteMessages, conversationID: conv.ID, data: map[string]any{"seqs": deleted}})
			}
			if findMember(members, uid) != nil {
				ups = append(ups, pendingUpdate{userID: id, kind: model.UpdateMember, conversationID: conv.ID, data: map[string]any{"user_id": dto.ID(uid), "action": "deleted"}})
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

func (s *Service) PruneUpdates(ctx context.Context) (int64, error) {
	cutoff := s.now().Add(-updateRetention)
	var total int64
	for {
		res := s.db.WithContext(ctx).Exec(`
			DELETE FROM chat_update WHERE ctid IN (
			    SELECT ctid FROM chat_update WHERE created_at < ? LIMIT 5000)`, cutoff)
		if res.Error != nil {
			return total, res.Error
		}
		total += res.RowsAffected
		if res.RowsAffected < 5000 {
			return total, nil
		}
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
