package repository

import (
	"time"

	"api/internal/platform/community/model"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PostRepository struct{ db *gorm.DB }

func NewPostRepository(db *gorm.DB) *PostRepository { return &PostRepository{db: db} }

func (r *PostRepository) ListByThread(threadID int64, afterNumber int32, limit int) ([]model.CommunityPost, error) {
	var rows []model.CommunityPost
	err := r.db.
		Where("thread_id = ? AND post_number > ?", threadID, afterNumber).
		Order("post_number ASC").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

func (r *PostRepository) CountByAuthorSince(authorID int64, since time.Time) (int64, error) {
	var n int64
	err := r.db.Model(&model.CommunityPost{}).
		Where("author_id = ? AND created_at >= ?", authorID, since).
		Count(&n).Error
	return n, err
}

func CreatePostTx(tx *gorm.DB, p *model.CommunityPost) error {
	return tx.Create(p).Error
}

func GetPostTx(tx *gorm.DB, id int64) (*model.CommunityPost, error) {
	var p model.CommunityPost
	err := tx.First(&p, id).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func UpdatePostContentTx(tx *gorm.DB, postID int64, raw, html string, version int32, editedAt time.Time, editedByModerator bool) error {
	return tx.Model(&model.CommunityPost{}).Where("id = ?", postID).Updates(map[string]any{
		"content_raw":         raw,
		"content_html":        html,
		"sanitizer_version":   version,
		"edited_at":           editedAt,
		"edited_by_moderator": editedByModerator,
	}).Error
}

func AuthorHasPostedTx(tx *gorm.DB, threadID, authorID int64) (bool, error) {
	var n int64
	if err := tx.Model(&model.CommunityPost{}).
		Where("thread_id = ? AND author_id = ?", threadID, authorID).
		Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

// A tombstone goes through TombstonePostTx: restore decides by who removed it.
func SetPostStatusTx(tx *gorm.DB, postID int64, status int16) error {
	return tx.Model(&model.CommunityPost{}).Where("id = ?", postID).Updates(map[string]any{
		"status": status, "delete_source": model.DeleteSourceUnknown, "deleted_by": nil,
		"deleted_site": nil, "deleted_from_status": model.PostStatusVisible,
	}).Error
}

type Removal struct {
	Source int16
	By     *int64
	Site   *string
}

func TombstonePostTx(tx *gorm.DB, postID int64, r Removal) error {
	return tx.Model(&model.CommunityPost{}).Where("id = ?", postID).Updates(map[string]any{
		"status": model.PostStatusDeleted, "delete_source": r.Source, "deleted_by": r.By,
		"deleted_site": r.Site, "deleted_from_status": gorm.Expr("status"),
	}).Error
}

func LockPostTx(tx *gorm.DB, id int64) (*model.CommunityPost, error) {
	var p model.CommunityPost
	err := tx.Clauses(clause.Locking{Strength: LockUpdate}).First(&p, id).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func SetMentionedUsersTx(tx *gorm.DB, postID int64, ids datatypes.JSON) error {
	return tx.Model(&model.CommunityPost{}).Where("id = ?", postID).
		Update("mentioned_user_ids", ids).Error
}
