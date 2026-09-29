package service

import (
	"context"

	"api/internal/platform/news/model"

	"gorm.io/gorm"
)

// ReleasePendingImports publishes every pending row an importer wrote for one
// source and records one decision per row it actually moved, the same audit
// shape Decide writes. It exists for sources the user ruled need no review:
// 批评 on 2026-09-05 and 月幕 on 2026-09-29. Native submissions are left alone
// because a publisher's pending row is still an editable draft.
func ReleasePendingImports(ctx context.Context, db *gorm.DB, sourceKey string, actorUID int64, reason string) (int, error) {
	const stmt = `
		WITH moved AS (
			UPDATE news_item SET status = ?, updated_at = now()
			WHERE source_key = ? AND status = ? AND dead_at IS NULL
			  AND NOT starts_with(external_id, ?)
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
