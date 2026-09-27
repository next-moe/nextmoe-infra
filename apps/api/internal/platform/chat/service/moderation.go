package service

import (
	"context"
	"errors"
	"strconv"

	"api/internal/platform/chat/model"
	"api/pkg/trustclient"

	"gorm.io/gorm"
)

type DispositionResult int

const (
	DispositionApplied DispositionResult = iota
	DispositionUnsupported
)

func (s *Service) ApplyDisposition(ctx context.Context, cb trustclient.Callback) (DispositionResult, error) {
	if cb.SubjectKind != SubjectKind {
		return DispositionUnsupported, nil
	}
	var resolution string
	switch cb.Action {
	case trustclient.ActionHide, trustclient.ActionRemove:
		resolution = model.ResolutionRemoved
	case trustclient.ActionNone:
		resolution = model.ResolutionDismissed
	default:
		return DispositionUnsupported, nil
	}
	messageID, err := strconv.ParseInt(cb.SubjectID, 10, 64)
	if err != nil {
		return DispositionUnsupported, nil
	}
	conversationID, err := s.conversationOfMessage(ctx, messageID)
	if errors.Is(err, ErrNotFound) {
		return DispositionApplied, nil
	}
	if err != nil {
		return 0, err
	}
	var updates []model.ChatUpdate
	err = s.withRetry(ctx, func(tx *gorm.DB) error {
		updates = nil
		now := s.now()
		if resolution == model.ResolutionRemoved {
			conv, err := lockConversation(tx, conversationID)
			if err != nil && !errors.Is(err, ErrNotFound) {
				return err
			}
			if conv != nil {
				members, err := lockMembers(tx, conv.ID)
				if err != nil {
					return err
				}
				var targets []model.ChatMessage
				if err := tx.Where("id = ? AND deleted_at IS NULL", messageID).Find(&targets).Error; err != nil {
					return err
				}
				if updates, err = eraseTx(tx, conv.ID, members, targets, now); err != nil {
					return err
				}
			}
		}
		open := "resolution IS NULL"
		if resolution == model.ResolutionRemoved {
			open = "resolution IS DISTINCT FROM 'removed'"
		}
		return tx.Model(&model.ChatReport{}).Where("message_id = ? AND "+open, messageID).
			Updates(map[string]any{"resolution": resolution, "trust_disposition_id": cb.DispositionID, "resolved_at": now}).Error
	})
	if err != nil {
		return 0, err
	}
	s.publishUpdates(ctx, updates, nil)
	return DispositionApplied, nil
}
