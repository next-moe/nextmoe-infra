package service

import (
	"context"
	"log/slog"

	"api/internal/platform/community/repository"

	"gorm.io/gorm"
)

type PurgeResult struct {
	PostsPurged                int64
	ReactionsDeleted           int64
	ReadStatesDeleted          int64
	AnchorSubscriptionsDeleted int64
	NotificationsDeleted       int64
}

func (s *PostService) ListAuthorPosts(site string, authorID, after int64, anchorKind int16, limit int) ([]repository.AuthorPostRow, error) {
	return s.posts.ListAuthorVisiblePosts(site, authorID, after, anchorKind, limit)
}

func (s *PostService) AuthorStats(site string, authorIDs []int64, kind, anchorKind int16) (map[int64]int64, error) {
	return s.posts.CountAuthorVisiblePosts(site, authorIDs, kind, anchorKind)
}

func (s *PostService) TopAuthors(site string, kind, anchorKind int16, limit int) ([]repository.AuthorStatRow, error) {
	return s.posts.TopAuthors(site, kind, anchorKind, limit)
}

func (s *PostService) ResolvePosts(site string, ids []int64) ([]repository.AuthorPostRow, error) {
	return s.posts.ResolveVisiblePosts(site, ids)
}

func (s *PostService) PurgeAuthor(ctx context.Context, site string, authorID int64) (PurgeResult, error) {
	var res PurgeResult
	var actorsCleared, eventsDeleted, eventsForgotten int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// A dispatch batch that read this user's rows before the purge committed
		// would insert their notification after the purge deleted the others.
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", NotifyDispatchLockKey).Error; err != nil {
			return err
		}
		posts, err := repository.PurgeAuthorPostsTx(tx, site, authorID)
		if err != nil {
			return err
		}
		reactions, err := repository.DeleteAuthorReactionsTx(tx, site, authorID)
		if err != nil {
			return err
		}
		readStates, err := repository.DeleteAuthorThreadUsersTx(tx, site, authorID)
		if err != nil {
			return err
		}
		anchorSubs, err := repository.DeleteAuthorAnchorSubscriptionsTx(tx, site, authorID)
		if err != nil {
			return err
		}
		notifs, err := repository.DeleteAuthorNotificationsTx(tx, site, authorID)
		if err != nil {
			return err
		}
		actorsCleared, err = repository.ClearAuthorNotificationActorsTx(tx, site, authorID)
		if err != nil {
			return err
		}
		eventsDeleted, err = repository.DeleteAuthorEventsTx(tx, site, authorID)
		if err != nil {
			return err
		}
		eventsForgotten, err = repository.ForgetUserInPendingEventsTx(tx, site, authorID)
		if err != nil {
			return err
		}
		res = PurgeResult{
			PostsPurged: posts, ReactionsDeleted: reactions, ReadStatesDeleted: readStates,
			AnchorSubscriptionsDeleted: anchorSubs, NotificationsDeleted: notifs,
		}
		return nil
	})
	if err != nil {
		return PurgeResult{}, err
	}
	slog.Info("community author purge", "site", site, "author_id", authorID,
		"posts_purged", res.PostsPurged, "reactions_deleted", res.ReactionsDeleted,
		"read_states_deleted", res.ReadStatesDeleted,
		"anchor_subscriptions_deleted", res.AnchorSubscriptionsDeleted,
		"notifications_deleted", res.NotificationsDeleted,
		"notification_actors_cleared", actorsCleared, "events_deleted", eventsDeleted,
		"pending_events_forgotten", eventsForgotten)
	return res, nil
}
