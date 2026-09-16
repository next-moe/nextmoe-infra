package service

import (
	"context"
	"time"

	"api/internal/platform/community/model"

	"gorm.io/gorm"
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
	res := feedbackScope(s.db.WithContext(ctx), callerSite(ctx), threadID).Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFeedback
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
