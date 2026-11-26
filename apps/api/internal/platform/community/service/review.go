package service

import (
	"context"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"

	"gorm.io/gorm"
)

type ReviewService struct {
	db      *gorm.DB
	reviews *repository.ReviewRepository
	sink    EventSink
}

func NewReviewService(db *gorm.DB, sink EventSink) *ReviewService {
	return &ReviewService{db: db, reviews: repository.NewReviewRepository(db), sink: sink}
}

func (s *ReviewService) List(site string, source int16, limit int) ([]repository.ReviewItemRow, error) {
	return s.reviews.ListPending(site, source, clampLimit(limit))
}

func (s *ReviewService) Approve(ctx context.Context, id, decidedBy int64) error {
	return s.decide(ctx, id, decidedBy, true)
}

func (s *ReviewService) Reject(ctx context.Context, id, decidedBy int64) error {
	return s.decide(ctx, id, decidedBy, false)
}

func (s *ReviewService) decide(ctx context.Context, id, decidedBy int64, approve bool) error {
	var forwarded bool
	txErr := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		item, err := repository.LockReviewItemTx(tx, id)
		if err != nil {
			return err
		}
		if item == nil {
			return ErrReviewNotFound
		}
		if cs := callerSite(ctx); cs != "" && (item.Site == nil || *item.Site != cs) {
			return ErrReviewNotFound
		}
		if item.Status != model.ReviewStatusPending {
			return nil
		}
		if item.PostID != nil {
			postID := *item.PostID
			post, err := repository.LockPostTx(tx, postID)
			if err != nil {
				return err
			}
			// An approve set every post visible, so a held post its author had
			// deleted came back once a moderator approved it.
			switch {
			case post == nil || post.Status == model.PostStatusDeleted:
			case approve:
				err = repository.SetPostStatusTx(tx, postID, model.PostStatusVisible)
			default:
				err = repository.TombstonePostTx(tx, postID, repository.Removal{Source: model.DeleteSourceReview, By: &decidedBy, Site: item.Site})
			}
			if err != nil {
				return err
			}
			if item.Source != nil && *item.Source == model.ReviewSourceFlags {
				if err := s.backfillFlags(tx, postID, approve); err != nil {
					return err
				}
			}
		}
		forwarded = item.TrustReviewItemID != nil
		status := model.ReviewStatusRejected
		if approve {
			status = model.ReviewStatusApproved
		}
		return repository.DecideReviewItemTx(tx, id, status, decidedBy)
	})
	if txErr != nil {
		return txErr
	}
	if forwarded && s.sink != nil {
		kind := EventReviewRejected
		if approve {
			kind = EventReviewApproved
		}
		s.sink.Emit(Event{Kind: kind, ReviewItemID: id, ActorID: decidedBy})
	}
	return nil
}

func (s *ReviewService) backfillFlags(tx *gorm.DB, postID int64, approve bool) error {
	flaggers, err := repository.PendingFlaggersTx(tx, postID)
	if err != nil {
		return err
	}
	reportsWereRight := !approve
	for _, f := range flaggers {
		if err := repository.IncFlagAccuracyTx(tx, f, reportsWereRight); err != nil {
			return err
		}
	}
	flagStatus := model.FlagStatusDisagreed
	if reportsWereRight {
		flagStatus = model.FlagStatusAgreed
	}
	return repository.ResolvePendingFlagsTx(tx, postID, flagStatus)
}
