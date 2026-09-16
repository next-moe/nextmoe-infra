package service

import (
	"context"
	"errors"
	"time"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type FeedbackService struct {
	db   *gorm.DB
	sink EventSink
}

func NewFeedbackService(db *gorm.DB, sink EventSink) *FeedbackService {
	return &FeedbackService{db: db, sink: sink}
}

func (s *FeedbackService) SetStatus(ctx context.Context, threadID int64, fbStatus int16, responderID int64, response *string) error {
	now := time.Now()
	updates := map[string]any{
		"fb_status":       fbStatus,
		"fb_responder_id": responderID,
		"fb_responded_at": now,
		"updated_at":      now,
	}
	if response != nil {
		updates["fb_response"] = *response
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var before model.CommunityThread
		if err := feedbackScope(tx, callerSite(ctx), threadID).
			Clauses(clause.Locking{Strength: repository.LockUpdate}).Take(&before).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFeedback
			}
			return err
		}
		if err := feedbackScope(tx, callerSite(ctx), threadID).Updates(updates).Error; err != nil {
			return err
		}
		sameStatus := before.FbStatus != nil && *before.FbStatus == fbStatus
		sameResponse := response == nil || (before.FbResponse != nil && *before.FbResponse == *response)
		if sameStatus && sameResponse {
			return nil
		}
		thread := &before
		site := callerSite(ctx)
		if site == "" {
			site = thread.Site
		}
		return repository.EnqueueEventTx(tx, &model.CommunityEvent{
			Site: site, Kind: model.EventKindFeedbackStatusChanged,
			ThreadID: threadID, ActorID: responderID,
			AttemptAfter: now,
		})
	})
	if err != nil {
		return err
	}
	s.sink.Emit(Event{Kind: EventFeedbackStatusChanged, ThreadID: threadID, ActorID: responderID})
	return nil
}

func (s *FeedbackService) Merge(ctx context.Context, threadID, intoID int64) error {
	res := feedbackScope(s.db.WithContext(ctx), callerSite(ctx), threadID).
		Updates(map[string]any{
			"merged_into_id": intoID,
			"status":         model.ThreadStatusClosed,
			"updated_at":     time.Now(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFeedback
	}
	return nil
}

func feedbackScope(db *gorm.DB, site string, threadID int64) *gorm.DB {
	q := db.Model(&model.CommunityThread{}).
		Where("id = ? AND kind = ?", threadID, model.ThreadKindFeedback)
	if site != "" {
		q = q.Where("site = ?", site)
	}
	return q
}

func (s *FeedbackService) Unmerge(ctx context.Context, threadID int64) error {
	res := s.db.WithContext(ctx).Model(&model.CommunityThread{}).
		Where("id = ? AND kind = ?", threadID, model.ThreadKindFeedback).
		Updates(map[string]any{
			"merged_into_id": gorm.Expr("NULL"),
			"status":         model.ThreadStatusOpen,
			"updated_at":     time.Now(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFeedback
	}
	return nil
}
