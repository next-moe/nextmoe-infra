package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"
)

func countAnchorRows(t *testing.T, site string, userID int64, kind int16, anchorID string) int64 {
	t.Helper()
	q := testDB.Model(&model.CommunityAnchorUser{})
	if site != "" {
		q = q.Where("site = ?", site)
	}
	if userID != 0 {
		q = q.Where("user_id = ?", userID)
	}
	if kind >= 0 {
		q = q.Where("anchor_kind = ?", kind)
	}
	if anchorID != "" {
		q = q.Where("anchor_id = ?", anchorID)
	}
	var n int64
	if err := q.Count(&n).Error; err != nil {
		t.Fatalf("count anchor rows: %v", err)
	}
	return n
}

func TestAnchorLevels(t *testing.T) {
	cleanTables(t)
	es := NewEngagementService(testDB)
	ctx := context.Background()
	const user int64 = 100

	for _, level := range []int16{
		model.NotificationLevelWatching,
		model.NotificationLevelWatchingFirstPost,
		model.NotificationLevelMuted,
	} {
		st, err := es.SetAnchorLevel(ctx, "letmoe", user, model.AnchorKindCatalogWork, "w1", level)
		if err != nil {
			t.Fatalf("set level %d: %v", level, err)
		}
		if st.NotificationLevel != level {
			t.Fatalf("want level %d, got %d", level, st.NotificationLevel)
		}
		if n := countAnchorRows(t, "letmoe", user, model.AnchorKindCatalogWork, "w1"); n != 1 {
			t.Fatalf("level %d must store a row, got %d", level, n)
		}
	}

	st, err := es.SetAnchorLevel(ctx, "letmoe", user, model.AnchorKindCatalogWork, "w1", model.NotificationLevelNormal)
	if err != nil {
		t.Fatalf("set normal: %v", err)
	}
	if st.NotificationLevel != model.NotificationLevelNormal {
		t.Fatalf("normal must answer level 1, got %d", st.NotificationLevel)
	}
	if n := countAnchorRows(t, "letmoe", user, model.AnchorKindCatalogWork, "w1"); n != 0 {
		t.Fatalf("normal must delete the row, got %d", n)
	}

	for _, level := range []int16{model.NotificationLevelTracking, 5, -1} {
		_, err := es.SetAnchorLevel(ctx, "letmoe", user, model.AnchorKindCatalogWork, "w2", level)
		wantErr[*InvalidError](t, err, "level")
	}
	_, err = es.SetAnchorLevel(ctx, "letmoe", user, 5, "w2", model.NotificationLevelWatching)
	wantErr[*InvalidError](t, err, "kind 5")
	_, err = es.SetAnchorLevel(ctx, "letmoe", user, model.AnchorKindCatalogWork, "", model.NotificationLevelWatching)
	wantErr[*InvalidError](t, err, "empty anchor id")
	_, err = es.SetAnchorLevel(ctx, "letmoe", 0, model.AnchorKindCatalogWork, "w2", model.NotificationLevelWatching)
	wantErr[*InvalidError](t, err, "user_id 0")

	bs := NewBoardService(testDB)
	foreign := createBoard(t, bs, "kungal", "main", 0)
	_, err = es.SetAnchorLevel(ctx, "letmoe", user, model.AnchorKindBoard, model.BoardAnchorID(foreign.ID), model.NotificationLevelWatching)
	if !errors.Is(err, ErrBoardNotFound) {
		t.Fatalf("another site's board must be ErrBoardNotFound, got %v", err)
	}
	_, err = es.SetAnchorLevel(ctx, "letmoe", user, model.AnchorKindBoard, "999999", model.NotificationLevelWatching)
	if !errors.Is(err, ErrBoardNotFound) {
		t.Fatalf("unknown board id must be ErrBoardNotFound, got %v", err)
	}

	if _, err := es.SetAnchorLevel(ctx, "letmoe", user, model.AnchorKindCatalogWork, "shared", model.NotificationLevelWatching); err != nil {
		t.Fatalf("letmoe catalog: %v", err)
	}
	if _, err := es.SetAnchorLevel(ctx, "kungal", user, model.AnchorKindCatalogWork, "shared", model.NotificationLevelWatching); err != nil {
		t.Fatalf("kungal catalog: %v", err)
	}
	if n := countAnchorRows(t, "", user, model.AnchorKindCatalogWork, "shared"); n != 2 {
		t.Fatalf("two sites watching a catalog work must yield two rows, got %d", n)
	}
}

func TestAnchorStatesAndList(t *testing.T) {
	cleanTables(t)
	es := NewEngagementService(testDB)
	ctx := context.Background()
	const user int64 = 200

	mustSet := func(site string, kind int16, id string, level int16) {
		t.Helper()
		if _, err := es.SetAnchorLevel(ctx, site, user, kind, id, level); err != nil {
			t.Fatalf("set %s %d/%s: %v", site, kind, id, err)
		}
	}
	mustSet("letmoe", model.AnchorKindCatalogWork, "w1", model.NotificationLevelWatching)
	mustSet("letmoe", model.AnchorKindCatalogWork, "w2", model.NotificationLevelMuted)
	mustSet("letmoe", model.AnchorKindSiteGame, "g1", model.NotificationLevelWatchingFirstPost)
	mustSet("kungal", model.AnchorKindCatalogWork, "w1", model.NotificationLevelWatching)

	states, err := es.AnchorStates("letmoe", user, []AnchorRef{
		{AnchorKind: model.AnchorKindCatalogWork, AnchorID: "w1"},
		{AnchorKind: model.AnchorKindCatalogWork, AnchorID: "w2"},
		{AnchorKind: model.AnchorKindCatalogWork, AnchorID: "w-missing"},
		{AnchorKind: model.AnchorKindSiteGame, AnchorID: "g1"},
	})
	if err != nil {
		t.Fatalf("states: %v", err)
	}
	got := map[string]int16{}
	for _, row := range states {
		got[row.AnchorID] = row.NotificationLevel
	}
	if len(states) != 3 || got["w1"] != model.NotificationLevelWatching || got["w2"] != model.NotificationLevelMuted || got["g1"] != model.NotificationLevelWatchingFirstPost {
		t.Fatalf("states must return only stored rows, got %+v", states)
	}

	tooMany := make([]AnchorRef, 101)
	for i := range tooMany {
		tooMany[i] = AnchorRef{AnchorKind: model.AnchorKindCatalogWork, AnchorID: "w"}
	}
	_, err = es.AnchorStates("letmoe", user, tooMany)
	wantErr[*InvalidError](t, err, ">100 anchors")

	all, err := es.ListAnchorSubscriptions("letmoe", user, -1, 0, 50)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("list must not include another site's rows, got %d", len(all))
	}
	for i := 1; i < len(all); i++ {
		if all[i-1].ID <= all[i].ID {
			t.Fatalf("list must be newest first: %d then %d", all[i-1].ID, all[i].ID)
		}
		if all[i].Site != "letmoe" {
			t.Fatalf("list leaked site %q", all[i].Site)
		}
	}

	games, err := es.ListAnchorSubscriptions("letmoe", user, model.AnchorKindSiteGame, 0, 50)
	if err != nil {
		t.Fatalf("list kind: %v", err)
	}
	if len(games) != 1 || games[0].AnchorID != "g1" {
		t.Fatalf("kind filter: %+v", games)
	}

	page1, err := es.ListAnchorSubscriptions("letmoe", user, -1, 0, 2)
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("page 1 want 2, got %d", len(page1))
	}
	page2, err := es.ListAnchorSubscriptions("letmoe", user, -1, page1[len(page1)-1].ID, 2)
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if len(page2) != 1 {
		t.Fatalf("page 2 want the remainder, got %d", len(page2))
	}
	seen := map[int64]bool{}
	for _, row := range append(page1, page2...) {
		if seen[row.ID] {
			t.Fatalf("keyset returned id %d twice", row.ID)
		}
		seen[row.ID] = true
	}
	if len(seen) != 3 {
		t.Fatalf("pages covered %d of 3", len(seen))
	}

	other, err := es.ListAnchorSubscriptions("kungal", user, -1, 0, 50)
	if err != nil {
		t.Fatalf("kungal list: %v", err)
	}
	if len(other) != 1 || other[0].AnchorID != "w1" || other[0].Site != "kungal" {
		t.Fatalf("kungal list: %+v", other)
	}
}

func TestBoardDeleteRemovesItsSubscriptions(t *testing.T) {
	cleanTables(t)
	bs := NewBoardService(testDB)
	es := NewEngagementService(testDB)
	ctx := context.Background()
	watched := createBoard(t, bs, "letmoe", "watched", 0)
	kept := createBoard(t, bs, "letmoe", "kept", 0)
	if _, err := es.SetAnchorLevel(ctx, "letmoe", 100, model.AnchorKindBoard, model.BoardAnchorID(watched.ID), model.NotificationLevelWatching); err != nil {
		t.Fatalf("watch deleted board: %v", err)
	}
	st, err := es.SetAnchorLevel(ctx, "letmoe", 101, model.AnchorKindBoard, "+0"+model.BoardAnchorID(watched.ID), model.NotificationLevelWatchingFirstPost)
	if err != nil {
		t.Fatalf("watch deleted board by a padded id: %v", err)
	}
	if st.AnchorID != model.BoardAnchorID(watched.ID) {
		t.Fatalf("a padded board id must answer the canonical id, got %q", st.AnchorID)
	}
	if n := countAnchorRows(t, "letmoe", 101, model.AnchorKindBoard, model.BoardAnchorID(watched.ID)); n != 1 {
		t.Fatalf("a padded board id must be stored canonically, got %d", n)
	}
	if _, err := es.SetAnchorLevel(ctx, "letmoe", 100, model.AnchorKindBoard, model.BoardAnchorID(kept.ID), model.NotificationLevelWatching); err != nil {
		t.Fatalf("watch kept board: %v", err)
	}
	if err := bs.Delete(ctx, "letmoe", watched.ID, 1); err != nil {
		t.Fatalf("delete board: %v", err)
	}
	if n := countAnchorRows(t, "letmoe", 0, model.AnchorKindBoard, model.BoardAnchorID(watched.ID)); n != 0 {
		t.Fatalf("deleted board's subscriptions must be gone, got %d", n)
	}
	if n := countAnchorRows(t, "letmoe", 100, model.AnchorKindBoard, model.BoardAnchorID(kept.ID)); n != 1 {
		t.Fatalf("the other board's subscription must remain, got %d", n)
	}
}

func TestBoardSubscribeWaitsForABoardBeingDeleted(t *testing.T) {
	cleanTables(t)
	bs := NewBoardService(testDB)
	es := NewEngagementService(testDB)
	board := createBoard(t, bs, "letmoe", "general", 0)

	deleter := testDB.Begin()
	if _, err := repository.LockBoardTx(deleter, "letmoe", board.ID, repository.LockUpdate); err != nil {
		t.Fatalf("lock board: %v", err)
	}
	if _, err := repository.DeleteBoardAnchorUsersTx(deleter, "letmoe", board.ID); err != nil {
		deleter.Rollback()
		t.Fatalf("delete subscriptions: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := es.SetAnchorLevel(context.Background(), "letmoe", 100, model.AnchorKindBoard,
			model.BoardAnchorID(board.ID), model.NotificationLevelWatching)
		done <- err
	}()

	select {
	case err := <-done:
		deleter.Rollback()
		t.Fatalf("subscribing must wait for the delete, returned %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	if err := repository.DeleteBoardTx(deleter, board.ID); err != nil {
		deleter.Rollback()
		t.Fatalf("delete board: %v", err)
	}
	if err := deleter.Commit().Error; err != nil {
		t.Fatalf("commit delete: %v", err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrBoardNotFound) {
			t.Fatalf("subscribing to a board deleted meanwhile: want ErrBoardNotFound, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("subscribing never returned")
	}
	if n := countAnchorRows(t, "", 0, model.AnchorKindBoard, ""); n != 0 {
		t.Fatalf("no subscription may name the deleted board, found %d", n)
	}
}

func threadUserLevel(t *testing.T, threadID, userID int64) int16 {
	t.Helper()
	var row model.CommunityThreadUser
	if err := testDB.Where("thread_id = ? AND user_id = ?", threadID, userID).First(&row).Error; err != nil {
		t.Fatalf("thread_user %d/%d: %v", threadID, userID, err)
	}
	return row.NotificationLevel
}

func TestFirstReadTakesWatchingFromTheAnchor(t *testing.T) {
	cleanTables(t)
	bs := NewBoardService(testDB)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	es := NewEngagementService(testDB)
	letmoe := WithCallerSite(context.Background(), "letmoe")
	seedTrust(t, 100, model.TrustLevelBasic, 0)
	board := createBoard(t, bs, "letmoe", "general", 0)
	topic, err := openOn(ts, board.ID, 100, false)
	if err != nil {
		t.Fatalf("open topic: %v", err)
	}
	levels := map[int64]int16{
		201: model.NotificationLevelWatching,
		202: model.NotificationLevelWatchingFirstPost,
		203: model.NotificationLevelMuted,
	}
	for user, level := range levels {
		if _, err := es.SetAnchorLevel(letmoe, "letmoe", user, model.AnchorKindBoard, model.BoardAnchorID(board.ID), level); err != nil {
			t.Fatalf("anchor level for %d: %v", user, err)
		}
	}
	for _, user := range []int64{201, 202, 203, 204} {
		if _, err := es.MarkRead(letmoe, topic.ID, user, 1); err != nil {
			t.Fatalf("mark read %d: %v", user, err)
		}
	}
	for user, want := range map[int64]int16{
		201: model.NotificationLevelWatching,
		202: model.NotificationLevelNormal,
		203: model.NotificationLevelNormal,
		204: model.NotificationLevelNormal,
	} {
		if got := threadUserLevel(t, topic.ID, user); got != want {
			t.Fatalf("user %d first read: want level %d, got %d", user, want, got)
		}
	}

	if _, err := es.SetAnchorLevel(letmoe, "letmoe", 201, model.AnchorKindBoard, model.BoardAnchorID(board.ID), model.NotificationLevelNormal); err != nil {
		t.Fatalf("unwatch board: %v", err)
	}
	if _, err := es.MarkRead(letmoe, topic.ID, 201, 1); err != nil {
		t.Fatalf("second read: %v", err)
	}
	if got := threadUserLevel(t, topic.ID, 201); got != model.NotificationLevelWatching {
		t.Fatalf("an existing row keeps its level, got %d", got)
	}
	if _, err := es.SetAnchorLevel(letmoe, "letmoe", 204, model.AnchorKindBoard, model.BoardAnchorID(board.ID), model.NotificationLevelWatching); err != nil {
		t.Fatalf("watch board late: %v", err)
	}
	if _, err := es.MarkRead(letmoe, topic.ID, 204, 1); err != nil {
		t.Fatalf("read after watching: %v", err)
	}
	if got := threadUserLevel(t, topic.ID, 204); got != model.NotificationLevelNormal {
		t.Fatalf("watching an anchor later does not rewrite an existing row, got %d", got)
	}

	wall, _, err := ps.Comment(context.Background(), CommentParams{
		Site: "letmoe", AnchorKind: model.AnchorKindCatalogWork, AnchorID: "w9",
		ContentRating: model.ContentRatingAll, AuthorID: 100, BodyRaw: "hello",
	})
	if err != nil {
		t.Fatalf("open wall: %v", err)
	}
	if _, err := es.SetAnchorLevel(letmoe, "letmoe", 301, model.AnchorKindCatalogWork, "w9", model.NotificationLevelWatching); err != nil {
		t.Fatalf("letmoe watches the work: %v", err)
	}
	if _, err := es.MarkRead(WithCallerSite(context.Background(), "kungal"), wall.ID, 301, 1); err != nil {
		t.Fatalf("read through kungal: %v", err)
	}
	if got := threadUserLevel(t, wall.ID, 301); got != model.NotificationLevelNormal {
		t.Fatalf("another site's anchor row must not set the level, got %d", got)
	}
	if _, err := es.MarkRead(letmoe, wall.ID, 302, 1); err != nil {
		t.Fatalf("read through letmoe: %v", err)
	}
	if _, err := es.SetAnchorLevel(letmoe, "letmoe", 303, model.AnchorKindCatalogWork, "w9", model.NotificationLevelWatching); err != nil {
		t.Fatalf("letmoe 303 watches the work: %v", err)
	}
	if _, err := es.MarkRead(letmoe, wall.ID, 303, 1); err != nil {
		t.Fatalf("read through letmoe: %v", err)
	}
	if got := threadUserLevel(t, wall.ID, 303); got != model.NotificationLevelWatching {
		t.Fatalf("the delivery site's anchor row sets the level, got %d", got)
	}
}

func threadUserSite(t *testing.T, threadID, userID int64) string {
	t.Helper()
	var row model.CommunityThreadUser
	if err := testDB.Where("thread_id = ? AND user_id = ?", threadID, userID).First(&row).Error; err != nil {
		t.Fatalf("thread_user %d/%d: %v", threadID, userID, err)
	}
	if row.Site == nil {
		return ""
	}
	return *row.Site
}

func TestThreadSubscriptionRecordsTheSite(t *testing.T) {
	cleanTables(t)
	ps := NewPostService(testDB, NoopSink{})
	es := NewEngagementService(testDB)
	seedTrust(t, 100, model.TrustLevelBasic, 0)
	seedTrust(t, 200, model.TrustLevelBasic, 0)

	th, _, err := ps.Comment(context.Background(), CommentParams{
		Site: "letmoe", AnchorKind: model.AnchorKindCatalogWork, AnchorID: "w42",
		ContentRating: model.ContentRatingAll, AuthorID: 100, BodyRaw: "hello",
	})
	if err != nil {
		t.Fatalf("open comment wall: %v", err)
	}
	if got := threadUserSite(t, th.ID, 100); got != "letmoe" {
		t.Fatalf("opener site: want letmoe, got %q", got)
	}

	if _, err := ps.Reply(WithCallerSite(context.Background(), "kungal"), ReplyParams{
		ThreadID: th.ID, AuthorID: 200, BodyRaw: "r",
	}); err != nil {
		t.Fatalf("kungal reply: %v", err)
	}
	if got := threadUserSite(t, th.ID, 200); got != "kungal" {
		t.Fatalf("replier site: want kungal, got %q", got)
	}

	if _, err := es.MarkRead(WithCallerSite(context.Background(), "moyu"), th.ID, 200, 2); err != nil {
		t.Fatalf("mark read through moyu: %v", err)
	}
	if got := threadUserSite(t, th.ID, 200); got != "moyu" {
		t.Fatalf("mark read must move the site to moyu, got %q", got)
	}
	if _, err := es.SetNotificationLevel(WithCallerSite(context.Background(), "kungal"), th.ID, 200, model.NotificationLevelTracking); err != nil {
		t.Fatalf("set level through kungal: %v", err)
	}
	if got := threadUserSite(t, th.ID, 200); got != "kungal" {
		t.Fatalf("set level must move the site to kungal, got %q", got)
	}
	if got := threadUserSite(t, th.ID, 100); got != "letmoe" {
		t.Fatalf("opener site must stay letmoe, got %q", got)
	}

	board := createBoard(t, NewBoardService(testDB), "letmoe", "general", 0)
	topic, err := openOn(NewThreadService(testDB, NoopSink{}), board.ID, 100, false)
	if err != nil {
		t.Fatalf("open topic: %v", err)
	}
	if got := threadUserSite(t, topic.ID, 100); got != "letmoe" {
		t.Fatalf("topic opener site: want letmoe, got %q", got)
	}
}
