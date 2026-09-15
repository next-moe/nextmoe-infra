package repository

import (
	"strings"

	"api/internal/platform/community/model"
)

type SearchQuery struct {
	Site   string
	Q      string
	Kind   int16 // -1 = every kind
	Cursor TimeCursor
	Limit  int
}

// SearchPosts matches the markdown source, not the cooked HTML: the HTML holds
// tag names and attributes a reader never typed, so searching it makes `<a>`
// match every post carrying a link.
func (r *PostRepository) SearchPosts(q SearchQuery) ([]AuthorPostRow, error) {
	db := r.db.Model(&model.CommunityPost{}).
		Select(threadContextSelect).
		Joins("JOIN community_thread ON community_thread.id = community_post.thread_id").
		Where("community_post.status = ?", model.PostStatusVisible).
		Where("community_thread.status <> ?", model.ThreadStatusDeleted).
		Where(`community_post.content_raw ILIKE ? ESCAPE '\'`, likeContains(q.Q))
	db = scopeVisibleToSite(db, q.Site)
	if q.Kind >= 0 {
		db = db.Where("community_thread.kind = ?", q.Kind)
	}
	db = scopePostKeyset(db, q.Cursor)

	var rows []AuthorPostRow
	err := db.Order("community_post.created_at DESC, community_post.id DESC").Limit(q.Limit).Scan(&rows).Error
	return rows, err
}

func (r *ThreadRepository) SearchThreads(q SearchQuery) ([]model.CommunityThread, error) {
	db := r.db.Model(&model.CommunityThread{}).
		Where("community_thread.status <> ?", model.ThreadStatusDeleted).
		Where(`community_thread.title ILIKE ? ESCAPE '\'`, likeContains(q.Q))
	db = scopeVisibleToSite(db, q.Site)
	if q.Kind >= 0 {
		db = db.Where("community_thread.kind = ?", q.Kind)
	}
	if q.Cursor.ID != 0 {
		db = db.Where(
			"community_thread.created_at < ? OR (community_thread.created_at = ? AND community_thread.id < ?)",
			q.Cursor.CreatedAt, q.Cursor.CreatedAt, q.Cursor.ID,
		)
	}
	var rows []model.CommunityThread
	err := db.Order("community_thread.created_at DESC, community_thread.id DESC").Limit(q.Limit).Find(&rows).Error
	return rows, err
}

func likeContains(q string) string {
	return "%" + escapeLikePattern(q) + "%"
}

func escapeLikePattern(q string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q)
}
