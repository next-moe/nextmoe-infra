package handler

import (
	"testing"

	"api/internal/platform/community/dto"
	"api/internal/platform/community/model"
	"api/internal/platform/community/service"
)

func TestAnchorFacesAreSiteScoped(t *testing.T) {
	cleanTables(t)
	sink := service.NoopSink{}
	s := &Server{
		threads:    service.NewThreadService(testDB, sink),
		posts:      service.NewPostService(testDB, sink),
		engagement: service.NewEngagementService(testDB),
		boards:     service.NewBoardService(testDB),
	}
	siteA, siteB := clientCtx("siteA"), clientCtx("siteB")
	boardID := testBoard(t, "siteA", "general")

	if _, err := s.setAnchorNotification(siteA, &anchorNotificationInput{Body: dto.AnchorNotificationRequest{
		UserID: 100, AnchorKind: model.AnchorKindBoard, AnchorID: model.BoardAnchorID(boardID), Level: model.NotificationLevelWatching,
	}}); err != nil {
		t.Fatalf("site A watch board: %v", err)
	}
	if _, err := s.setAnchorNotification(siteA, &anchorNotificationInput{Body: dto.AnchorNotificationRequest{
		UserID: 100, AnchorKind: model.AnchorKindCatalogWork, AnchorID: "w1", Level: model.NotificationLevelWatching,
	}}); err != nil {
		t.Fatalf("site A watch catalog: %v", err)
	}

	listed, err := s.listAnchorSubscriptions(siteB, &listAnchorSubscriptionsInput{ID: 100, AnchorKind: -1})
	if err != nil {
		t.Fatalf("site B list: %v", err)
	}
	if len(listed.Body.Data.Subscriptions) != 0 {
		t.Fatalf("site B must not see site A's rows, got %+v", listed.Body.Data.Subscriptions)
	}

	_, err = s.setAnchorNotification(siteB, &anchorNotificationInput{Body: dto.AnchorNotificationRequest{
		UserID: 100, AnchorKind: model.AnchorKindBoard, AnchorID: model.BoardAnchorID(boardID), Level: model.NotificationLevelWatching,
	}})
	wantStatus(t, err, 404)
}
