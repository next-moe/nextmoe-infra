package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"api/internal/jobs/newsmoderate"
	"api/internal/platform/news/model"
)

const (
	minePublisher  = int64(4242)
	otherPublisher = int64(9999)
)

func newSubmissionFixture(t *testing.T) *SubmissionService {
	t.Helper()
	newFixture(t)
	seedSource(t, "moyu", minePublisher, true)
	seedSource(t, "sleeping", minePublisher, false)
	seedSource(t, "someone_else", otherPublisher, true)
	return NewSubmissionService(testDB, "https://img.example.test")
}

func seedSource(t *testing.T, key string, uid int64, active bool) {
	t.Helper()
	if err := testDB.Exec(`
		INSERT INTO news_source (key, display_name, homepage_url, attribution, publisher_uid, column_url, active)
		VALUES (?, ?, 'https://example.test', 'attribution text', ?, '', ?)
		ON CONFLICT (key) DO UPDATE SET publisher_uid = EXCLUDED.publisher_uid, active = EXCLUDED.active`,
		key, key, uid, active).Error; err != nil {
		t.Fatalf("seed source %s: %v", key, err)
	}
}

func mineParams(title, preview string) CreateParams {
	return CreateParams{
		PublisherUID: minePublisher, SourceKey: "moyu", Lane: model.LaneNews,
		Title: title, Preview: preview, SourceURL: "https://example.test/a",
		PublishedAt: time.Now().UTC().Truncate(time.Second),
	}
}

func TestSourceRowIsTheGrant(t *testing.T) {
	svc := newSubmissionFixture(t)
	ctx := context.Background()

	p := mineParams("t", "p")
	p.SourceKey = "no_such_source"
	if _, err := svc.Create(ctx, p); !errors.Is(err, ErrSourceNotYours) {
		t.Fatalf("unknown source: %v", err)
	}
	p.SourceKey = "someone_else"
	if _, err := svc.Create(ctx, p); !errors.Is(err, ErrSourceNotYours) {
		t.Fatalf("another publisher's source: %v", err)
	}
	p.SourceKey = "sleeping"
	if _, err := svc.Create(ctx, p); !errors.Is(err, ErrSourceInactive) {
		t.Fatalf("deactivated source: %v", err)
	}
	if _, err := svc.Create(ctx, mineParams("t", "p")); err != nil {
		t.Fatalf("own active source: %v", err)
	}
}

func TestCreateLandsPendingWithNativeIdentity(t *testing.T) {
	svc := newSubmissionFixture(t)
	ctx := context.Background()

	p := mineParams("Hello", "A lede")
	p.WorkIDs = []int64{7, 7, 3}
	sub, err := svc.Create(ctx, p)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if sub.Status != model.StatusPending {
		t.Fatalf("status = %d; POST always lands on pending", sub.Status)
	}
	if len(sub.WorkIDs) != 2 || sub.WorkIDs[0] != 3 || sub.WorkIDs[1] != 7 {
		t.Fatalf("work_ids = %v; duplicates must collapse", sub.WorkIDs)
	}
	var row model.NewsItem
	if err := testDB.Where("id = ?", sub.ID).Take(&row).Error; err != nil {
		t.Fatalf("load: %v", err)
	}
	if !strings.HasPrefix(row.ExternalID, nativeExternalIDPrefix) {
		t.Fatalf("external_id = %q; native rows must carry the api_ prefix", row.ExternalID)
	}
	if row.UpstreamCategory != "" || row.BannerOriginURL != "" {
		t.Fatalf("upstream_category = %q banner_origin_url = %q; both are importer-only",
			row.UpstreamCategory, row.BannerOriginURL)
	}
	var links []model.NewsItemWork
	if err := testDB.Where("item_id = ?", sub.ID).Find(&links).Error; err != nil {
		t.Fatalf("links: %v", err)
	}
	for _, l := range links {
		if l.Confidence != model.WorkConfidenceManual {
			t.Fatalf("work %d confidence = %d; API links are manual", l.WorkID, l.Confidence)
		}
	}
}

// TestNativeExternalIDCannotCollideWithImporterKeys guards the one shared
// namespace this face touches: (source_key, external_id) is UNIQUE, and a
// publisher may also be the publisher_uid on a partner source. 月幕 writes bare
// snowflake digits, galgame 批评 writes cv<id>#<ordinal>.
func TestNativeExternalIDCannotCollideWithImporterKeys(t *testing.T) {
	newSubmissionFixture(t)
	seen := map[string]bool{}
	for range 500 {
		id := mintExternalID()
		if seen[id] {
			t.Fatalf("mint repeated %q", id)
		}
		seen[id] = true
		if !strings.HasPrefix(id, nativeExternalIDPrefix) {
			t.Fatalf("mint %q lost its prefix", id)
		}
		if strings.HasPrefix(id, "cv") || strings.IndexFunc(id, func(r rune) bool { return r < '0' || r > '9' }) < 0 {
			t.Fatalf("mint %q is shaped like an importer key", id)
		}
	}
}

// TestEditedPendingTextReturnsToTheMachineQueue drives the real newsmoderate
// runner: a settled verdict for the old text must stop re-scoring, and a text
// edit must start it again. Without the second half a publisher could edit a
// scored item into anything after the machine had already cleared it.
func TestEditedPendingTextReturnsToTheMachineQueue(t *testing.T) {
	svc := newSubmissionFixture(t)
	ctx := context.Background()

	sub, err := svc.Create(ctx, mineParams("Original title", "Original lede"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	runner := newsmoderate.New(testDB, stubScorer{}, newsmoderate.Opts{Apply: true, Limit: 50})

	st, err := runner.Run(ctx)
	if err != nil {
		t.Fatalf("first pass: %v", err)
	}
	if st.NeedScoring != 1 || st.Scored != 1 {
		t.Fatalf("first pass %s; a fresh pending item must be scored", st)
	}
	if st, err = runner.Run(ctx); err != nil || st.NeedScoring != 0 {
		t.Fatalf("second pass %s err=%v; a settled verdict must stop re-scoring", st, err)
	}

	url := "https://example.test/moved"
	if _, err := svc.Update(ctx, minePublisher, sub.ID, UpdateParams{SourceURL: &url}); err != nil {
		t.Fatalf("metadata edit: %v", err)
	}
	if st, err = runner.Run(ctx); err != nil || st.NeedScoring != 0 {
		t.Fatalf("after metadata edit %s err=%v; source_url is not graded text", st, err)
	}

	title := "Rewritten title"
	if _, err := svc.Update(ctx, minePublisher, sub.ID, UpdateParams{Title: &title}); err != nil {
		t.Fatalf("text edit: %v", err)
	}
	if st, err = runner.Run(ctx); err != nil || st.NeedScoring != 1 || st.Scored != 1 {
		t.Fatalf("after text edit %s err=%v; a changed fingerprint must re-enter the queue", st, err)
	}
}

func TestWithdrawOnlyFromPublished(t *testing.T) {
	svc := newSubmissionFixture(t)
	ctx := context.Background()

	sub, err := svc.Create(ctx, mineParams("t", "p"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.Withdraw(ctx, minePublisher, sub.ID); !errors.Is(err, ErrIllegalTransition) {
		t.Fatalf("withdraw while pending: %v", err)
	}
	if err := testDB.Model(&model.NewsItem{}).Where("id = ?", sub.ID).
		Update("status", model.StatusPublished).Error; err != nil {
		t.Fatalf("publish: %v", err)
	}
	out, err := svc.Withdraw(ctx, minePublisher, sub.ID)
	if err != nil || out.Status != model.StatusWithdrawn {
		t.Fatalf("withdraw: %v status=%d", err, out.Status)
	}
	var decisions []model.NewsModerationDecision
	if err := testDB.Where("item_id = ?", sub.ID).Find(&decisions).Error; err != nil {
		t.Fatalf("decisions: %v", err)
	}
	if len(decisions) != 1 || decisions[0].ActorUID != minePublisher ||
		decisions[0].FromStatus != model.StatusPublished || decisions[0].ToStatus != model.StatusWithdrawn {
		t.Fatalf("audit line = %+v; a withdrawal is a decision row plus a status", decisions)
	}
	if _, err := svc.Withdraw(ctx, minePublisher, sub.ID); !errors.Is(err, ErrIllegalTransition) {
		t.Fatalf("second withdraw: %v", err)
	}
}

func TestTextIsEditableOnlyWhilePending(t *testing.T) {
	svc := newSubmissionFixture(t)
	ctx := context.Background()

	sub, err := svc.Create(ctx, mineParams("t", "p"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	title := "edited"
	if _, err := svc.Update(ctx, minePublisher, sub.ID, UpdateParams{Title: &title}); err != nil {
		t.Fatalf("pending edit: %v", err)
	}
	for _, status := range []int16{model.StatusPublished, model.StatusRejected, model.StatusWithdrawn} {
		if err := testDB.Model(&model.NewsItem{}).Where("id = ?", sub.ID).
			Update("status", status).Error; err != nil {
			t.Fatalf("set status %d: %v", status, err)
		}
		if _, err := svc.Update(ctx, minePublisher, sub.ID, UpdateParams{Title: &title}); !errors.Is(err, ErrNotEditable) {
			t.Fatalf("edit at status %d: %v", status, err)
		}
	}
}

func TestOverlongPreviewIsRefused(t *testing.T) {
	svc := newSubmissionFixture(t)
	ctx := context.Background()

	long := strings.Repeat("あ", model.PreviewMaxRunes+1)
	if _, err := svc.Create(ctx, mineParams("t", long)); !errors.Is(err, ErrPreviewTooLong) {
		t.Fatalf("create with %d runes: %v", model.PreviewMaxRunes+1, err)
	}
	exact := strings.Repeat("あ", model.PreviewMaxRunes)
	sub, err := svc.Create(ctx, mineParams("t", exact))
	if err != nil {
		t.Fatalf("create with exactly %d runes: %v", model.PreviewMaxRunes, err)
	}
	if _, err := svc.Update(ctx, minePublisher, sub.ID, UpdateParams{Preview: &long}); !errors.Is(err, ErrPreviewTooLong) {
		t.Fatalf("edit to %d runes: %v", model.PreviewMaxRunes+1, err)
	}
}

func TestReadsAreFencedToTheCallersSources(t *testing.T) {
	svc := newSubmissionFixture(t)
	ctx := context.Background()

	mine, err := svc.Create(ctx, mineParams("mine", "p"))
	if err != nil {
		t.Fatalf("create mine: %v", err)
	}
	theirs, err := svc.Create(ctx, CreateParams{
		PublisherUID: otherPublisher, SourceKey: "someone_else", Lane: model.LaneNews,
		Title: "theirs", Preview: "p", SourceURL: "https://example.test/b",
	})
	if err != nil {
		t.Fatalf("create theirs: %v", err)
	}

	rows, err := svc.List(ctx, minePublisher, 0, 50)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != mine.ID {
		t.Fatalf("list returned %d rows; the caller sees only their own sources", len(rows))
	}
	n, err := svc.Count(ctx, minePublisher)
	if err != nil || n != 1 {
		t.Fatalf("count = %d err = %v", n, err)
	}
	if _, err := svc.Get(ctx, minePublisher, theirs.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get another publisher's item: %v", err)
	}
	if _, err := svc.Update(ctx, minePublisher, theirs.ID, UpdateParams{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("edit another publisher's item: %v", err)
	}
	if _, err := svc.Withdraw(ctx, minePublisher, theirs.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("withdraw another publisher's item: %v", err)
	}
}

func TestListPagesNewestFirst(t *testing.T) {
	svc := newSubmissionFixture(t)
	ctx := context.Background()

	var ids []int64
	for range 3 {
		sub, err := svc.Create(ctx, mineParams("t", "p"))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		ids = append(ids, sub.ID)
	}
	page, err := svc.List(ctx, minePublisher, 0, 2)
	if err != nil || len(page) != 2 || page[0].ID != ids[2] || page[1].ID != ids[1] {
		t.Fatalf("first page %v err=%v", page, err)
	}
	next, err := svc.List(ctx, minePublisher, page[1].ID, 2)
	if err != nil || len(next) != 1 || next[0].ID != ids[0] {
		t.Fatalf("second page %v err=%v", next, err)
	}
}

// TestPendingSubmissionNeverReachesThePublicFace is the write-face half of the
// obligation the read face already carries: nothing a publisher POSTs is
// addressable on /v2/news until a human decides.
func TestPendingSubmissionNeverReachesThePublicFace(t *testing.T) {
	svc := newSubmissionFixture(t)
	ctx := context.Background()
	pub := NewPublicService(testDB, testCDNBase)

	sub, err := svc.Create(ctx, mineParams("Pending", "p"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	feed, err := pub.Feed(ctx, FeedFilter{}, "", 50)
	if err != nil {
		t.Fatalf("feed: %v", err)
	}
	if len(feed.Items) != 0 {
		t.Fatalf("public feed has %d items; a pending submission must not appear", len(feed.Items))
	}
	if _, err := pub.Item(ctx, sub.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("public detail of a pending item: %v", err)
	}

	if err := testDB.Model(&model.NewsItem{}).Where("id = ?", sub.ID).
		Update("status", model.StatusPublished).Error; err != nil {
		t.Fatalf("publish: %v", err)
	}
	feed, err = pub.Feed(ctx, FeedFilter{}, "", 50)
	if err != nil || len(feed.Items) != 1 {
		t.Fatalf("after publishing: %d items err=%v — the control proves the filter is the status", len(feed.Items), err)
	}
	if _, err := svc.Withdraw(ctx, minePublisher, sub.ID); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	feed, err = pub.Feed(ctx, FeedFilter{}, "", 50)
	if err != nil || len(feed.Items) != 0 {
		t.Fatalf("after withdrawal: %d items err=%v", len(feed.Items), err)
	}
}

type stubScorer struct{}

func (stubScorer) Tier0(context.Context, string) (newsmoderate.Tier0Verdict, error) {
	return newsmoderate.Tier0Verdict{Decision: model.Tier0Allow}, nil
}

func (stubScorer) Score(context.Context, string) (newsmoderate.ScoreVerdict, error) {
	score := float32(0)
	return newsmoderate.ScoreVerdict{Score: &score, Channel: "stub"}, nil
}
