package service

import (
	"context"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"

	"gorm.io/gorm"
)

const blockingLimit = 10000

// Block also removes the follow edges between the two users in both
// directions, and neither can follow the other again while the block stands.
func (s *FollowService) Block(ctx context.Context, site string, blockerID, blockedID int64) (created bool, err error) {
	if err := validateFollowIDs(blockerID, blockedID); err != nil {
		return false, err
	}
	if blockerID == blockedID {
		return false, &InvalidError{Reason: "cannot block yourself"}
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := repository.LockUserPairTx(tx, blockerID, blockedID); err != nil {
			return err
		}
		id, err := repository.InsertBlock(tx, site, blockerID, blockedID)
		if err != nil {
			return err
		}
		if id == 0 {
			return nil
		}
		n, err := repository.CountBlocking(tx, blockerID)
		if err != nil {
			return err
		}
		if n > blockingLimit {
			return &InvalidError{Reason: "blocking limit reached (max 10000)"}
		}
		if _, err := repository.DeleteFollow(tx, blockerID, blockedID); err != nil {
			return err
		}
		if _, err := repository.DeleteFollow(tx, blockedID, blockerID); err != nil {
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return created, nil
}

func (s *FollowService) Unblock(ctx context.Context, blockerID, blockedID int64) (bool, error) {
	if err := validateFollowIDs(blockerID, blockedID); err != nil {
		return false, err
	}
	return repository.DeleteBlock(s.db.WithContext(ctx), blockerID, blockedID)
}

func (s *FollowService) ListBlocking(userID, beforeID int64, limit int) ([]model.CommunityUserBlock, error) {
	return repository.ListBlocking(s.db, userID, beforeID, clampLimit(limit))
}
