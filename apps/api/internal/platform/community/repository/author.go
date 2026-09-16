package repository

import (
	"api/internal/platform/community/model"

	"gorm.io/gorm"
)

type AuthorPostRow struct {
	model.CommunityPost
	ThreadTitle      *string `gorm:"column:thread_title"`
	ThreadAnchorKind int16   `gorm:"column:thread_anchor_kind"`
	ThreadAnchorID   string  `gorm:"column:thread_anchor_id"`
}

func (r *PostRepository) ListAuthorVisiblePosts(site string, authorID, after int64, anchorKind int16, limit int) ([]AuthorPostRow, error) {
	q := r.db.Model(&model.CommunityPost{}).
		Select(threadContextSelect).
		Joins("JOIN community_thread ON community_thread.id = community_post.thread_id").
		Where("community_post.author_id = ? AND community_thread.site = ? AND community_post.status = ?",
			authorID, site, model.PostStatusVisible)
	if after > 0 {
		q = q.Where("community_post.id < ?", after)
	}
	if anchorKind >= 0 {
		q = q.Where("community_thread.anchor_kind = ?", anchorKind)
	}
	var rows []AuthorPostRow
	err := q.Order("community_post.id DESC").Limit(limit).Scan(&rows).Error
	return rows, err
}

func (r *PostRepository) ResolveVisiblePosts(site string, ids []int64) ([]AuthorPostRow, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []AuthorPostRow
	err := r.db.Model(&model.CommunityPost{}).
		Select(threadContextSelect).
		Joins("JOIN community_thread ON community_thread.id = community_post.thread_id").
		Where("community_post.id IN ? AND community_thread.site = ? AND community_post.status = ?",
			ids, site, model.PostStatusVisible).
		Scan(&rows).Error
	return rows, err
}

func (r *PostRepository) CountAuthorVisiblePosts(site string, authorIDs []int64, kind, anchorKind int16) (map[int64]int64, error) {
	out := make(map[int64]int64, len(authorIDs))
	if len(authorIDs) == 0 {
		return out, nil
	}
	type countRow struct {
		AuthorID int64 `gorm:"column:author_id"`
		N        int64 `gorm:"column:n"`
	}
	q := r.db.Model(&model.CommunityPost{}).
		Select("community_post.author_id AS author_id, COUNT(*) AS n").
		Joins("JOIN community_thread ON community_thread.id = community_post.thread_id").
		Where("community_post.author_id IN ? AND community_thread.site = ? AND community_post.status = ?",
			authorIDs, site, model.PostStatusVisible)
	if kind >= 0 {
		q = q.Where("community_thread.kind = ?", kind)
	}
	if anchorKind >= 0 {
		q = q.Where("community_thread.anchor_kind = ?", anchorKind)
	}
	var rows []countRow
	err := q.Group("community_post.author_id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.AuthorID] = row.N
	}
	return out, nil
}

type AuthorStatRow struct {
	AuthorID     int64 `gorm:"column:author_id"`
	VisiblePosts int64 `gorm:"column:n"`
}

// TopAuthors ranks a site's authors by visible posts, most first.
//
// CountAuthorVisiblePosts answers a caller that already knows which authors it
// means; this one answers "who are they", which no batch of <=100 ids can
// produce. Ties break on the lower author id so a page is stable between calls.
func (r *PostRepository) TopAuthors(site string, kind, anchorKind int16, limit int) ([]AuthorStatRow, error) {
	q := r.db.Model(&model.CommunityPost{}).
		Select("community_post.author_id AS author_id, COUNT(*) AS n").
		Joins("JOIN community_thread ON community_thread.id = community_post.thread_id").
		Where("community_thread.site = ? AND community_post.status = ?", site, model.PostStatusVisible).
		Where("community_thread.status <> ?", model.ThreadStatusDeleted)
	if kind >= 0 {
		q = q.Where("community_thread.kind = ?", kind)
	}
	if anchorKind >= 0 {
		q = q.Where("community_thread.anchor_kind = ?", anchorKind)
	}
	var rows []AuthorStatRow
	err := q.Group("community_post.author_id").
		Order("n DESC, community_post.author_id ASC").
		Limit(limit).Scan(&rows).Error
	return rows, err
}

func PurgeAuthorPostsTx(tx *gorm.DB, site string, authorID int64) (int64, error) {
	res := tx.Exec(`
		UPDATE community_post AS p
		   SET status = ?, content_raw = '', content_html = ''
		  FROM community_thread AS t
		 WHERE p.thread_id = t.id
		   AND t.site = ?
		   AND p.author_id = ?
		   AND (p.status <> ? OR p.content_raw <> '' OR p.content_html <> '')`,
		model.PostStatusDeleted, site, authorID, model.PostStatusDeleted)
	return res.RowsAffected, res.Error
}

func DeleteAuthorReactionsTx(tx *gorm.DB, site string, authorID int64) (int64, error) {
	res := tx.Exec(`
		DELETE FROM community_reaction AS r
		 USING community_post AS p, community_thread AS t
		 WHERE r.post_id = p.id
		   AND p.thread_id = t.id
		   AND t.site = ?
		   AND r.user_id = ?`,
		site, authorID)
	return res.RowsAffected, res.Error
}

// DeleteAuthorThreadUsersTx drops the author's read/subscription rows on this
// site. They record which threads a person opened and how far they read, so a
// compliance purge that left them behind would keep exactly the kind of trace
// it exists to remove.
func DeleteAuthorThreadUsersTx(tx *gorm.DB, site string, userID int64) (int64, error) {
	res := tx.Exec(`
		DELETE FROM community_thread_user AS tu
		 USING community_thread AS t
		 WHERE tu.thread_id = t.id
		   AND t.site = ?
		   AND tu.user_id = ?`,
		site, userID)
	return res.RowsAffected, res.Error
}
