package service

import (
	"context"

	"api/internal/platform/news/model"

	"gorm.io/gorm"
)

// ReleasePendingImports publishes every pending row an importer wrote for one
// source and records one decision per row it actually moved, the same audit
// shape Decide writes. It acts only while the source's auto_publish is on —
// the user ruled 批评 (2026-09-05) and 月幕 (2026-09-29) need no review, and
// turning the flag off is how an operator puts one back behind the console.
// Native submissions are left alone: they are published, or not, when written.
func ReleasePendingImports(ctx context.Context, db *gorm.DB, sourceKey string, actorUID int64, reason string) (int, error) {
	const stmt = `
		WITH moved AS (
			UPDATE news_item SET status = ?, updated_at = now()
			WHERE source_key = ? AND status = ? AND dead_at IS NULL
			  AND NOT starts_with(external_id, ?)
			  AND EXISTS (SELECT 1 FROM news_source s WHERE s.key = news_item.source_key AND s.auto_publish)
			RETURNING id
		)
		INSERT INTO news_moderation_decision (item_id, actor_uid, from_status, to_status, reason)
		SELECT id, ?, ?, ?, ? FROM moved`
	res := db.WithContext(ctx).Exec(stmt,
		model.StatusPublished, sourceKey, model.StatusPending, nativeExternalIDPrefix,
		actorUID, model.StatusPending, model.StatusPublished, reason)
	if res.Error != nil {
		return 0, res.Error
	}
	return int(res.RowsAffected), nil
}
