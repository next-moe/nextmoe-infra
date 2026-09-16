package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"api/internal/platform/community/model"
	"api/internal/platform/settings"
	"api/internal/platform/settings/keys"
	"api/pkg/trustclient"
)

type fakeChecker struct {
	mu       sync.Mutex
	calls    []trustclient.CheckRequest
	decision string
	fail     bool
}

func (f *fakeChecker) Check(_ context.Context, req trustclient.CheckRequest) (string, []string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	fail, decision := f.fail, f.decision
	f.mu.Unlock()
	if fail {
		return "", nil, errors.New("check boom")
	}
	if decision == "" {
		decision = checkAllow
	}
	matched := []string{}
	if decision == checkDeny || decision == checkHold {
		matched = []string{"badword"}
	}
	return decision, matched, nil
}

func (f *fakeChecker) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeChecker) last() trustclient.CheckRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}

func reviewItemsForPost(t *testing.T, postID int64) []model.CommunityReviewItem {
	t.Helper()
	var items []model.CommunityReviewItem
	if err := testDB.Where("post_id = ?", postID).Find(&items).Error; err != nil {
		t.Fatalf("load review items for post %d: %v", postID, err)
	}
	return items
}

func checkWiring(t *testing.T, f *fakeChecker, sink EventSink) (*ThreadService, *PostService) {
	t.Helper()
	settings.Override(t, keys.TrustCheckEnabled, true)
	chk := NewCheckService(f)
	ts := NewThreadService(testDB, sink, WithThreadChecker(chk))
	ps := NewPostService(testDB, sink, WithPostChecker(chk))
	return ts, ps
}

func TestCheckDisabledZeroDial(t *testing.T) {
	cleanTables(t)
	ctx := context.Background()
	fake := &fakeChecker{decision: checkDeny}
	off := NewCheckService(nil)
	if off.Enabled() {
		t.Fatal("nil checker must report disabled")
	}
	ts := NewThreadService(testDB, NoopSink{}, WithThreadChecker(off))
	ps := NewPostService(testDB, NoopSink{}, WithPostChecker(off))

	seedTrust(t, 100, model.TrustLevelBasic, 0)
	_, post, err := ts.OpenTopic(ctx, OpenTopicParams{
		Site: "letmoe", AuthorID: 100, BoardID: testBoard(t, "letmoe", "b1"),
		Title: "t", ContentRating: model.ContentRatingAll, BodyRaw: "opening",
	})
	if err != nil {
		t.Fatalf("open topic: %v", err)
	}
	seedTrust(t, 200, model.TrustLevelBasic, 0)
	rp, err := ps.Reply(ctx, ReplyParams{ThreadID: post.ThreadID, AuthorID: 200, BodyRaw: "reply body"})
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	if _, err := ps.Edit(ctx, EditParams{PostID: rp.ID, AuthorID: 200, BodyRaw: "edited body"}); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if fake.count() != 0 {
		t.Fatalf("disabled gate must never dial, got %d calls", fake.count())
	}
	if items := reviewItemsForPost(t, post.ID); len(items) != 0 {
		t.Fatalf("first post has unexpected review items: %+v", items)
	}
	if items := reviewItemsForPost(t, rp.ID); len(items) != 0 {
		t.Fatalf("reply has unexpected review items: %+v", items)
	}
}

func TestCheckDenyBlocksAllWritePaths(t *testing.T) {
	cleanTables(t)
	ctx := context.Background()
	fake := &fakeChecker{decision: checkDeny}
	ts, ps := checkWiring(t, fake, NoopSink{})

	seedTrust(t, 100, model.TrustLevelBasic, 0)
	_, _, err := ts.OpenTopic(ctx, OpenTopicParams{
		Site: "letmoe", AuthorID: 100, BoardID: testBoard(t, "letmoe", "b1"),
		Title: "t", ContentRating: model.ContentRatingAll, BodyRaw: "banned opening",
	})
	if !errors.Is(err, ErrContentBlocked) {
		t.Fatalf("open topic deny: err = %v, want ErrContentBlocked", err)
	}
	var threadN int64
	testDB.Model(&model.CommunityThread{}).Count(&threadN)
	if threadN != 0 {
		t.Fatalf("deny must persist no thread, found %d", threadN)
	}
	var postN int64
	testDB.Model(&model.CommunityPost{}).Count(&postN)
	if postN != 0 {
		t.Fatalf("deny must persist no post, found %d", postN)
	}

	tsOff := NewThreadService(testDB, NoopSink{})
	th := openTopic(t, tsOff, "letmoe", 100, "b2", "clean opening")
	seedTrust(t, 200, model.TrustLevelBasic, 0)
	_, err = ps.Reply(ctx, ReplyParams{ThreadID: th.ID, AuthorID: 200, BodyRaw: "banned reply"})
	if !errors.Is(err, ErrContentBlocked) {
		t.Fatalf("reply deny: err = %v, want ErrContentBlocked", err)
	}
	var postsOnThread int64
	testDB.Model(&model.CommunityPost{}).Where("thread_id = ?", th.ID).Count(&postsOnThread)
	if postsOnThread != 1 {
		t.Fatalf("deny reply must persist no post; thread has %d posts (want just the opener)", postsOnThread)
	}

	psOff := NewPostService(testDB, NoopSink{})
	seedTrust(t, 300, model.TrustLevelBasic, 0)
	clean, err := psOff.Reply(ctx, ReplyParams{ThreadID: th.ID, AuthorID: 300, BodyRaw: "clean reply"})
	if err != nil {
		t.Fatalf("seed clean reply: %v", err)
	}
	_, err = ps.Edit(ctx, EditParams{PostID: clean.ID, AuthorID: 300, BodyRaw: "banned edit"})
	if !errors.Is(err, ErrContentBlocked) {
		t.Fatalf("edit deny: err = %v, want ErrContentBlocked", err)
	}
	if got := getPost(t, clean.ID); got.ContentRaw != "clean reply" || got.EditedAt != nil {
		t.Fatalf("deny edit must not persist: raw=%q edited_at=%v", got.ContentRaw, got.EditedAt)
	}
	var reviewN int64
	testDB.Model(&model.CommunityReviewItem{}).Count(&reviewN)
	if reviewN != 0 {
		t.Fatalf("deny must never enqueue a review item, found %d", reviewN)
	}
}

func TestCheckHoldEnqueuesSuspectOnce(t *testing.T) {
	settings.Override(t, keys.TrustCheckEnabled, true)
	cleanTables(t)
	ctx := context.Background()
	fake := &fakeChecker{decision: checkHold}
	sink := &recordingSink{}
	tsOff := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, sink, WithPostChecker(NewCheckService(fake)))

	th := openTopic(t, tsOff, "letmoe", 100, "b1", "opening")
	seedTrust(t, 200, model.TrustLevelBasic, 0)

	post, err := ps.Reply(ctx, ReplyParams{ThreadID: th.ID, AuthorID: 200, BodyRaw: "suspect reply"})
	if err != nil {
		t.Fatalf("hold reply: %v", err)
	}
	if post.Status != model.PostStatusVisible {
		t.Fatalf("hold must publish normally (visible), got status %d", post.Status)
	}
	items := reviewItemsForPost(t, post.ID)
	if len(items) != 1 {
		t.Fatalf("hold reply must enqueue exactly one review item, got %d", len(items))
	}
	if items[0].Source == nil || *items[0].Source != model.ReviewSourceSuspectWords {
		t.Fatalf("review source = %v, want suspect_words (%d)", items[0].Source, model.ReviewSourceSuspectWords)
	}
	if sink.count(EventReviewEnqueued) != 1 {
		t.Fatalf("review.enqueued fired %d times, want 1", sink.count(EventReviewEnqueued))
	}

	if _, err := ps.Edit(ctx, EditParams{PostID: post.ID, AuthorID: 200, BodyRaw: "still suspect"}); err != nil {
		t.Fatalf("hold edit: %v", err)
	}
	if items := reviewItemsForPost(t, post.ID); len(items) != 1 {
		t.Fatalf("edit hold must be idempotent, review items now %d (want 1)", len(items))
	}
	if sink.count(EventReviewEnqueued) != 1 {
		t.Fatalf("idempotent edit must not re-emit review.enqueued, got %d", sink.count(EventReviewEnqueued))
	}
}

func TestCheckFailOpen(t *testing.T) {
	settings.Override(t, keys.TrustCheckEnabled, true)
	cleanTables(t)
	ctx := context.Background()
	fake := &fakeChecker{fail: true}
	tsOff := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{}, WithPostChecker(NewCheckService(fake)))

	th := openTopic(t, tsOff, "letmoe", 100, "b1", "opening")
	seedTrust(t, 200, model.TrustLevelBasic, 0)

	post, err := ps.Reply(ctx, ReplyParams{ThreadID: th.ID, AuthorID: 200, BodyRaw: "reply body"})
	if err != nil {
		t.Fatalf("reply must succeed despite check error (fail-open): %v", err)
	}
	if post.Status != model.PostStatusVisible {
		t.Fatalf("fail-open reply must be visible, got status %d", post.Status)
	}
	edited, err := ps.Edit(ctx, EditParams{PostID: post.ID, AuthorID: 200, BodyRaw: "edited body"})
	if err != nil {
		t.Fatalf("edit must succeed despite check error (fail-open): %v", err)
	}
	if edited.ContentRaw != "edited body" {
		t.Fatalf("fail-open edit must persist, raw = %q", edited.ContentRaw)
	}
	if got := getPost(t, post.ID); got.ContentRaw != "edited body" || got.EditedAt == nil {
		t.Fatalf("fail-open edit not persisted: raw=%q edited_at=%v", got.ContentRaw, got.EditedAt)
	}
	if items := reviewItemsForPost(t, post.ID); len(items) != 0 {
		t.Fatalf("fail-open must not enqueue, got %d items", len(items))
	}
	if fake.count() != 2 {
		t.Fatalf("fail-open expected 2 dial attempts (create+edit), got %d", fake.count())
	}
}

func TestCheckFirstPostTitleComposition(t *testing.T) {
	cleanTables(t)
	ctx := context.Background()
	fake := &fakeChecker{decision: checkAllow}
	ts, ps := checkWiring(t, fake, NoopSink{})

	seedTrust(t, 100, model.TrustLevelBasic, 0)
	th, _, err := ts.OpenTopic(ctx, OpenTopicParams{
		Site: "letmoe", AuthorID: 100, BoardID: testBoard(t, "letmoe", "b1"),
		Title: "My Title", ContentRating: model.ContentRatingAll, BodyRaw: "opening body",
	})
	if err != nil {
		t.Fatalf("open topic: %v", err)
	}
	firstReq := fake.last()
	if want := "My Title\n\nopening body"; firstReq.Text != want {
		t.Fatalf("first-post check text = %q, want %q", firstReq.Text, want)
	}
	if firstReq.Site != "letmoe" {
		t.Fatalf("first-post check site = %q, want letmoe", firstReq.Site)
	}
	if firstReq.AuthorID == nil || *firstReq.AuthorID != 100 {
		t.Fatalf("first-post check author = %v, want 100", firstReq.AuthorID)
	}

	seedTrust(t, 200, model.TrustLevelBasic, 0)
	if _, err := ps.Reply(ctx, ReplyParams{ThreadID: th.ID, AuthorID: 200, BodyRaw: "reply raw"}); err != nil {
		t.Fatalf("reply: %v", err)
	}
	replyReq := fake.last()
	if replyReq.Text != "reply raw" {
		t.Fatalf("reply check text = %q, want the raw body (no title prefix)", replyReq.Text)
	}
}
