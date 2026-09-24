package repository

import (
	"fmt"
	"time"

	"api/internal/platform/community/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func EnqueueEventTx(tx *gorm.DB, ev *model.CommunityEvent) error {
	return tx.Create(ev).Error
}

func LockPendingEventsTx(tx *gorm.DB, limit int) ([]model.CommunityEvent, error) {
	var rows []model.CommunityEvent
	err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
		Where("processed_at IS NULL AND attempt_after <= now()").
		Order("id ASC").Limit(limit).Find(&rows).Error
	return rows, err
}

func MarkEventProcessedTx(tx *gorm.DB, id int64) error {
	return tx.Model(&model.CommunityEvent{}).Where("id = ?", id).
		Update("processed_at", gorm.Expr("now()")).Error
}

func ParkEventTx(tx *gorm.DB, id int64, attempts int) error {
	mins := 1 << attempts
	if attempts >= 6 || mins > 60 {
		mins = 60
	}
	return tx.Model(&model.CommunityEvent{}).Where("id = ?", id).Updates(map[string]any{
		"attempts":      attempts + 1,
		"attempt_after": time.Now().Add(time.Duration(mins) * time.Minute),
	}).Error
}

func HasPendingReviewTx(tx *gorm.DB, postID int64) (bool, error) {
	var n int64
	err := tx.Model(&model.CommunityReviewItem{}).
		Where("post_id = ? AND status = ?", postID, model.ReviewStatusPending).
		Count(&n).Error
	return n > 0, err
}

func GetReactionTx(tx *gorm.DB, postID, userID int64, kind int16) (*model.CommunityReaction, error) {
	var row model.CommunityReaction
	err := tx.Where("post_id = ? AND user_id = ? AND kind = ?", postID, userID, kind).
		Take(&row).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func ListThreadUsersTx(tx *gorm.DB, threadID int64) ([]model.CommunityThreadUser, error) {
	var rows []model.CommunityThreadUser
	err := tx.Where("thread_id = ?", threadID).Find(&rows).Error
	return rows, err
}

func ListAnchorUsersForThreadTx(tx *gorm.DB, th *model.CommunityThread) ([]model.CommunityAnchorUser, error) {
	q := tx.Where("anchor_kind = ? AND anchor_id = ?", th.AnchorKind, th.AnchorID)
	if model.AnchorIsSiteLocal(th.AnchorKind) {
		q = q.Where("site = ?", th.Site)
	}
	var rows []model.CommunityAnchorUser
	err := q.Find(&rows).Error
	return rows, err
}

func nextNotificationSeq(tx *gorm.DB) (int64, error) {
	// Taken outside the dispatcher's advisory lock, a seq could commit behind a
	// larger one, and a feed reader already past the larger one skips the row.
	var seq int64
	err := tx.Raw("SELECT nextval('community_notification_seq')").Scan(&seq).Error
	return seq, err
}

func InsertNotificationTx(tx *gorm.DB, n *model.CommunityNotification) error {
	seq, err := nextNotificationSeq(tx)
	if err != nil {
		return err
	}
	n.Seq = seq
	return tx.Create(n).Error
}

// A parked post is delivered after the posts that followed it, so its event
// can reach a fold whose pointer is already past it: the fold widens backwards
// instead of moving its pointer (and its count range) back.
const foldNewer = `(EXCLUDED.post_number IS NULL OR community_notification.post_number IS NULL
	OR EXCLUDED.post_number >= community_notification.post_number)`

func UpsertFoldedNotificationTx(tx *gorm.DB, n *model.CommunityNotification, bumpItem bool) (*model.CommunityNotification, error) {
	seq, err := nextNotificationSeq(tx)
	if err != nil {
		return nil, err
	}
	n.Seq = seq
	itemBump := 0
	if bumpItem {
		itemBump = 1
	}
	var row model.CommunityNotification
	err = tx.Raw(`
		INSERT INTO community_notification (
			site, user_id, kind, thread_id, anchor_kind, anchor_id,
			post_id, post_number, first_post_number, since_at,
			actor_id, actor_count, item_count, fold_key, seq, created_at, updated_at
		) VALUES (
			?, ?, ?, ?, ?, ?,
			?, ?, ?, ?,
			?, ?, ?, ?, ?, now(), now()
		)
		ON CONFLICT (site, user_id, fold_key) WHERE read_at IS NULL AND fold_key IS NOT NULL
		DO UPDATE SET
			post_id = CASE WHEN `+foldNewer+` THEN EXCLUDED.post_id ELSE community_notification.post_id END,
			actor_id = CASE WHEN `+foldNewer+` THEN EXCLUDED.actor_id ELSE community_notification.actor_id END,
			post_number = GREATEST(community_notification.post_number, EXCLUDED.post_number),
			first_post_number = LEAST(community_notification.first_post_number, EXCLUDED.first_post_number),
			since_at = LEAST(community_notification.since_at, EXCLUDED.since_at),
			seq = EXCLUDED.seq,
			updated_at = now(),
			item_count = community_notification.item_count + ?
		RETURNING *`,
		n.Site, n.UserID, n.Kind, n.ThreadID, n.AnchorKind, n.AnchorID,
		n.PostID, n.PostNumber, n.FirstPostNumber, n.SinceAt,
		n.ActorID, n.ActorCount, n.ItemCount, n.FoldKey, n.Seq, itemBump,
	).Scan(&row).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func RecomputePostedFoldTx(tx *gorm.DB, id int64) error {
	return tx.Exec(`
		UPDATE community_notification AS n SET
			item_count = (
				SELECT COUNT(*) FROM community_post p
				 WHERE p.thread_id = n.thread_id
				   AND p.status = ?
				   AND p.post_number BETWEEN n.first_post_number AND n.post_number
				   AND p.author_id <> n.user_id),
			actor_count = (
				SELECT COUNT(DISTINCT p.author_id) FROM community_post p
				 WHERE p.thread_id = n.thread_id
				   AND p.status = ?
				   AND p.post_number BETWEEN n.first_post_number AND n.post_number
				   AND p.author_id <> n.user_id)
		 WHERE n.id = ?`,
		model.PostStatusVisible, model.PostStatusVisible, id).Error
}

func RecomputeLikedFoldTx(tx *gorm.DB, id int64) error {
	return tx.Exec(`
		UPDATE community_notification AS n SET
			item_count = (
				SELECT COUNT(*) FROM community_reaction r
				 WHERE r.post_id = n.post_id AND r.kind = ?
				   AND r.created_at >= n.since_at
				   AND r.user_id <> n.user_id),
			actor_count = (
				SELECT COUNT(*) FROM community_reaction r
				 WHERE r.post_id = n.post_id AND r.kind = ?
				   AND r.created_at >= n.since_at
				   AND r.user_id <> n.user_id)
		 WHERE n.id = ?`,
		model.ReactionKindLike, model.ReactionKindLike, id).Error
}

func MarkThreadNotificationsReadTx(tx *gorm.DB, userID, threadID int64, lastRead int32) error {
	return tx.Model(&model.CommunityNotification{}).
		Where("user_id = ? AND thread_id = ? AND read_at IS NULL", userID, threadID).
		Where("kind IN ?", []int16{
			model.NotificationKindReplied, model.NotificationKindMentioned,
			model.NotificationKindPosted, model.NotificationKindThreadCreated,
		}).
		Where("post_number <= ?", lastRead).
		Update("read_at", gorm.Expr("now()")).Error
}

func ListNotifications(db *gorm.DB, site string, userID, beforeSeq int64, unreadOnly bool, limit int) ([]model.CommunityNotification, error) {
	q := db.Model(&model.CommunityNotification{}).Where("site = ? AND user_id = ?", site, userID)
	if unreadOnly {
		q = q.Where("read_at IS NULL")
	}
	if beforeSeq > 0 {
		q = q.Where("seq < ?", beforeSeq)
	}
	var rows []model.CommunityNotification
	err := q.Order("seq DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

func CountUnreadNotifications(db *gorm.DB, site string, userID int64) (int64, error) {
	var n int64
	err := db.Model(&model.CommunityNotification{}).
		Where("site = ? AND user_id = ? AND read_at IS NULL", site, userID).
		Count(&n).Error
	return n, err
}

func MarkNotificationsReadByIDs(db *gorm.DB, site string, userID int64, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	res := db.Model(&model.CommunityNotification{}).
		Where("site = ? AND user_id = ? AND read_at IS NULL AND id IN ?", site, userID, ids).
		Update("read_at", gorm.Expr("now()"))
	return res.RowsAffected, res.Error
}

func MarkAllNotificationsRead(db *gorm.DB, site string, userID int64) (int64, error) {
	res := db.Model(&model.CommunityNotification{}).
		Where("site = ? AND user_id = ? AND read_at IS NULL", site, userID).
		Update("read_at", gorm.Expr("now()"))
	return res.RowsAffected, res.Error
}

func ListNotificationFeed(db *gorm.DB, site string, after int64, limit int) ([]model.CommunityNotification, error) {
	var rows []model.CommunityNotification
	err := db.Model(&model.CommunityNotification{}).
		Where("site = ? AND seq > ?", site, after).
		Order("seq ASC").Limit(limit).Find(&rows).Error
	return rows, err
}

func PruneProcessedEvents(db *gorm.DB, olderThan time.Duration) (int64, error) {
	res := db.Where("processed_at IS NOT NULL AND processed_at < ?", time.Now().Add(-olderThan)).
		Delete(&model.CommunityEvent{})
	return res.RowsAffected, res.Error
}

func PruneReadNotifications(db *gorm.DB, olderThan time.Duration) (int64, error) {
	res := db.Where("read_at IS NOT NULL AND read_at < ?", time.Now().Add(-olderThan)).
		Delete(&model.CommunityNotification{})
	return res.RowsAffected, res.Error
}

func DeleteAuthorNotificationsTx(tx *gorm.DB, site string, userID int64) (int64, error) {
	return archiveTx(tx, site, userID, model.PurgeStepNotification,
		`DELETE FROM community_notification WHERE site = ? AND user_id = ? RETURNING *`, site, userID)
}

func ClearAuthorNotificationActorsTx(tx *gorm.DB, site string, userID int64) (int64, error) {
	return archiveTx(tx, site, userID, model.PurgeStepNotificationActor, `
		UPDATE community_notification AS n
		   SET actor_id = NULL, updated_at = now()
		  FROM (SELECT * FROM community_notification
		         WHERE site = ? AND actor_id = ?
		           FOR UPDATE) AS prev
		 WHERE n.id = prev.id
		RETURNING prev.*`,
		site, userID)
}

func ForgetUserInPendingEventsTx(tx *gorm.DB, site string, userID int64) (int64, error) {
	return archiveTx(tx, site, userID, model.PurgeStepEventRecipient, `
		UPDATE community_event AS ev SET
		       target_user_id = NULLIF(ev.target_user_id, ?),
		       mention_user_ids = (
		           SELECT jsonb_agg(e) FROM jsonb_array_elements(ev.mention_user_ids) AS e
		            WHERE e <> to_jsonb(?::bigint))
		  FROM (SELECT * FROM community_event
		         WHERE site = ? AND processed_at IS NULL
		           AND (target_user_id = ? OR mention_user_ids @> jsonb_build_array(?::bigint))
		           FOR UPDATE) AS prev
		 WHERE ev.id = prev.id
		RETURNING prev.*`,
		userID, userID, site, userID, userID)
}

func DeleteAuthorEventsTx(tx *gorm.DB, site string, userID int64) (int64, error) {
	return archiveTx(tx, site, userID, model.PurgeStepEvent,
		`DELETE FROM community_event WHERE site = ? AND actor_id = ? RETURNING *`, site, userID)
}

func PostedFoldKey(threadID int64) string { return fmt.Sprintf("posted:%d", threadID) }

func LikedFoldKey(postID int64) string { return fmt.Sprintf("like:%d", postID) }

func FeedbackFoldKey(threadID int64) string { return fmt.Sprintf("fb:%d", threadID) }
