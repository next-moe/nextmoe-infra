package service

import (
	"context"
	"time"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"

	"gorm.io/gorm"
)

type ReactionService struct{ db *gorm.DB }

func NewReactionService(db *gorm.DB) *ReactionService { return &ReactionService{db: db} }

type ToggleResult struct {
	Added   bool
	Changed bool
	// Count is the post's like count after this toggle, read in the same
	// transaction. Without it a consumer that dropped its mirror table had to
	// re-read the post after every click to show the number the click changed.
	Count int32
	Post  repository.PostContext
}

type reactionOp int8

const (
	reactionToggle reactionOp = iota
	reactionAdd
	reactionRemove
)

func (s *ReactionService) Toggle(ctx context.Context, postID, userID int64, kind int16) (ToggleResult, error) {
	return s.react(ctx, postID, userID, kind, reactionToggle)
}

// Set puts the reaction in the state asked for and is idempotent, so a retried
// call cannot undo the first the way a retried Toggle does.
func (s *ReactionService) Set(ctx context.Context, postID, userID int64, kind int16, on bool) (ToggleResult, error) {
	op := reactionRemove
	if on {
		op = reactionAdd
	}
	return s.react(ctx, postID, userID, kind, op)
}

func (s *ReactionService) react(ctx context.Context, postID, userID int64, kind int16, op reactionOp) (ToggleResult, error) {
	var added, changed bool
	var count int32
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
		switch op {
		case reactionAdd:
			added = true
			changed, err = repository.AddReactionTx(tx, postID, userID, kind)
		case reactionRemove:
			changed, err = repository.RemoveReactionTx(tx, postID, userID, kind)
		default:
			changed = true
			added, err = repository.ToggleReactionTx(tx, postID, userID, kind)
		}
		if err != nil {
			return err
		}
		var n int64
		if err := tx.Model(&model.CommunityReaction{}).
			Where("post_id = ? AND kind = ?", postID, model.ReactionKindLike).
			Count(&n).Error; err != nil {
			return err
		}
		count = int32(n)
		if kind != model.ReactionKindLike || !changed {
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
		if err := repository.AdjustLikesTx(tx, pc.AuthorID, 0, delta); err != nil {
			return err
		}
		if !added {
			return nil
		}
		site := callerSite(ctx)
		if site == "" {
			site = pc.Site
		}
		return repository.EnqueueEventTx(tx, &model.CommunityEvent{
			Site: site, Kind: model.EventKindPostLiked,
			ThreadID: pc.ThreadID, PostID: &postID, ActorID: userID,
			AttemptAfter: time.Now(),
		})
	})
	return ToggleResult{Added: added, Changed: changed, Count: count, Post: pc}, err
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
