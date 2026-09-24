package service

import (
	"context"
	"log/slog"
	"time"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"

	"gorm.io/gorm"
)

const PurgeArchiveRetain = 30 * 24 * time.Hour

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

type RestoreResult struct {
	PostsRestored               int64
	ReactionsRestored           int64
	ReadStatesRestored          int64
	AnchorSubscriptionsRestored int64
	NotificationsRestored       int64
}

func (s *PostService) RestoreAuthor(ctx context.Context, site string, authorID int64) (RestoreResult, error) {
	var res RestoreResult
	var actorsRestored, eventsRestored, recipientsRestored, archived int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		if res.PostsRestored, err = repository.RestorePurgedPostsTx(tx, site, authorID); err != nil {
			return err
		}
		for _, step := range []struct {
			name, table string
			n           *int64
		}{
			{model.PurgeStepReaction, "community_reaction", &res.ReactionsRestored},
			{model.PurgeStepThreadUser, "community_thread_user", &res.ReadStatesRestored},
			{model.PurgeStepAnchorUser, "community_anchor_user", &res.AnchorSubscriptionsRestored},
			{model.PurgeStepNotification, "community_notification", &res.NotificationsRestored},
			{model.PurgeStepEvent, "community_event", &eventsRestored},
		} {
			if *step.n, err = repository.ReinsertPurgedRowsTx(tx, site, authorID, step.name, step.table); err != nil {
				return err
			}
		}
		if actorsRestored, err = repository.RestoreNotificationActorsTx(tx, site, authorID); err != nil {
			return err
		}
		if recipientsRestored, err = repository.RestoreEventRecipientsTx(tx, site, authorID); err != nil {
			return err
		}
		if archived, err = repository.MarkPurgeRestoredTx(tx, site, authorID); err != nil {
			return err
		}
		if archived == 0 {
			return ErrNothingToRestore
		}
		return nil
	})
	if err != nil {
		return RestoreResult{}, err
	}
	slog.Info("community author purge restored", "site", site, "author_id", authorID,
		"archived_rows", archived, "posts_restored", res.PostsRestored,
		"reactions_restored", res.ReactionsRestored, "read_states_restored", res.ReadStatesRestored,
		"anchor_subscriptions_restored", res.AnchorSubscriptionsRestored,
		"notifications_restored", res.NotificationsRestored,
		"notification_actors_restored", actorsRestored, "events_restored", eventsRestored,
		"event_recipients_restored", recipientsRestored)
	return res, nil
}

func (s *PostService) PrunePurgeArchive(ctx context.Context) (int64, error) {
	return repository.PrunePurgeArchive(s.db.WithContext(ctx), PurgeArchiveRetain)
}
