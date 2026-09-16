package service

import (
	"context"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"

	"gorm.io/gorm"
)

type AnchorRef struct {
	AnchorKind int16
	AnchorID   string
}

type AnchorState struct {
	UserID            int64
	AnchorKind        int16
	AnchorID          string
	NotificationLevel int16
}

func (s *EngagementService) SetAnchorLevel(ctx context.Context, site string, userID int64, anchorKind int16, anchorID string, level int16) (*AnchorState, error) {
	if err := validateAnchorIdentity(userID, anchorKind, anchorID); err != nil {
		return nil, err
	}
	if err := validateAnchorLevel(level); err != nil {
		return nil, err
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if anchorKind == model.AnchorKindBoard {
			id, err := lockBoardAnchorTx(tx, site, anchorID)
			if err != nil {
				return err
			}
			anchorID = model.BoardAnchorID(id)
		}
		if level == model.NotificationLevelNormal {
			return repository.DeleteAnchorUserTx(tx, site, userID, anchorKind, anchorID)
		}
		return repository.UpsertAnchorUserTx(tx, site, userID, anchorKind, anchorID, level)
	})
	if err != nil {
		return nil, err
	}
	return &AnchorState{UserID: userID, AnchorKind: anchorKind, AnchorID: anchorID, NotificationLevel: level}, nil
}

func (s *EngagementService) AnchorStates(site string, userID int64, anchors []AnchorRef) ([]model.CommunityAnchorUser, error) {
	if userID <= 0 {
		return nil, &InvalidError{Reason: "user_id must be positive"}
	}
	if len(anchors) > 100 {
		return nil, &InvalidError{Reason: "too many anchors (max 100)"}
	}
	keys := make([]repository.AnchorKey, len(anchors))
	for i, a := range anchors {
		keys[i] = repository.AnchorKey{Kind: a.AnchorKind, ID: a.AnchorID}
	}
	return s.rows.AnchorStates(site, userID, keys)
}

func (s *EngagementService) ListAnchorSubscriptions(site string, userID int64, anchorKind int16, afterID int64, limit int) ([]model.CommunityAnchorUser, error) {
	if userID <= 0 {
		return nil, &InvalidError{Reason: "user_id must be positive"}
	}
	return s.rows.ListAnchorSubscriptions(site, userID, anchorKind, afterID, clampLimit(limit))
}

// A board delete removes the board's subscriptions in its own transaction, so
// a subscribe that only read the board could land between that delete and the
// commit and leave a row naming a board that no longer exists.
func lockBoardAnchorTx(tx *gorm.DB, site, anchorID string) (int64, error) {
	id, ok := model.BoardIDFromAnchor(model.AnchorKindBoard, anchorID)
	if !ok {
		return 0, ErrBoardNotFound
	}
	b, err := repository.LockBoardTx(tx, site, id, repository.LockShare)
	if err != nil {
		return 0, err
	}
	if b == nil {
		return 0, ErrBoardNotFound
	}
	return id, nil
}

func validateAnchorIdentity(userID int64, anchorKind int16, anchorID string) error {
	if userID <= 0 {
		return &InvalidError{Reason: "user_id must be positive"}
	}
	if anchorID == "" {
		return &InvalidError{Reason: "anchor_id is required"}
	}
	if anchorKind < model.AnchorKindBoard || anchorKind > model.AnchorKindCatalogPerson {
		return &InvalidError{Reason: "anchor_kind must be 0-4"}
	}
	return nil
}

func validateAnchorLevel(level int16) error {
	switch level {
	case model.NotificationLevelMuted, model.NotificationLevelNormal, model.NotificationLevelWatching, model.NotificationLevelWatchingFirstPost:
		return nil
	case model.NotificationLevelTracking:
		return &InvalidError{Reason: "tracking is a thread-only level"}
	default:
		return &InvalidError{Reason: "notification level out of range"}
	}
}
