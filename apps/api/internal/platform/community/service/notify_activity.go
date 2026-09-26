package service

import (
	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"

	"gorm.io/gorm"
)

func (s *NotificationService) dispatchActivityPublished(tx *gorm.DB, ev *model.CommunityEvent) (eventOutcome, error) {
	eventIDs, activityIDs, err := repository.ClaimActivityPublishedEventsTx(tx, ev.Site, ev.ActorID)
	if err != nil {
		return 0, err
	}
	if len(eventIDs) == 0 {
		return eventAlreadyDelivered, nil
	}
	since, err := repository.EarliestLiveNotifiedTx(tx, activityIDs)
	if err != nil {
		return 0, err
	}
	if since == nil {
		return eventDropped, nil
	}
	ids, err := repository.UpsertFolloweeFoldsTx(tx, ev.Site, ev.ActorID, *since)
	if err != nil {
		return 0, err
	}
	if err := repository.RecountFolloweeFoldRowsTx(tx, ev.ActorID, ids); err != nil {
		return 0, err
	}
	return eventDelivered, nil
}

func (s *NotificationService) dispatchActivityChanged(tx *gorm.DB, ev *model.CommunityEvent) (eventOutcome, error) {
	if _, err := repository.RecountFolloweeFoldKeyTx(tx, ev.Site, ev.ActorID); err != nil {
		return 0, err
	}
	if err := repository.MarkEventProcessedTx(tx, ev.ID); err != nil {
		return 0, err
	}
	return eventDelivered, nil
}
