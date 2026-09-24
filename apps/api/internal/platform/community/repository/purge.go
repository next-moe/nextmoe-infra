package repository

import (
	"time"

	"api/internal/platform/community/model"

	"gorm.io/gorm"
)

// archiveTx keeps every row stmt touches, as it was before stmt, in
// community_purge_archive. stmt's RETURNING must yield that old row, which an
// UPDATE's own RETURNING does not: it returns the row already scrubbed.
func archiveTx(tx *gorm.DB, site string, authorID int64, step, stmt string, args ...any) (int64, error) {
	res := tx.Exec(`
		WITH touched AS (`+stmt+`)
		INSERT INTO community_purge_archive (site, author_id, step, data, created_at)
		SELECT ?, ?, ?, to_jsonb(touched), now() FROM touched`,
		append(args, site, authorID, step)...)
	return res.RowsAffected, res.Error
}

func archivedRows(table string) string {
	return `community_purge_archive AS a
		 CROSS JOIN LATERAL jsonb_populate_record(NULL::` + table + `, a.data) AS r
		 WHERE a.site = ? AND a.author_id = ? AND a.step = ? AND a.restored_at IS NULL`
}

func RestorePurgedPostsTx(tx *gorm.DB, site string, authorID int64) (int64, error) {
	res := tx.Exec(`
		UPDATE community_post AS p
		   SET status = r.status, content_raw = r.content_raw, content_html = r.content_html
		  FROM (SELECT DISTINCT ON (r.id) r.* FROM `+archivedRows("community_post")+`
		         ORDER BY r.id, a.id) AS r
		 WHERE p.id = r.id AND p.status = ?`,
		site, authorID, model.PurgeStepPost, model.PostStatusDeleted)
	return res.RowsAffected, res.Error
}

func ReinsertPurgedRowsTx(tx *gorm.DB, site string, authorID int64, step, table string) (int64, error) {
	res := tx.Exec(`
		INSERT INTO `+table+`
		SELECT r.* FROM `+archivedRows(table)+`
		 ORDER BY a.id DESC
		ON CONFLICT DO NOTHING`,
		site, authorID, step)
	return res.RowsAffected, res.Error
}

func RestoreNotificationActorsTx(tx *gorm.DB, site string, authorID int64) (int64, error) {
	res := tx.Exec(`
		UPDATE community_notification AS n
		   SET actor_id = r.actor_id, updated_at = now()
		  FROM (SELECT r.* FROM `+archivedRows("community_notification")+`) AS r
		 WHERE n.id = r.id AND n.actor_id IS NULL`,
		site, authorID, model.PurgeStepNotificationActor)
	return res.RowsAffected, res.Error
}

func RestoreEventRecipientsTx(tx *gorm.DB, site string, authorID int64) (int64, error) {
	res := tx.Exec(`
		UPDATE community_event AS ev
		   SET target_user_id = r.target_user_id, mention_user_ids = r.mention_user_ids
		  FROM (SELECT r.* FROM `+archivedRows("community_event")+`) AS r
		 WHERE ev.id = r.id`,
		site, authorID, model.PurgeStepEventRecipient)
	return res.RowsAffected, res.Error
}

func MarkPurgeRestoredTx(tx *gorm.DB, site string, authorID int64) (int64, error) {
	res := tx.Model(&model.CommunityPurgeArchive{}).
		Where("site = ? AND author_id = ? AND restored_at IS NULL", site, authorID).
		Update("restored_at", gorm.Expr("now()"))
	return res.RowsAffected, res.Error
}

func PrunePurgeArchive(db *gorm.DB, olderThan time.Duration) (int64, error) {
	res := db.Where("created_at < ?", time.Now().Add(-olderThan)).Delete(&model.CommunityPurgeArchive{})
	return res.RowsAffected, res.Error
}
