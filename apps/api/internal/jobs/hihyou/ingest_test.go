package hihyou

import (
	"context"
	"path"
	"testing"
	"time"

	"api/internal/platform/news/model"
	"api/internal/platform/news/newstest"
	"api/internal/testsupport/dbtest"
	"api/pkg/imageclient"

	"gorm.io/gorm"
)

// healthy is four items under two sections. The third heading carries only a
// picture, which is real upstream shape and must be dropped per item rather than
// stored as a row with an empty preview.
func healthy(cv int64) *Article {
	a := article("【Gal周报200期】x",
		text(17, true, "新闻日期：2026年8月1日~2026年8月8日"),
		text(17, false, "新作资讯"),
		text(17, true, "1.《A》情报公开"), text(17, false, body),
		picture("http://i0.hdslb.com/bfs/new_dyn/a.png"),
		text(17, true, "2.《B》发售日决定"), text(17, false, body),
		text(17, false, "汉化情报"),
		text(17, true, "3.《C》汉化发布"), text(17, false, body),
		text(17, true, "4.《D》图集"), picture("//i0.hdslb.com/bfs/article/d.png"),
	)
	a.Data.ID = cv
	a.Data.PublishTime = 1786257779
	return a
}

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, release, ok := newstest.Open()
	if !ok {
		dbtest.Skipf(t, "news test database unavailable")
	}
	t.Cleanup(release)
	if err := newstest.Truncate(db); err != nil {
		t.Fatal(err)
	}
	if err := (&writer{db: db}).seedSource(context.Background()); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestSeedSourceRow(t *testing.T) {
	db := openTestDB(t)
	var src model.NewsSource
	if err := db.Where("key = ?", model.SourceKeyHihyou).Take(&src).Error; err != nil {
		t.Fatal(err)
	}
	// 「注明出处就行」 is the whole permission: a row published without the
	// attribution and the column link breaks the only condition she set.
	if src.Attribution == "" || src.ColumnURL == "" || src.PublisherUID != 115235 {
		t.Errorf("source row = %+v", src)
	}
}

// A row that already exists with the right text is the case 04c actually meets:
// the whole 04b backfill ran with --no-images. If pictures only ever arrive on
// create or on a text change, that run leaves 4,425 rows without a banner
// forever and reports success while uploading nothing.
func TestPicturesArriveForRowsWhoseTextDidNotChange(t *testing.T) {
	db := openTestDB(t)
	// Three pictures under one heading: a banner plus a gallery, which is the
	// shape that makes the two storage paths distinguishable.
	a := article("【Gal周报202期】x",
		text(17, false, "新作资讯"),
		text(17, true, "1.《A》情报公开"), text(17, false, body),
		picture("http://i0.hdslb.com/bfs/new_dyn/a.png"),
		picture("//i0.hdslb.com/bfs/article/b.png"),
		picture("https://i0.hdslb.com/bfs/article/c.png"),
		text(17, true, "2.《B》发售日决定"), text(17, false, body),
		text(17, true, "3.《C》汉化发布"), text(17, false, body),
	)
	a.Data.ID = 1003
	a.Data.PublishTime = 1786257779
	seg := Segment(a)
	it := seg.Items[0]
	if len(it.Pictures) != 3 {
		t.Fatalf("fixture yields %d pictures", len(it.Pictures))
	}
	published := time.Unix(1786257779, 0).UTC()
	ctx := context.Background()

	dry := &writer{db: db, opts: Opts{Apply: true}, uploaded: map[string]string{}}
	if err := dry.applyItem(ctx, 1003, published, it, &stats{}); err != nil {
		t.Fatal(err)
	}
	var row model.NewsItem
	if err := db.Where("external_id = ?", ExternalID(1003, it.Ordinal)).Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.BannerHash != "" {
		t.Fatalf("--no-images stored a banner %q", row.BannerHash)
	}

	// A pre-seeded upload cache stands in for the image host: picture() consults
	// it before the client, so nothing here reaches the network.
	hashes := map[string]string{}
	for _, u := range it.Pictures {
		hashes[u] = "hash-" + path.Base(u)
	}
	st := &stats{}
	live := &writer{db: db, opts: Opts{Apply: true}, uploaded: hashes,
		images: &imageclient.Client{}}
	if err := live.applyItem(ctx, 1003, published, it, st); err != nil {
		t.Fatal(err)
	}
	if st.unchanged != 1 || st.updated != 0 {
		t.Fatalf("unchanged=%d updated=%d — the text must not have changed", st.unchanged, st.updated)
	}
	if err := db.Where("external_id = ?", ExternalID(1003, it.Ordinal)).Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.BannerHash != hashes[it.Pictures[0]] {
		t.Errorf("banner_hash = %q, want the first picture's hash", row.BannerHash)
	}
	var n int64
	db.Model(&model.NewsItemImage{}).Where("item_id = ?", row.ID).Count(&n)
	if want := int64(len(it.Pictures) - 1); n != want {
		t.Errorf("stored %d gallery rows, want %d", n, want)
	}

	// And a third run must not fetch them again: origin URLs already on disk are
	// what decides the work, so an empty cache would fail on a network attempt.
	blank := &writer{db: db, opts: Opts{Apply: true}, uploaded: map[string]string{},
		images: &imageclient.Client{}}
	if err := blank.applyItem(ctx, 1003, published, it, &stats{}); err != nil {
		t.Fatal(err)
	}
	db.Model(&model.NewsItemImage{}).Where("item_id = ?", row.ID).Count(&n)
	if want := int64(len(it.Pictures) - 1); n != want {
		t.Errorf("after re-run: %d gallery rows, want %d", n, want)
	}
}

// warm is what makes the 8,080-picture pass concurrent, so it is also what
// would re-download the whole corpus on a second run. Its first job is to seed
// the cache from what is already stored: every URL resolved here is a fetch
// that does not happen, and the writer holds no image client, so a fetch would
// fail the test rather than quietly succeed.
func TestWarmResolvesStoredPicturesWithoutFetching(t *testing.T) {
	db := openTestDB(t)
	a := article("【Gal周报203期】x",
		text(17, false, "新作资讯"),
		text(17, true, "1.《A》情报公开"), text(17, false, body),
		picture("https://i0.hdslb.com/bfs/article/w1.png"),
		picture("https://i0.hdslb.com/bfs/article/w2.png"),
		text(17, true, "2.《B》发售日决定"), text(17, false, body),
		text(17, true, "3.《C》汉化发布"), text(17, false, body),
	)
	a.Data.ID = 1004
	a.Data.PublishTime = 1786257779
	it := Segment(a).Items[0]
	ctx := context.Background()

	seeded := map[string]string{it.Pictures[0]: "hash-w1", it.Pictures[1]: "hash-w2"}
	first := &writer{db: db, opts: Opts{Apply: true}, uploaded: seeded,
		failed: map[string]bool{}, images: &imageclient.Client{}}
	if err := first.applyItem(ctx, 1004, time.Unix(1786257779, 0).UTC(), it, &stats{}); err != nil {
		t.Fatal(err)
	}

	second := &writer{db: db, opts: Opts{Apply: true, Concurrency: 4},
		uploaded: map[string]string{}, failed: map[string]bool{}, images: &imageclient.Client{}}
	st := &stats{}
	second.warm(ctx, it.Pictures, st)
	if st.imagesUp != 0 || st.imagesFail != 0 {
		t.Errorf("warm fetched %d / failed %d — everything was already stored", st.imagesUp, st.imagesFail)
	}
	for _, u := range it.Pictures {
		if second.uploaded[u] != seeded[u] {
			t.Errorf("%s resolved to %q, want %q", u, second.uploaded[u], seeded[u])
		}
	}
}
