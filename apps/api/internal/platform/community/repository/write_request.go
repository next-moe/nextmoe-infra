package repository

import (
	"time"

	"api/internal/platform/community/model"

	"gorm.io/gorm"
)

func ClaimWriteRequestTx(tx *gorm.DB, site, key, hash string) (int64, error) {
	var id int64
	err := tx.Raw(`
		INSERT INTO community_write_request (site, idempotency_key, request_hash, created_at)
		VALUES (?, ?, ?, now())
		ON CONFLICT (site, idempotency_key) DO NOTHING
		RETURNING id`, site, key, hash).Scan(&id).Error
	return id, err
}

func BindWriteRequestTx(tx *gorm.DB, id, postID int64) error {
	return tx.Model(&model.CommunityWriteRequest{}).Where("id = ?", id).Update("post_id", postID).Error
}

func FindWriteRequest(db *gorm.DB, site, key string) (*model.CommunityWriteRequest, error) {
	var r model.CommunityWriteRequest
	err := db.Where("site = ? AND idempotency_key = ?", site, key).Take(&r).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func PruneWriteRequests(db *gorm.DB, olderThan time.Duration) (int64, error) {
	res := db.Where("created_at < ?", time.Now().Add(-olderThan)).Delete(&model.CommunityWriteRequest{})
	return res.RowsAffected, res.Error
}
