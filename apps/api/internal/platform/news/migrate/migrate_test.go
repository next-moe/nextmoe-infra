package migrate

import (
	"fmt"
	"os"
	"strings"
	"testing"

	suitelock "api/internal/platform/news/dbtest"
	"api/internal/platform/news/model"
	"api/internal/testsupport/dbtest"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDB *gorm.DB

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("news/migrate")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("news/migrate", "cannot connect to test database: %v", err)
	}
	sqlDB, _ := db.DB()
	release := suitelock.AcquireSuiteLock(sqlDB)

	if err := Run(db); err != nil {
		release()
		dbtest.SkipMainf("news/migrate", "news migration failed: %v", err)
	}
	// Second run is the idempotency probe: every deploy reruns this migration.
	if err := Run(db); err != nil {
		release()
		fmt.Fprintf(os.Stderr, "news migration is NOT idempotent: %v\n", err)
		os.Exit(1)
	}

	testDB = db
	code := m.Run()
	release()
	os.Exit(code)
}

// TestBodyIsCommunityOnly is the physical form of 月幕's condition: preview
// message and banner, "不要文章的全部内容". Since 2026-09-29 news_item has a body
// column for original community submissions, so the promise moved from "the
// column does not exist" to "no partner row can hold one" — enforced by the
// database, where no future importer or refactor can route around it.
func TestBodyIsCommunityOnly(t *testing.T) {
	seedSource(t)
	if err := insertItem(testDB, "body-ymgal", "preview", model.StatusPending); err != nil {
		t.Fatal(err)
	}
	if err := testDB.Exec(`UPDATE news_item SET body = 'full article' WHERE external_id = 'body-ymgal'`).Error; err == nil {
		t.Error("a partner row accepted a body; the CHECK constraint is missing")
	}
	insertCommunity := func(extID, body string) error {
		return testDB.Exec(`
			INSERT INTO news_item (source_key, lane, upstream_category, external_id, title, preview,
				source_url, banner_origin_url, published_at, status, body)
			VALUES (?, ?, '', ?, 't', 'p', '', '', now(), 0, ?)`,
			model.SourceKeyCommunity, model.LaneNews, extID, body).Error
	}
	if err := insertCommunity("body-at", strings.Repeat("字", model.BodyMaxRunes)); err != nil {
		t.Errorf("a community body of exactly %d runes must be accepted: %v", model.BodyMaxRunes, err)
	}
	if err := insertCommunity("body-over", strings.Repeat("字", model.BodyMaxRunes+1)); err == nil {
		t.Errorf("a community body of %d runes was accepted", model.BodyMaxRunes+1)
	}
	testDB.Exec(`DELETE FROM news_item WHERE external_id IN ('body-ymgal','body-at','body-over')`)
}

// The backfill must run once: an operator who later turns a source's
// auto-publish off must not have every redeploy turn it back on.
func TestAutoPublishBackfillRunsOnce(t *testing.T) {
	autoPublish := func(key string) bool {
		var v bool
		testDB.Raw(`SELECT auto_publish FROM news_source WHERE key = ?`, key).Scan(&v)
		return v
	}
	if err := testDB.Exec(`ALTER TABLE news_source DROP COLUMN auto_publish`).Error; err != nil {
		t.Fatal(err)
	}
	if err := Run(testDB); err != nil {
		t.Fatal(err)
	}
	if !autoPublish(model.SourceKeyYmgal) || autoPublish(model.SourceKeyCommunity) {
		t.Fatalf("after the column is added: ymgal=%v community=%v, want true/false",
			autoPublish(model.SourceKeyYmgal), autoPublish(model.SourceKeyCommunity))
	}
	testDB.Exec(`UPDATE news_source SET auto_publish = false WHERE key = ?`, model.SourceKeyYmgal)
	if err := Run(testDB); err != nil {
		t.Fatal(err)
	}
	if autoPublish(model.SourceKeyYmgal) {
		t.Error("a redeploy turned ymgal's auto-publish back on")
	}
	testDB.Exec(`UPDATE news_source SET auto_publish = true WHERE key = ?`, model.SourceKeyYmgal)
}

// TestPreviewLengthEnforced pins the ceiling in the DATABASE, not only in the
// importer: an extraction bug that stopped truncating would otherwise store a
// whole article and nothing would complain.
func TestPreviewLengthEnforced(t *testing.T) {
	seedSource(t)
	over := strings.Repeat("字", model.PreviewMaxRunes+1)
	err := insertItem(testDB, "len-over", over, model.StatusPublished)
	if err == nil {
		t.Fatalf("preview of %d runes was accepted; the CHECK constraint is missing", model.PreviewMaxRunes+1)
	}
	at := strings.Repeat("字", model.PreviewMaxRunes)
	if err := insertItem(testDB, "len-at", at, model.StatusPublished); err != nil {
		t.Fatalf("preview of exactly %d runes must be accepted: %v", model.PreviewMaxRunes, err)
	}
	testDB.Exec(`DELETE FROM news_item WHERE external_id IN ('len-over','len-at')`)
}

// TestColumnDiscipline asserts intent columns carry NO DB default (the GORM
// zero-value trap: a `default:` tag makes GORM skip the zero value on insert and
// let the database decide instead).
func TestColumnDiscipline(t *testing.T) {
	noDefault := []struct{ table, column string }{
		{"news_source", "display_name"}, {"news_source", "homepage_url"},
		{"news_source", "attribution"}, {"news_source", "publisher_uid"},
		// active=false is a meaningful value: a `DEFAULT true` would make it
		// impossible to deactivate a source through a GORM struct write.
		{"news_source", "active"},
		{"news_item", "source_key"}, {"news_item", "external_id"}, {"news_item", "title"},
		{"news_item", "preview"}, {"news_item", "source_url"}, {"news_item", "published_at"},
		{"news_item", "status"},
		// lane decides which upstream section an item came from and the read face
		// filters on it; a DB default would let a row that never had a lane assigned
		// silently answer a lane query.
		{"news_item", "lane"}, {"news_item", "upstream_category"},
		{"news_item", "banner_origin_url"},
		{"news_item_work", "confidence"},
		// degraded=false is the meaningful value here — it is what makes a verdict
		// count as scored. A DB default would let a row that never recorded whether
		// the model actually spoke pass as a clean judgement.
		{"news_moderation_verdict", "degraded"},
		{"news_moderation_verdict", "item_id"},
		{"news_moderation_verdict", "content_fingerprint"},
		{"news_moderation_verdict", "tier0_decision"},
		{"news_moderation_decision", "item_id"}, {"news_moderation_decision", "actor_uid"},
		{"news_moderation_decision", "from_status"}, {"news_moderation_decision", "to_status"},
	}
	for _, c := range noDefault {
		if def := columnDefault(t, c.table, c.column); def != "" {
			t.Errorf("%s.%s must have NO default, got %q", c.table, c.column, def)
		}
	}
	// dead_at must stay NULLABLE: NULL means "still upstream", and it is a
	// different fact from status=withdrawn ("we pulled it").
	if nullable := columnNullable(t, "news_item", "dead_at"); nullable != "YES" {
		t.Errorf("news_item.dead_at must be nullable, got is_nullable=%q", nullable)
	}
	// ai_flagged must stay NULLABLE. NULL is "the model never spoke"; false is
	// "the model said it is clean". A NOT NULL here would merge the two, which is
	// precisely how a degraded verdict becomes an accidental approval.
	if nullable := columnNullable(t, "news_moderation_verdict", "ai_flagged"); nullable != "YES" {
		t.Errorf("news_moderation_verdict.ai_flagged must be nullable, got is_nullable=%q", nullable)
	}
}

func TestIndexesAndConstraints(t *testing.T) {
	for _, want := range []struct{ table, index, contains string }{
		{"news_item", "news_item_src_ext", "UNIQUE"},
		{"news_item", "news_item_feed", "published_at DESC"},
		{"news_item_image", "news_item_image_uk", "UNIQUE"},
	} {
		var def string
		if err := testDB.Raw(
			`SELECT indexdef FROM pg_indexes WHERE tablename = ? AND indexname = ?`,
			want.table, want.index,
		).Scan(&def).Error; err != nil {
			t.Fatalf("read indexdef: %v", err)
		}
		if def == "" {
			t.Errorf("index %s on %s is missing", want.index, want.table)
			continue
		}
		if !strings.Contains(def, want.contains) {
			t.Errorf("index %s: %q does not contain %q", want.index, def, want.contains)
		}
	}
}

// TestImageCascade pins ON DELETE CASCADE: a deleted item must not leave image
// rows whose only purpose is to keep bytes alive in the refping sweep.
func TestImageCascade(t *testing.T) {
	seedSource(t)
	if err := insertItem(testDB, "cascade-1", "preview", model.StatusPublished); err != nil {
		t.Fatalf("insert item: %v", err)
	}
	var id int64
	testDB.Raw(`SELECT id FROM news_item WHERE external_id = 'cascade-1'`).Scan(&id)
	if err := testDB.Exec(
		`INSERT INTO news_item_image (item_id, image_hash, origin_url, position) VALUES (?, 'h1', 'https://x/1.jpg', 0)`, id,
	).Error; err != nil {
		t.Fatalf("insert image: %v", err)
	}
	if err := testDB.Exec(`DELETE FROM news_item WHERE id = ?`, id).Error; err != nil {
		t.Fatalf("delete item: %v", err)
	}
	var n int64
	testDB.Raw(`SELECT count(*) FROM news_item_image WHERE item_id = ?`, id).Scan(&n)
	if n != 0 {
		t.Errorf("news_item_image rows survived the item delete: %d", n)
	}
}

func TestSeedIsIdempotentAndComplete(t *testing.T) {
	var row struct {
		Attribution  string
		HomepageURL  string `gorm:"column:homepage_url"`
		PublisherUID int64  `gorm:"column:publisher_uid"`
	}
	if err := testDB.Raw(
		`SELECT attribution, homepage_url, publisher_uid FROM news_source WHERE key = ?`,
		model.SourceKeyYmgal,
	).Scan(&row).Error; err != nil {
		t.Fatalf("read seed: %v", err)
	}
	if row.Attribution == "" {
		t.Error("the ymgal source must carry attribution text — it is what every item renders")
	}
	if row.HomepageURL == "" || row.PublisherUID == 0 {
		t.Errorf("incomplete ymgal seed: homepage=%q uid=%d", row.HomepageURL, row.PublisherUID)
	}
	var n int64
	testDB.Raw(`SELECT count(*) FROM news_source WHERE key = ?`, model.SourceKeyYmgal).Scan(&n)
	if n != 1 {
		t.Errorf("seed ran twice: %d ymgal rows", n)
	}
}

func seedSource(t *testing.T) {
	t.Helper()
	if err := testDB.Exec(`
		INSERT INTO news_source (key, display_name, homepage_url, attribution, publisher_uid, column_url, active)
		VALUES (?, 'x', 'https://x', 'attr', 1, '', true) ON CONFLICT (key) DO NOTHING`,
		model.SourceKeyYmgal).Error; err != nil {
		t.Fatalf("seed source: %v", err)
	}
}

func insertItem(db *gorm.DB, extID, preview string, status int16) error {
	return db.Exec(`
		INSERT INTO news_item (source_key, lane, upstream_category, external_id, title, preview,
			source_url, banner_origin_url, published_at, status)
		VALUES (?, ?, '资讯', ?, 't', ?, 'https://x/1', '', now(), ?)`,
		model.SourceKeyYmgal, model.LaneNews, extID, preview, status).Error
}

func columnDefault(t *testing.T, table, column string) string {
	t.Helper()
	var def *string
	if err := testDB.Raw(
		`SELECT column_default FROM information_schema.columns WHERE table_name = ? AND column_name = ?`,
		table, column,
	).Scan(&def).Error; err != nil {
		t.Fatalf("read column_default %s.%s: %v", table, column, err)
	}
	if def == nil {
		return ""
	}
	return *def
}

func columnNullable(t *testing.T, table, column string) string {
	t.Helper()
	var v string
	if err := testDB.Raw(
		`SELECT is_nullable FROM information_schema.columns WHERE table_name = ? AND column_name = ?`,
		table, column,
	).Scan(&v).Error; err != nil {
		t.Fatalf("read is_nullable %s.%s: %v", table, column, err)
	}
	return v
}
