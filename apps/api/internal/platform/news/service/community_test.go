package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"api/internal/platform/news/model"
)

const (
	reader      = int64(777)
	otherReader = int64(778)
)

func communityParams(uid int64, title string) CreateParams {
	return CreateParams{
		PublisherUID: uid, SourceKey: model.SourceKeyCommunity, Lane: model.LaneNews,
		Title: title, Preview: "summary", Body: "# 正文\n\n原创内容",
	}
}

func TestAnyAccountSubmitsToCommunityAndOwnsIt(t *testing.T) {
	svc := newSubmissionFixture(t)
	ctx := context.Background()

	sub, err := svc.Create(ctx, communityParams(reader, "t"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if sub.Status != model.StatusPending || sub.SubmitterUID == nil || *sub.SubmitterUID != reader ||
		sub.Body != "# 正文\n\n原创内容" || sub.SourceURL != "" {
		t.Fatalf("submission = %+v", sub)
	}
	if _, err := svc.Get(ctx, otherReader, sub.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("another account read the submission: %v", err)
	}
	title := "hijack"
	if _, err := svc.Update(ctx, otherReader, sub.ID, UpdateParams{Title: &title}); !errors.Is(err, ErrNotFound) {
		t.Errorf("another account edited the submission: %v", err)
	}
	mine, err := svc.List(ctx, reader, 0, 10)
	if err != nil || len(mine) != 1 || mine[0].ID != sub.ID {
		t.Fatalf("own list = %v, %v", mine, err)
	}
	theirs, err := svc.List(ctx, otherReader, 0, 10)
	if err != nil || len(theirs) != 0 {
		t.Fatalf("other list = %v, %v", theirs, err)
	}
}

func TestOnlyCommunitySubmissionsCarryABody(t *testing.T) {
	svc := newSubmissionFixture(t)
	ctx := context.Background()

	p := mineParams("t", "p")
	p.Body = "full article"
	if _, err := svc.Create(ctx, p); !errors.Is(err, ErrBodyNotAllowed) {
		t.Fatalf("partner body: %v", err)
	}
	sub, err := svc.Create(ctx, mineParams("t", "p"))
	if err != nil {
		t.Fatal(err)
	}
	body := "full article"
	if _, err := svc.Update(ctx, minePublisher, sub.ID, UpdateParams{Body: &body}); !errors.Is(err, ErrBodyNotAllowed) {
		t.Fatalf("partner body edit: %v", err)
	}

	long := communityParams(reader, "t")
	long.Body = strings.Repeat("字", model.BodyMaxRunes+1)
	if _, err := svc.Create(ctx, long); !errors.Is(err, ErrBodyTooLong) {
		t.Fatalf("over-long body: %v", err)
	}
}

func TestCountCommunitySinceCountsOnlyTheAccountsCommunityItems(t *testing.T) {
	svc := newSubmissionFixture(t)
	ctx := context.Background()

	old, err := svc.Create(ctx, communityParams(reader, "old"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []CreateParams{communityParams(reader, "a"), communityParams(reader, "b"),
		communityParams(otherReader, "c")} {
		if _, err := svc.Create(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	seedSource(t, "readers_own", reader, true)
	partner := mineParams("t", "p")
	partner.PublisherUID, partner.SourceKey = reader, "readers_own"
	if _, err := svc.Create(ctx, partner); err != nil {
		t.Fatal(err)
	}
	if err := testDB.Exec(`UPDATE news_item SET created_at = ? WHERE id = ?`,
		time.Now().Add(-25*time.Hour), old.ID).Error; err != nil {
		t.Fatal(err)
	}
	n, err := svc.CountCommunitySince(ctx, reader, time.Now().Add(-24*time.Hour))
	if err != nil || n != 2 {
		t.Errorf("count = %d, %v; want 2 (own community items inside the window)", n, err)
	}
}

func TestTrustedSourcePublishesOnSubmissionAndStaysPublishedOnEdit(t *testing.T) {
	svc := newSubmissionFixture(t)
	ctx := context.Background()
	if err := testDB.Exec(`UPDATE news_source SET auto_publish = true WHERE key = 'moyu'`).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testDB.Exec(`UPDATE news_source SET auto_publish = false WHERE key = 'moyu'`) })

	sub, err := svc.Create(ctx, mineParams("t", "p"))
	if err != nil {
		t.Fatal(err)
	}
	if sub.Status != model.StatusPublished {
		t.Fatalf("status = %d, want published", sub.Status)
	}
	var d model.NewsModerationDecision
	if err := testDB.Where("item_id = ?", sub.ID).Take(&d).Error; err != nil {
		t.Fatalf("decision: %v", err)
	}
	if d.ActorUID != model.SystemActorUID || d.ToStatus != model.StatusPublished || d.Reason != reasonTrustedSubmission {
		t.Errorf("decision = %+v", d)
	}

	title := "edited"
	got, err := svc.Update(ctx, minePublisher, sub.ID, UpdateParams{Title: &title})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.StatusPublished {
		t.Errorf("a trusted source's edit left the feed: status %d", got.Status)
	}

	c, err := svc.Create(ctx, communityParams(reader, "t"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != model.StatusPending {
		t.Errorf("a community submission skipped review: status %d", c.Status)
	}
}

func TestPurgeAccountWithdrawsAndScrubsSubmissions(t *testing.T) {
	svc := newSubmissionFixture(t)
	ctx := context.Background()

	pending, err := svc.Create(ctx, communityParams(reader, "pending"))
	if err != nil {
		t.Fatal(err)
	}
	published, err := svc.Create(ctx, communityParams(reader, "published"))
	if err != nil {
		t.Fatal(err)
	}
	testDB.Model(&model.NewsItem{}).Where("id = ?", published.ID).Update("status", model.StatusPublished)
	kept, err := svc.Create(ctx, communityParams(otherReader, "kept"))
	if err != nil {
		t.Fatal(err)
	}

	n, err := PurgeAccount(ctx, testDB, reader)
	if err != nil || n != 2 {
		t.Fatalf("purge = %d, %v; want 2", n, err)
	}
	for _, id := range []int64{pending.ID, published.ID} {
		var row model.NewsItem
		testDB.Where("id = ?", id).Take(&row)
		if row.Status != model.StatusWithdrawn || row.Preview != "" || row.Body != "" {
			t.Errorf("item %d after purge: status=%d preview=%q body=%q", id, row.Status, row.Preview, row.Body)
		}
	}
	var other model.NewsItem
	testDB.Where("id = ?", kept.ID).Take(&other)
	if other.Status != model.StatusPending || other.Body == "" {
		t.Errorf("another account's item was touched: %+v", other)
	}
	if n, err := PurgeAccount(ctx, testDB, reader); err != nil || n != 0 {
		t.Errorf("second purge = %d, %v; want 0", n, err)
	}
}
