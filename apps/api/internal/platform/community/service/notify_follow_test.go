package service

import (
	"context"
	"testing"

	"api/internal/platform/community/model"
)

func mustFollow(t *testing.T, site string, followerID, followeeID int64) {
	t.Helper()
	created, err := NewFollowService(testDB).Follow(context.Background(), site, followerID, followeeID)
	if err != nil {
		t.Fatalf("follow %d->%d via %s: %v", followerID, followeeID, site, err)
	}
	if !created {
		t.Fatalf("follow %d->%d via %s must create", followerID, followeeID, site)
	}
}

func TestFollowEnqueuesOneEvent(t *testing.T) {
	cleanTables(t)
	s := NewFollowService(testDB)
	ctx := context.Background()

	created, err := s.Follow(ctx, "kungal", 1, 2)
	if err != nil {
		t.Fatalf("first follow: %v", err)
	}
	if !created {
		t.Fatal("first follow must create")
	}
	pending := pendingEvents(t)
	if len(pending) != 1 {
		t.Fatalf("want 1 pending event, got %d", len(pending))
	}
	ev := pending[0]
	if ev.Kind != model.EventKindUserFollowed || ev.Site != "kungal" || ev.ThreadID != 0 ||
		ev.ActorID != 1 || ev.TargetUserID == nil || *ev.TargetUserID != 2 || ev.PostID != nil {
		t.Fatalf("kind-5 event: %+v", ev)
	}

	created, err = s.Follow(ctx, "moyu", 1, 2)
	if err != nil {
		t.Fatalf("repeat follow: %v", err)
	}
	if created {
		t.Fatal("repeat follow must not create")
	}
	if n := pendingEvents(t); len(n) != 1 {
		t.Fatalf("repeat follow must enqueue nothing, pending=%d", len(n))
	}

	if err := testDB.Exec(`
		INSERT INTO community_user_follow (follower_id, followee_id, origin_site, created_at)
		SELECT 10, generate_series(11, 5010), 'kungal', now()`).Error; err != nil {
		t.Fatalf("seed 5000 edges: %v", err)
	}
	_, err = s.Follow(ctx, "kungal", 10, 5011)
	wantInvalid(t, err, "following limit reached (max 5000)")
	if n := pendingEvents(t); len(n) != 1 {
		t.Fatalf("capped follow must enqueue nothing, pending=%d", len(n))
	}
}

func TestNotifyFollowed(t *testing.T) {
	cleanTables(t)
	const followee, first, second, third int64 = 100, 1, 2, 3

	mustFollow(t, "kungal", first, followee)
	processBatch(t)
	rows := notifsOfSite(t, "kungal", followee)
	if len(rows) != 1 {
		t.Fatalf("one follow must write one row, got %+v", rows)
	}
	n := rows[0]
	if n.Kind != model.NotificationKindFollowed || n.ThreadID != 0 || n.AnchorKind != 0 || n.AnchorID != "" ||
		n.PostID != nil || n.PostNumber != nil || n.FirstPostNumber != nil ||
		n.ActorID == nil || *n.ActorID != first || n.ItemCount != 1 || n.ActorCount != 1 {
		t.Fatalf("kind-8 row: %+v", n)
	}

	mustFollow(t, "kungal", second, followee)
	processBatch(t)
	rows = notifsOfSite(t, "kungal", followee)
	if len(rows) != 1 || rows[0].ItemCount != 2 || rows[0].ActorCount != 2 ||
		rows[0].ActorID == nil || *rows[0].ActorID != second {
		t.Fatalf("second follower must fold, got %+v", rows)
	}

	if _, _, err := NewNotificationService(testDB).MarkRead("kungal", followee, []int64{rows[0].ID}, false); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	mustFollow(t, "kungal", third, followee)
	processBatch(t)
	rows = notifsOfSite(t, "kungal", followee)
	if len(rows) != 2 || rows[0].ReadAt == nil || rows[1].ReadAt != nil ||
		rows[1].Kind != model.NotificationKindFollowed || rows[1].ItemCount != 1 ||
		rows[1].ActorID == nil || *rows[1].ActorID != third {
		t.Fatalf("a follow after read must start a new row, got %+v", rows)
	}
}

func TestNotifyFollowedDeliversOnTheFollowSite(t *testing.T) {
	cleanTables(t)
	const followee, a, b int64 = 100, 1, 2

	mustFollow(t, "kungal", a, followee)
	mustFollow(t, "moyu", b, followee)
	processBatch(t)

	kungal := notifsOfSite(t, "kungal", followee)
	moyu := notifsOfSite(t, "moyu", followee)
	if len(kungal) != 1 || kungal[0].Kind != model.NotificationKindFollowed ||
		kungal[0].ActorID == nil || *kungal[0].ActorID != a {
		t.Fatalf("kungal row: %+v", kungal)
	}
	if len(moyu) != 1 || moyu[0].Kind != model.NotificationKindFollowed ||
		moyu[0].ActorID == nil || *moyu[0].ActorID != b {
		t.Fatalf("moyu row: %+v", moyu)
	}
}

func TestNotifyFollowedDropsWhenUndone(t *testing.T) {
	cleanTables(t)
	s := NewFollowService(testDB)
	ctx := context.Background()
	const follower, followee int64 = 1, 2

	mustFollow(t, "kungal", follower, followee)
	if _, err := s.Unfollow(ctx, follower, followee); err != nil {
		t.Fatalf("unfollow: %v", err)
	}
	_, _, dropped := processBatch(t)
	if dropped != 1 {
		t.Fatalf("undone follow must drop, dropped=%d", dropped)
	}
	if rows := notifsOf(t, followee); len(rows) != 0 {
		t.Fatalf("undone follow must write no row, got %+v", rows)
	}
}

func TestNotifyFollowedFoldCountsStandingFollows(t *testing.T) {
	cleanTables(t)
	s := NewFollowService(testDB)
	const followee, first, second, third int64 = 100, 1, 2, 3

	mustFollow(t, "kungal", first, followee)
	mustFollow(t, "kungal", second, followee)
	processBatch(t)
	rows := notifsOfSite(t, "kungal", followee)
	if len(rows) != 1 || rows[0].ItemCount != 2 || rows[0].ActorCount != 2 {
		t.Fatalf("two follows fold to 2/2, got %+v", rows)
	}

	if _, err := s.Unfollow(context.Background(), first, followee); err != nil {
		t.Fatalf("unfollow first: %v", err)
	}
	mustFollow(t, "kungal", third, followee)
	processBatch(t)
	rows = notifsOfSite(t, "kungal", followee)
	if len(rows) != 1 || rows[0].ItemCount != 2 || rows[0].ActorCount != 2 {
		t.Fatalf("counts must be standing follows since since_at, got %+v", rows)
	}
}

func TestNotifyFollowedDropsForgottenTarget(t *testing.T) {
	cleanTables(t)
	const follower, followee int64 = 1, 2

	mustFollow(t, "kungal", follower, followee)
	if err := testDB.Exec(`UPDATE community_event SET target_user_id = NULL WHERE kind = ?`,
		model.EventKindUserFollowed).Error; err != nil {
		t.Fatalf("null target: %v", err)
	}
	_, _, dropped := processBatch(t)
	if dropped != 1 {
		t.Fatalf("forgotten target must drop, dropped=%d", dropped)
	}
	if rows := notifsOf(t, followee); len(rows) != 0 {
		t.Fatalf("forgotten target must write no row, got %+v", rows)
	}
}

func TestNotifyFolloweeThreadCreated(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	es := NewEngagementService(testDB)
	ctx := letmoeCtx()
	const A, B, M, W, N int64 = 10, 20, 30, 40, 50

	boardID := testBoard(t, "letmoe", "b1")
	anchor := model.BoardAnchorID(boardID)
	if _, err := es.SetAnchorLevel(ctx, "letmoe", M, model.AnchorKindBoard, anchor, model.NotificationLevelMuted); err != nil {
		t.Fatalf("mute M: %v", err)
	}
	if _, err := es.SetAnchorLevel(ctx, "letmoe", W, model.AnchorKindBoard, anchor, model.NotificationLevelWatching); err != nil {
		t.Fatalf("watch W: %v", err)
	}
	mustFollow(t, "letmoe", A, B)
	mustFollow(t, "letmoe", M, B)
	mustFollow(t, "letmoe", W, B)
	mustFollow(t, "letmoe", N, B)
	processBatch(t)

	seedTrust(t, B, model.TrustLevelBasic, 0)
	th, _, err := ts.OpenTopic(ctx, OpenTopicParams{
		Site: "letmoe", AuthorID: B, BoardID: boardID,
		Title: "t", ContentRating: model.ContentRatingAll, BodyRaw: "opening",
		MentionUserIDs: []int64{N},
	})
	if err != nil {
		t.Fatalf("open topic: %v", err)
	}
	processBatch(t)

	aRows := notifsOfSite(t, "letmoe", A)
	if len(aRows) != 1 || aRows[0].Kind != model.NotificationKindFolloweeThreadCreated || aRows[0].ThreadID != th.ID {
		t.Fatalf("A should have one kind-9 on the topic, got %+v", aRows)
	}
	if n := notifsOf(t, M); len(n) != 0 {
		t.Fatalf("muted follower must get nothing, got %+v", n)
	}
	wRows := notifsOf(t, W)
	if len(wRows) != 1 || wRows[0].Kind != model.NotificationKindFolloweeThreadCreated {
		t.Fatalf("watching follower should have one kind-9, got %+v kinds %v", wRows, kindsOf(wRows))
	}
	nRows := notifsOf(t, N)
	if len(nRows) != 1 || nRows[0].Kind != model.NotificationKindMentioned {
		t.Fatalf("mentioned follower should have one kind-2, got %+v kinds %v", nRows, kindsOf(nRows))
	}

	replyTo(t, ps, ctx, th.ID, B, nil, nil, "later")
	processBatch(t)
	if rows := notifsOf(t, A); len(rows) != 1 || rows[0].Kind != model.NotificationKindFolloweeThreadCreated {
		t.Fatalf("B's later reply must not notify A, got %+v", rows)
	}

	if _, _, err := ps.Comment(ctx, CommentParams{
		Site: "letmoe", AnchorKind: model.AnchorKindSiteGame, AnchorID: "g1",
		AuthorID: B, BodyRaw: "wall",
	}); err != nil {
		t.Fatalf("comment: %v", err)
	}
	processBatch(t)
	if rows := notifsOf(t, A); len(rows) != 1 {
		t.Fatalf("B's comment wall must not notify A, got %+v", rows)
	}

	openFeedback(t, ts, "letmoe", B, model.AnchorKindSiteResource, "r1", "bug")
	processBatch(t)
	if rows := notifsOf(t, A); len(rows) != 1 {
		t.Fatalf("B's feedback thread must not notify A, got %+v", rows)
	}
}

func TestReadingTheTopicClearsFolloweeThreadCreated(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	es := NewEngagementService(testDB)
	ctx := letmoeCtx()
	const A, B int64 = 10, 20

	mustFollow(t, "letmoe", A, B)
	th := openTopic(t, ts, "letmoe", B, "b1", "opening")
	processBatch(t)
	rows := notifsOf(t, A)
	if len(rows) != 1 || rows[0].Kind != model.NotificationKindFolloweeThreadCreated || rows[0].ReadAt != nil {
		t.Fatalf("A should have an unread kind-9, got %+v", rows)
	}

	if _, err := es.MarkRead(ctx, th.ID, A, getThread(t, th.ID).HighestPostNumber); err != nil {
		t.Fatalf("read topic: %v", err)
	}
	rows = notifsOf(t, A)
	if len(rows) != 1 || rows[0].ReadAt == nil {
		t.Fatalf("reading the topic must mark kind-9 read, got %+v", rows)
	}
}

func TestNotifyFollowedCountsOnlyItsSitesFollows(t *testing.T) {
	cleanTables(t)
	const followee, a, b, c int64 = 100, 1, 2, 3

	mustFollow(t, "kungal", a, followee)
	processBatch(t)
	mustFollow(t, "moyu", b, followee)
	processBatch(t)
	mustFollow(t, "kungal", c, followee)
	processBatch(t)

	kungal := notifsOfSite(t, "kungal", followee)
	if len(kungal) != 1 || kungal[0].ItemCount != 2 || kungal[0].ActorCount != 2 ||
		kungal[0].ActorID == nil || *kungal[0].ActorID != c {
		t.Fatalf("kungal row must fold a and c only, got %+v", kungal)
	}
	moyu := notifsOfSite(t, "moyu", followee)
	if len(moyu) != 1 || moyu[0].ItemCount != 1 || moyu[0].ActorCount != 1 {
		t.Fatalf("moyu row must count b only, got %+v", moyu)
	}
}

func TestNotifyFollowedDropsReimportedEdge(t *testing.T) {
	cleanTables(t)
	const follower, followee int64 = 1, 2

	mustFollow(t, "moyu", follower, followee)
	if err := testDB.Exec(`UPDATE community_user_follow SET created_at = NULL, imported_at = now()
		WHERE follower_id = ? AND followee_id = ?`, follower, followee).Error; err != nil {
		t.Fatalf("re-import edge: %v", err)
	}
	_, _, dropped := processBatch(t)
	if dropped != 1 {
		t.Fatalf("an edge with no created_at must drop its event, dropped=%d", dropped)
	}
	if rows := notifsOf(t, followee); len(rows) != 0 {
		t.Fatalf("a re-imported edge must write no row, got %+v", rows)
	}
}
