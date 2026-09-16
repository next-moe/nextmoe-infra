package main

import (
	"time"

	"gorm.io/gorm"
)

// maxRunes is moyu's own comment ceiling, carried here only so the report can
// name rows the community write path would refuse on a later edit. Nothing is
// truncated: the import preserves what people wrote.
const maxRunes = 10007

type srcComment struct {
	ID         int        `gorm:"column:id"`
	GalgameID  int        `gorm:"column:galgame_id"`
	ResourceID *int       `gorm:"column:resource_id"`
	UserID     int        `gorm:"column:user_id"`
	Content    string     `gorm:"column:content"`
	ParentID   *int       `gorm:"column:parent_id"`
	Edit       string     `gorm:"column:edit"`
	Status     int        `gorm:"column:status"`
	Created    time.Time  `gorm:"column:created"`
	Updated    time.Time  `gorm:"column:updated"`
	EditedAt   *time.Time `gorm:"-"`
}

func loadComments(src *gorm.DB) ([]srcComment, int, error) {
	var rows []srcComment
	if err := src.Raw(`
		SELECT id, galgame_id, resource_id, user_id, content, parent_id, edit, status, created, updated
		  FROM patch_comment
		 ORDER BY created ASC, id ASC`).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	unparsable := 0
	for i := range rows {
		if rows[i].Edit == "" {
			continue
		}
		// `edit` is text, not a timestamp: moyu writes an RFC3339 string there.
		// A value this cannot read is an anomaly to report, never a reason to
		// drop a comment -- edited_at is a decoration, the post is the payload.
		t, err := time.Parse(time.RFC3339, rows[i].Edit)
		if err != nil {
			unparsable++
			continue
		}
		rows[i].EditedAt = &t
	}
	return rows, unparsable, nil
}

type srcLike struct {
	UserID    int       `gorm:"column:user_id"`
	CommentID int       `gorm:"column:comment_id"`
	Created   time.Time `gorm:"column:created"`
}

func loadLikes(src *gorm.DB) ([]srcLike, error) {
	var rows []srcLike
	err := src.Raw(`
		SELECT user_id, comment_id, created
		  FROM user_patch_comment_like_relation
		 ORDER BY comment_id ASC, user_id ASC`).Scan(&rows).Error
	return rows, err
}

// commentMap is the ledger, written back into moyu's own database: it is both
// this import's idempotency key and the only way a legacy `#comment-<id>` deep
// link can still find the post it became. moyu's migration 040 creates the same
// table with the same DDL, so either side may run first.
type commentMap struct {
	OldCommentID int   `gorm:"column:old_comment_id;primaryKey"`
	ThreadID     int64 `gorm:"column:thread_id"`
	PostID       int64 `gorm:"column:post_id"`
	GalgameID    int   `gorm:"column:galgame_id"`
	ResourceID   *int  `gorm:"column:resource_id"`
}

func (commentMap) TableName() string { return "patch_comment_community_map" }

const mapTableDDL = `
CREATE TABLE IF NOT EXISTS patch_comment_community_map (
  old_comment_id int    PRIMARY KEY,
  thread_id      bigint NOT NULL,
  post_id        bigint NOT NULL,
  galgame_id     int    NOT NULL,
  resource_id    int
)`

func loadExistingMap(src *gorm.DB) (map[int]int64, error) {
	out := make(map[int]int64)
	var reg *string
	if err := src.Raw("SELECT to_regclass('patch_comment_community_map')::text").Scan(&reg).Error; err != nil {
		return nil, err
	}
	if reg == nil {
		return out, nil
	}
	var rows []commentMap
	if err := src.Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.OldCommentID] = r.PostID
	}
	return out, nil
}
