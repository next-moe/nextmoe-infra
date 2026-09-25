package service

import (
	"context"
	"time"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"

	"gorm.io/gorm"
)

const followingLimit = 5000

type FollowService struct{ db *gorm.DB }

func NewFollowService(db *gorm.DB) *FollowService { return &FollowService{db: db} }

type FollowState struct {
	UserID         int64
	FollowersCount int64
	FollowingCount int64
	ViewerFollows  bool
	FollowsViewer  bool
}

func (s *FollowService) Follow(ctx context.Context, site string, followerID, followeeID int64) (created bool, err error) {
	if err := validateFollowIDs(followerID, followeeID); err != nil {
		return false, err
	}
	if followerID == followeeID {
		return false, &InvalidError{Reason: "cannot follow yourself"}
	}
	var inserted bool
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		id, err := repository.InsertFollow(tx, site, followerID, followeeID)
		if err != nil {
			return err
		}
		if id == 0 {
			return nil
		}
		n, err := repository.CountFollowing(tx, followerID)
		if err != nil {
			return err
		}
		if n > followingLimit {
			return &InvalidError{Reason: "following limit reached (max 5000)"}
		}
		if err := repository.EnqueueEventTx(tx, &model.CommunityEvent{
			Site: site, Kind: model.EventKindUserFollowed,
			ThreadID: 0, ActorID: followerID, TargetUserID: &followeeID,
			AttemptAfter: time.Now(),
		}); err != nil {
			return err
		}
		inserted = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return inserted, nil
}

func (s *FollowService) Unfollow(ctx context.Context, followerID, followeeID int64) (bool, error) {
	if err := validateFollowIDs(followerID, followeeID); err != nil {
		return false, err
	}
	return repository.DeleteFollow(s.db.WithContext(ctx), followerID, followeeID)
}

func (s *FollowService) ListFollowers(userID, beforeID int64, limit int) ([]model.CommunityUserFollow, error) {
	return repository.ListFollowers(s.db, userID, beforeID, clampLimit(limit))
}

func (s *FollowService) ListFollowing(userID, beforeID int64, limit int) ([]model.CommunityUserFollow, error) {
	return repository.ListFollowing(s.db, userID, beforeID, clampLimit(limit))
}

func (s *FollowService) States(viewerID int64, userIDs []int64) ([]FollowState, error) {
	if len(userIDs) > 100 {
		return nil, &InvalidError{Reason: "too many user_ids (max 100)"}
	}
	if viewerID < 0 {
		return nil, &InvalidError{Reason: "user ids must be positive"}
	}
	seen := make(map[int64]struct{}, len(userIDs))
	ids := make([]int64, 0, len(userIDs))
	for _, id := range userIDs {
		if id <= 0 {
			return nil, &InvalidError{Reason: "user ids must be positive"}
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return []FollowState{}, nil
	}
	followers, err := repository.FollowerCounts(s.db, ids)
	if err != nil {
		return nil, err
	}
	following, err := repository.FollowingCounts(s.db, ids)
	if err != nil {
		return nil, err
	}
	viewerFollows := map[int64]struct{}{}
	followsViewer := map[int64]struct{}{}
	if viewerID > 0 {
		followed, err := repository.FollowingAmong(s.db, viewerID, ids)
		if err != nil {
			return nil, err
		}
		for _, id := range followed {
			viewerFollows[id] = struct{}{}
		}
		followersOfViewer, err := repository.FollowersAmong(s.db, viewerID, ids)
		if err != nil {
			return nil, err
		}
		for _, id := range followersOfViewer {
			followsViewer[id] = struct{}{}
		}
	}
	out := make([]FollowState, len(ids))
	for i, id := range ids {
		_, viewerFollowsID := viewerFollows[id]
		_, idFollowsViewer := followsViewer[id]
		out[i] = FollowState{
			UserID:         id,
			FollowersCount: followers[id],
			FollowingCount: following[id],
			ViewerFollows:  viewerFollowsID,
			FollowsViewer:  idFollowsViewer,
		}
	}
	return out, nil
}

func validateFollowIDs(followerID, followeeID int64) error {
	if followerID <= 0 || followeeID <= 0 {
		return &InvalidError{Reason: "user ids must be positive"}
	}
	return nil
}
