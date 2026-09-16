package service

import (
	"context"
	"fmt"
	"testing"

	"api/internal/platform/community/model"
	"api/internal/platform/settings/keys"
)

func TestSandbox_ContentLimits(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	th := openTopic(t, ts, "letmoe", 100, "b1", "opening")

	tooManyLinks := "[a](https://a.com) [b](https://b.com) [c](https://c.com)"
	if _, err := ps.Reply(context.Background(), ReplyParams{ThreadID: th.ID, AuthorID: 201, BodyRaw: tooManyLinks}); !isSandbox(err) {
		t.Fatalf("TL0 over link cap should be a SandboxError, got %v", err)
	}
	if _, err := ps.Reply(context.Background(), ReplyParams{ThreadID: th.ID, AuthorID: 201, BodyRaw: "[a](https://a.com) [b](https://b.com)"}); err != nil {
		t.Fatalf("TL0 within link cap should pass: %v", err)
	}

	seedTrust(t, 202, model.TrustLevelBasic, 0)
	if _, err := ps.Reply(context.Background(), ReplyParams{ThreadID: th.ID, AuthorID: 202, BodyRaw: tooManyLinks}); err != nil {
		t.Fatalf("TL1 should be exempt from the link cap: %v", err)
	}
}

func TestSandbox_FirstPostHold(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	th := openTopic(t, ts, "letmoe", 100, "b1", "opening")

	author := int64(500)
	seedTrust(t, author, model.TrustLevelNew, 2)
	p1, err := ps.Reply(context.Background(), ReplyParams{ThreadID: th.ID, AuthorID: author, BodyRaw: "one"})
	if err != nil {
		t.Fatalf("p1: %v", err)
	}
	p2, _ := ps.Reply(context.Background(), ReplyParams{ThreadID: th.ID, AuthorID: author, BodyRaw: "two"})
	p3, _ := ps.Reply(context.Background(), ReplyParams{ThreadID: th.ID, AuthorID: author, BodyRaw: "three"})
	if p1.Status != model.PostStatusHidden || p2.Status != model.PostStatusHidden {
		t.Fatalf("first two posts should be held: p1=%d p2=%d", p1.Status, p2.Status)
	}
	if p3.Status != model.PostStatusVisible {
		t.Fatalf("third post should be visible: p3=%d", p3.Status)
	}
	var holds int32
	testDB.Model(&model.CommunityTrust{}).Where("user_id = ?", author).Select("first_posts_held_remaining").Scan(&holds)
	if holds != 0 {
		t.Fatalf("holds should be spent to 0, got %d", holds)
	}
}

func TestSandbox_DailyLimits(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})

	author := int64(600)
	seedTrust(t, author, model.TrustLevelNew, 0)
	for i := range keys.CommunitySandboxMaxTopicsPerDay.Get() {
		if _, _, err := ts.OpenTopic(context.Background(), OpenTopicParams{
			Site: "letmoe", AuthorID: author, BoardID: testBoard(t, "letmoe", fmt.Sprintf("b%d", i)),
			Title: "t", ContentRating: model.ContentRatingAll, BodyRaw: "x",
		}); err != nil {
			t.Fatalf("topic %d should pass: %v", i, err)
		}
	}
	if _, _, err := ts.OpenTopic(context.Background(), OpenTopicParams{
		Site: "letmoe", AuthorID: author, BoardID: testBoard(t, "letmoe", "over"),
		Title: "t", ContentRating: model.ContentRatingAll, BodyRaw: "x",
	}); !isSandbox(err) {
		t.Fatalf("4th topic should hit the daily cap, got %v", err)
	}

	th := openTopic(t, ts, "letmoe", 100, "host", "opening")
	replier := int64(700)
	seedTrust(t, replier, model.TrustLevelNew, 0)
	for i := range keys.CommunitySandboxMaxRepliesPerDay.Get() {
		if _, err := ps.Reply(context.Background(), ReplyParams{ThreadID: th.ID, AuthorID: replier, BodyRaw: "r"}); err != nil {
			t.Fatalf("reply %d should pass: %v", i, err)
		}
	}
	if _, err := ps.Reply(context.Background(), ReplyParams{ThreadID: th.ID, AuthorID: replier, BodyRaw: "r"}); !isSandbox(err) {
		t.Fatalf("11th reply should hit the daily cap, got %v", err)
	}
}

func isSandbox(err error) bool {
	_, ok := err.(*SandboxError)
	return ok
}
