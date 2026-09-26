package repository

import (
	"fmt"
	"strings"
	"time"

	"api/internal/platform/community/model"

	"gorm.io/gorm"
)

type ActivityCursor struct {
	At time.Time
	ID int64
}

type ActivityGroupQuery struct {
	FollowerID int64
	ActorID    int64
	SFW        bool
	Sites      []string
	Verbs      []int16
	Before     *ActivityCursor
	Limit      int
}

type ActivityGroupRow struct {
	ID          int64     `gorm:"column:id"`
	Site        string    `gorm:"column:site"`
	ActorID     int64     `gorm:"column:actor_id"`
	Verb        int16     `gorm:"column:verb"`
	ObjectKind  string    `gorm:"column:object_kind"`
	ObjectLabel string    `gorm:"column:object_label"`
	BucketDate  string    `gorm:"column:bucket_date"`
	ItemCount   int       `gorm:"column:item_count"`
	LatestAt    time.Time `gorm:"column:latest_at"`
}

type activityGroupView struct{ count, at string }

func groupView(sfw bool) activityGroupView {
	if sfw {
		return activityGroupView{"count_sfw", "latest_sfw_at"}
	}
	return activityGroupView{"count_all", "latest_all_at"}
}

func (q ActivityGroupQuery) groupFilter(v activityGroupView) (string, []any) {
	var b strings.Builder
	var args []any
	fmt.Fprintf(&b, " AND g.%s > 0", v.count)
	if len(q.Sites) > 0 {
		b.WriteString(" AND g.site IN ?")
		args = append(args, q.Sites)
	}
	if len(q.Verbs) > 0 {
		b.WriteString(" AND g.verb IN ?")
		args = append(args, q.Verbs)
	}
	if q.Before != nil {
		fmt.Fprintf(&b, " AND (g.%s, g.id) < (?, ?)", v.at)
		args = append(args, q.Before.At, q.Before.ID)
	}
	return b.String(), args
}

func groupColumns(v activityGroupView) string {
	return fmt.Sprintf(`g.id, g.site, g.actor_id, g.verb, g.object_kind, g.object_label,
		g.bucket_date::text AS bucket_date, g.%s AS item_count, g.%s AS latest_at`, v.count, v.at)
}

func ListFollowingActivityGroups(db *gorm.DB, q ActivityGroupQuery) ([]ActivityGroupRow, error) {
	v := groupView(q.SFW)
	filter, args := q.groupFilter(v)
	sql := fmt.Sprintf(`
		SELECT g.* FROM community_user_follow f
		CROSS JOIN LATERAL (
		    SELECT %s FROM community_activity_group g
		     WHERE g.actor_id = f.followee_id%s
		     ORDER BY g.%s DESC, g.id DESC
		     LIMIT ?) g
		 WHERE f.follower_id = ?
		 ORDER BY g.latest_at DESC, g.id DESC
		 LIMIT ?`, groupColumns(v), filter, v.at)
	args = append(args, q.Limit, q.FollowerID, q.Limit)
	var rows []ActivityGroupRow
	err := db.Raw(sql, args...).Scan(&rows).Error
	return rows, err
}

func ListActorActivityGroups(db *gorm.DB, q ActivityGroupQuery) ([]ActivityGroupRow, error) {
	v := groupView(q.SFW)
	filter, args := q.groupFilter(v)
	sql := fmt.Sprintf(`
		SELECT %s FROM community_activity_group g
		 WHERE g.actor_id = ?%s
		 ORDER BY g.%s DESC, g.id DESC
		 LIMIT ?`, groupColumns(v), filter, v.at)
	args = append([]any{q.ActorID}, append(args, q.Limit)...)
	var rows []ActivityGroupRow
	err := db.Raw(sql, args...).Scan(&rows).Error
	return rows, err
}

func GetActivityGroup(db *gorm.DB, id int64) (*model.CommunityActivityGroup, error) {
	var g model.CommunityActivityGroup
	err := db.Where("id = ?", id).Take(&g).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &g, nil
}

type ActivityItemRow struct {
	GroupID        int64     `gorm:"column:group_id"`
	ID             int64     `gorm:"column:id"`
	Site           string    `gorm:"column:site"`
	Key            string    `gorm:"column:key"`
	ActorID        int64     `gorm:"column:actor_id"`
	Verb           int16     `gorm:"column:verb"`
	ObjectKind     string    `gorm:"column:object_kind"`
	ObjectLabel    string    `gorm:"column:object_label"`
	Title          string    `gorm:"column:title"`
	Excerpt        string    `gorm:"column:excerpt"`
	URL            string    `gorm:"column:url"`
	CoverImageHash *string   `gorm:"column:cover_image_hash"`
	WorkID         *int64    `gorm:"column:work_id"`
	ContentLimit   int16     `gorm:"column:content_limit"`
	OccurredAt     time.Time `gorm:"column:occurred_at"`
}

const activityItemColumns = `a.id, a.site, a.key, a.actor_id, a.verb, a.object_kind, a.object_label,
	a.title, a.excerpt, a.url, a.cover_image_hash, a.work_id, a.content_limit, a.occurred_at`

func sfwItemFilter(sfw bool) string {
	if sfw {
		return fmt.Sprintf(" AND a.content_limit = %d", model.ContentLimitSFW)
	}
	return ""
}

func ListActivityGroupPreviews(db *gorm.DB, groupIDs []int64, sfw bool, perGroup int) ([]ActivityItemRow, error) {
	if len(groupIDs) == 0 {
		return nil, nil
	}
	var rows []ActivityItemRow
	err := db.Raw(fmt.Sprintf(`
		SELECT g.id AS group_id, a.* FROM community_activity_group g
		CROSS JOIN LATERAL (
		    SELECT %s FROM community_activity a
		     WHERE a.site = g.site AND a.actor_id = g.actor_id AND a.verb = g.verb
		       AND a.object_kind = g.object_kind AND a.bucket_date = g.bucket_date
		       AND a.removed_at IS NULL%s
		     ORDER BY a.occurred_at DESC, a.id DESC
		     LIMIT ?) a
		 WHERE g.id IN ?
		 ORDER BY g.id, a.occurred_at DESC, a.id DESC`, activityItemColumns, sfwItemFilter(sfw)),
		perGroup, groupIDs).Scan(&rows).Error
	return rows, err
}

func ListActivityGroupItems(db *gorm.DB, g *model.CommunityActivityGroup, sfw bool, before *ActivityCursor, limit int) ([]ActivityItemRow, error) {
	cursor := ""
	args := []any{g.Site, g.ActorID, g.Verb, g.ObjectKind, g.BucketDate.Format(time.DateOnly)}
	if before != nil {
		cursor = " AND (a.occurred_at, a.id) < (?, ?)"
		args = append(args, before.At, before.ID)
	}
	args = append(args, limit)
	var rows []ActivityItemRow
	err := db.Raw(fmt.Sprintf(`
		SELECT %d AS group_id, %s FROM community_activity a
		 WHERE a.site = ? AND a.actor_id = ? AND a.verb = ? AND a.object_kind = ?
		   AND a.bucket_date = ?::date AND a.removed_at IS NULL%s%s
		 ORDER BY a.occurred_at DESC, a.id DESC
		 LIMIT ?`, g.ID, activityItemColumns, sfwItemFilter(sfw), cursor),
		args...).Scan(&rows).Error
	return rows, err
}

func CountUnseenActivityGroups(db *gorm.DB, q ActivityGroupQuery, limit int) (int, error) {
	v := groupView(q.SFW)
	q.Before = nil
	filter, args := q.groupFilter(v)
	sql := fmt.Sprintf(`
		SELECT count(*) FROM (
		    SELECT 1 FROM community_user_follow f
		    LEFT JOIN community_feed_seen s ON s.user_id = f.follower_id
		    CROSS JOIN LATERAL (
		        SELECT 1 FROM community_activity_group g
		         WHERE g.actor_id = f.followee_id%s
		           AND g.%s > GREATEST(s.seen_at, COALESCE(f.created_at, f.imported_at))
		         LIMIT ?) g
		     WHERE f.follower_id = ?
		     LIMIT ?) unseen`, filter, v.at)
	args = append(args, limit, q.FollowerID, limit)
	var n int
	err := db.Raw(sql, args...).Scan(&n).Error
	return n, err
}

func GetFeedSeen(db *gorm.DB, userID int64) (*time.Time, error) {
	var rows []model.CommunityFeedSeen
	if err := db.Where("user_id = ?", userID).Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0].SeenAt, nil
}

func AdvanceFeedSeen(db *gorm.DB, userID int64, at *time.Time) (time.Time, error) {
	var seen time.Time
	err := db.Raw(`
		INSERT INTO community_feed_seen (user_id, seen_at, updated_at)
		VALUES (?, LEAST(COALESCE(?::timestamptz, now()), now()), now())
		ON CONFLICT (user_id) DO UPDATE SET
		    seen_at = GREATEST(community_feed_seen.seen_at, EXCLUDED.seen_at), updated_at = now()
		RETURNING seen_at`, userID, at).Scan(&seen).Error
	return seen, err
}
