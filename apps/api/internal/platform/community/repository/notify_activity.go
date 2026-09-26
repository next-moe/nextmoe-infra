package repository

import (
	"fmt"
	"time"

	"api/internal/platform/community/model"

	"gorm.io/gorm"
)

const FolloweeFoldCountCap = 100

func FolloweeFoldKey(actorID int64) string { return fmt.Sprintf("followee:%d", actorID) }

func ClaimActivityPublishedEventsTx(tx *gorm.DB, site string, actorID int64) (eventIDs, activityIDs []int64, err error) {
	var rows []struct {
		ID         int64  `gorm:"column:id"`
		ActivityID *int64 `gorm:"column:activity_id"`
	}
	err = tx.Raw(`
		UPDATE community_event SET processed_at = now()
		 WHERE kind = ? AND site = ? AND actor_id = ? AND processed_at IS NULL
		RETURNING id, activity_id`,
		model.EventKindActivityPublished, site, actorID).Scan(&rows).Error
	for _, r := range rows {
		eventIDs = append(eventIDs, r.ID)
		if r.ActivityID != nil {
			activityIDs = append(activityIDs, *r.ActivityID)
		}
	}
	return eventIDs, activityIDs, err
}

func EarliestLiveNotifiedTx(tx *gorm.DB, ids []int64) (*time.Time, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var at *time.Time
	err := tx.Raw(`
		SELECT min(occurred_at) FROM community_activity
		 WHERE id IN ? AND removed_at IS NULL AND notified_at IS NOT NULL`, ids).Scan(&at).Error
	return at, err
}

// The caller must hold the dispatcher lock: only under it is seq order commit
// order.
func UpsertFolloweeFoldsTx(tx *gorm.DB, site string, actorID int64, since time.Time) ([]int64, error) {
	var ids []int64
	err := tx.Raw(`
		INSERT INTO community_notification (
		    site, user_id, kind, thread_id, anchor_kind, anchor_id, since_at,
		    actor_id, actor_count, item_count, fold_key, seq, created_at, updated_at)
		SELECT ?, f.follower_id, ?, 0, 0, '', ?::timestamptz,
		       ?, 1, 1, ?, nextval('community_notification_seq'), now(), now()
		  FROM community_user_follow f
		 WHERE f.followee_id = ? AND f.notify_level = ? AND f.follower_id <> ?
		ON CONFLICT (site, user_id, fold_key) WHERE read_at IS NULL AND fold_key IS NOT NULL
		DO UPDATE SET
		    since_at = LEAST(community_notification.since_at, EXCLUDED.since_at),
		    seq = EXCLUDED.seq,
		    updated_at = now()
		RETURNING id`,
		site, model.NotificationKindFolloweeActivity, since,
		actorID, FolloweeFoldKey(actorID),
		actorID, model.FollowNotifyAll, actorID).Scan(&ids).Error
	return ids, err
}

func RecountFolloweeFoldRowsTx(tx *gorm.DB, actorID int64, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	return tx.Exec(recountFolloweeFoldsSQL("n.seq", "n.id IN ?", ""),
		actorID, FolloweeFoldCountCap, actorID, ids).Error
}

func RecountFolloweeFoldKeyTx(tx *gorm.DB, site string, actorID int64) (int64, error) {
	res := tx.Exec(recountFolloweeFoldsSQL(
		"nextval('community_notification_seq')",
		"n.site = ? AND n.fold_key = ? AND n.read_at IS NULL",
		" AND (n.item_count, n.activity_id) IS DISTINCT FROM (c.cnt, c.latest_id)"),
		actorID, FolloweeFoldCountCap, actorID, site, FolloweeFoldKey(actorID))
	return res.RowsAffected, res.Error
}

func recountFolloweeFoldsSQL(seq, target, changed string) string {
	return fmt.Sprintf(`
		UPDATE community_notification AS n SET
		    item_count = c.cnt,
		    activity_id = c.latest_id,
		    actor_count = 1,
		    read_at = CASE WHEN c.cnt = 0 THEN now() ELSE n.read_at END,
		    seq = %s,
		    updated_at = now()
		  FROM (SELECT n.id,
		               (SELECT count(*) FROM (
		                    SELECT 1 FROM community_activity a
		                     WHERE a.site = n.site AND a.actor_id = ? AND a.removed_at IS NULL
		                       AND a.notified_at IS NOT NULL AND a.occurred_at >= n.since_at
		                     LIMIT ?) m) AS cnt,
		               (SELECT a.id FROM community_activity a
		                 WHERE a.site = n.site AND a.actor_id = ? AND a.removed_at IS NULL
		                   AND a.notified_at IS NOT NULL AND a.occurred_at >= n.since_at
		                 ORDER BY a.occurred_at DESC, a.id DESC
		                 LIMIT 1) AS latest_id
		          FROM community_notification n
		         WHERE %s) AS c
		 WHERE n.id = c.id%s`, seq, target, changed)
}

// RetractFolloweeFoldsTx is the purge's half: besides retracting the author's
// unread folds it drops the fold key from their read ones, which would
// otherwise keep naming the purged uid after actor_id was cleared.
func RetractFolloweeFoldsTx(tx *gorm.DB, site string, actorID int64) (int64, error) {
	res := tx.Exec(`
		UPDATE community_notification SET
		    item_count = 0, activity_id = NULL, read_at = now(), fold_key = NULL,
		    seq = nextval('community_notification_seq'), updated_at = now()
		 WHERE site = ? AND fold_key = ? AND read_at IS NULL`,
		site, FolloweeFoldKey(actorID))
	if res.Error != nil {
		return 0, res.Error
	}
	err := tx.Exec(`UPDATE community_notification SET fold_key = NULL WHERE site = ? AND fold_key = ?`,
		site, FolloweeFoldKey(actorID)).Error
	return res.RowsAffected, err
}

type NotificationActivityRow struct {
	ID           int64     `gorm:"column:id"`
	Site         string    `gorm:"column:site"`
	Key          string    `gorm:"column:key"`
	Verb         int16     `gorm:"column:verb"`
	ObjectKind   string    `gorm:"column:object_kind"`
	ObjectLabel  string    `gorm:"column:object_label"`
	Title        string    `gorm:"column:title"`
	URL          string    `gorm:"column:url"`
	ContentLimit int16     `gorm:"column:content_limit"`
	OccurredAt   time.Time `gorm:"column:occurred_at"`
}

func LiveActivitiesByID(db *gorm.DB, ids []int64) (map[int64]NotificationActivityRow, error) {
	out := map[int64]NotificationActivityRow{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []NotificationActivityRow
	if err := db.Raw(`
		SELECT id, site, key, verb, object_kind, object_label, title, url, content_limit, occurred_at
		  FROM community_activity
		 WHERE id IN ? AND removed_at IS NULL`, ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ID] = r
	}
	return out, nil
}
