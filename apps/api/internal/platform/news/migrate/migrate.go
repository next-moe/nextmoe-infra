// Package migrate owns the kun_news schema: the AutoMigrate table list, the
// idempotent raw-SQL section (feed index, unique keys, the image FK, the
// preview-length CHECK) and the news_source seed. It is called by
// cmd/migrate-news and by the news integration tests, which provision their
// database with the exact production schema.
package migrate

import (
	"fmt"

	"api/internal/platform/accountpurge"
	"api/internal/platform/news/model"

	"gorm.io/gorm"
)

// Run applies the full kun_news schema. Idempotent: safe on every deploy and
// repeatedly against the same database.
//
// 2026-08-11 (wave 02) added news_item.lane NOT NULL with no default, to carry
// 月幕's news/column split. Adding a NOT NULL column without a default is only
// legal on an empty table; news_item held zero rows in every environment at that
// point (prod's kun_news was created 08-11 and ingestion had not started). A
// later lane-like column will need a default-then-backfill-then-drop-default
// sequence instead.
//
// 2026-08-11 (wave 03) added news_moderation_verdict and news_moderation_decision.
// Both are new tables, so the NOT NULL-without-default columns on them carry none
// of the constraint above. news_item itself is unchanged: "has this text been
// scored" is answered by comparing a fingerprint computed from the item against
// the verdict log, which needs no column on the item.
//
// 2026-09-29 opened /v2/me/news to every signed-in user under a new community
// source. news_item gained submitter_uid (NULL on every existing row: all of
// them were imported) and body (NOT NULL DEFAULT ”, so existing rows get the
// empty string, which is what they would have carried anyway). news_source
// gained auto_publish; the user had ruled 月幕 (2026-09-29) and 批评
// (2026-09-05) need no review, so the two existing rows are set true once, in
// the run that adds the column, and a later operator change survives
// redeploys. account_purge_cursor is the catalog process's erasure cursor for
// the submissions this database now holds.
func Run(db *gorm.DB) error {
	hadAutoPublish := db.Migrator().HasColumn(&model.NewsSource{}, "auto_publish")
	if err := db.AutoMigrate(
		&model.NewsSource{},
		&model.NewsItem{},
		&model.NewsItemImage{},
		&model.NewsItemWork{},
		&model.NewsModerationVerdict{},
		&model.NewsModerationDecision{},
		&accountpurge.Cursor{},
	); err != nil {
		return fmt.Errorf("news automigrate: %w", err)
	}
	if !hadAutoPublish {
		if err := db.Exec(`UPDATE news_source SET auto_publish = true WHERE key IN ?`,
			[]string{model.SourceKeyYmgal, model.SourceKeyHihyou}).Error; err != nil {
			return fmt.Errorf("news backfill auto_publish: %w", err)
		}
	}
	if err := rawSQL(db); err != nil {
		return err
	}
	return seedSources(db)
}

func rawSQL(db *gorm.DB) error {
	for _, s := range []struct{ name, stmt string }{
		// (source_key, external_id) is the ingestion identity: re-running an
		// importer must UPDATE the row it wrote last time, never append a second
		// copy of the same upstream article.
		//
		// lane is deliberately NOT part of this key. 月幕 serves news and column
		// from two endpoints but both return a field called topicId out of one
		// topic family, so the id space is assumed shared — under that assumption
		// an article that moves between the two lanes must stay ONE row. The
		// assumption is not provable from their docs, so the importer counts
		// lane_flip on every upsert that finds a different lane: a non-zero count
		// means the id spaces are actually separate and this key is wrong.
		{"news_item_src_ext", `
			CREATE UNIQUE INDEX IF NOT EXISTS news_item_src_ext
			    ON news_item (source_key, external_id)`},
		{"news_item_feed", `
			CREATE INDEX IF NOT EXISTS news_item_feed
			    ON news_item (published_at DESC, id DESC)`},
		{"news_item_image_uk", `
			CREATE UNIQUE INDEX IF NOT EXISTS news_item_image_uk
			    ON news_item_image (item_id, image_hash)`},
		// The source FK is declared here rather than through a GORM association:
		// news_item.id is an IDENTITY primary key, and GORM copies a referenced
		// PK's full type onto the FK column, which would make item_id an identity
		// column too (gorm-identity-pk-recipe).
		{"news_item_source_fk", `
			DO $$ BEGIN
			    ALTER TABLE news_item
			        ADD CONSTRAINT news_item_source_fk
			        FOREIGN KEY (source_key) REFERENCES news_source(key);
			EXCEPTION WHEN duplicate_object THEN NULL; END $$`},
		{"news_item_image_item_fk", `
			DO $$ BEGIN
			    ALTER TABLE news_item_image
			        ADD CONSTRAINT news_item_image_item_fk
			        FOREIGN KEY (item_id) REFERENCES news_item(id) ON DELETE CASCADE;
			EXCEPTION WHEN duplicate_object THEN NULL; END $$`},
		{"news_item_work_item_fk", `
			DO $$ BEGIN
			    ALTER TABLE news_item_work
			        ADD CONSTRAINT news_item_work_item_fk
			        FOREIGN KEY (item_id) REFERENCES news_item(id) ON DELETE CASCADE;
			EXCEPTION WHEN duplicate_object THEN NULL; END $$`},
		// Both moderation logs are append-only and are always read newest-first
		// for one item, so (item_id, id DESC) is the only access path either of
		// them needs. The verdict table's per-fingerprint attempt count rides the
		// same index: once item_id is fixed the handful of rows behind it is a
		// trivial scan, and a second index on content_fingerprint would earn
		// nothing.
		{"news_moderation_verdict_item", `
			CREATE INDEX IF NOT EXISTS news_moderation_verdict_item
			    ON news_moderation_verdict (item_id, id DESC)`},
		{"news_moderation_decision_item", `
			CREATE INDEX IF NOT EXISTS news_moderation_decision_item
			    ON news_moderation_decision (item_id, id DESC)`},
		{"news_moderation_verdict_item_fk", `
			DO $$ BEGIN
			    ALTER TABLE news_moderation_verdict
			        ADD CONSTRAINT news_moderation_verdict_item_fk
			        FOREIGN KEY (item_id) REFERENCES news_item(id) ON DELETE CASCADE;
			EXCEPTION WHEN duplicate_object THEN NULL; END $$`},
		{"news_moderation_decision_item_fk", `
			DO $$ BEGIN
			    ALTER TABLE news_moderation_decision
			        ADD CONSTRAINT news_moderation_decision_item_fk
			        FOREIGN KEY (item_id) REFERENCES news_item(id) ON DELETE CASCADE;
			EXCEPTION WHEN duplicate_object THEN NULL; END $$`},
		// 月幕 authorised the preview message and the banner, explicitly not the
		// article body. The importers truncate; this constraint is what makes an
		// importer bug fail loudly instead of quietly storing a full article.
		{"news_item_preview_len", fmt.Sprintf(`
			DO $$ BEGIN
			    ALTER TABLE news_item
			        ADD CONSTRAINT news_item_preview_len
			        CHECK (char_length(preview) <= %d);
			EXCEPTION WHEN duplicate_object THEN NULL; END $$`, model.PreviewMaxRunes)},
		// Same reasoning for the body: only the community source may carry one,
		// so no partner row can ever hold the article text neither of them granted.
		{"news_item_body_community", fmt.Sprintf(`
			DO $$ BEGIN
			    ALTER TABLE news_item
			        ADD CONSTRAINT news_item_body_community
			        CHECK (body = '' OR (source_key = '%s' AND char_length(body) <= %d));
			EXCEPTION WHEN duplicate_object THEN NULL; END $$`, model.SourceKeyCommunity, model.BodyMaxRunes)},
		{"news_item_submitter", `
			CREATE INDEX IF NOT EXISTS news_item_submitter
			    ON news_item (submitter_uid, id DESC) WHERE submitter_uid IS NOT NULL`},
	} {
		if err := db.Exec(s.stmt).Error; err != nil {
			return fmt.Errorf("news schema %s: %w", s.name, err)
		}
	}
	return nil
}

// seedSources inserts the partner rows the read face needs. ON CONFLICT DO
// NOTHING, so a row edited in place (attribution wording, column_url) survives
// every redeploy.
//
// 月幕 and the community source are seeded here. Galgame 批评's row lands with
// its backfill, which is where the column URLs it must carry are first known — a
// placeholder homepage_url would put a wrong link on every item published under
// her attribution, which is the one condition she asked for.
//
// The community row has publisher_uid 0: no account publishes it, every
// signed-in user may submit to it, and its items are owned by submitter_uid.
func seedSources(db *gorm.DB) error {
	const stmt = `
		INSERT INTO news_source (key, display_name, homepage_url, attribution, publisher_uid, column_url, active, auto_publish)
		VALUES (?, ?, ?, ?, ?, '', true, ?)
		ON CONFLICT (key) DO NOTHING`
	for _, src := range []struct {
		key, name, homepage, attribution string
		publisher                        int64
		autoPublish                      bool
	}{
		{model.SourceKeyYmgal, "月幕 Galgame", "https://www.ymgal.games",
			"本条情报转载自月幕 Galgame,点击标题可跳转至月幕原文", 114748, true},
		{model.SourceKeyCommunity, "NextMoe 用户投稿", "",
			"本条情报由 NextMoe 用户投稿,内容由投稿人负责", 0, false},
	} {
		if err := db.Exec(stmt, src.key, src.name, src.homepage, src.attribution, src.publisher, src.autoPublish).Error; err != nil {
			return fmt.Errorf("news seed source %s: %w", src.key, err)
		}
	}
	return nil
}
