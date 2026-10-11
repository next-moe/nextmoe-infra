package repository

import (
	"api/internal/platform/community/model"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func NoticeFoldKey(topic, ref string) string { return "notice:" + topic + ":" + ref }

func UpsertNoticeNotificationTx(tx *gorm.DB, site string, userID int64, foldKey string, payload datatypes.JSON) error {
	seq, err := nextNotificationSeq(tx)
	if err != nil {
		return err
	}
	return tx.Exec(`
		INSERT INTO community_notification (
			site, user_id, kind, thread_id, anchor_kind, anchor_id,
			actor_count, item_count, fold_key, notice, seq, created_at, updated_at
		) VALUES (?, ?, ?, 0, 0, '', 0, 1, ?, ?, ?, now(), now())
		ON CONFLICT (site, user_id, fold_key) WHERE read_at IS NULL AND fold_key IS NOT NULL
		DO UPDATE SET
			notice = EXCLUDED.notice,
			seq = EXCLUDED.seq,
			updated_at = now(),
			item_count = community_notification.item_count + 1`,
		site, userID, model.NotificationKindNotice, foldKey, payload, seq,
	).Error
}
