package service

import (
	"context"
	"errors"
	"time"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"

	"gorm.io/gorm"
)

// WriteKey makes a create face safe to retry: a later request carrying the same
// Key in the same tenant answers with the post the first one wrote instead of
// writing another. The zero value writes unconditionally.
type WriteKey struct {
	Key         string
	RequestHash string
}

const WriteKeyRetain = 24 * time.Hour

var errWriteKeyTaken = errors.New("community: idempotency key already committed")

func claimWriteKeyTx(tx *gorm.DB, site string, k WriteKey) (int64, error) {
	if k.Key == "" {
		return 0, nil
	}
	id, err := repository.ClaimWriteRequestTx(tx, site, k.Key, k.RequestHash)
	if err != nil {
		return 0, err
	}
	if id == 0 {
		return 0, errWriteKeyTaken
	}
	return id, nil
}

func bindWriteKeyTx(tx *gorm.DB, requestID, postID int64) error {
	if requestID == 0 {
		return nil
	}
	return repository.BindWriteRequestTx(tx, requestID, postID)
}

func (s *PostService) keyedPost(ctx context.Context, site string, k WriteKey) (*model.CommunityPost, error) {
	if k.Key == "" {
		return nil, nil
	}
	db := s.db.WithContext(ctx)
	req, err := repository.FindWriteRequest(db, site, k.Key)
	if err != nil || req == nil || req.PostID == nil {
		return nil, err
	}
	if req.RequestHash != k.RequestHash {
		return nil, &ConflictError{Reason: "Idempotency-Key was reused with a different request"}
	}
	post, err := repository.GetPostTx(db, *req.PostID)
	if err != nil {
		return nil, err
	}
	if post == nil {
		return nil, ErrPostNotFound
	}
	return post, nil
}

func (s *PostService) PruneWriteKeys(ctx context.Context) (int64, error) {
	return repository.PruneWriteRequests(s.db.WithContext(ctx), WriteKeyRetain)
}
