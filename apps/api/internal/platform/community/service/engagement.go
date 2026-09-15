package service

import (
	"context"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"

	"gorm.io/gorm"
)

type EngagementService struct {
	db   *gorm.DB
	rows *repository.EngagementRepository
}

func NewEngagementService(db *gorm.DB) *EngagementService {
	return &EngagementService{db: db, rows: repository.NewEngagementRepository(db)}
}

// MarkRead is the reader's own receipt: the site reports how far the user has
// read. It is never inferred from a GET — a read face with a write side effect
// cannot be cached, retried or prefetched safely.
func (s *EngagementService) MarkRead(ctx context.Context, threadID, userID int64, lastRead int32) (*repository.ThreadUserState, error) {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		thread, err := s.reachableThread(ctx, tx, threadID)
		if err != nil {
			return err
		}
		if lastRead > thread.HighestPostNumber {
			lastRead = thread.HighestPostNumber
		}
		if lastRead < 0 {
			lastRead = 0
		}
		return repository.MarkReadTx(tx, threadID, userID, lastRead)
	})
	if err != nil {
		return nil, err
	}
	return s.state(threadID, userID)
}

func (s *EngagementService) SetNotificationLevel(ctx context.Context, threadID, userID int64, level int16) (*repository.ThreadUserState, error) {
	if level < model.NotificationLevelMuted || level > model.NotificationLevelWatching {
		return nil, ErrInvalidNotificationLevel
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := s.reachableThread(ctx, tx, threadID); err != nil {
			return err
		}
		return repository.SetNotificationLevelTx(tx, threadID, userID, level)
	})
	if err != nil {
		return nil, err
	}
	return s.state(threadID, userID)
}

func (s *EngagementService) States(site string, userID int64, threadIDs []int64) ([]repository.ThreadUserState, error) {
	return s.rows.States(site, userID, threadIDs)
}

func (s *EngagementService) ListUnread(site string, userID int64, cursor repository.ThreadCursor, limit int) ([]repository.UnreadThreadRow, int64, error) {
	rows, err := s.rows.ListUnread(site, userID, cursor, clampLimit(limit))
	if err != nil {
		return nil, 0, err
	}
	total, err := s.rows.CountUnread(site, userID)
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (s *EngagementService) reachableThread(ctx context.Context, tx *gorm.DB, threadID int64) (*model.CommunityThread, error) {
	thread, err := repository.GetThreadTx(tx, threadID)
	if err != nil {
		return nil, err
	}
	if thread == nil || crossTenantCtx(ctx, thread.Site, thread.AnchorKind) {
		return nil, ErrThreadNotFound
	}
	return thread, nil
}

func (s *EngagementService) state(threadID, userID int64) (*repository.ThreadUserState, error) {
	st, err := s.rows.State(threadID, userID)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, ErrThreadNotFound
	}
	return st, nil
}
