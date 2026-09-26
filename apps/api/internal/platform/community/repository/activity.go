package repository

import (
	"fmt"
	"time"

	"api/internal/platform/community/model"

	"gorm.io/gorm"
)

type ActivityWrite struct {
	Key            string
	ActorID        int64
	Verb           int16
	ObjectKind     string
	ObjectLabel    string
	Title          string
	Excerpt        string
	URL            string
	CoverImageHash *string
	WorkID         *int64
	ContentLimit   int16
	Notify         bool
	OccurredAt     time.Time
	Revision       int64
	Removed        bool
}

type ActivityGroupKey struct {
	Site       string
	ActorID    int64
	Verb       int16
	ObjectKind string
	BucketDate string
}

func (k ActivityGroupKey) String() string {
	return fmt.Sprintf("%s\x00%d\x00%d\x00%s\x00%s", k.Site, k.ActorID, k.Verb, k.ObjectKind, k.BucketDate)
}

type ActivityWriteResult struct {
	Applied      bool
	ID           int64
	Existed      bool
	WasRemoved   bool
	IsRemoved    bool
	NotifiesNow  bool
	EverNotified bool
	NewGroup     ActivityGroupKey
	OldGroup     *ActivityGroupKey
}

type activityWriteRow struct {
	ID            int64   `gorm:"column:id"`
	ActorID       int64   `gorm:"column:actor_id"`
	Verb          int16   `gorm:"column:verb"`
	ObjectKind    string  `gorm:"column:object_kind"`
	BucketDate    string  `gorm:"column:bucket_date"`
	IsRemoved     bool    `gorm:"column:is_removed"`
	IsNotified    bool    `gorm:"column:is_notified"`
	OldID         *int64  `gorm:"column:old_id"`
	OldRemoved    *bool   `gorm:"column:old_removed"`
	OldActorID    *int64  `gorm:"column:old_actor_id"`
	OldVerb       *int16  `gorm:"column:old_verb"`
	OldObjectKind *string `gorm:"column:old_object_kind"`
	OldBucketDate *string `gorm:"column:old_bucket_date"`
}

const activityReturning = `
	RETURNING new.id, new.actor_id, new.verb, new.object_kind, new.bucket_date::text AS bucket_date,
	          new.removed_at IS NOT NULL AS is_removed, new.notified_at IS NOT NULL AS is_notified,
	          old.id AS old_id, old.removed_at IS NOT NULL AS old_removed, old.actor_id AS old_actor_id,
	          old.verb AS old_verb, old.object_kind AS old_object_kind, old.bucket_date::text AS old_bucket_date`

func UpsertActivityTx(tx *gorm.DB, site string, w ActivityWrite, notifyEligible bool) (ActivityWriteResult, error) {
	var rows []activityWriteRow
	var err error
	if w.Removed {
		err = tx.Raw(`
			INSERT INTO community_activity AS a (
			    site, key, actor_id, verb, object_kind, object_label, title, excerpt, url,
			    cover_image_hash, work_id, content_limit, notify, occurred_at, revision,
			    notified_at, removed_at, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, '', '', '', '', NULL, NULL, ?, false, ?, ?, NULL, now(), now(), now())
			ON CONFLICT (site, key) DO UPDATE SET
			    title = '', excerpt = '', url = '', cover_image_hash = NULL, work_id = NULL,
			    revision = EXCLUDED.revision, removed_at = COALESCE(a.removed_at, now()), updated_at = now()
			  WHERE a.revision < EXCLUDED.revision`+activityReturning,
			site, w.Key, w.ActorID, w.Verb, w.ObjectKind, model.ContentLimitNSFW, w.OccurredAt, w.Revision,
		).Scan(&rows).Error
	} else {
		err = tx.Raw(`
			INSERT INTO community_activity AS a (
			    site, key, actor_id, verb, object_kind, object_label, title, excerpt, url,
			    cover_image_hash, work_id, content_limit, notify, occurred_at, revision,
			    notified_at, removed_at, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
			    CASE WHEN ? AND ?::timestamptz >= now() - interval '24 hours' THEN now() END,
			    NULL, now(), now())
			ON CONFLICT (site, key) DO UPDATE SET
			    actor_id = EXCLUDED.actor_id, verb = EXCLUDED.verb, object_kind = EXCLUDED.object_kind,
			    object_label = EXCLUDED.object_label, title = EXCLUDED.title, excerpt = EXCLUDED.excerpt,
			    url = EXCLUDED.url, cover_image_hash = EXCLUDED.cover_image_hash, work_id = EXCLUDED.work_id,
			    content_limit = EXCLUDED.content_limit, notify = EXCLUDED.notify,
			    occurred_at = EXCLUDED.occurred_at, revision = EXCLUDED.revision,
			    removed_at = NULL, updated_at = now()
			  WHERE a.revision < EXCLUDED.revision`+activityReturning,
			site, w.Key, w.ActorID, w.Verb, w.ObjectKind, w.ObjectLabel, w.Title, w.Excerpt, w.URL,
			w.CoverImageHash, w.WorkID, w.ContentLimit, w.Notify, w.OccurredAt, w.Revision,
			notifyEligible, w.OccurredAt,
		).Scan(&rows).Error
	}
	if err != nil || len(rows) == 0 {
		return ActivityWriteResult{}, err
	}
	r := rows[0]
	res := ActivityWriteResult{
		Applied:      true,
		ID:           r.ID,
		Existed:      r.OldID != nil,
		IsRemoved:    r.IsRemoved,
		NotifiesNow:  r.OldID == nil && r.IsNotified,
		EverNotified: r.IsNotified,
		NewGroup:     ActivityGroupKey{Site: site, ActorID: r.ActorID, Verb: r.Verb, ObjectKind: r.ObjectKind, BucketDate: r.BucketDate},
	}
	if r.OldID != nil {
		res.WasRemoved = *r.OldRemoved
		old := ActivityGroupKey{Site: site, ActorID: *r.OldActorID, Verb: *r.OldVerb, ObjectKind: *r.OldObjectKind, BucketDate: *r.OldBucketDate}
		res.OldGroup = &old
	}
	return res, nil
}

// The upsert takes the group row's lock before the recount, so a concurrent
// writer of the same group recounts after this commits and sees its rows.
func RefreshActivityGroupTx(tx *gorm.DB, k ActivityGroupKey) error {
	var id int64
	if err := tx.Raw(`
		INSERT INTO community_activity_group (
		    site, actor_id, verb, object_kind, bucket_date, object_label,
		    count_all, count_sfw, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?::date, '', 0, 0, now(), now())
		ON CONFLICT (site, actor_id, verb, object_kind, bucket_date) DO UPDATE SET updated_at = now()
		RETURNING id`,
		k.Site, k.ActorID, k.Verb, k.ObjectKind, k.BucketDate).Scan(&id).Error; err != nil {
		return err
	}
	if err := tx.Exec(`
		UPDATE community_activity_group AS g SET
		    count_all = s.count_all, count_sfw = s.count_sfw,
		    latest_all_id = s.latest_all_id, latest_all_at = s.latest_all_at,
		    latest_sfw_id = s.latest_sfw_id, latest_sfw_at = s.latest_sfw_at,
		    object_label = COALESCE(s.object_label, g.object_label), updated_at = now()
		  FROM (SELECT count(*) AS count_all,
		               count(*) FILTER (WHERE content_limit = ?) AS count_sfw,
		               (array_agg(id ORDER BY occurred_at DESC, id DESC))[1] AS latest_all_id,
		               max(occurred_at) AS latest_all_at,
		               (array_agg(id ORDER BY occurred_at DESC, id DESC) FILTER (WHERE content_limit = ?))[1] AS latest_sfw_id,
		               max(occurred_at) FILTER (WHERE content_limit = ?) AS latest_sfw_at,
		               (array_agg(object_label ORDER BY occurred_at DESC, id DESC))[1] AS object_label
		          FROM community_activity
		         WHERE site = ? AND actor_id = ? AND verb = ? AND object_kind = ?
		           AND bucket_date = ?::date AND removed_at IS NULL) AS s
		 WHERE g.id = ?`,
		model.ContentLimitSFW, model.ContentLimitSFW, model.ContentLimitSFW,
		k.Site, k.ActorID, k.Verb, k.ObjectKind, k.BucketDate, id).Error; err != nil {
		return err
	}
	return tx.Exec(`DELETE FROM community_activity_group WHERE id = ? AND count_all = 0`, id).Error
}

func ListSiteActivities(db *gorm.DB, site string, afterID int64, limit int) ([]model.CommunityActivity, error) {
	var rows []model.CommunityActivity
	err := db.Where("site = ? AND id > ?", site, afterID).Order("id ASC").Limit(limit).Find(&rows).Error
	return rows, err
}

func PruneActivityTombstones(db *gorm.DB, olderThan time.Duration) (int64, error) {
	res := db.Where("removed_at IS NOT NULL AND removed_at < ?", time.Now().Add(-olderThan)).
		Delete(&model.CommunityActivity{})
	return res.RowsAffected, res.Error
}

// A site write locks activity rows in key order and then their groups; a
// purge that took the author's groups first deadlocked with a concurrent write
// of the same author, so it follows the same order.
func DeleteAuthorActivitiesTx(tx *gorm.DB, site string, actorID int64) (int64, error) {
	if err := tx.Exec(`SELECT 1 FROM community_activity WHERE site = ? AND actor_id = ? ORDER BY key FOR UPDATE`,
		site, actorID).Error; err != nil {
		return 0, err
	}
	res := tx.Where("site = ? AND actor_id = ?", site, actorID).Delete(&model.CommunityActivity{})
	if res.Error != nil {
		return 0, res.Error
	}
	err := tx.Where("site = ? AND actor_id = ?", site, actorID).Delete(&model.CommunityActivityGroup{}).Error
	return res.RowsAffected, err
}

func DeleteFeedSeen(db *gorm.DB, userID int64) error {
	return db.Where("user_id = ?", userID).Delete(&model.CommunityFeedSeen{}).Error
}
