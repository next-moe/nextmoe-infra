package repository

import (
	"time"

	"api/internal/platform/community/model"

	"gorm.io/gorm"
)

// threadContextSelect is the projection every post read that carries its thread
// context shares (author feed, posts-by-ids, site feed, search).
const threadContextSelect = "community_post.*, community_thread.title AS thread_title, " +
	"community_thread.anchor_kind AS thread_anchor_kind, community_thread.anchor_id AS thread_anchor_id"

// scopeVisibleToSite is service.CrossTenant written in SQL: a caller reaches its
// own site's threads plus every network-shared anchor. Both halves must move
// together — model.SiteLocalAnchorKinds is the one list they read from.
func scopeVisibleToSite(db *gorm.DB, site string) *gorm.DB {
	return db.Where("community_thread.site = ? OR community_thread.anchor_kind NOT IN ?",
		site, model.SiteLocalAnchorKinds)
}

// PostFeedCursor keys on created_at, not id: the kungal import gave historical
// comments new ids, so id order is import order, not time order (measured
// corr(id, created_at) = 0.72 in production).
type PostFeedCursor struct {
	CreatedAt time.Time
	ID        int64
}

type PostFeedQuery struct {
	Site        string
	Kind        int16 // -1 = every kind
	AnchorKind  int16 // -1 = every anchor kind
	AnchorID    string
	RepliesOnly bool
	Cursor      PostFeedCursor
	Limit       int
}

func (r *PostRepository) ListSiteFeed(q PostFeedQuery) ([]AuthorPostRow, error) {
	db := r.db.Model(&model.CommunityPost{}).
		Select(threadContextSelect).
		Joins("JOIN community_thread ON community_thread.id = community_post.thread_id").
		Where("community_post.status = ?", model.PostStatusVisible)
	db = scopeVisibleToSite(db, q.Site)
	if q.Kind >= 0 {
		db = db.Where("community_thread.kind = ?", q.Kind)
	}
	if q.AnchorKind >= 0 {
		db = db.Where("community_thread.anchor_kind = ?", q.AnchorKind)
	}
	if q.AnchorID != "" {
		db = db.Where("community_thread.anchor_id = ?", q.AnchorID)
	}
	if q.RepliesOnly {
		db = db.Where("community_post.post_number > 1")
	}
	db = scopePostKeyset(db, q.Cursor)

	var rows []AuthorPostRow
	err := db.Order("community_post.created_at DESC, community_post.id DESC").Limit(q.Limit).Scan(&rows).Error
	return rows, err
}

func scopePostKeyset(db *gorm.DB, cursor PostFeedCursor) *gorm.DB {
	if cursor.ID == 0 {
		return db
	}
	return db.Where(
		"community_post.created_at < ? OR (community_post.created_at = ? AND community_post.id < ?)",
		cursor.CreatedAt, cursor.CreatedAt, cursor.ID,
	)
}
