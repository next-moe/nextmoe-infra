package service

import (
	"context"

	"api/internal/platform/news/model"

	"gorm.io/gorm"
)

// PurgeAccount withdraws everything an erased account submitted and clears its
// summary and body, the same scrub community applies to an erased author's
// posts. The title stays so the audit trail still says what was pulled.
func PurgeAccount(ctx context.Context, db *gorm.DB, uid int64) (int, error) {
	const stmt = `
		WITH target AS (
			SELECT id, status FROM news_item
			WHERE submitter_uid = ? AND (status <> ? OR preview <> '' OR body <> '')
			FOR UPDATE
		), moved AS (
			UPDATE news_item i SET status = ?, preview = '', body = '', updated_at = now()
			FROM target t WHERE i.id = t.id
			RETURNING i.id, t.status AS from_status
		)
		INSERT INTO news_moderation_decision (item_id, actor_uid, from_status, to_status, reason)
		SELECT id, ?, from_status, ?, ? FROM moved WHERE from_status <> ?`
	res := db.WithContext(ctx).Exec(stmt,
		uid, model.StatusWithdrawn, model.StatusWithdrawn,
		model.SystemActorUID, model.StatusWithdrawn, reasonAccountErased, model.StatusWithdrawn)
	if res.Error != nil {
		return 0, res.Error
	}
	return int(res.RowsAffected), nil
}
