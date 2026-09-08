package service

import (
	"context"
	"os"
	"testing"
	"time"

	"api/internal/platform/news/model"
	"api/internal/platform/news/newstest"

	"gorm.io/gorm"
)

const testCDNBase = "https://image.example.test/image"

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

func newFixture(t *testing.T) *PublicService {
	t.Helper()
	if err := newstest.Truncate(testDB); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	if err := testDB.Exec(`
		INSERT INTO news_source (key, display_name, homepage_url, attribution, publisher_uid, column_url, active)
		VALUES ('ymgal', '月幕 Galgame', 'https://www.ymgal.games', 'attribution text', 114748, '', true)
		ON CONFLICT (key) DO NOTHING`).Error; err != nil {
		t.Fatalf("seed source: %v", err)
	}
	return NewPublicService(testDB, testCDNBase)
}

func insert(t *testing.T, extID string, status int16, published time.Time, dead bool) int64 {
	t.Helper()
	return insertLane(t, extID, model.LaneNews, status, published, dead)
}

func insertLane(t *testing.T, extID, lane string, status int16, published time.Time, dead bool) int64 {
	t.Helper()
	deadExpr := "NULL"
	if dead {
		deadExpr = "now()"
	}
	if err := testDB.Exec(`
		INSERT INTO news_item (source_key, lane, upstream_category, external_id, title, preview,
			source_url, banner_hash, banner_origin_url, published_at, status, dead_at)
		VALUES ('ymgal', ?, '资讯', ?, 'title', 'preview',
			'https://www.ymgal.games/a/`+extID+`', '', '', ?, ?, `+deadExpr+`)`,
		lane, extID, published, status).Error; err != nil {
		t.Fatalf("insert %s: %v", extID, err)
	}
	var id int64
	testDB.Raw(`SELECT id FROM news_item WHERE external_id = ?`, extID).Scan(&id)
	return id
}

// TestVisibilityContract: pending, rejected, withdrawn and upstream-dead rows
// are absent from the list and 404 on detail. status=3 and dead_at are separate
// causes with the same public consequence, and both must be honoured.
func TestVisibilityContract(t *testing.T) {
	svc := newFixture(t)
	base := time.Now().UTC().Truncate(time.Second)

	live := insert(t, "live", model.StatusPublished, base, false)
	hidden := map[string]int64{
		"pending":   insert(t, "pending", model.StatusPending, base.Add(-time.Minute), false),
		"rejected":  insert(t, "rejected", model.StatusRejected, base.Add(-2*time.Minute), false),
		"withdrawn": insert(t, "withdrawn", model.StatusWithdrawn, base.Add(-3*time.Minute), false),
		"dead":      insert(t, "dead", model.StatusPublished, base.Add(-4*time.Minute), true),
	}

	feed, err := svc.Feed(context.Background(), FeedFilter{}, "", 50)
	if err != nil {
		t.Fatalf("feed: %v", err)
	}
	if len(feed.Items) != 1 || feed.Items[0].ID != live {
		t.Fatalf("feed must contain only the live item, got %d items", len(feed.Items))
	}
	// Withdrawn is the one state that owes a distinguishable answer: the face is
	// public and credential-less, so mirrors exist, and a mirror that only sees
	// the item leave the list never learns its copy was pulled. 03-resources.md
	// obligation 3 makes that a 410, which needs an error 404 cannot carry.
	// dead_at is not a withdrawal -- it means the upstream item vanished on us --
	// so it stays a 404.
	want := map[string]error{
		"pending": ErrNotFound, "rejected": ErrNotFound,
		"withdrawn": ErrGone, "dead": ErrNotFound,
	}
	for name, id := range hidden {
		if _, err := svc.Item(context.Background(), id); err != want[name] {
			t.Errorf("detail of a %s item returned %v, want %v", name, err, want[name])
		}
	}
	if _, err := svc.Item(context.Background(), live); err != nil {
		t.Errorf("detail of the live item failed: %v", err)
	}
}

// TestAttributionAlwaysPresent: attribution was the ONLY condition one partner
// attached, and click-through was the other's. Both ride every item.
func TestAttributionAlwaysPresent(t *testing.T) {
	svc := newFixture(t)
	insert(t, "a", model.StatusPublished, time.Now().UTC(), false)

	feed, err := svc.Feed(context.Background(), FeedFilter{}, "", 50)
	if err != nil {
		t.Fatalf("feed: %v", err)
	}
	for _, it := range feed.Items {
		if it.Source.Attribution == "" || it.Source.DisplayName == "" || it.Source.HomepageURL == "" {
			t.Errorf("item %d carries an incomplete source block: %+v", it.ID, it.Source)
		}
		if it.SourceURL == "" {
			t.Errorf("item %d has no source_url — the click-through target is mandatory", it.ID)
		}
		if it.Source.PublisherUID == 0 {
			t.Errorf("item %d has no publisher uid", it.ID)
		}
	}
}

func TestKeysetPaging(t *testing.T) {
	svc := newFixture(t)
	base := time.Now().UTC().Truncate(time.Second)
	for i := range 5 {
		insert(t, string(rune('a'+i)), model.StatusPublished, base.Add(-time.Duration(i)*time.Minute), false)
	}

	seen := map[int64]bool{}
	cursor := ""
	for range 5 {
		page, err := svc.Feed(context.Background(), FeedFilter{}, cursor, 2)
		if err != nil {
			t.Fatalf("page: %v", err)
		}
		for _, it := range page.Items {
			if seen[it.ID] {
				t.Fatalf("item %d served twice across pages", it.ID)
			}
			seen[it.ID] = true
		}
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
	}
	if len(seen) != 5 {
		t.Errorf("keyset walk covered %d/5 items", len(seen))
	}
	if _, err := svc.Feed(context.Background(), FeedFilter{}, "not-a-cursor!!", 2); err != ErrBadCursor {
		t.Errorf("malformed cursor returned %v, want ErrBadCursor", err)
	}
}

func TestFeedMetaFeedsETag(t *testing.T) {
	svc := newFixture(t)
	f := FeedFilter{}
	insert(t, "e1", model.StatusPublished, time.Now().UTC(), false)
	c1, p1, i1, err := svc.FeedMeta(context.Background(), f)
	if err != nil {
		t.Fatalf("meta: %v", err)
	}
	before := FeedETag(f.PopulationKey(), c1, p1, i1)

	insert(t, "e2", model.StatusPublished, time.Now().UTC().Add(time.Minute), false)
	c2, p2, i2, _ := svc.FeedMeta(context.Background(), f)
	if after := FeedETag(f.PopulationKey(), c2, p2, i2); after == before {
		t.Errorf("ETag did not move after a new item: %s", after)
	}
}

func TestWorkFilterAndImages(t *testing.T) {
	svc := newFixture(t)
	id := insert(t, "w1", model.StatusPublished, time.Now().UTC(), false)
	other := insert(t, "w2", model.StatusPublished, time.Now().UTC().Add(-time.Minute), false)
	if err := testDB.Exec(
		`INSERT INTO news_item_work (item_id, work_id, confidence) VALUES (?, 42, 0)`, id).Error; err != nil {
		t.Fatalf("anchor: %v", err)
	}
	if err := testDB.Exec(`
		INSERT INTO news_item_image (item_id, image_hash, origin_url, position)
		VALUES (?, 'aabbccdd', 'https://i0.hdslb.com/x.jpg', 0)`, id).Error; err != nil {
		t.Fatalf("image: %v", err)
	}

	feed, err := svc.Feed(context.Background(), FeedFilter{WorkID: 42}, "", 50)
	if err != nil {
		t.Fatalf("feed: %v", err)
	}
	if len(feed.Items) != 1 || feed.Items[0].ID != id {
		t.Fatalf("work filter returned %d items, want only %d", len(feed.Items), id)
	}
	it := feed.Items[0]
	if len(it.WorkIDs) != 1 || it.WorkIDs[0] != 42 {
		t.Errorf("work_ids = %v, want [42]", it.WorkIDs)
	}
	if len(it.Images) != 1 || it.Images[0] != testCDNBase+"/aa/bb/aabbccdd.webp" {
		t.Errorf("images = %v", it.Images)
	}
	_ = other
}

// TestLaneFilterAndPopulationKey: news and column share one feed, so a consumer
// that wants only one of them must be able to say so — and the ETag population
// key must move with the lane, or a client switching lanes would be served the
// other lane's cached page.
func TestLaneFilterAndPopulationKey(t *testing.T) {
	svc := newFixture(t)
	base := time.Now().UTC().Truncate(time.Second)
	insertLane(t, "ln-news", model.LaneNews, model.StatusPublished, base, false)
	insertLane(t, "ln-col", model.LaneColumn, model.StatusPublished, base.Add(-time.Minute), false)

	all, err := svc.Feed(context.Background(), FeedFilter{}, "", 20)
	if err != nil {
		t.Fatalf("feed all: %v", err)
	}
	if len(all.Items) != 2 {
		t.Fatalf("unfiltered feed returned %d items, want both lanes", len(all.Items))
	}
	for _, it := range all.Items {
		if it.Lane == "" {
			t.Error("every item must state its lane")
		}
	}

	only, err := svc.Feed(context.Background(), FeedFilter{Lanes: []string{model.LaneColumn}}, "", 20)
	if err != nil {
		t.Fatalf("feed column: %v", err)
	}
	if len(only.Items) != 1 || only.Items[0].Lane != model.LaneColumn {
		t.Fatalf("lane filter returned %d items (%+v)", len(only.Items), only.Items)
	}

	a := FeedFilter{Lanes: []string{model.LaneNews}}.PopulationKey()
	b := FeedFilter{Lanes: []string{model.LaneColumn}}.PopulationKey()
	if a == b {
		t.Errorf("population key does not vary with lane (%q) — two lanes would share one ETag", a)
	}
}

func TestSourcesFace(t *testing.T) {
	svc := newFixture(t)
	data, err := svc.Sources(context.Background())
	if err != nil {
		t.Fatalf("sources: %v", err)
	}
	if len(data.Sources) == 0 {
		t.Fatal("sources face returned nothing")
	}
	for _, s := range data.Sources {
		if s.Attribution == "" {
			t.Errorf("source %s has no attribution text", s.Key)
		}
	}
}
