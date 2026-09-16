package repository

import (
	"api/internal/platform/community/model"

	"gorm.io/gorm"
)

// The sparse thread_user row (Discourse topic_users): it exists only for a
// (thread, user) pair that has actually interacted, so a thread a user never
// opened reports no state at all rather than "everything unread".
const upsertThreadUser = `
	INSERT INTO community_thread_user (thread_id, user_id, last_read_post_number, notification_level, last_visited_at, site)
	VALUES (?, ?, ?, ?, now(), ?)
	ON CONFLICT (thread_id, user_id) DO UPDATE
	   SET last_read_post_number = GREATEST(community_thread_user.last_read_post_number, EXCLUDED.last_read_post_number),
	       last_visited_at = now(),
	       site = EXCLUDED.site`

// MarkReadTx advances a reader's high-water mark. It never walks backwards: a
// late-arriving receipt from a slower tab cannot un-read what was already read.
func MarkReadTx(tx *gorm.DB, threadID, userID int64, lastRead int32, site string) error {
	return tx.Exec(upsertThreadUser, threadID, userID, lastRead, model.NotificationLevelNormal, site).Error
}

// EnsureSubscribedTx is the poster's own row: writing in a thread subscribes you
// to it and marks your own post read. An existing row keeps its level — someone
// who muted a thread and then replies stays muted, because the mute was a
// deliberate choice and the reply is not a request to undo it.
func EnsureSubscribedTx(tx *gorm.DB, threadID, userID int64, postNumber int32, site string) error {
	return tx.Exec(upsertThreadUser, threadID, userID, postNumber, model.NotificationLevelWatching, site).Error
}

func SetNotificationLevelTx(tx *gorm.DB, threadID, userID int64, level int16, site string) error {
	return tx.Exec(`
		INSERT INTO community_thread_user (thread_id, user_id, last_read_post_number, notification_level, last_visited_at, site)
		VALUES (?, ?, 0, ?, now(), ?)
		ON CONFLICT (thread_id, user_id) DO UPDATE SET notification_level = EXCLUDED.notification_level, site = EXCLUDED.site`,
		threadID, userID, level, site).Error
}

type ThreadUserState struct {
	ThreadID           int64 `gorm:"column:thread_id"`
	UserID             int64 `gorm:"column:user_id"`
	LastReadPostNumber int32 `gorm:"column:last_read_post_number"`
	NotificationLevel  int16 `gorm:"column:notification_level"`
	HighestPostNumber  int32 `gorm:"column:highest_post_number"`
	UnreadCount        int32 `gorm:"column:unread_count"`
}

// unreadCountExpr is the doc 11 definition: allocated numbers minus what the
// reader has seen. A tombstoned post keeps its number (invariant 13), so a
// thread whose only new post was deleted still reads as one unread.
const unreadCountExpr = "GREATEST(community_thread.highest_post_number - community_thread_user.last_read_post_number, 0) AS unread_count"

const threadUserStateSelect = "community_thread_user.thread_id, community_thread_user.user_id, " +
	"community_thread_user.last_read_post_number, community_thread_user.notification_level, " +
	"community_thread.highest_post_number, " + unreadCountExpr

type EngagementRepository struct{ db *gorm.DB }

func NewEngagementRepository(db *gorm.DB) *EngagementRepository { return &EngagementRepository{db: db} }

func (r *EngagementRepository) State(threadID, userID int64) (*ThreadUserState, error) {
	var row ThreadUserState
	err := r.stateQuery().
		Where("community_thread_user.thread_id = ? AND community_thread_user.user_id = ?", threadID, userID).
		Take(&row).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *EngagementRepository) States(site string, userID int64, threadIDs []int64) ([]ThreadUserState, error) {
	if len(threadIDs) == 0 {
		return nil, nil
	}
	var rows []ThreadUserState
	err := scopeVisibleToSite(r.stateQuery(), site).
		Where("community_thread_user.user_id = ? AND community_thread_user.thread_id IN ?", userID, threadIDs).
		Scan(&rows).Error
	return rows, err
}

func (r *EngagementRepository) stateQuery() *gorm.DB {
	return r.db.Model(&model.CommunityThreadUser{}).
		Select(threadUserStateSelect).
		Joins("JOIN community_thread ON community_thread.id = community_thread_user.thread_id")
}

type UnreadThreadRow struct {
	model.CommunityThread
	LastReadPostNumber int32 `gorm:"column:last_read_post_number"`
	NotificationLevel  int16 `gorm:"column:notification_level"`
	UnreadCount        int32 `gorm:"column:unread_count"`
}

func (r *EngagementRepository) ListUnread(site string, userID int64, cursor ThreadCursor, limit int) ([]UnreadThreadRow, error) {
	db := r.unreadQuery(site, userID).
		Select("community_thread.*, community_thread_user.last_read_post_number, " +
			"community_thread_user.notification_level, " + unreadCountExpr)
	if cursor.ID != 0 {
		if cursor.LastPostedNull {
			db = db.Where("community_thread.last_posted_at IS NULL AND community_thread.id < ?", cursor.ID)
		} else {
			db = db.Where(
				"community_thread.last_posted_at IS NULL OR community_thread.last_posted_at < ? "+
					"OR (community_thread.last_posted_at = ? AND community_thread.id < ?)",
				cursor.LastPosted, cursor.LastPosted, cursor.ID,
			)
		}
	}
	var rows []UnreadThreadRow
	err := db.Order("community_thread.last_posted_at DESC NULLS LAST, community_thread.id DESC").
		Limit(limit).Scan(&rows).Error
	return rows, err
}

func (r *EngagementRepository) CountUnread(site string, userID int64) (int64, error) {
	var n int64
	err := r.unreadQuery(site, userID).Count(&n).Error
	return n, err
}

func (r *EngagementRepository) unreadQuery(site string, userID int64) *gorm.DB {
	return scopeVisibleToSite(r.db.Model(&model.CommunityThreadUser{}), site).
		Joins("JOIN community_thread ON community_thread.id = community_thread_user.thread_id").
		Where("community_thread_user.user_id = ?", userID).
		Where("community_thread_user.notification_level <> ?", model.NotificationLevelMuted).
		Where("community_thread.status <> ?", model.ThreadStatusDeleted).
		Where("community_thread.highest_post_number > community_thread_user.last_read_post_number")
}
