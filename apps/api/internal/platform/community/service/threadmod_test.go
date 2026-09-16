package service

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"
)

func openOn(ts *ThreadService, boardID, author int64, asModerator bool) (*model.CommunityThread, error) {
	th, _, err := ts.OpenTopic(context.Background(), OpenTopicParams{
		Site: "letmoe", AuthorID: author, BoardID: boardID, AsModerator: asModerator,
		Title: "t", BodyRaw: "x",
	})
	return th, err
}

func TestTopicGates(t *testing.T) {
	cleanTables(t)
	bs := NewBoardService(testDB)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	ctx := context.Background()
	seedTrust(t, 100, model.TrustLevelBasic, 0)
	seedTrust(t, 101, model.TrustLevelNew, 0)

	news := createBoard(t, bs, "letmoe", "news", 0, func(f *BoardFields) { f.Format = model.BoardFormatAnnouncement })
	_, err := openOn(ts, news.ID, 100, false)
	wantErr[*ForbiddenError](t, err, "a member opening an announcement")
	announcement, err := openOn(ts, news.ID, 100, true)
	if err != nil {
		t.Fatalf("a moderator opens an announcement: %v", err)
	}
	if _, err := ps.Reply(ctx, ReplyParams{ThreadID: announcement.ID, AuthorID: 101, BodyRaw: "thanks"}); err != nil {
		t.Fatalf("anyone replies to an announcement: %v", err)
	}

	gated := createBoard(t, bs, "letmoe", "gated", 0, func(f *BoardFields) {
		f.TopicMinTrustLevel, f.ReplyMinTrustLevel = model.TrustLevelBasic, model.TrustLevelBasic
	})
	_, err = openOn(ts, gated.ID, 101, false)
	wantErr[*ForbiddenError](t, err, "a newcomer opening on a TL1 board")
	if _, err := openOn(ts, gated.ID, 101, true); err != nil {
		t.Fatalf("a moderator is exempt from the topic gate: %v", err)
	}
	topic, err := openOn(ts, gated.ID, 100, false)
	if err != nil {
		t.Fatalf("a TL1 member opens: %v", err)
	}
	_, err = ps.Reply(ctx, ReplyParams{ThreadID: topic.ID, AuthorID: 101, BodyRaw: "hi"})
	wantErr[*ForbiddenError](t, err, "a newcomer replying on a TL1 board")

	archived := model.BoardStatusArchived
	if _, err := bs.Update(ctx, "letmoe", gated.ID, 1, BoardPatch{Status: &archived}); err != nil {
		t.Fatalf("archive: %v", err)
	}
	_, err = openOn(ts, gated.ID, 100, true)
	wantErr[*ConflictError](t, err, "opening on an archived board")
	_, err = ps.Reply(ctx, ReplyParams{ThreadID: topic.ID, AuthorID: 100, BodyRaw: "late"})
	wantErr[*ConflictError](t, err, "replying on an archived board")

	foreign := createBoard(t, bs, "other", "general", 0)
	if _, err := openOn(ts, foreign.ID, 100, false); !errors.Is(err, ErrBoardNotFound) {
		t.Fatalf("another site's board: want ErrBoardNotFound, got %v", err)
	}
	_, _, err = ts.OpenTopic(ctx, OpenTopicParams{Site: "letmoe", AuthorID: 100, Title: "t", BodyRaw: "x"})
	wantErr[*InvalidError](t, err, "no board named")
}

func TestTopicInheritsTheBoardRating(t *testing.T) {
	cleanTables(t)
	bs := NewBoardService(testDB)
	ts := NewThreadService(testDB, NoopSink{})
	seedTrust(t, 100, model.TrustLevelBasic, 0)
	adult := createBoard(t, bs, "letmoe", "adult", 0, func(f *BoardFields) { f.ContentRating = model.ContentRatingR18 })

	th, post, err := ts.OpenTopic(context.Background(), OpenTopicParams{
		Site: "letmoe", AuthorID: 100, BoardID: adult.ID, Title: "t", BodyRaw: "x", ContentRating: model.ContentRatingAll,
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if th.ContentRating != model.ContentRatingR18 || post.ContentRating != model.ContentRatingR18 {
		t.Fatalf("an all-ages request on an R18 board must land R18: thread %d post %d", th.ContentRating, post.ContentRating)
	}
}

func TestTopicLegacyBoardKey(t *testing.T) {
	cleanTables(t)
	bs := NewBoardService(testDB)
	ts := NewThreadService(testDB, NoopSink{})
	seedTrust(t, 100, model.TrustLevelBasic, 0)
	main := createBoard(t, bs, "letmoe", "main", 0)

	for _, key := range []string{"main", model.BoardAnchorID(main.ID)} {
		th, _, err := ts.OpenTopic(context.Background(), OpenTopicParams{
			Site: "letmoe", AuthorID: 100, LegacyBoardKey: key, Title: "t", BodyRaw: "x",
		})
		if err != nil {
			t.Fatalf("key %q: %v", key, err)
		}
		if th.AnchorKind != model.AnchorKindBoard || th.AnchorID != model.BoardAnchorID(main.ID) {
			t.Fatalf("key %q must anchor on the board id, got %d/%q", key, th.AnchorKind, th.AnchorID)
		}
	}
	_, _, err := ts.OpenTopic(context.Background(), OpenTopicParams{
		Site: "letmoe", AuthorID: 100, LegacyBoardKey: "missing", Title: "t", BodyRaw: "x",
	})
	if !errors.Is(err, ErrBoardNotFound) {
		t.Fatalf("unknown slug: want ErrBoardNotFound, got %v", err)
	}
}

func TestMoveTopic(t *testing.T) {
	cleanTables(t)
	bs := NewBoardService(testDB)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	ctx := WithCallerSite(context.Background(), "letmoe")
	general := createBoard(t, bs, "letmoe", "general", 0)
	adult := createBoard(t, bs, "letmoe", "adult", 0, func(f *BoardFields) { f.ContentRating = model.ContentRatingR18 })

	th := openTopic(t, ts, "letmoe", 100, "general", "x")
	reply, err := ps.Reply(ctx, ReplyParams{ThreadID: th.ID, AuthorID: 100, BodyRaw: "r"})
	if err != nil {
		t.Fatalf("reply: %v", err)
	}

	moved, err := ts.Move(ctx, th.ID, adult.ID, 9)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if moved.AnchorID != model.BoardAnchorID(adult.ID) || moved.ContentRating != model.ContentRatingR18 {
		t.Fatalf("moved topic: anchor %q rating %d", moved.AnchorID, moved.ContentRating)
	}
	if getPost(t, reply.ID).ContentRating != model.ContentRatingR18 {
		t.Fatal("the posts must follow the thread's raised rating")
	}
	back, err := ts.Move(ctx, th.ID, general.ID, 9)
	if err != nil {
		t.Fatalf("move back: %v", err)
	}
	if back.AnchorID != model.BoardAnchorID(general.ID) || back.ContentRating != model.ContentRatingR18 {
		t.Fatalf("moving back keeps the higher rating: anchor %q rating %d", back.AnchorID, back.ContentRating)
	}

	pinned := openTopic(t, ts, "letmoe", 100, "general", "pinned")
	if _, err := ts.Pin(ctx, pinned.ID, model.PinScopeBoard, nil, 9); err != nil {
		t.Fatalf("board pin: %v", err)
	}
	if moved, err := ts.Move(ctx, pinned.ID, adult.ID, 9); err != nil || moved.PinScope != model.PinScopeNone || moved.PinnedAt != nil {
		t.Fatalf("a board pin stays behind on its board: %v %+v", err, moved)
	}
	if _, err := ts.Pin(ctx, pinned.ID, model.PinScopeSite, nil, 9); err != nil {
		t.Fatalf("site pin: %v", err)
	}
	if moved, err := ts.Move(ctx, pinned.ID, general.ID, 9); err != nil || moved.PinScope != model.PinScopeSite {
		t.Fatalf("a site pin travels with the topic: %v %+v", err, moved)
	}

	foreign := createBoard(t, bs, "other", "general", 0)
	if _, err := ts.Move(ctx, th.ID, foreign.ID, 9); !errors.Is(err, ErrBoardNotFound) {
		t.Fatalf("moving onto another site's board: want ErrBoardNotFound, got %v", err)
	}
	if _, err := ts.Move(WithCallerSite(context.Background(), "other"), th.ID, foreign.ID, 9); !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("another site moving this topic: want ErrThreadNotFound, got %v", err)
	}
	wall, err := ts.GetOrCreateCommentsThread(ctx, CommentsThreadParams{Site: "letmoe", AnchorKind: model.AnchorKindSiteGame, AnchorID: "g1"})
	if err != nil {
		t.Fatalf("wall: %v", err)
	}
	_, err = ts.Move(ctx, wall.ID, general.ID, 9)
	wantErr[*InvalidError](t, err, "moving a comment wall")
}

func listIDs(t *testing.T, ts *ThreadService, q repository.ThreadListQuery) []int64 {
	t.Helper()
	q.Site, q.Kind, q.Sort, q.Limit = "letmoe", model.ThreadKindTopic, repository.ThreadSortActivity, 50
	rows, err := ts.List(q)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	ids := make([]int64, len(rows))
	for i := range rows {
		ids[i] = rows[i].ID
	}
	slices.Sort(ids)
	return ids
}

func TestPinnedListings(t *testing.T) {
	cleanTables(t)
	bs := NewBoardService(testDB)
	ts := NewThreadService(testDB, NoopSink{})
	ctx := WithCallerSite(context.Background(), "letmoe")
	general := createBoard(t, bs, "letmoe", "general", 0)
	other := createBoard(t, bs, "letmoe", "other", 0)

	plain := openTopic(t, ts, "letmoe", 100, "general", "plain")
	onBoard := openTopic(t, ts, "letmoe", 100, "general", "board pin")
	siteWide := openTopic(t, ts, "letmoe", 100, "other", "site pin")
	lapsed := openTopic(t, ts, "letmoe", 100, "general", "lapsed pin")

	if _, err := ts.Pin(ctx, onBoard.ID, model.PinScopeBoard, nil, 9); err != nil {
		t.Fatalf("board pin: %v", err)
	}
	later := time.Now().Add(time.Hour)
	pinned, err := ts.Pin(ctx, siteWide.ID, model.PinScopeSite, &later, 9)
	if err != nil {
		t.Fatalf("site pin: %v", err)
	}
	if pinned.PinnedAt == nil || pinned.PinnedUntil == nil || !pinned.PinActive(time.Now()) {
		t.Fatalf("a fresh pin is active: %+v", pinned)
	}
	if _, err := ts.Pin(ctx, lapsed.ID, model.PinScopeSite, &later, 9); err != nil {
		t.Fatalf("pin to lapse: %v", err)
	}
	if err := testDB.Exec(`UPDATE community_thread SET pinned_until = now() - interval '1 minute' WHERE id = ?`, lapsed.ID).Error; err != nil {
		t.Fatalf("lapse pin: %v", err)
	}

	boardIDs := []int64{general.ID}
	want := func(ids ...int64) []int64 { slices.Sort(ids); return ids }
	for _, c := range []struct {
		name string
		q    repository.ThreadListQuery
		want []int64
	}{
		{"site pins on the site listing", repository.ThreadListQuery{Pinned: repository.PinFilterOnly}, want(siteWide.ID)},
		{"everything else on the site listing", repository.ThreadListQuery{Pinned: repository.PinFilterExclude}, want(plain.ID, onBoard.ID, lapsed.ID)},
		{"both pin scopes on a board", repository.ThreadListQuery{BoardIDs: boardIDs, Pinned: repository.PinFilterOnly}, want(onBoard.ID)},
		{"the rest of the board", repository.ThreadListQuery{BoardIDs: boardIDs, Pinned: repository.PinFilterExclude}, want(plain.ID, lapsed.ID)},
		{"the board unfiltered", repository.ThreadListQuery{BoardIDs: boardIDs}, want(plain.ID, onBoard.ID, lapsed.ID)},
		{"the other board", repository.ThreadListQuery{BoardIDs: []int64{other.ID}, Pinned: repository.PinFilterOnly}, want(siteWide.ID)},
	} {
		if got := listIDs(t, ts, c.q); !slices.Equal(got, c.want) {
			t.Errorf("%s: want %v, got %v", c.name, c.want, got)
		}
	}

	var all []model.CommunityThread
	if err := testDB.Where("kind = ?", model.ThreadKindTopic).Find(&all).Error; err != nil {
		t.Fatalf("load topics: %v", err)
	}
	var active []int64
	now := time.Now()
	for i := range all {
		if all[i].PinActive(now) && all[i].PinScope == model.PinScopeSite {
			active = append(active, all[i].ID)
		}
	}
	slices.Sort(active)
	if got := listIDs(t, ts, repository.ThreadListQuery{Pinned: repository.PinFilterOnly}); !slices.Equal(got, active) {
		t.Fatalf("SQL pins %v, Go pins %v", got, active)
	}

	unpinned, err := ts.Pin(ctx, onBoard.ID, model.PinScopeNone, nil, 9)
	if err != nil {
		t.Fatalf("unpin: %v", err)
	}
	if unpinned.PinScope != model.PinScopeNone || unpinned.PinnedAt != nil || unpinned.PinnedUntil != nil {
		t.Fatalf("unpin must clear the pin: %+v", unpinned)
	}
	past := time.Now().Add(-time.Minute)
	_, err = ts.Pin(ctx, plain.ID, model.PinScopeBoard, &past, 9)
	wantErr[*InvalidError](t, err, "a pin that already lapsed")
	_, err = ts.Pin(ctx, plain.ID, model.PinScopeNone, &later, 9)
	wantErr[*InvalidError](t, err, "an unpin with a lapse time")
	_, err = ts.Pin(ctx, plain.ID, 3, nil, 9)
	wantErr[*InvalidError](t, err, "an unknown scope")
}

func TestCloseAndReopen(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	fs := NewFeedbackService(testDB, NoopSink{})
	ctx := WithCallerSite(context.Background(), "letmoe")
	th := openTopic(t, ts, "letmoe", 100, "general", "x")

	closed, err := ts.SetClosed(ctx, th.ID, true, 9)
	if err != nil || closed.Status != model.ThreadStatusClosed {
		t.Fatalf("close: %v %+v", err, closed)
	}
	if _, err := ps.Reply(ctx, ReplyParams{ThreadID: th.ID, AuthorID: 100, BodyRaw: "r"}); !errors.Is(err, ErrThreadNotOpen) {
		t.Fatalf("reply to a closed topic: want ErrThreadNotOpen, got %v", err)
	}
	if again, err := ts.SetClosed(ctx, th.ID, true, 9); err != nil || again.Status != model.ThreadStatusClosed {
		t.Fatalf("closing twice is a no-op: %v", err)
	}
	if reopened, err := ts.SetClosed(ctx, th.ID, false, 9); err != nil || reopened.Status != model.ThreadStatusOpen {
		t.Fatalf("reopen: %v", err)
	}

	fb, _, err := ts.OpenFeedback(context.Background(), OpenFeedbackParams{
		Site: "letmoe", AuthorID: 100, AnchorKind: model.AnchorKindSiteResource, AnchorID: "r1", Title: "a", BodyRaw: "x",
	})
	if err != nil {
		t.Fatalf("feedback: %v", err)
	}
	target, _, err := ts.OpenFeedback(context.Background(), OpenFeedbackParams{
		Site: "letmoe", AuthorID: 100, AnchorKind: model.AnchorKindSiteResource, AnchorID: "r1", Title: "b", BodyRaw: "x",
	})
	if err != nil {
		t.Fatalf("feedback target: %v", err)
	}
	if err := fs.Merge(ctx, fb.ID, target.ID); err != nil {
		t.Fatalf("merge: %v", err)
	}
	_, err = ts.SetClosed(ctx, fb.ID, false, 9)
	wantErr[*ConflictError](t, err, "reopening a merged feedback thread")
}

func TestMarkAnswer(t *testing.T) {
	cleanTables(t)
	bs := NewBoardService(testDB)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	ctx := WithCallerSite(context.Background(), "letmoe")
	createBoard(t, bs, "letmoe", "qa", 0, func(f *BoardFields) { f.Format = model.BoardFormatQA })
	seedTrust(t, 200, model.TrustLevelBasic, 0)

	chat := openTopic(t, ts, "letmoe", 100, "general", "x")
	chatReply, err := ps.Reply(ctx, ReplyParams{ThreadID: chat.ID, AuthorID: 200, BodyRaw: "r"})
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	_, err = ts.SetAnswer(ctx, chat.ID, chatReply.ID, 100, false)
	wantErr[*InvalidError](t, err, "an answer on a discussion board")

	q := openTopic(t, ts, "letmoe", 100, "qa", "how?")
	answer, err := ps.Reply(ctx, ReplyParams{ThreadID: q.ID, AuthorID: 200, BodyRaw: "like this"})
	if err != nil {
		t.Fatalf("answer reply: %v", err)
	}
	hidden, err := ps.Reply(ctx, ReplyParams{ThreadID: q.ID, AuthorID: 200, BodyRaw: "spam"})
	if err != nil {
		t.Fatalf("hidden reply: %v", err)
	}
	if err := testDB.Model(&model.CommunityPost{}).Where("id = ?", hidden.ID).Update("status", model.PostStatusHidden).Error; err != nil {
		t.Fatalf("hide: %v", err)
	}
	opening := firstPostID(t, q.ID)

	_, err = ts.SetAnswer(ctx, q.ID, answer.ID, 200, false)
	wantErr[*ForbiddenError](t, err, "someone else marking the answer")
	for name, postID := range map[string]int64{
		"the opening post": opening, "a hidden reply": hidden.ID, "another thread's reply": chatReply.ID, "no such post": 999999,
	} {
		_, err = ts.SetAnswer(ctx, q.ID, postID, 100, false)
		wantErr[*InvalidError](t, err, name)
	}

	marked, err := ts.SetAnswer(ctx, q.ID, answer.ID, 100, false)
	if err != nil || marked.AnswerPostID == nil || *marked.AnswerPostID != answer.ID {
		t.Fatalf("the author marks the answer: %v %+v", err, marked)
	}
	cleared, err := ts.SetAnswer(ctx, q.ID, 0, 300, true)
	if err != nil || cleared.AnswerPostID != nil {
		t.Fatalf("a moderator clears the answer: %v %+v", err, cleared)
	}

	fb, _, err := ts.OpenFeedback(context.Background(), OpenFeedbackParams{
		Site: "letmoe", AuthorID: 100, AnchorKind: model.AnchorKindSiteResource, AnchorID: "r1", Title: "bug", BodyRaw: "x",
	})
	if err != nil {
		t.Fatalf("feedback: %v", err)
	}
	fix, err := ps.Reply(ctx, ReplyParams{ThreadID: fb.ID, AuthorID: 200, BodyRaw: "fixed"})
	if err != nil {
		t.Fatalf("feedback reply: %v", err)
	}
	if got, err := ts.SetAnswer(ctx, fb.ID, fix.ID, 300, true); err != nil || got.AnswerPostID == nil {
		t.Fatalf("a feedback thread takes an answer: %v", err)
	}
}

func firstPostID(t *testing.T, threadID int64) int64 {
	t.Helper()
	var id int64
	if err := testDB.Raw(`SELECT id FROM community_post WHERE thread_id = ? AND post_number = 1`, threadID).
		Scan(&id).Error; err != nil {
		t.Fatalf("opening post of %d: %v", threadID, err)
	}
	return id
}

func TestModerationStaysOnTheOpeningSite(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	mine, theirs := WithCallerSite(context.Background(), "letmoe"), WithCallerSite(context.Background(), "kungal")
	seedTrust(t, 100, model.TrustLevelBasic, 0)
	seedTrust(t, 200, model.TrustLevelBasic, 0)

	fb, _, err := ts.OpenFeedback(context.Background(), OpenFeedbackParams{
		Site: "letmoe", AuthorID: 100, AnchorKind: model.AnchorKindCatalogWork, AnchorID: "w1", Title: "bug", BodyRaw: "x",
	})
	if err != nil {
		t.Fatalf("feedback: %v", err)
	}
	reply, err := ps.Reply(theirs, ReplyParams{ThreadID: fb.ID, AuthorID: 200, BodyRaw: "a catalog thread takes every site's replies"})
	if err != nil {
		t.Fatalf("cross-site reply: %v", err)
	}
	if _, err := ts.SetClosed(theirs, fb.ID, true, 9); !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("another site closing it: want ErrThreadNotFound, got %v", err)
	}
	if _, err := ts.SetAnswer(theirs, fb.ID, reply.ID, 9, true); !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("another site marking its answer: want ErrThreadNotFound, got %v", err)
	}
	if _, err := ts.SetAnswer(mine, fb.ID, reply.ID, 9, true); err != nil {
		t.Fatalf("the opening site marks the answer: %v", err)
	}
	if _, err := ts.SetClosed(mine, fb.ID, true, 9); err != nil {
		t.Fatalf("the opening site closes it: %v", err)
	}
}
