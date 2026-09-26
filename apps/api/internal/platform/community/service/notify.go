package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"

	"gorm.io/gorm"
)

const (
	notifyBatchSize     = 50
	notifyBatchInterval = 2 * time.Second
	notifyPruneInterval = time.Hour
	notifyEventRetain   = 7 * 24 * time.Hour
	notifyReadRetain    = 90 * 24 * time.Hour
	mentionIDLimit      = 20
)

// 0x636e7466 ("cntf") must differ from dbtest.suiteLockKey 0x636f6d6d: advisory
// lock keys share one space across the whole Postgres instance.
const NotifyDispatchLockKey int64 = 0x636e7466

type NotificationService struct{ db *gorm.DB }

func NewNotificationService(db *gorm.DB) *NotificationService {
	return &NotificationService{db: db}
}

func (s *NotificationService) Run(ctx context.Context) {
	batch := time.NewTicker(notifyBatchInterval)
	prune := time.NewTicker(notifyPruneInterval)
	defer batch.Stop()
	defer prune.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-batch.C:
			if _, _, _, err := s.ProcessBatch(ctx); err != nil {
				slog.Error("community notification dispatch", "err", err)
			}
		case <-prune.C:
			if _, _, err := s.Prune(ctx); err != nil {
				slog.Error("community notification prune", "err", err)
			}
		}
	}
}

func (s *NotificationService) ProcessBatch(ctx context.Context) (delivered, parked, dropped int, err error) {
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var locked bool
		if e := tx.Raw("SELECT pg_try_advisory_xact_lock(?) AS locked", NotifyDispatchLockKey).
			Scan(&locked).Error; e != nil {
			return e
		}
		if !locked {
			return nil
		}
		events, e := repository.LockPendingEventsTx(tx, notifyBatchSize)
		if e != nil {
			return e
		}
		for i := range events {
			ev := &events[i]
			sp := fmt.Sprintf("ev%d", ev.ID)
			// gorm's postgres SavePoint and RollbackTo drop the statement's error.
			if e := tx.Exec("SAVEPOINT " + sp).Error; e != nil {
				return e
			}
			outcome, e := s.dispatchEvent(tx, ev)
			if e != nil {
				if rb := tx.Exec("ROLLBACK TO SAVEPOINT " + sp).Error; rb != nil {
					return rb
				}
				slog.Error("community notification event", "event_id", ev.ID, "kind", ev.Kind, "err", e)
				if e := repository.ParkEventTx(tx, ev.ID, ev.Attempts); e != nil {
					return e
				}
				parked++
				continue
			}
			switch outcome {
			case eventDelivered, eventAlreadyDelivered:
				delivered++
			case eventParked:
				parked++
			case eventDropped:
				dropped++
			}
		}
		return nil
	})
	return delivered, parked, dropped, err
}

func (s *NotificationService) Prune(ctx context.Context) (events, notifications int64, err error) {
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		n, e := repository.PruneProcessedEvents(tx, notifyEventRetain)
		if e != nil {
			return e
		}
		events = n
		n, e = repository.PruneReadNotifications(tx, notifyReadRetain)
		if e != nil {
			return e
		}
		notifications = n
		return nil
	})
	return events, notifications, err
}

type eventOutcome int

const (
	eventDelivered eventOutcome = iota
	eventParked
	eventDropped
	eventAlreadyDelivered
)

func (s *NotificationService) dispatchEvent(tx *gorm.DB, ev *model.CommunityEvent) (eventOutcome, error) {
	switch ev.Kind {
	case model.EventKindUserFollowed:
		return s.dispatchFollow(tx, ev)
	case model.EventKindActivityPublished:
		return s.dispatchActivityPublished(tx, ev)
	case model.EventKindActivityChanged:
		return s.dispatchActivityChanged(tx, ev)
	}
	switch ev.Kind {
	case model.EventKindPostCreated, model.EventKindPostLiked, model.EventKindFeedbackStatusChanged, model.EventKindAnswerAccepted:
	default:
		return 0, fmt.Errorf("community: unknown event kind %d", ev.Kind)
	}

	thread, err := repository.GetThreadTx(tx, ev.ThreadID)
	if err != nil {
		return 0, err
	}
	if thread == nil || thread.Status == model.ThreadStatusHidden || thread.Status == model.ThreadStatusDeleted {
		if err := repository.MarkEventProcessedTx(tx, ev.ID); err != nil {
			return 0, err
		}
		return eventDropped, nil
	}

	var post *model.CommunityPost
	if ev.Kind == model.EventKindPostCreated || ev.Kind == model.EventKindPostLiked || ev.Kind == model.EventKindAnswerAccepted {
		if ev.PostID == nil {
			if err := repository.MarkEventProcessedTx(tx, ev.ID); err != nil {
				return 0, err
			}
			return eventDropped, nil
		}
		post, err = repository.GetPostTx(tx, *ev.PostID)
		if err != nil {
			return 0, err
		}
		if post == nil || post.ThreadID != thread.ID || post.Status == model.PostStatusDeleted {
			if err := repository.MarkEventProcessedTx(tx, ev.ID); err != nil {
				return 0, err
			}
			return eventDropped, nil
		}
		if post.Status == model.PostStatusHidden {
			pending, err := repository.HasPendingReviewTx(tx, post.ID)
			if err != nil {
				return 0, err
			}
			if pending {
				if err := repository.ParkEventTx(tx, ev.ID, ev.Attempts); err != nil {
					return 0, err
				}
				return eventParked, nil
			}
			if err := repository.MarkEventProcessedTx(tx, ev.ID); err != nil {
				return 0, err
			}
			return eventDropped, nil
		}
	}

	if ev.Kind == model.EventKindAnswerAccepted && (thread.AnswerPostID == nil || *thread.AnswerPostID != post.ID) {
		if err := repository.MarkEventProcessedTx(tx, ev.ID); err != nil {
			return 0, err
		}
		return eventDropped, nil
	}

	var like *model.CommunityReaction
	if ev.Kind == model.EventKindPostLiked {
		like, err = repository.GetReactionTx(tx, post.ID, ev.ActorID, model.ReactionKindLike)
		if err != nil {
			return 0, err
		}
		if like == nil {
			if err := repository.MarkEventProcessedTx(tx, ev.ID); err != nil {
				return 0, err
			}
			return eventDropped, nil
		}
	}

	cands, err := recipientsForEvent(tx, ev, thread, post)
	if err != nil {
		return 0, err
	}
	for i := range cands {
		if err := writeNotification(tx, ev, thread, post, like, &cands[i]); err != nil {
			return 0, err
		}
	}
	if err := repository.MarkEventProcessedTx(tx, ev.ID); err != nil {
		return 0, err
	}
	return eventDelivered, nil
}

func enqueuePostCreatedTx(tx *gorm.DB, site string, threadID, postID, actorID int64, target *int64, mentions []int64) error {
	return repository.EnqueueEventTx(tx, &model.CommunityEvent{
		Site: site, Kind: model.EventKindPostCreated,
		ThreadID: threadID, PostID: &postID, ActorID: actorID,
		TargetUserID: target, MentionUserIDs: mentionIDsJSON(mentions),
		AttemptAfter: time.Now(),
	})
}

func (s *NotificationService) List(site string, userID, cursor int64, unreadOnly bool, limit int) ([]model.CommunityNotification, int64, error) {
	rows, err := repository.ListNotifications(s.db, site, userID, cursor, unreadOnly, clampLimit(limit))
	if err != nil {
		return nil, 0, err
	}
	unread, err := repository.CountUnreadNotifications(s.db, site, userID)
	if err != nil {
		return nil, 0, err
	}
	return rows, unread, nil
}

func (s *NotificationService) MarkRead(site string, userID int64, ids []int64, all bool) (int64, int64, error) {
	if all == (len(ids) > 0) {
		return 0, 0, &InvalidError{Reason: "exactly one of ids or all"}
	}
	if len(ids) > 100 {
		return 0, 0, &InvalidError{Reason: "too many ids (max 100)"}
	}
	var marked int64
	var err error
	if all {
		marked, err = repository.MarkAllNotificationsRead(s.db, site, userID)
	} else {
		marked, err = repository.MarkNotificationsReadByIDs(s.db, site, userID, ids)
	}
	if err != nil {
		return 0, 0, err
	}
	unread, err := repository.CountUnreadNotifications(s.db, site, userID)
	if err != nil {
		return 0, 0, err
	}
	return marked, unread, nil
}

func (s *NotificationService) Activities(rows []model.CommunityNotification) (map[int64]repository.NotificationActivityRow, error) {
	var ids []int64
	for i := range rows {
		if rows[i].ActivityID != nil {
			ids = append(ids, *rows[i].ActivityID)
		}
	}
	return repository.LiveActivitiesByID(s.db, ids)
}

func (s *NotificationService) Feed(site string, after int64, limit int) ([]model.CommunityNotification, int64, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := repository.ListNotificationFeed(s.db, site, after, limit)
	if err != nil {
		return nil, 0, err
	}
	next := after
	if len(rows) > 0 {
		next = rows[len(rows)-1].Seq
	}
	return rows, next, nil
}
