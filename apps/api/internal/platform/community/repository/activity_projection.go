package repository

import (
	"time"

	"api/internal/platform/community/model"

	"gorm.io/gorm"
)

func ClaimActivityProjectionsTx(tx *gorm.DB, limit int) ([]model.CommunityActivityProjection, error) {
	var rows []model.CommunityActivityProjection
	err := tx.Order("enqueued_at, post_id").Limit(limit).Find(&rows).Error
	return rows, err
}

// AckActivityProjectionsTx deletes the claimed rows that were not queued again
// while they were being projected.
func AckActivityProjectionsTx(tx *gorm.DB, claims []model.CommunityActivityProjection) error {
	if len(claims) == 0 {
		return nil
	}
	pairs := make([][]any, len(claims))
	for i, c := range claims {
		pairs[i] = []any{c.PostID, c.EnqueuedAt}
	}
	return tx.Exec(`DELETE FROM community_activity_projection WHERE (post_id, enqueued_at) IN ?`, pairs).Error
}

type ProjectedPostRow struct {
	PostID             int64     `gorm:"column:post_id"`
	AuthorID           int64     `gorm:"column:author_id"`
	PostNumber         int32     `gorm:"column:post_number"`
	PostStatus         int16     `gorm:"column:post_status"`
	PostRating         int16     `gorm:"column:post_rating"`
	ContentRaw         string    `gorm:"column:content_raw"`
	CreatedAt          time.Time `gorm:"column:created_at"`
	ThreadID           int64     `gorm:"column:thread_id"`
	Site               string    `gorm:"column:site"`
	ThreadKind         int16     `gorm:"column:thread_kind"`
	AnchorKind         int16     `gorm:"column:anchor_kind"`
	AnchorID           string    `gorm:"column:anchor_id"`
	ThreadTitle        *string   `gorm:"column:thread_title"`
	ThreadRating       int16     `gorm:"column:thread_rating"`
	ThreadStatus       int16     `gorm:"column:thread_status"`
	Merged             bool      `gorm:"column:merged"`
	AnchorLive         bool      `gorm:"column:anchor_live"`
	AnchorTitle        string    `gorm:"column:anchor_title"`
	AnchorURL          string    `gorm:"column:anchor_url"`
	AnchorWorkID       *int64    `gorm:"column:anchor_work_id"`
	AnchorCover        *string   `gorm:"column:anchor_cover"`
	AnchorContentLimit int16     `gorm:"column:anchor_content_limit"`
}

func ProjectedPostsTx(tx *gorm.DB, postIDs []int64) (map[int64]ProjectedPostRow, error) {
	out := map[int64]ProjectedPostRow{}
	if len(postIDs) == 0 {
		return out, nil
	}
	var rows []ProjectedPostRow
	err := tx.Raw(`
		SELECT p.id AS post_id, p.author_id, p.post_number, p.status AS post_status,
		       p.content_rating AS post_rating, p.content_raw, p.created_at,
		       t.id AS thread_id, t.site, t.kind AS thread_kind, t.anchor_kind, t.anchor_id,
		       t.title AS thread_title, t.content_rating AS thread_rating, t.status AS thread_status,
		       t.merged_into_id IS NOT NULL AS merged,
		       (ap.site IS NOT NULL AND ap.removed_at IS NULL) AS anchor_live,
		       COALESCE(ap.title, '') AS anchor_title, COALESCE(ap.url, '') AS anchor_url,
		       ap.work_id AS anchor_work_id, ap.cover_image_hash AS anchor_cover, COALESCE(ap.content_limit, ?) AS anchor_content_limit
		  FROM community_post p
		  JOIN community_thread t ON t.id = p.thread_id
		  LEFT JOIN community_anchor_presentation ap
		         ON ap.site = t.site AND ap.anchor_kind = t.anchor_kind AND ap.anchor_id = t.anchor_id
		 WHERE p.id IN ?`, model.ContentLimitNSFW, postIDs).Scan(&rows).Error
	for _, r := range rows {
		out[r.PostID] = r
	}
	return out, err
}

type OwnActivityRow struct {
	Site       string `gorm:"column:site"`
	Key        string `gorm:"column:key"`
	ActorID    int64  `gorm:"column:actor_id"`
	Verb       int16  `gorm:"column:verb"`
	ObjectKind string `gorm:"column:object_kind"`
	Removed    bool   `gorm:"column:removed"`
}

func OwnActivitiesByKeyTx(tx *gorm.DB, keys []string) (map[string][]OwnActivityRow, error) {
	out := map[string][]OwnActivityRow{}
	if len(keys) == 0 {
		return out, nil
	}
	var rows []OwnActivityRow
	err := tx.Raw(`
		SELECT site, key, actor_id, verb, object_kind, removed_at IS NOT NULL AS removed
		  FROM community_activity
		 WHERE key LIKE 'community:%' AND key IN ?`, keys).Scan(&rows).Error
	for _, r := range rows {
		out[r.Key] = append(out[r.Key], r)
	}
	return out, err
}

func ActivitySitesTx(tx *gorm.DB) (map[string]model.CommunityActivitySite, error) {
	var rows []model.CommunityActivitySite
	if err := tx.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]model.CommunityActivitySite, len(rows))
	for _, r := range rows {
		out[r.Site] = r
	}
	return out, nil
}

func ActivitySiteTx(tx *gorm.DB, site string) (*model.CommunityActivitySite, error) {
	var rows []model.CommunityActivitySite
	if err := tx.Where("site = ?", site).Limit(1).Find(&rows).Error; err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}
