package service

import (
	"context"
	"errors"
	"testing"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"
)

func letmoeCtx() context.Context { return WithCallerSite(context.Background(), "letmoe") }

func replyN(t *testing.T, ps *PostService, threadID, author int64, n int) {
	t.Helper()
	seedTrust(t, author, model.TrustLevelBasic, 0)
	for range n {
		if _, err := ps.Reply(letmoeCtx(), ReplyParams{ThreadID: threadID, AuthorID: author, BodyRaw: "r"}); err != nil {
			t.Fatalf("reply: %v", err)
		}
	}
}

func TestMarkRead_MonotonicAndClamped(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	es := NewEngagementService(testDB)
	th := openTopic(t, ts, "letmoe", 100, "b1", "opening")
	replyN(t, ps, th.ID, 200, 3)

	st, err := es.MarkRead(letmoeCtx(), th.ID, 300, 3)
	if err != nil {
		t.Fatalf("mark read: %v", err)
	}
	if st.LastReadPostNumber != 3 || st.UnreadCount != 1 || st.NotificationLevel != model.NotificationLevelNormal {
		t.Fatalf("unexpected state after reading to 3: %+v", st)
	}

	st, err = es.MarkRead(letmoeCtx(), th.ID, 300, 1)
	if err != nil {
		t.Fatalf("stale receipt: %v", err)
	}
	if st.LastReadPostNumber != 3 {
		t.Fatalf("a stale receipt must not un-read: got %d", st.LastReadPostNumber)
	}

	st, err = es.MarkRead(letmoeCtx(), th.ID, 300, 9999)
	if err != nil {
		t.Fatalf("overshooting receipt: %v", err)
	}
	if st.LastReadPostNumber != 4 || st.UnreadCount != 0 {
		t.Fatalf("a receipt beyond the thread must clamp to its highest post: %+v", st)
	}

	if _, err := es.MarkRead(WithCallerSite(context.Background(), "kungal"), th.ID, 300, 1); !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("another tenant must not touch this thread's read state: %v", err)
	}
}

func TestPosting_SubscribesButNeverOverridesAMute(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	es := NewEngagementService(testDB)

	th := openTopic(t, ts, "letmoe", 100, "b1", "opening")
	states, err := es.States("letmoe", 100, []int64{th.ID})
	if err != nil {
		t.Fatalf("states: %v", err)
	}
	if len(states) != 1 || states[0].NotificationLevel != model.NotificationLevelWatching || states[0].LastReadPostNumber != 1 {
		t.Fatalf("opening a thread must subscribe its author and mark it read: %+v", states)
	}

	if _, err := es.SetNotificationLevel(letmoeCtx(), th.ID, 200, model.NotificationLevelMuted); err != nil {
		t.Fatalf("mute: %v", err)
	}
	replyN(t, ps, th.ID, 200, 1)
	muted, err := es.States("letmoe", 200, []int64{th.ID})
	if err != nil {
		t.Fatalf("states: %v", err)
	}
	if len(muted) != 1 || muted[0].NotificationLevel != model.NotificationLevelMuted {
		t.Fatalf("replying must not undo a deliberate mute: %+v", muted)
	}
	if muted[0].LastReadPostNumber != 2 {
		t.Fatalf("a poster has read their own post: %+v", muted[0])
	}

	if _, err := es.SetNotificationLevel(letmoeCtx(), th.ID, 200, 7); !errors.Is(err, ErrInvalidNotificationLevel) {
		t.Fatalf("a level outside 0-3 must be refused: %v", err)
	}
}

func TestStates_SparseRowsStayAbsent(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	es := NewEngagementService(testDB)
	th := openTopic(t, ts, "letmoe", 100, "b1", "opening")

	states, err := es.States("letmoe", 999, []int64{th.ID})
	if err != nil {
		t.Fatalf("states: %v", err)
	}
	if len(states) != 0 {
		t.Fatalf("a user who never touched the thread must carry no row: %+v", states)
	}
}

func TestUnreadList_ScopeMutesAndTotal(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	es := NewEngagementService(testDB)

	mine := openTopic(t, ts, "letmoe", 100, "b1", "mine")
	mutedThread := openTopic(t, ts, "letmoe", 100, "b2", "muted")
	theirs := openTopic(t, ts, "kungal", 100, "b3", "theirs")
	shared := openFeedback(t, ts, "kungal", 100, model.AnchorKindCatalogWork, "w42", "shared")

	const reader int64 = 700
	for _, th := range []int64{mine.ID, mutedThread.ID, shared.ID} {
		if _, err := es.MarkRead(letmoeCtx(), th, reader, 1); err != nil {
			t.Fatalf("mark read %d: %v", th, err)
		}
	}
	if _, err := es.MarkRead(WithCallerSite(context.Background(), "kungal"), theirs.ID, reader, 1); err != nil {
		t.Fatalf("mark read theirs: %v", err)
	}

	replyN(t, ps, mine.ID, 200, 2)
	replyN(t, ps, mutedThread.ID, 200, 1)
	replyN(t, ps, shared.ID, 200, 1)
	if _, err := ps.Reply(WithCallerSite(context.Background(), "kungal"), ReplyParams{ThreadID: theirs.ID, AuthorID: 200, BodyRaw: "r"}); err != nil {
		t.Fatalf("reply theirs: %v", err)
	}

	if _, err := es.SetNotificationLevel(letmoeCtx(), mutedThread.ID, reader, model.NotificationLevelMuted); err != nil {
		t.Fatalf("mute: %v", err)
	}

	rows, total, err := es.ListUnread("letmoe", reader, repository.ThreadCursor{}, 50)
	if err != nil {
		t.Fatalf("list unread: %v", err)
	}
	got := map[int64]int32{}
	for _, r := range rows {
		got[r.ID] = r.UnreadCount
	}
	if len(rows) != 2 {
		t.Fatalf("expected the caller's own unread thread plus the shared anchor, got %v", got)
	}
	if got[mine.ID] != 2 {
		t.Fatalf("unread count should be highest minus last read: %v", got)
	}
	if _, ok := got[shared.ID]; !ok {
		t.Fatal("a catalog-anchored thread is one network-wide conversation and must be listed")
	}
	if _, ok := got[theirs.ID]; ok {
		t.Fatal("another site's site-local thread leaked into the unread list")
	}
	if _, ok := got[mutedThread.ID]; ok {
		t.Fatal("a muted thread must not be listed as unread")
	}
	if total != 2 {
		t.Fatalf("total should match the listed threads, got %d", total)
	}

	states, err := es.States("letmoe", reader, []int64{mutedThread.ID})
	if err != nil {
		t.Fatalf("states: %v", err)
	}
	if len(states) != 1 || states[0].UnreadCount != 1 {
		t.Fatalf("the batch face still reports a muted thread's state: %+v", states)
	}
}

func TestUnreadList_KeysetCoversEveryThread(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	es := NewEngagementService(testDB)

	const reader int64 = 800
	ids := map[int64]bool{}
	for i := range 5 {
		th := openTopic(t, ts, "letmoe", 100, string(rune('a'+i)), "x")
		if _, err := es.MarkRead(letmoeCtx(), th.ID, reader, 1); err != nil {
			t.Fatalf("mark read: %v", err)
		}
		replyN(t, ps, th.ID, 200, 1)
		ids[th.ID] = true
	}

	seen := map[int64]bool{}
	cursor := repository.ThreadCursor{}
	for {
		page, _, err := es.ListUnread("letmoe", reader, cursor, 2)
		if err != nil {
			t.Fatalf("page: %v", err)
		}
		if len(page) == 0 {
			break
		}
		for _, row := range page {
			if seen[row.ID] {
				t.Fatalf("unread keyset returned %d twice", row.ID)
			}
			seen[row.ID] = true
		}
		last := page[len(page)-1]
		cursor = repository.ThreadCursor{Sort: repository.ThreadSortActivity, LastPosted: *last.LastPostedAt, ID: last.ID}
		if len(page) < 2 {
			break
		}
	}
	if len(seen) != len(ids) {
		t.Fatalf("unread keyset covered %d of %d threads", len(seen), len(ids))
	}
}
