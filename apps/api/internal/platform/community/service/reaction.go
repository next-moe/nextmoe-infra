package service

import (
	"context"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"

	"gorm.io/gorm"
)

type ReactionService struct{ db *gorm.DB }

func NewReactionService(db *gorm.DB) *ReactionService { return &ReactionService{db: db} }

func (s *ReactionService) Toggle(ctx context.Context, postID, userID int64, kind int16) (bool, repository.PostContext, error) {
	var added bool
	var pc repository.PostContext
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		loaded, found, err := repository.PostContextTx(tx, postID)
		if err != nil {
			return err
		}
		if !found {
			return ErrPostNotFound
		}
		if crossTenantCtx(ctx, loaded.Site, loaded.AnchorKind) {
			return ErrPostNotFound
		}
		pc = loaded
		a, err := repository.ToggleReactionTx(tx, postID, userID, kind)
		if err != nil {
			return err
		}
		added = a
		if kind != model.ReactionKindLike {
			return nil
		}
		delta := int32(1)
		if !added {
			delta = -1
		}
		if _, err := repository.GetOrCreateTrustTx(tx, userID); err != nil {
			return err
		}
		if err := repository.AdjustLikesTx(tx, userID, delta, 0); err != nil {
			return err
		}
		if pc.AuthorID != userID {
			if _, err := repository.GetOrCreateTrustTx(tx, pc.AuthorID); err != nil {
				return err
			}
		}
		return repository.AdjustLikesTx(tx, pc.AuthorID, 0, delta)
	})
	return added, pc, err
}

// Counts answers how many like-reactions each post carries.
//
// The read faces hydrate this because the primitive is the only place that
// knows: before it existed, a consumer that wanted to render a like count had
// to keep its own mirror table beside every post id and dual-write it on every
// toggle, which drifts from community_reaction the first time one of the two
// writes fails, and drifts from community_trust.likes_received — which this
// service maintains from the same rows — permanently.
func (s *ReactionService) Counts(postIDs []int64) (map[int64]int32, error) {
	out := make(map[int64]int32, len(postIDs))
	if len(postIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		PostID int64 `gorm:"column:post_id"`
		N      int32 `gorm:"column:n"`
	}
	if err := s.db.Model(&model.CommunityReaction{}).
		Select("post_id, COUNT(*) AS n").
		Where("post_id IN ? AND kind = ?", postIDs, model.ReactionKindLike).
		Group("post_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.PostID] = row.N
	}
	return out, nil
}

// ReactedBy answers which of these posts one user has already liked.
func (s *ReactionService) ReactedBy(userID int64, postIDs []int64) (map[int64]bool, error) {
	out := make(map[int64]bool, len(postIDs))
	if userID <= 0 || len(postIDs) == 0 {
		return out, nil
	}
	var ids []int64
	if err := s.db.Model(&model.CommunityReaction{}).
		Where("post_id IN ? AND user_id = ? AND kind = ?", postIDs, userID, model.ReactionKindLike).
		Pluck("post_id", &ids).Error; err != nil {
		return nil, err
	}
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}
