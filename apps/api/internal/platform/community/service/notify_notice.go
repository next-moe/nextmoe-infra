package service

import (
	"context"
	"encoding/json"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"
	"api/internal/platform/notice"

	"gorm.io/gorm"
)

const (
	noticeConsumer  = "community"
	noticeBatchSize = 100
)

func (s *NotificationService) WithNoticeSource(platform *gorm.DB) *NotificationService {
	s.notices = platform
	return s
}

func (s *NotificationService) DeliverNotices(ctx context.Context) (delivered int, err error) {
	if s.notices == nil {
		return 0, nil
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var locked bool
		if e := tx.Raw("SELECT pg_try_advisory_xact_lock(?) AS locked", NotifyDispatchLockKey).
			Scan(&locked).Error; e != nil {
			return e
		}
		if !locked {
			return nil
		}
		last, e := notice.Position(tx, noticeConsumer)
		if e != nil {
			return e
		}
		rows, e := notice.After(s.notices.WithContext(ctx), last, noticeBatchSize)
		if e != nil || len(rows) == 0 {
			return e
		}
		for i := range rows {
			if e := writeNoticeNotification(tx, &rows[i]); e != nil {
				return e
			}
		}
		delivered = len(rows)
		return notice.Advance(tx, noticeConsumer, rows[len(rows)-1].ID)
	})
	return delivered, err
}

func writeNoticeNotification(tx *gorm.DB, n *notice.Notice) error {
	payload, err := json.Marshal(model.NoticePayload{
		ID: n.ID, Topic: n.Topic, Ref: n.Ref, Title: n.Title, URL: n.URL, RaisedAt: n.CreatedAt,
	})
	if err != nil {
		return err
	}
	return repository.UpsertNoticeNotificationTx(tx, n.Site, n.UserID, repository.NoticeFoldKey(n.Topic, n.Ref), payload)
}
