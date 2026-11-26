package repository

import (
	"fmt"
	"time"

	"api/internal/platform/community/model"

	"gorm.io/gorm"
)

type WallSort string

const (
	WallSortOldest WallSort = "oldest"
	WallSortNewest WallSort = "newest"
	WallSortTop    WallSort = "top"
)

type WallCursor struct {
	Likes      int32
	PostNumber int32
}

type WallQuery struct {
	ThreadID  int64
	Sort      WallSort
	RootsOnly bool
	Cursor    WallCursor
	Limit     int
}

type WallRow struct {
	model.CommunityPost
	Likes int32 `gorm:"column:likes"`
}

func (r *PostRepository) ListWall(q WallQuery) ([]WallRow, error) {
	roots := ""
	if q.RootsOnly {
		roots = " AND p.root_post_id IS NULL"
	}
	var rows []WallRow
	switch q.Sort {
	case WallSortOldest, WallSortNewest:
		op, dir := ">", "ASC"
		if q.Sort == WallSortNewest {
			op, dir = "<", "DESC"
		}
		where, args := "p.thread_id = ?"+roots, []any{q.ThreadID}
		if q.Cursor.PostNumber > 0 {
			where += " AND p.post_number " + op + " ?"
			args = append(args, q.Cursor.PostNumber)
		}
		err := r.db.Raw(fmt.Sprintf(`
			SELECT p.*, 0 AS likes FROM community_post AS p
			 WHERE %s ORDER BY p.post_number %s LIMIT ?`, where, dir), append(args, q.Limit)...).
			Scan(&rows).Error
		return rows, err
	case WallSortTop:
		where, args := "", []any{model.ReactionKindLike, q.ThreadID}
		if q.Cursor.PostNumber > 0 {
			where = " WHERE (w.likes, w.post_number) < (?, ?)"
			args = append(args, q.Cursor.Likes, q.Cursor.PostNumber)
		}
		err := r.db.Raw(`
			SELECT w.* FROM (
			    SELECT p.*, l.n AS likes FROM community_post AS p
			      CROSS JOIN LATERAL (SELECT count(*)::int AS n FROM community_reaction AS r
			                           WHERE r.post_id = p.id AND r.kind = ?) AS l
			     WHERE p.thread_id = ?`+roots+`) AS w`+where+`
			 ORDER BY w.likes DESC, w.post_number DESC LIMIT ?`, append(args, q.Limit)...).
			Scan(&rows).Error
		return rows, err
	}
	return nil, fmt.Errorf("unknown wall sort %q", q.Sort)
}

func (r *PostRepository) VisibleReplyCounts(threadID int64, rootIDs []int64) (map[int64]int32, error) {
	out := make(map[int64]int32, len(rootIDs))
	if len(rootIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		RootPostID int64
		N          int32
	}
	err := r.db.Model(&model.CommunityPost{}).
		Select("root_post_id, count(*) AS n").
		Where("thread_id = ? AND root_post_id IN ? AND status = ?", threadID, rootIDs, model.PostStatusVisible).
		Group("root_post_id").Scan(&rows).Error
	for _, row := range rows {
		out[row.RootPostID] = row.N
	}
	return out, err
}

func (r *PostRepository) ReplyPreviews(threadID int64, rootIDs []int64, n int) ([]model.CommunityPost, error) {
	if len(rootIDs) == 0 || n <= 0 {
		return nil, nil
	}
	var rows []model.CommunityPost
	err := r.db.Raw(`
		SELECT x.* FROM (
		    SELECT p.*, row_number() OVER (PARTITION BY p.root_post_id ORDER BY p.post_number) AS rn
		      FROM community_post AS p
		     WHERE p.thread_id = ? AND p.root_post_id IN ?) AS x
		 WHERE x.rn <= ? ORDER BY x.root_post_id, x.post_number`, threadID, rootIDs, n).
		Scan(&rows).Error
	return rows, err
}

func (r *PostRepository) ListReplies(threadID, rootID int64, afterNumber int32, limit int) ([]model.CommunityPost, error) {
	var rows []model.CommunityPost
	err := r.db.
		Where("thread_id = ? AND root_post_id = ? AND post_number > ?", threadID, rootID, afterNumber).
		Order("post_number ASC").Limit(limit).Find(&rows).Error
	return rows, err
}

type PinnedPost struct {
	model.CommunityPost
	PinnedAt time.Time `gorm:"column:pinned_at"`
}

func PinnedPosts(db *gorm.DB, site string, threadID int64) ([]PinnedPost, error) {
	var rows []PinnedPost
	err := db.Raw(`
		SELECT p.*, pin.pinned_at FROM community_post_pin AS pin
		  JOIN community_post AS p ON p.id = pin.post_id
		 WHERE pin.site = ? AND p.thread_id = ? AND p.status = ?
		 ORDER BY pin.pinned_at DESC, p.id DESC LIMIT ?`,
		site, threadID, model.PostStatusVisible, model.PostPinLimit).Scan(&rows).Error
	return rows, err
}

func GetPostPinTx(tx *gorm.DB, site string, postID int64) (*model.CommunityPostPin, error) {
	var pin model.CommunityPostPin
	err := tx.Where("site = ? AND post_id = ?", site, postID).Take(&pin).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &pin, nil
}

func CreatePostPinTx(tx *gorm.DB, pin *model.CommunityPostPin) error {
	return tx.Create(pin).Error
}

func DeletePostPinTx(tx *gorm.DB, site string, postID int64) (bool, error) {
	res := tx.Where("site = ? AND post_id = ?", site, postID).Delete(&model.CommunityPostPin{})
	return res.RowsAffected > 0, res.Error
}
