package ymgalnews

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"api/internal/platform/news/model"
	"api/internal/platform/news/newstest"
	"api/pkg/config"

	"gorm.io/gorm"
)

var testDB *gorm.DB

func TestMain(m *testing.M) {
	db, release, ok := newstest.Open()
	if !ok {
		os.Exit(0)
	}
	testDB = db
	code := m.Run()
	release()
	os.Exit(code)
}

func newTestWriter(t *testing.T, apply bool) (*writer, *stats) {
	t.Helper()
	if err := newstest.Truncate(testDB); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	if err := testDB.Exec(`
		INSERT INTO news_source (key, display_name, homepage_url, attribution, publisher_uid, column_url, active)
		VALUES ('ymgal', '月幕 Galgame', 'https://www.ymgal.games', 'attr', 114748, '', true)
		ON CONFLICT (key) DO NOTHING`).Error; err != nil {
		t.Fatalf("seed source: %v", err)
	}
	// NoImages keeps the test off the image service entirely: this file is about
	// the upsert rules, and a banner upload would make it an integration test of
	// something else.
	w, err := newWriter(&config.Config{}, testDB, Opts{Apply: apply, NoImages: true})
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}
	return w, &stats{}
}

func topic(id, title, intro string) Topic {
	return Topic{
		TopicID:       id,
		Title:         title,
		Introduction:  intro,
		TopicURL:      "https://www.ymgal.games/co/article/" + id,
		PublishTime:   "2026-08-08 09:36:31",
		TopicCategory: "资讯",
		CreateAt:      "新月酱",
	}
}

func row(t *testing.T, extID string) model.NewsItem {
	t.Helper()
	var out model.NewsItem
	if err := testDB.Where("external_id = ?", extID).Take(&out).Error; err != nil {
		t.Fatalf("read %s: %v", extID, err)
	}
	return out
}

// TestPublishTimeIsShanghai is the only check that can catch the missing zone:
// the upstream sends "2026-08-08 09:36:31" with no offset, so a parser that
// assumes UTC produces a valid time that is simply eight hours wrong, and the
// feed order and ETag go wrong with it.
func TestPublishTimeIsShanghai(t *testing.T) {
	got, err := topic("1", "t", "i").PublishedAt()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := time.Date(2026, 8, 8, 1, 36, 31, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("publishTime parsed to %s, want %s (UTC+8 source)", got, want)
	}
}

func TestTruncatePreviewCountsRunes(t *testing.T) {
	at := strings.Repeat("字", model.PreviewMaxRunes)
	if got, cut := truncatePreview(at); cut || len([]rune(got)) != model.PreviewMaxRunes {
		t.Errorf("exactly %d runes must pass untouched, got cut=%v len=%d", model.PreviewMaxRunes, cut, len([]rune(got)))
	}
	over := strings.Repeat("字", model.PreviewMaxRunes+50)
	got, cut := truncatePreview(over)
	if !cut {
		t.Error("an over-length preview must report that it was truncated")
	}
	if n := len([]rune(got)); n != model.PreviewMaxRunes {
		t.Errorf("truncated to %d runes, want %d", n, model.PreviewMaxRunes)
	}
	// Bytes, not runes, would cut a CJK character into invalid UTF-8.
	if !strings.HasSuffix(got, "字") {
		t.Error("truncation split a multi-byte character")
	}
}

func TestNewItemLandsPending(t *testing.T) {
	w, st := newTestWriter(t, true)
	if _, err := w.apply(context.Background(), LaneNews, topic("100", "hello", "intro"), st); err != nil {
		t.Fatalf("apply: %v", err)
	}
	r := row(t, "100")
	if r.Status != model.StatusPending {
		t.Errorf("new item status = %d, want pending (%d) — publishing is the run's standing release, which writes the decision row",
			r.Status, model.StatusPending)
	}
	if r.Lane != LaneNews || r.UpstreamCategory != "资讯" {
		t.Errorf("lane/category = %q/%q", r.Lane, r.UpstreamCategory)
	}
	if r.SourceURL == "" {
		t.Error("source_url is the only route to the full article and must never be empty")
	}
}

// TestUnchangedDoesNotTouchUpdatedAt: the poll runs every ten minutes. If an
// unchanged row bumped updated_at, every row in the table would look freshly
// modified all day and the "content actually changed" signal would be gone.
func TestUnchangedDoesNotTouchUpdatedAt(t *testing.T) {
	w, st := newTestWriter(t, true)
	ctx := context.Background()
	tp := topic("200", "hello", "intro")
	if _, err := w.apply(ctx, LaneNews, tp, st); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	before := row(t, "200")

	time.Sleep(10 * time.Millisecond)
	out, err := w.apply(ctx, LaneNews, tp, st)
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if out != outcomeUnchanged {
		t.Fatalf("re-applying an identical topic returned %v, want unchanged", out)
	}
	after := row(t, "200")
	if !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("updated_at moved on an unchanged row: %s -> %s", before.UpdatedAt, after.UpdatedAt)
	}
	if !after.LastSeenAt.After(before.LastSeenAt) {
		t.Errorf("last_seen_at did not advance: %s -> %s", before.LastSeenAt, after.LastSeenAt)
	}
}

// TestModerationDecisionsSurviveIngestion is the status-pollution guard: the
// importer may move a published row back to the queue when its text changes, but
// it must never overturn a human's rejection by re-seeing the article upstream.
func TestModerationDecisionsSurviveIngestion(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name     string
		start    int16
		want     int16
		newTitle string
	}{
		{"published + edited => back to the queue", model.StatusPublished, model.StatusPending, "edited"},
		{"rejected + edited => stays rejected", model.StatusRejected, model.StatusRejected, "edited"},
		{"withdrawn + edited => stays withdrawn", model.StatusWithdrawn, model.StatusWithdrawn, "edited"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, st := newTestWriter(t, true)
			if _, err := w.apply(ctx, LaneNews, topic("300", "original", "intro"), st); err != nil {
				t.Fatalf("seed: %v", err)
			}
			if err := testDB.Exec(`UPDATE news_item SET status = ? WHERE external_id = '300'`, tc.start).Error; err != nil {
				t.Fatalf("set status: %v", err)
			}
			if _, err := w.apply(ctx, LaneNews, topic("300", tc.newTitle, "intro"), st); err != nil {
				t.Fatalf("apply: %v", err)
			}
			if got := row(t, "300").Status; got != tc.want {
				t.Errorf("status = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	w, st := newTestWriter(t, false)
	if _, err := w.apply(context.Background(), LaneNews, topic("400", "hello", "intro"), st); err != nil {
		t.Fatalf("apply: %v", err)
	}
	var n int64
	testDB.Model(&model.NewsItem{}).Count(&n)
	if n != 0 {
		t.Errorf("dry run wrote %d rows", n)
	}
	if st.wouldCreate != 1 {
		t.Errorf("would_create = %d, want 1", st.wouldCreate)
	}
}

// TestLivenessNeedsACompleteScan: a run that hit any upstream error must not
// conclude anything about absence. "We did not see it" is only evidence when the
// scan actually finished.
func TestLivenessNeedsACompleteScan(t *testing.T) {
	w, st := newTestWriter(t, true)
	ctx := context.Background()
	if _, err := w.apply(ctx, LaneNews, topic("500", "held", "intro"), st); err != nil {
		t.Fatalf("seed: %v", err)
	}
	oldest := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seen := map[string]struct{}{"999": {}}

	got, err := w.livenessCandidates(ctx, false, oldest, seen)
	if err != nil {
		t.Fatalf("incomplete: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("an incomplete scan produced %d dead candidates; it must produce none", len(got))
	}

	got, err = w.livenessCandidates(ctx, true, oldest, seen)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if len(got) != 1 || got[0] != "500" {
		t.Errorf("complete scan candidates = %v, want [500]", got)
	}

	// Reporting is not hiding: dead_at stays NULL until someone passes --mark-dead.
	if r := row(t, "500"); r.DeadAt != nil {
		t.Error("livenessCandidates hid a row; it must only report")
	}
}
