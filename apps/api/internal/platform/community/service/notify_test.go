package service

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"api/internal/platform/community/model"
)

func processBatch(t *testing.T) (delivered, parked, dropped int) {
	t.Helper()
	d, p, dr, err := NewNotificationService(testDB).ProcessBatch(context.Background())
	if err != nil {
		t.Fatalf("ProcessBatch: %v", err)
	}
	return d, p, dr
}

func notifsOf(t *testing.T, userID int64) []model.CommunityNotification {
	t.Helper()
	var rows []model.CommunityNotification
	if err := testDB.Where("user_id = ?", userID).Order("id").Find(&rows).Error; err != nil {
		t.Fatalf("list notifications %d: %v", userID, err)
	}
	return rows
}

func notifsOfSite(t *testing.T, site string, userID int64) []model.CommunityNotification {
	t.Helper()
	var rows []model.CommunityNotification
	if err := testDB.Where("site = ? AND user_id = ?", site, userID).Order("id").Find(&rows).Error; err != nil {
		t.Fatalf("list notifications %s/%d: %v", site, userID, err)
	}
	return rows
}

func kindsOf(rows []model.CommunityNotification) []int16 {
	out := make([]int16, len(rows))
	for i, r := range rows {
		out[i] = r.Kind
	}
	return out
}

func pendingEvents(t *testing.T) []model.CommunityEvent {
	t.Helper()
	var rows []model.CommunityEvent
	if err := testDB.Where("processed_at IS NULL").Order("id").Find(&rows).Error; err != nil {
		t.Fatalf("list pending events: %v", err)
	}
	return rows
}

func replyTo(t *testing.T, ps *PostService, ctx context.Context, threadID, author int64, target *int64, mentions []int64, body string) *model.CommunityPost {
	t.Helper()
	seedTrust(t, author, model.TrustLevelBasic, 0)
	p, err := ps.Reply(ctx, ReplyParams{
		ThreadID: threadID, AuthorID: author, BodyRaw: body,
		TargetUserID: target, MentionUserIDs: mentions,
	})
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	return p
}

func TestNotifyReplyMentionAndWatchers(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	es := NewEngagementService(testDB)
	ctx := letmoeCtx()
	const A, B, C, E int64 = 100, 200, 300, 400

	th := openTopic(t, ts, "letmoe", A, "b1", "opening")
	if _, err := es.SetNotificationLevel(ctx, th.ID, C, model.NotificationLevelWatching); err != nil {
		t.Fatalf("C watch: %v", err)
	}
	if _, err := es.SetNotificationLevel(ctx, th.ID, E, model.NotificationLevelMuted); err != nil {
		t.Fatalf("E mute: %v", err)
	}
	target := A
	replyTo(t, ps, ctx, th.ID, B, &target, []int64{C, E}, "hi")
	processBatch(t)

	aRows := notifsOf(t, A)
	if len(aRows) != 1 || aRows[0].Kind != model.NotificationKindReplied {
		t.Fatalf("A should have one replied, got %+v", aRows)
	}
	cRows := notifsOf(t, C)
	if len(cRows) != 1 || cRows[0].Kind != model.NotificationKindMentioned {
		t.Fatalf("C should have one mentioned (not posted), got %+v kinds %v", cRows, kindsOf(cRows))
	}
	if n := notifsOf(t, E); len(n) != 0 {
		t.Fatalf("E muted the thread and must get nothing, got %+v", n)
	}
	if n := notifsOf(t, B); len(n) != 0 {
		t.Fatalf("B is the actor and must get nothing, got %+v", n)
	}

	replyTo(t, ps, ctx, th.ID, B, nil, nil, "again")
	processBatch(t)
	cRows = notifsOf(t, C)
	if len(cRows) != 2 {
		t.Fatalf("C should have mentioned + posted, got %d %+v", len(cRows), kindsOf(cRows))
	}
	got := map[int16]int{}
	for _, r := range cRows {
		got[r.Kind]++
	}
	if got[model.NotificationKindMentioned] != 1 || got[model.NotificationKindPosted] != 1 {
		t.Fatalf("C kinds: %+v", got)
	}
	aRows = notifsOf(t, A)
	gotA := map[int16]int{}
	for _, r := range aRows {
		gotA[r.Kind]++
	}
	if gotA[model.NotificationKindReplied] != 1 || gotA[model.NotificationKindPosted] != 1 {
		t.Fatalf("A kinds after second reply: %+v", gotA)
	}
}

func TestNotifyAnchorWatchers(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	es := NewEngagementService(testDB)
	ctx := letmoeCtx()
	const W, N, F, M, P, Q int64 = 10, 11, 12, 13, 20, 21

	boardID := testBoard(t, "letmoe", "b1")
	anchor := model.BoardAnchorID(boardID)
	for _, u := range []struct {
		id    int64
		level int16
	}{
		{W, model.NotificationLevelWatching},
		{N, model.NotificationLevelWatching},
		{F, model.NotificationLevelWatchingFirstPost},
		{M, model.NotificationLevelMuted},
	} {
		if _, err := es.SetAnchorLevel(ctx, "letmoe", u.id, model.AnchorKindBoard, anchor, u.level); err != nil {
			t.Fatalf("watch %d: %v", u.id, err)
		}
	}

	th := openTopic(t, ts, "letmoe", P, "b1", "topic")
	processBatch(t)
	for _, u := range []int64{W, N, F} {
		rows := notifsOf(t, u)
		if len(rows) != 1 || rows[0].Kind != model.NotificationKindThreadCreated {
			t.Fatalf("%d should have thread_created, got %+v", u, rows)
		}
	}
	if n := notifsOf(t, M); len(n) != 0 {
		t.Fatalf("muted board must get nothing for a new topic, got %+v", n)
	}

	replyTo(t, ps, ctx, th.ID, P, nil, nil, "follow")
	processBatch(t)
	for _, u := range []int64{W, N} {
		got := map[int16]int{}
		for _, r := range notifsOf(t, u) {
			got[r.Kind]++
		}
		if got[model.NotificationKindThreadCreated] != 1 || got[model.NotificationKindPosted] != 1 {
			t.Fatalf("%d after reply: %+v", u, got)
		}
	}
	if rows := notifsOf(t, F); len(rows) != 1 || rows[0].Kind != model.NotificationKindThreadCreated {
		t.Fatalf("F first-post-only must not get posted, got %+v", rows)
	}

	if _, err := es.MarkRead(ctx, th.ID, W, getThread(t, th.ID).HighestPostNumber); err != nil {
		t.Fatalf("W read: %v", err)
	}
	if _, err := es.SetNotificationLevel(ctx, th.ID, N, model.NotificationLevelNormal); err != nil {
		t.Fatalf("N normal: %v", err)
	}

	qPost := replyTo(t, ps, ctx, th.ID, Q, nil, []int64{M}, "mentions M")
	processBatch(t)

	var wUnreadPosted []model.CommunityNotification
	for _, r := range notifsOf(t, W) {
		if r.Kind == model.NotificationKindPosted && r.ReadAt == nil {
			wUnreadPosted = append(wUnreadPosted, r)
		}
	}
	if len(wUnreadPosted) != 1 || wUnreadPosted[0].PostID == nil || *wUnreadPosted[0].PostID != qPost.ID {
		t.Fatalf("W's first read seeded watching so Q's reply must land as posted, got %+v", wUnreadPosted)
	}

	nPostedPost := int32(0)
	for _, r := range notifsOf(t, N) {
		if r.Kind == model.NotificationKindPosted && r.PostNumber != nil {
			nPostedPost = *r.PostNumber
		}
	}
	if nPostedPost == qPost.PostNumber {
		t.Fatalf("N set the thread to normal so Q's reply must not notify, post_number=%d", nPostedPost)
	}
	if n := notifsOf(t, M); len(n) != 0 {
		t.Fatalf("M is muted at the board with no thread row, got %+v", n)
	}

	replyTo(t, ps, ctx, th.ID, M, nil, nil, "I speak")
	processBatch(t)
	replyTo(t, ps, ctx, th.ID, Q, nil, nil, "after M")
	processBatch(t)
	mPosted := 0
	for _, r := range notifsOf(t, M) {
		if r.Kind == model.NotificationKindPosted {
			mPosted++
		}
	}
	if mPosted != 1 {
		t.Fatalf("M's own thread row outranks the muted board, want 1 posted, got %d %+v", mPosted, notifsOf(t, M))
	}
}

func TestNotifyCommentWallFirstPost(t *testing.T) {
	cleanTables(t)
	ps := NewPostService(testDB, NoopSink{})
	es := NewEngagementService(testDB)
	ctx := letmoeCtx()
	const firstOnly, watching, author int64 = 1, 2, 3

	if _, err := es.SetAnchorLevel(ctx, "letmoe", firstOnly, model.AnchorKindSiteGame, "g1", model.NotificationLevelWatchingFirstPost); err != nil {
		t.Fatalf("first-post watch: %v", err)
	}
	if _, err := es.SetAnchorLevel(ctx, "letmoe", watching, model.AnchorKindSiteGame, "g1", model.NotificationLevelWatching); err != nil {
		t.Fatalf("watch: %v", err)
	}
	seedTrust(t, author, model.TrustLevelBasic, 0)
	if _, _, err := ps.Comment(ctx, CommentParams{
		Site: "letmoe", AnchorKind: model.AnchorKindSiteGame, AnchorID: "g1",
		AuthorID: author, BodyRaw: "first comment",
	}); err != nil {
		t.Fatalf("comment: %v", err)
	}
	processBatch(t)
	if n := notifsOf(t, firstOnly); len(n) != 0 {
		t.Fatalf("watching-first-post must ignore a comment wall's first comment, got %+v", n)
	}
	rows := notifsOf(t, watching)
	if len(rows) != 1 || rows[0].Kind != model.NotificationKindPosted {
		t.Fatalf("a watching user gets posted for the wall's first comment, got %+v", rows)
	}
}

func TestNotifyFoldsAndReadResets(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	es := NewEngagementService(testDB)
	ctx := letmoeCtx()
	const A, B, C, D int64 = 100, 200, 300, 400

	th := openTopic(t, ts, "letmoe", A, "b1", "watched")
	other := openTopic(t, ts, "letmoe", D, "b2", "other")
	target := A
	replyTo(t, ps, ctx, other.ID, B, &target, nil, "side")
	processBatch(t)
	side := notifsOf(t, A)
	if len(side) != 1 || side[0].Kind != model.NotificationKindReplied || side[0].ThreadID != other.ID {
		t.Fatalf("A should have a replied row on the other thread, got %+v", side)
	}
	sideSeq := side[0].Seq
	if side[0].ReadAt != nil {
		t.Fatal("the other thread's replied row starts unread")
	}

	p1 := replyTo(t, ps, ctx, th.ID, B, nil, nil, "1")
	processBatch(t)
	posted := notifsOfSite(t, "letmoe", A)
	var fold *model.CommunityNotification
	for i := range posted {
		if posted[i].Kind == model.NotificationKindPosted && posted[i].ThreadID == th.ID {
			fold = &posted[i]
		}
	}
	if fold == nil {
		t.Fatalf("A should have a posted fold after the first reply, got %+v", posted)
	}
	firstSeq := fold.Seq
	if fold.FirstPostNumber == nil || *fold.FirstPostNumber != p1.PostNumber || fold.PostNumber == nil || *fold.PostNumber != p1.PostNumber {
		t.Fatalf("first fold numbers: %+v (p1=%d)", fold, p1.PostNumber)
	}

	p2 := replyTo(t, ps, ctx, th.ID, C, nil, nil, "2")
	p3 := replyTo(t, ps, ctx, th.ID, B, nil, nil, "3")
	processBatch(t)
	var folded model.CommunityNotification
	if err := testDB.Where("user_id = ? AND kind = ? AND thread_id = ? AND read_at IS NULL", A, model.NotificationKindPosted, th.ID).
		First(&folded).Error; err != nil {
		t.Fatalf("reload fold: %v", err)
	}
	if folded.ItemCount != 3 || folded.ActorCount != 2 {
		t.Fatalf("fold counts: item=%d actor=%d", folded.ItemCount, folded.ActorCount)
	}
	if folded.FirstPostNumber == nil || *folded.FirstPostNumber != p1.PostNumber {
		t.Fatalf("first_post_number must stay %d, got %v", p1.PostNumber, folded.FirstPostNumber)
	}
	if folded.PostNumber == nil || *folded.PostNumber != p3.PostNumber {
		t.Fatalf("post_number must be the last reply %d, got %v", p3.PostNumber, folded.PostNumber)
	}
	if folded.Seq <= firstSeq {
		t.Fatalf("fold seq must rise (%d then %d)", firstSeq, folded.Seq)
	}
	_ = p2

	if _, err := es.MarkRead(ctx, th.ID, A, p3.PostNumber-1); err != nil {
		t.Fatalf("mark before last: %v", err)
	}
	if err := testDB.First(&folded, folded.ID).Error; err != nil {
		t.Fatalf("reload after partial read: %v", err)
	}
	if folded.ReadAt != nil {
		t.Fatal("MarkRead before the last folded post must leave the row unread")
	}

	seqBefore := folded.Seq
	if _, err := es.MarkRead(ctx, th.ID, A, p3.PostNumber); err != nil {
		t.Fatalf("mark last: %v", err)
	}
	if err := testDB.First(&folded, folded.ID).Error; err != nil {
		t.Fatalf("reload after full read: %v", err)
	}
	if folded.ReadAt == nil {
		t.Fatal("MarkRead to the last post must mark the fold read")
	}
	if folded.Seq != seqBefore {
		t.Fatalf("marking read must not change seq: %d -> %d", seqBefore, folded.Seq)
	}

	replyTo(t, ps, ctx, th.ID, C, nil, nil, "4")
	processBatch(t)
	var fresh []model.CommunityNotification
	if err := testDB.Where("user_id = ? AND kind = ? AND thread_id = ?", A, model.NotificationKindPosted, th.ID).
		Order("id").Find(&fresh).Error; err != nil {
		t.Fatalf("list posted rows: %v", err)
	}
	if len(fresh) != 2 || fresh[1].ReadAt != nil || fresh[1].ID == folded.ID {
		t.Fatalf("the next reply must start a new posted row, got %+v", fresh)
	}

	var sideRow model.CommunityNotification
	if err := testDB.First(&sideRow, side[0].ID).Error; err != nil {
		t.Fatalf("reload side replied: %v", err)
	}
	if sideRow.ReadAt != nil || sideRow.Seq != sideSeq {
		t.Fatalf("the other thread's replied row must stay unread, seq unchanged: %+v", sideRow)
	}
}

func TestNotifyLikes(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	rs := NewReactionService(testDB)
	ctx := letmoeCtx()
	const A, B, C, D int64 = 100, 200, 300, 400

	th := openTopic(t, ts, "letmoe", A, "b1", "opening")
	var opening model.CommunityPost
	if err := testDB.Where("thread_id = ? AND post_number = 1", th.ID).First(&opening).Error; err != nil {
		t.Fatalf("opening: %v", err)
	}
	post := &opening

	if _, err := rs.Toggle(ctx, post.ID, B, model.ReactionKindLike); err != nil {
		t.Fatalf("B like: %v", err)
	}
	if _, err := rs.Toggle(ctx, post.ID, C, model.ReactionKindLike); err != nil {
		t.Fatalf("C like: %v", err)
	}
	processBatch(t)
	rows := notifsOf(t, A)
	if len(rows) != 1 || rows[0].Kind != model.NotificationKindLiked || rows[0].ItemCount != 2 || rows[0].ActorCount != 2 {
		t.Fatalf("two likers fold to one liked row count 2, got %+v", rows)
	}

	if _, err := rs.Toggle(ctx, post.ID, D, model.ReactionKindLike); err != nil {
		t.Fatalf("D like: %v", err)
	}
	var likeEvents int64
	if err := testDB.Model(&model.CommunityEvent{}).Where("kind = ?", model.EventKindPostLiked).Count(&likeEvents).Error; err != nil {
		t.Fatalf("count like events: %v", err)
	}
	if _, err := rs.Toggle(ctx, post.ID, D, model.ReactionKindLike); err != nil {
		t.Fatalf("D unlike: %v", err)
	}
	var afterUnlike int64
	if err := testDB.Model(&model.CommunityEvent{}).Where("kind = ?", model.EventKindPostLiked).Count(&afterUnlike).Error; err != nil {
		t.Fatalf("count like events after unlike: %v", err)
	}
	if afterUnlike != likeEvents {
		t.Fatalf("an unlike must enqueue nothing: %d -> %d", likeEvents, afterUnlike)
	}
	_, _, dropped := processBatch(t)
	if dropped < 1 {
		t.Fatalf("the unlike must drop D's event, dropped=%d", dropped)
	}
	rows = notifsOf(t, A)
	if len(rows) != 1 || rows[0].ItemCount != 2 {
		t.Fatalf("D's unlike must not change the fold, got %+v", rows)
	}

	if _, err := rs.Toggle(ctx, post.ID, A, model.ReactionKindLike); err != nil {
		t.Fatalf("author like: %v", err)
	}
	processBatch(t)
	if n := notifsOf(t, A); len(n) != 1 {
		t.Fatalf("the author liking their own post notifies nobody, got %+v", n)
	}
}

func TestNotifyHeldPostParksThenDelivers(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	rs := NewReviewService(testDB, NoopSink{})
	ctx := letmoeCtx()
	const A, X, Y, held int64 = 100, 500, 600, 700

	th := openTopic(t, ts, "letmoe", A, "b1", "opening")
	seedTrust(t, held, model.TrustLevelNew, 2)
	p, err := ps.Reply(ctx, ReplyParams{ThreadID: th.ID, AuthorID: held, BodyRaw: "held", MentionUserIDs: []int64{X}})
	if err != nil {
		t.Fatalf("held reply: %v", err)
	}
	if p.Status != model.PostStatusHidden {
		t.Fatalf("held reply status: %d", p.Status)
	}
	_, parked, dropped := processBatch(t)
	if parked < 1 || dropped != 0 {
		t.Fatalf("held post must park, got parked=%d dropped=%d", parked, dropped)
	}
	if n := notifsOf(t, X); len(n) != 0 {
		t.Fatalf("parked event must write no rows, got %+v", n)
	}
	pending := pendingEvents(t)
	if len(pending) == 0 || !pending[len(pending)-1].AttemptAfter.After(time.Now()) {
		t.Fatalf("parked event must have attempt_after in the future, got %+v", pending)
	}

	var item model.CommunityReviewItem
	if err := testDB.Where("post_id = ? AND status = ?", p.ID, model.ReviewStatusPending).First(&item).Error; err != nil {
		t.Fatalf("review item: %v", err)
	}
	if err := rs.Approve(ctx, item.ID, 1); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := testDB.Model(&model.CommunityEvent{}).Where("id = ?", pending[len(pending)-1].ID).
		Update("attempt_after", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatalf("move attempt_after: %v", err)
	}
	processBatch(t)
	xRows := notifsOf(t, X)
	if len(xRows) != 1 || xRows[0].Kind != model.NotificationKindMentioned {
		t.Fatalf("after approve X gets mentioned, got %+v", xRows)
	}

	p2, err := ps.Reply(ctx, ReplyParams{ThreadID: th.ID, AuthorID: held, BodyRaw: "held2", MentionUserIDs: []int64{Y}})
	if err != nil {
		t.Fatalf("second held: %v", err)
	}
	processBatch(t)
	pending = pendingEvents(t)
	if len(pending) == 0 {
		t.Fatal("second held event should be parked")
	}
	evID := pending[len(pending)-1].ID
	var item2 model.CommunityReviewItem
	if err := testDB.Where("post_id = ? AND status = ?", p2.ID, model.ReviewStatusPending).First(&item2).Error; err != nil {
		t.Fatalf("review item 2: %v", err)
	}
	if err := rs.Reject(ctx, item2.ID, 1); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if err := testDB.Model(&model.CommunityEvent{}).Where("id = ?", evID).
		Update("attempt_after", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatalf("move attempt_after 2: %v", err)
	}
	_, _, dropped = processBatch(t)
	if dropped < 1 {
		t.Fatalf("rejected hold must drop, dropped=%d", dropped)
	}
	if n := notifsOf(t, Y); len(n) != 0 {
		t.Fatalf("rejected hold must write no rows for Y, got %+v", n)
	}
}

func TestNotifyDeliverySite(t *testing.T) {
	cleanTables(t)
	ps := NewPostService(testDB, NoopSink{})
	es := NewEngagementService(testDB)
	const U, V, third, opener int64 = 1, 2, 3, 4

	seedTrust(t, opener, model.TrustLevelBasic, 0)
	th, _, err := ps.Comment(WithCallerSite(context.Background(), "letmoe"), CommentParams{
		Site: "letmoe", AnchorKind: model.AnchorKindCatalogWork, AnchorID: "w9",
		AuthorID: opener, BodyRaw: "wall",
	})
	if err != nil {
		t.Fatalf("open wall: %v", err)
	}
	replyTo(t, ps, WithCallerSite(context.Background(), "kungal"), th.ID, U, nil, nil, "from kungal")
	if _, err := es.SetAnchorLevel(context.Background(), "moyu", V, model.AnchorKindCatalogWork, "w9", model.NotificationLevelWatching); err != nil {
		t.Fatalf("V watch moyu: %v", err)
	}
	processBatch(t)
	replyTo(t, ps, WithCallerSite(context.Background(), "letmoe"), th.ID, third, nil, nil, "third")
	processBatch(t)

	uRows := notifsOfSite(t, "kungal", U)
	if len(uRows) != 1 || uRows[0].Kind != model.NotificationKindPosted || uRows[0].Site != "kungal" {
		t.Fatalf("U's posted row must be site kungal, got %+v", uRows)
	}
	vRows := notifsOfSite(t, "moyu", V)
	if len(vRows) != 1 || vRows[0].Kind != model.NotificationKindPosted || vRows[0].Site != "moyu" {
		t.Fatalf("V's posted row must be site moyu, got %+v", vRows)
	}
}

func TestNotifyFeedbackAndAnswer(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	fs := NewFeedbackService(testDB, NoopSink{})
	bs := NewBoardService(testDB)
	ctx := letmoeCtx()
	const creator, responder, answerer int64 = 100, 200, 300

	fb := openFeedback(t, ts, "letmoe", creator, model.AnchorKindSiteResource, "r1", "bug")
	if err := fs.SetStatus(ctx, fb.ID, model.FeedbackStatusConfirmed, responder, nil); err != nil {
		t.Fatalf("status 1: %v", err)
	}
	if err := fs.SetStatus(ctx, fb.ID, model.FeedbackStatusFixed, responder, nil); err != nil {
		t.Fatalf("status 2: %v", err)
	}
	processBatch(t)
	rows := notifsOf(t, creator)
	if len(rows) != 1 || rows[0].Kind != model.NotificationKindFeedbackStatus || rows[0].ItemCount != 2 {
		t.Fatalf("two status changes fold to item_count=2, got %+v", rows)
	}

	createBoard(t, bs, "letmoe", "qa", 0, func(f *BoardFields) { f.Format = model.BoardFormatQA })
	q := openTopic(t, ts, "letmoe", creator, "qa", "how?")
	ans := replyTo(t, ps, ctx, q.ID, answerer, nil, nil, "like this")
	if _, err := ts.SetAnswer(ctx, q.ID, ans.ID, creator, false); err != nil {
		t.Fatalf("mark answer: %v", err)
	}
	processBatch(t)
	ansRows := notifsOf(t, answerer)
	found := false
	for _, r := range ansRows {
		if r.Kind == model.NotificationKindAnswerAccepted {
			found = true
		}
	}
	if !found {
		t.Fatalf("answer author should get answer_accepted, got %+v", ansRows)
	}

	var before int64
	if err := testDB.Model(&model.CommunityEvent{}).Where("kind = ?", model.EventKindAnswerAccepted).Count(&before).Error; err != nil {
		t.Fatalf("count answer events: %v", err)
	}
	if _, err := ts.SetAnswer(ctx, q.ID, ans.ID, creator, false); err != nil {
		t.Fatalf("re-mark the same answer: %v", err)
	}
	if _, err := ts.SetAnswer(ctx, q.ID, 0, creator, true); err != nil {
		t.Fatalf("clear answer: %v", err)
	}
	var after int64
	if err := testDB.Model(&model.CommunityEvent{}).Where("kind = ?", model.EventKindAnswerAccepted).Count(&after).Error; err != nil {
		t.Fatalf("count answer events after clear: %v", err)
	}
	if after != before {
		t.Fatalf("re-marking the same answer or clearing it must enqueue nothing: %d -> %d", before, after)
	}
}

func TestNotifyDropsWhatNoLongerShows(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	ctx := letmoeCtx()
	const A, B int64 = 100, 200

	th := openTopic(t, ts, "letmoe", A, "b1", "opening")
	processBatch(t)
	replyTo(t, ps, ctx, th.ID, B, nil, nil, "in a thread about to be hidden")
	if err := testDB.Model(&model.CommunityThread{}).Where("id = ?", th.ID).
		Update("status", model.ThreadStatusHidden).Error; err != nil {
		t.Fatalf("hide thread: %v", err)
	}
	if _, _, dropped := processBatch(t); dropped != 1 {
		t.Fatalf("an event on a hidden thread must drop, dropped=%d", dropped)
	}
	if err := testDB.Model(&model.CommunityThread{}).Where("id = ?", th.ID).
		Update("status", model.ThreadStatusOpen).Error; err != nil {
		t.Fatalf("reopen thread: %v", err)
	}

	p := replyTo(t, ps, ctx, th.ID, B, nil, nil, "hidden without a review")
	if err := testDB.Model(&model.CommunityPost{}).Where("id = ?", p.ID).
		Update("status", model.PostStatusHidden).Error; err != nil {
		t.Fatalf("hide post: %v", err)
	}
	if _, parked, dropped := processBatch(t); dropped != 1 || parked != 0 {
		t.Fatalf("a hidden post with no pending review must drop, parked=%d dropped=%d", parked, dropped)
	}
	if rows := notifsOf(t, A); len(rows) != 0 {
		t.Fatalf("nothing that no longer shows may notify, got %+v", rows)
	}
	if n := pendingEvents(t); len(n) != 0 {
		t.Fatalf("dropped events are processed, pending %+v", n)
	}
}

func TestNotifySiteLocalAnchorStaysOnItsSite(t *testing.T) {
	cleanTables(t)
	ps := NewPostService(testDB, NoopSink{})
	es := NewEngagementService(testDB)
	const opener, replier, L, K int64 = 1, 2, 3, 4

	for _, w := range []struct {
		site string
		user int64
	}{{"letmoe", L}, {"kungal", K}} {
		if _, err := es.SetAnchorLevel(context.Background(), w.site, w.user, model.AnchorKindSiteGame, "g1", model.NotificationLevelWatching); err != nil {
			t.Fatalf("%s watches its g1: %v", w.site, err)
		}
	}
	seedTrust(t, opener, model.TrustLevelBasic, 0)
	th, _, err := ps.Comment(letmoeCtx(), CommentParams{
		Site: "letmoe", AnchorKind: model.AnchorKindSiteGame, AnchorID: "g1",
		AuthorID: opener, BodyRaw: "wall",
	})
	if err != nil {
		t.Fatalf("open wall: %v", err)
	}
	replyTo(t, ps, letmoeCtx(), th.ID, replier, nil, nil, "reply")
	processBatch(t)

	if rows := notifsOf(t, L); len(rows) != 1 || rows[0].Site != "letmoe" {
		t.Fatalf("letmoe's watcher of its g1 is notified, got %+v", rows)
	}
	if rows := notifsOf(t, K); len(rows) != 0 {
		t.Fatalf("kungal's g1 is another game and must not be notified, got %+v", rows)
	}
}

func TestNotifyPostingReadsTheThread(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	ctx := letmoeCtx()
	const A, B int64 = 100, 200

	th := openTopic(t, ts, "letmoe", A, "b1", "opening")
	replyTo(t, ps, ctx, th.ID, B, nil, nil, "one")
	processBatch(t)
	replyTo(t, ps, ctx, th.ID, A, nil, nil, "mine")
	rows := notifsOf(t, A)
	if len(rows) != 1 || rows[0].ReadAt == nil {
		t.Fatalf("replying reads the thread's notifications, got %+v", rows)
	}
	replyTo(t, ps, ctx, th.ID, B, nil, nil, "two")
	processBatch(t)
	rows = notifsOf(t, A)
	if len(rows) != 2 || rows[1].ReadAt != nil {
		t.Fatalf("the next reply starts a new row, got %+v", rows)
	}

	replyTo(t, ps, ctx, th.ID, B, nil, nil, "three")
	mine := replyTo(t, ps, ctx, th.ID, A, nil, nil, "mine again")
	processBatch(t)
	last := replyTo(t, ps, ctx, th.ID, B, nil, nil, "four")
	processBatch(t)
	rows = notifsOf(t, A)
	r := rows[len(rows)-1]
	if r.ReadAt != nil || *r.PostNumber != last.PostNumber || *r.FirstPostNumber >= mine.PostNumber {
		t.Fatalf("an unread fold must span the recipient's own post, got %+v", r)
	}
	if r.ItemCount != 2 || r.ActorCount != 1 {
		t.Fatalf("the recipient's own post is not counted: items %d actors %d", r.ItemCount, r.ActorCount)
	}
}

func TestNotifyLateHeldPostWidensTheFold(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	rs := NewReviewService(testDB, NoopSink{})
	ctx := letmoeCtx()
	const A, B, held int64 = 100, 200, 700

	th := openTopic(t, ts, "letmoe", A, "b1", "opening")
	seedTrust(t, held, model.TrustLevelNew, 1)
	early, err := ps.Reply(ctx, ReplyParams{ThreadID: th.ID, AuthorID: held, BodyRaw: "held"})
	if err != nil {
		t.Fatalf("held reply: %v", err)
	}
	processBatch(t)
	late := replyTo(t, ps, ctx, th.ID, B, nil, nil, "visible")
	processBatch(t)
	if early.PostNumber >= late.PostNumber {
		t.Fatalf("the held reply must be numbered first: %d, %d", early.PostNumber, late.PostNumber)
	}

	var item model.CommunityReviewItem
	if err := testDB.Where("post_id = ? AND status = ?", early.ID, model.ReviewStatusPending).First(&item).Error; err != nil {
		t.Fatalf("review item: %v", err)
	}
	if err := rs.Approve(ctx, item.ID, 1); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := testDB.Model(&model.CommunityEvent{}).Where("processed_at IS NULL").
		Update("attempt_after", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatalf("move attempt_after: %v", err)
	}
	processBatch(t)

	rows := notifsOf(t, A)
	if len(rows) != 1 || rows[0].Kind != model.NotificationKindPosted {
		t.Fatalf("A should have one posted fold, got %+v", rows)
	}
	r := rows[0]
	if *r.FirstPostNumber != early.PostNumber || *r.PostNumber != late.PostNumber {
		t.Fatalf("fold range: want %d..%d, got %d..%d", early.PostNumber, late.PostNumber, *r.FirstPostNumber, *r.PostNumber)
	}
	if *r.PostID != late.ID || *r.ActorID != B {
		t.Fatalf("the fold must keep pointing at the latest post: post %d actor %d", *r.PostID, *r.ActorID)
	}
	if r.ItemCount != 2 || r.ActorCount != 2 {
		t.Fatalf("the late post must be counted: items %d actors %d", r.ItemCount, r.ActorCount)
	}
}

func TestPurgeForgetsTheUserInPendingEvents(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	ctx := letmoeCtx()
	const A, B, U, V int64 = 100, 200, 300, 400

	th := openTopic(t, ts, "letmoe", A, "b1", "opening")
	processBatch(t)
	target := U
	replyTo(t, ps, ctx, th.ID, B, &target, []int64{U, V}, "hi")
	if _, err := ps.PurgeAuthor(ctx, "letmoe", U); err != nil {
		t.Fatalf("purge U: %v", err)
	}
	if _, err := ps.PurgeAuthor(ctx, "kungal", V); err != nil {
		t.Fatalf("purge V on another site: %v", err)
	}
	processBatch(t)

	if rows := notifsOf(t, U); len(rows) != 0 {
		t.Fatalf("a purged user must not be notified by a pending event, got %+v", rows)
	}
	vRows := notifsOf(t, V)
	if len(vRows) != 1 || vRows[0].Kind != model.NotificationKindMentioned {
		t.Fatalf("V was purged on another site only and keeps the mention, got %+v", vRows)
	}
	aRows := notifsOf(t, A)
	if len(aRows) != 1 || aRows[0].Kind != model.NotificationKindPosted {
		t.Fatalf("the rest of the event is still delivered, got %+v", aRows)
	}
}

func TestNotifySingleDispatcher(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	ctx := letmoeCtx()
	const A, B int64 = 100, 200

	th := openTopic(t, ts, "letmoe", A, "b1", "t")
	replyTo(t, ps, ctx, th.ID, B, nil, nil, "r")

	holder := testDB.Begin()
	if err := holder.Exec("SELECT pg_advisory_xact_lock(?)", NotifyDispatchLockKey).Error; err != nil {
		t.Fatalf("hold lock: %v", err)
	}
	d, p, dr, err := NewNotificationService(testDB).ProcessBatch(context.Background())
	if err != nil || d != 0 || p != 0 || dr != 0 {
		holder.Rollback()
		t.Fatalf("locked dispatcher must return zeros, got %d %d %d err=%v", d, p, dr, err)
	}
	if n := pendingEvents(t); len(n) == 0 {
		holder.Rollback()
		t.Fatal("events must stay unprocessed while the lock is held")
	}
	if err := holder.Rollback().Error; err != nil {
		t.Fatalf("release lock: %v", err)
	}
	processBatch(t)
	if n := pendingEvents(t); len(n) != 0 {
		t.Fatalf("after the lock is released a batch must deliver, pending=%d", len(n))
	}

	cleanTables(t)
	th = openTopic(t, ts, "letmoe", A, "b1", "t")
	for i := range 6 {
		replyTo(t, ps, ctx, th.ID, B, nil, nil, "r")
		_ = i
	}
	svc := NewNotificationService(testDB)
	for range 5 {
		var wg sync.WaitGroup
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _, _, _ = svc.ProcessBatch(context.Background())
			}()
		}
		wg.Wait()
	}
	aRows := notifsOf(t, A)
	posted := 0
	for _, r := range aRows {
		if r.Kind == model.NotificationKindPosted {
			posted++
		}
	}
	if posted != 1 {
		t.Fatalf("concurrent dispatchers must not duplicate the posted fold, got %d rows %+v", posted, aRows)
	}
	if n := notifsOf(t, B); len(n) != 0 {
		t.Fatalf("actor B must still have nothing, got %+v", n)
	}
}

func TestNotifyBadEventDoesNotSinkTheBatch(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	ctx := letmoeCtx()
	const A, B int64 = 100, 200

	if err := testDB.Exec(`
		CREATE OR REPLACE FUNCTION community_notify_reject_666() RETURNS trigger AS $$
		BEGIN
		  IF NEW.user_id = 666 THEN
		    RAISE EXCEPTION 'blocked user 666';
		  END IF;
		  RETURN NEW;
		END;
		$$ LANGUAGE plpgsql`).Error; err != nil {
		t.Fatalf("create function: %v", err)
	}
	if err := testDB.Exec(`
		CREATE TRIGGER community_notify_reject_666
		BEFORE INSERT ON community_notification
		FOR EACH ROW EXECUTE FUNCTION community_notify_reject_666()`).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	t.Cleanup(func() {
		_ = testDB.Exec(`DROP TRIGGER IF EXISTS community_notify_reject_666 ON community_notification`).Error
		_ = testDB.Exec(`DROP FUNCTION IF EXISTS community_notify_reject_666()`).Error
	})

	th := openTopic(t, ts, "letmoe", A, "b1", "t")
	processBatch(t)
	replyTo(t, ps, ctx, th.ID, B, nil, []int64{666}, "bad")
	replyTo(t, ps, ctx, th.ID, B, nil, []int64{777}, "ok")
	processBatch(t)

	if n := notifsOf(t, 666); len(n) != 0 {
		t.Fatalf("no row for 666, got %+v", n)
	}
	rows777 := notifsOf(t, 777)
	if len(rows777) != 1 || rows777[0].Kind != model.NotificationKindMentioned {
		t.Fatalf("777 should have mentioned, got %+v", rows777)
	}
	pending := pendingEvents(t)
	if len(pending) != 1 || pending[0].Attempts != 1 || !pending[0].AttemptAfter.After(time.Now()) {
		t.Fatalf("the 666 event must be unprocessed with attempts=1, got %+v", pending)
	}

	if err := testDB.Create(&model.CommunityEvent{
		Site: "letmoe", Kind: 99, ThreadID: th.ID, ActorID: B, AttemptAfter: time.Now(),
	}).Error; err != nil {
		t.Fatalf("insert kind 99: %v", err)
	}
	replyTo(t, ps, ctx, th.ID, B, nil, []int64{888}, "valid")
	processBatch(t)
	if n := notifsOf(t, 888); len(n) != 1 {
		t.Fatalf("valid event in the same batch must deliver, got %+v", notifsOf(t, 888))
	}
	var bad model.CommunityEvent
	if err := testDB.Where("kind = 99").First(&bad).Error; err != nil {
		t.Fatalf("reload kind 99: %v", err)
	}
	if bad.ProcessedAt != nil || bad.Attempts != 1 {
		t.Fatalf("kind 99 must stay unprocessed with attempts=1, got %+v", bad)
	}
}

func TestNotifyMentionValidation(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	ctx := letmoeCtx()
	const A, B int64 = 100, 200
	th := openTopic(t, ts, "letmoe", A, "b1", "t")
	seedTrust(t, B, model.TrustLevelBasic, 0)

	ids := make([]int64, 21)
	for i := range ids {
		ids[i] = int64(i + 2)
	}
	_, err := ps.Reply(ctx, ReplyParams{ThreadID: th.ID, AuthorID: B, BodyRaw: "x", MentionUserIDs: ids})
	wantErr[*InvalidError](t, err, "21 mentions")

	_, err = ps.Reply(ctx, ReplyParams{
		ThreadID: th.ID, AuthorID: B, BodyRaw: "y",
		MentionUserIDs: []int64{B, B, 0, -3, 55, 55, 66},
	})
	if err != nil {
		t.Fatalf("author and duplicates must be dropped silently: %v", err)
	}
	var ev model.CommunityEvent
	if err := testDB.Where("kind = ? AND actor_id = ?", model.EventKindPostCreated, B).
		Order("id DESC").First(&ev).Error; err != nil {
		t.Fatalf("event: %v", err)
	}
	var got []int64
	if err := json.Unmarshal(ev.MentionUserIDs, &got); err != nil {
		t.Fatalf("mentions json: %v", err)
	}
	if len(got) != 2 || got[0] != 55 || got[1] != 66 {
		t.Fatalf("want [55 66] keeping order, got %v", got)
	}
}

func TestNotificationPrune(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	th := openTopic(t, ts, "letmoe", 100, "b1", "t")

	insertEvent := func(processed *time.Time) int64 {
		t.Helper()
		ev := model.CommunityEvent{
			Site: "letmoe", Kind: model.EventKindPostCreated, ThreadID: th.ID, ActorID: 1,
			AttemptAfter: time.Now(), ProcessedAt: processed,
		}
		if err := testDB.Create(&ev).Error; err != nil {
			t.Fatalf("insert event: %v", err)
		}
		return ev.ID
	}
	oldProc := time.Now().Add(-8 * 24 * time.Hour)
	newProc := time.Now().Add(-2 * 24 * time.Hour)
	oldID := insertEvent(&oldProc)
	newID := insertEvent(&newProc)
	pendingID := insertEvent(nil)
	if err := testDB.Model(&model.CommunityEvent{}).Where("id = ?", oldID).
		Update("processed_at", oldProc).Error; err != nil {
		t.Fatalf("backdate old event: %v", err)
	}

	insertNotif := func(readAt *time.Time, seq int64) int64 {
		t.Helper()
		n := model.CommunityNotification{
			Site: "letmoe", UserID: 1, Kind: model.NotificationKindPosted, ThreadID: th.ID,
			AnchorKind: model.AnchorKindBoard, AnchorID: "1", ActorCount: 1, ItemCount: 1, Seq: seq, ReadAt: readAt,
		}
		if err := testDB.Create(&n).Error; err != nil {
			t.Fatalf("insert notif: %v", err)
		}
		return n.ID
	}
	oldRead := time.Now().Add(-100 * 24 * time.Hour)
	newRead := time.Now().Add(-10 * 24 * time.Hour)
	oldReadID := insertNotif(&oldRead, 1)
	newReadID := insertNotif(&newRead, 2)
	unreadID := insertNotif(nil, 3)
	if err := testDB.Model(&model.CommunityNotification{}).Where("id = ?", oldReadID).
		Update("read_at", oldRead).Error; err != nil {
		t.Fatalf("backdate old notif: %v", err)
	}

	events, notifs, err := NewNotificationService(testDB).Prune(context.Background())
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if events != 1 || notifs != 1 {
		t.Fatalf("prune counts: events=%d notifications=%d", events, notifs)
	}
	mustExist := func(table string, id int64) {
		t.Helper()
		var n int64
		if err := testDB.Table(table).Where("id = ?", id).Count(&n).Error; err != nil || n != 1 {
			t.Fatalf("%s %d should remain (n=%d err=%v)", table, id, n, err)
		}
	}
	mustGone := func(table string, id int64) {
		t.Helper()
		var n int64
		if err := testDB.Table(table).Where("id = ?", id).Count(&n).Error; err != nil || n != 0 {
			t.Fatalf("%s %d should be gone (n=%d err=%v)", table, id, n, err)
		}
	}
	mustGone("community_event", oldID)
	mustExist("community_event", newID)
	mustExist("community_event", pendingID)
	mustGone("community_notification", oldReadID)
	mustExist("community_notification", newReadID)
	mustExist("community_notification", unreadID)
}
