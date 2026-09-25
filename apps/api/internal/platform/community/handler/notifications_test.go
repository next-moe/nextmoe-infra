package handler

import (
	"testing"

	"api/internal/platform/community/dto"
	"api/internal/platform/community/model"
	"api/internal/platform/community/service"
)

func processNotifications(t *testing.T) {
	t.Helper()
	if _, _, _, err := service.NewNotificationService(testDB).ProcessBatch(clientCtx("letmoe")); err != nil {
		t.Fatalf("ProcessBatch: %v", err)
	}
}

func TestNotificationFaces(t *testing.T) {
	cleanTables(t)
	s := newTenantServer()
	ctx := clientCtx("letmoe")
	seedTL1(t, 100)
	seedTL1(t, 200)

	topic, err := s.openTopic(ctx, &openTopicInput{Body: dto.OpenTopicRequest{
		AuthorID: 100, BoardID: testBoard(t, "letmoe", "b1"), Title: "t", Body: "opening",
	}})
	if err != nil {
		t.Fatalf("openTopic: %v", err)
	}
	threadID := topic.Body.Data.Thread.ID
	target := int64(100)
	for i := range 3 {
		if _, err := s.reply(ctx, &replyInput{ID: threadID, Body: dto.ReplyRequest{
			AuthorID: 200, Body: "r", TargetUserID: &target,
		}}); err != nil {
			t.Fatalf("reply %d: %v", i, err)
		}
	}
	processNotifications(t)

	page1, err := s.listNotifications(ctx, &listNotificationsInput{ID: 100, Limit: 2})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page1.Body.Data.Notifications) != 2 || page1.Body.Data.NextCursor == "" || page1.Body.Data.UnreadCount != 3 {
		t.Fatalf("newest page: %+v", page1.Body.Data)
	}
	if page1.Body.Data.Notifications[0].Seq <= page1.Body.Data.Notifications[1].Seq {
		t.Fatalf("list must be seq DESC: %+v", page1.Body.Data.Notifications)
	}
	page2, err := s.listNotifications(ctx, &listNotificationsInput{ID: 100, Cursor: page1.Body.Data.NextCursor, Limit: 2})
	if err != nil {
		t.Fatalf("list page2: %v", err)
	}
	if len(page2.Body.Data.Notifications) != 1 {
		t.Fatalf("cursor page: %+v", page2.Body.Data)
	}
	if page2.Body.Data.Notifications[0].ID == page1.Body.Data.Notifications[0].ID ||
		page2.Body.Data.Notifications[0].ID == page1.Body.Data.Notifications[1].ID {
		t.Fatalf("cursor repeated a row")
	}

	unread, err := s.listNotifications(ctx, &listNotificationsInput{ID: 100, UnreadOnly: true, Limit: 50})
	if err != nil {
		t.Fatalf("unread_only: %v", err)
	}
	if len(unread.Body.Data.Notifications) != 3 {
		t.Fatalf("unread_only before mark: %d", len(unread.Body.Data.Notifications))
	}

	id := page1.Body.Data.Notifications[0].ID
	seq := page1.Body.Data.Notifications[0].Seq
	marked, err := s.markNotificationsRead(ctx, &markNotificationsReadInput{
		ID: 100, Body: dto.MarkNotificationsReadRequest{IDs: []int64{id}},
	})
	if err != nil {
		t.Fatalf("mark ids: %v", err)
	}
	if marked.Body.Data.Marked != 1 || marked.Body.Data.UnreadCount != 2 {
		t.Fatalf("mark ids: %+v", marked.Body.Data)
	}
	var row model.CommunityNotification
	if err := testDB.First(&row, id).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if row.ReadAt == nil || row.Seq != seq {
		t.Fatalf("marking read must leave seq unchanged: seq %d -> %d read_at=%v", seq, row.Seq, row.ReadAt)
	}

	unread, err = s.listNotifications(ctx, &listNotificationsInput{ID: 100, UnreadOnly: true, Limit: 50})
	if err != nil {
		t.Fatalf("unread_only after: %v", err)
	}
	if len(unread.Body.Data.Notifications) != 2 || unread.Body.Data.UnreadCount != 2 {
		t.Fatalf("unread_only after one mark: %+v", unread.Body.Data)
	}

	_, err = s.markNotificationsRead(ctx, &markNotificationsReadInput{ID: 100, Body: dto.MarkNotificationsReadRequest{}})
	wantStatus(t, err, 422)
	_, err = s.markNotificationsRead(ctx, &markNotificationsReadInput{
		ID: 100, Body: dto.MarkNotificationsReadRequest{IDs: []int64{id}, All: true},
	})
	wantStatus(t, err, 422)

	seqs := map[int64]int64{}
	var all []model.CommunityNotification
	if err := testDB.Where("user_id = ?", 100).Find(&all).Error; err != nil {
		t.Fatalf("all rows: %v", err)
	}
	for _, n := range all {
		seqs[n.ID] = n.Seq
	}
	allMarked, err := s.markNotificationsRead(ctx, &markNotificationsReadInput{
		ID: 100, Body: dto.MarkNotificationsReadRequest{All: true},
	})
	if err != nil {
		t.Fatalf("mark all: %v", err)
	}
	if allMarked.Body.Data.Marked != 2 || allMarked.Body.Data.UnreadCount != 0 {
		t.Fatalf("mark all: %+v", allMarked.Body.Data)
	}
	if err := testDB.Where("user_id = ?", 100).Find(&all).Error; err != nil {
		t.Fatalf("reload all: %v", err)
	}
	for _, n := range all {
		if n.Seq != seqs[n.ID] {
			t.Fatalf("mark all moved seq of %d: %d -> %d", n.ID, seqs[n.ID], n.Seq)
		}
	}

	other := clientCtx("kungal")
	listed, err := s.listNotifications(other, &listNotificationsInput{ID: 100, Limit: 50})
	if err != nil {
		t.Fatalf("other site list: %v", err)
	}
	if len(listed.Body.Data.Notifications) != 0 || listed.Body.Data.UnreadCount != 0 {
		t.Fatalf("another site must see none, got %+v", listed.Body.Data)
	}
	none, err := s.markNotificationsRead(other, &markNotificationsReadInput{
		ID: 100, Body: dto.MarkNotificationsReadRequest{All: true},
	})
	if err != nil {
		t.Fatalf("other site mark: %v", err)
	}
	if none.Body.Data.Marked != 0 {
		t.Fatalf("another site must mark none, got %d", none.Body.Data.Marked)
	}

	fresh := model.CommunityNotification{
		Site: "letmoe", UserID: 100, Kind: model.NotificationKindReplied, ThreadID: 1,
		AnchorKind: model.AnchorKindBoard, AnchorID: "1", ActorCount: 1, ItemCount: 1, Seq: 1 << 40,
	}
	if err := testDB.Create(&fresh).Error; err != nil {
		t.Fatalf("seed unread row: %v", err)
	}
	byID, err := s.markNotificationsRead(other, &markNotificationsReadInput{
		ID: 100, Body: dto.MarkNotificationsReadRequest{IDs: []int64{fresh.ID}},
	})
	if err != nil {
		t.Fatalf("other site mark by id: %v", err)
	}
	if err := testDB.First(&fresh, fresh.ID).Error; err != nil {
		t.Fatalf("reload seeded row: %v", err)
	}
	if byID.Body.Data.Marked != 0 || fresh.ReadAt != nil {
		t.Fatalf("another site must not mark this site's row by id: marked %d read_at %v", byID.Body.Data.Marked, fresh.ReadAt)
	}
}

func TestNotificationFeed(t *testing.T) {
	cleanTables(t)
	s := newTenantServer()
	ctx := clientCtx("letmoe")
	seedTL1(t, 100)
	seedTL1(t, 200)

	topic, err := s.openTopic(ctx, &openTopicInput{Body: dto.OpenTopicRequest{
		AuthorID: 100, BoardID: testBoard(t, "letmoe", "b1"), Title: "t", Body: "opening",
	}})
	if err != nil {
		t.Fatalf("openTopic: %v", err)
	}
	threadID := topic.Body.Data.Thread.ID
	if _, err := s.reply(ctx, &replyInput{ID: threadID, Body: dto.ReplyRequest{AuthorID: 200, Body: "r1"}}); err != nil {
		t.Fatalf("reply 1: %v", err)
	}
	processNotifications(t)

	feed1, err := s.notificationFeed(ctx, &notificationFeedInput{Limit: 100})
	if err != nil {
		t.Fatalf("feed: %v", err)
	}
	if len(feed1.Body.Data.Notifications) == 0 {
		t.Fatal("feed should have rows")
	}
	for i := 1; i < len(feed1.Body.Data.Notifications); i++ {
		if feed1.Body.Data.Notifications[i].Seq <= feed1.Body.Data.Notifications[i-1].Seq {
			t.Fatalf("feed must be seq ASC: %+v", feed1.Body.Data.Notifications)
		}
	}
	next := feed1.Body.Data.NextAfter
	if next != feed1.Body.Data.Notifications[len(feed1.Body.Data.Notifications)-1].Seq {
		t.Fatalf("next_after must be the last seq, got %d", next)
	}
	empty, err := s.notificationFeed(ctx, &notificationFeedInput{After: next, Limit: 100})
	if err != nil {
		t.Fatalf("empty feed: %v", err)
	}
	if len(empty.Body.Data.Notifications) != 0 || empty.Body.Data.NextAfter != next {
		t.Fatalf("empty page must return after, got %+v", empty.Body.Data)
	}

	other, err := s.notificationFeed(clientCtx("kungal"), &notificationFeedInput{Limit: 100})
	if err != nil {
		t.Fatalf("other feed: %v", err)
	}
	if len(other.Body.Data.Notifications) != 0 {
		t.Fatalf("another site's feed must exclude the rows, got %+v", other.Body.Data)
	}

	var foldID int64
	for _, n := range feed1.Body.Data.Notifications {
		if n.Kind == model.NotificationKindPosted && n.UserID == 100 {
			foldID = n.ID
		}
	}
	if foldID == 0 {
		t.Fatal("expected a posted fold for the opener")
	}
	if _, err := s.reply(ctx, &replyInput{ID: threadID, Body: dto.ReplyRequest{AuthorID: 200, Body: "r2"}}); err != nil {
		t.Fatalf("reply 2: %v", err)
	}
	processNotifications(t)
	feed2, err := s.notificationFeed(ctx, &notificationFeedInput{After: next, Limit: 100})
	if err != nil {
		t.Fatalf("feed after fold: %v", err)
	}
	found := false
	for _, n := range feed2.Body.Data.Notifications {
		if n.ID == foldID {
			found = true
			if n.Seq <= next {
				t.Fatalf("folded row must reappear with a higher seq, got %d after %d", n.Seq, next)
			}
		}
	}
	if !found {
		t.Fatalf("fold update must reappear after the cursor with the same id %d, got %+v", foldID, feed2.Body.Data.Notifications)
	}
}

func TestNotificationFeedFollowedHasNoThread(t *testing.T) {
	cleanTables(t)
	s := newTenantServer()
	s.follows = service.NewFollowService(testDB)
	ctx := clientCtx("letmoe")

	if _, err := s.followUser(ctx, &followUserInput{ID: 1, TargetID: 2}); err != nil {
		t.Fatalf("follow: %v", err)
	}
	processNotifications(t)

	feed, err := s.notificationFeed(ctx, &notificationFeedInput{Limit: 100})
	if err != nil {
		t.Fatalf("feed: %v", err)
	}
	var found *dto.NotificationView
	for i := range feed.Body.Data.Notifications {
		n := &feed.Body.Data.Notifications[i]
		if n.Kind == model.NotificationKindFollowed && n.UserID == 2 {
			found = n
			break
		}
	}
	if found == nil {
		t.Fatalf("feed must include the kind-8 row, got %+v", feed.Body.Data.Notifications)
	}
	if found.ThreadID != 0 || found.AnchorID != "" || found.BoardID != nil {
		t.Fatalf("kind 8 names no thread: thread_id=%d anchor_id=%q board_id=%v", found.ThreadID, found.AnchorID, found.BoardID)
	}
}
