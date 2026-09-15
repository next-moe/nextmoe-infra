package handler

import (
	"testing"

	"api/internal/platform/community/dto"
	"api/internal/platform/community/service"
)

// Every id-addressed handler must stamp the caller's site onto the context; a
// handler that forgets leaves its guard reading an empty site and waves every
// tenant through.
func TestEngagementHandlers_StampTheCallerSite(t *testing.T) {
	cleanTables(t)
	sink := service.NoopSink{}
	s := &Server{
		threads:    service.NewThreadService(testDB, sink),
		posts:      service.NewPostService(testDB, sink),
		trust:      service.NewTrustService(testDB),
		engagement: service.NewEngagementService(testDB),
	}
	mine, theirs := clientCtx("letmoe"), clientCtx("kungal")

	topic, err := s.openTopic(mine, &openTopicInput{Body: dto.OpenTopicRequest{
		AuthorID: 100, AnchorID: "1", Title: "t", Body: "opening",
	}})
	if err != nil {
		t.Fatalf("openTopic: %v", err)
	}
	threadID := topic.Body.Data.Thread.ID

	read, err := s.markThreadRead(mine, &threadReadInput{ID: threadID, Body: dto.ThreadReadRequest{UserID: 300, LastReadPostNumber: 1}})
	if err != nil {
		t.Fatalf("markThreadRead: %v", err)
	}
	if read.Body.Data.LastReadPostNumber != 1 || read.Body.Data.UnreadCount != 0 {
		t.Fatalf("unexpected state: %+v", read.Body.Data)
	}
	if _, err := s.markThreadRead(theirs, &threadReadInput{ID: threadID, Body: dto.ThreadReadRequest{UserID: 300, LastReadPostNumber: 1}}); err == nil {
		t.Fatal("another tenant must not mark this thread read")
	}
	if _, err := s.setThreadNotification(theirs, &threadNotificationInput{ID: threadID, Body: dto.ThreadNotificationRequest{UserID: 300, Level: 3}}); err == nil {
		t.Fatal("another tenant must not set this thread's notification level")
	}

	if _, err := s.setThreadNotification(mine, &threadNotificationInput{ID: threadID, Body: dto.ThreadNotificationRequest{UserID: 300, Level: 9}}); err == nil {
		t.Fatal("an out-of-range level must be refused")
	}

	states, err := s.threadStates(mine, &threadStatesInput{Body: dto.ThreadStatesRequest{UserID: 300, ThreadIDs: []int64{threadID}}})
	if err != nil {
		t.Fatalf("threadStates: %v", err)
	}
	if len(states.Body.Data.States) != 1 {
		t.Fatalf("expected one state row, got %+v", states.Body.Data.States)
	}
	foreign, err := s.threadStates(theirs, &threadStatesInput{Body: dto.ThreadStatesRequest{UserID: 300, ThreadIDs: []int64{threadID}}})
	if err != nil {
		t.Fatalf("threadStates (other tenant): %v", err)
	}
	if len(foreign.Body.Data.States) != 0 {
		t.Fatal("another tenant must not read this thread's state")
	}

	if err := testDB.Exec(
		`INSERT INTO community_trust (user_id, level, first_posts_held_remaining) VALUES (200, 1, 0)
		 ON CONFLICT (user_id) DO UPDATE SET level = 1, first_posts_held_remaining = 0`,
	).Error; err != nil {
		t.Fatalf("seed trust: %v", err)
	}
	if _, err := s.reply(mine, &replyInput{ID: threadID, Body: dto.ReplyRequest{AuthorID: 200, Body: "r"}}); err != nil {
		t.Fatalf("reply: %v", err)
	}

	unread, err := s.listUnread(mine, &unreadListInput{ID: 300})
	if err != nil {
		t.Fatalf("listUnread: %v", err)
	}
	if unread.Body.Data.Total != 1 || len(unread.Body.Data.Threads) != 1 {
		t.Fatalf("the reader should have exactly one unread thread: %+v", unread.Body.Data)
	}
	if unread.Body.Data.Threads[0].State.UnreadCount != 1 {
		t.Fatalf("unread count should be 1: %+v", unread.Body.Data.Threads[0].State)
	}
	empty, err := s.listUnread(theirs, &unreadListInput{ID: 300})
	if err != nil {
		t.Fatalf("listUnread (other tenant): %v", err)
	}
	if empty.Body.Data.Total != 0 || len(empty.Body.Data.Threads) != 0 {
		t.Fatalf("another tenant must see none of it: %+v", empty.Body.Data)
	}
}
