package service

import (
	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"

	"gorm.io/gorm"
)

func (s *NotificationService) dispatchFollow(tx *gorm.DB, ev *model.CommunityEvent) (eventOutcome, error) {
	if ev.TargetUserID == nil {
		if err := repository.MarkEventProcessedTx(tx, ev.ID); err != nil {
			return 0, err
		}
		return eventDropped, nil
	}
	edge, err := repository.GetFollowTx(tx, ev.ActorID, *ev.TargetUserID)
	if err != nil {
		return 0, err
	}
	if edge == nil || edge.CreatedAt == nil {
		if err := repository.MarkEventProcessedTx(tx, ev.ID); err != nil {
			return 0, err
		}
		return eventDropped, nil
	}
	if err := writeFollowedNotification(tx, ev, edge); err != nil {
		return 0, err
	}
	if err := repository.MarkEventProcessedTx(tx, ev.ID); err != nil {
		return 0, err
	}
	return eventDelivered, nil
}

func writeFollowedNotification(tx *gorm.DB, ev *model.CommunityEvent, edge *model.CommunityUserFollow) error {
	actorID := ev.ActorID
	since := *edge.CreatedAt
	key := repository.FollowedFoldKey()
	n := model.CommunityNotification{
		Site:       ev.Site,
		UserID:     *ev.TargetUserID,
		Kind:       model.NotificationKindFollowed,
		ThreadID:   0,
		AnchorKind: 0,
		AnchorID:   "",
		SinceAt:    &since,
		ActorID:    &actorID,
		ActorCount: 1,
		ItemCount:  1,
		FoldKey:    &key,
	}
	row, err := repository.UpsertFoldedNotificationTx(tx, &n, false)
	if err != nil {
		return err
	}
	return repository.RecomputeFollowedFoldTx(tx, row.ID)
}
