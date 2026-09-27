package repository

import (
	"fmt"

	"api/internal/platform/community/model"

	"gorm.io/gorm"
)

func InsertBlock(db *gorm.DB, site string, blockerID, blockedID int64) (int64, error) {
	var id int64
	err := db.Raw(`
		INSERT INTO community_user_block (blocker_id, blocked_id, origin_site, created_at)
		VALUES (?, ?, ?, now())
		ON CONFLICT (blocker_id, blocked_id) DO NOTHING
		RETURNING id`, blockerID, blockedID, site).Scan(&id).Error
	return id, err
}

func DeleteBlock(db *gorm.DB, blockerID, blockedID int64) (bool, error) {
	res := db.Where("blocker_id = ? AND blocked_id = ?", blockerID, blockedID).
		Delete(&model.CommunityUserBlock{})
	return res.RowsAffected > 0, res.Error
}

func CountBlocking(db *gorm.DB, blockerID int64) (int64, error) {
	var n int64
	err := db.Model(&model.CommunityUserBlock{}).Where("blocker_id = ?", blockerID).Count(&n).Error
	return n, err
}

func ListBlocking(db *gorm.DB, blockerID, beforeID int64, limit int) ([]model.CommunityUserBlock, error) {
	q := db.Model(&model.CommunityUserBlock{}).Where("blocker_id = ?", blockerID)
	if beforeID > 0 {
		q = q.Where("id < ?", beforeID)
	}
	var rows []model.CommunityUserBlock
	err := q.Order("id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

// BlockedEitherWay reports whether a blocks b or b blocks a.
func BlockedEitherWay(db *gorm.DB, a, b int64) (bool, error) {
	var n int64
	err := db.Model(&model.CommunityUserBlock{}).
		Where("(blocker_id = ? AND blocked_id = ?) OR (blocker_id = ? AND blocked_id = ?)", a, b, b, a).
		Count(&n).Error
	return n > 0, err
}

// BlockedAmong returns which of userIDs the blocker has blocked.
func BlockedAmong(db *gorm.DB, blockerID int64, userIDs []int64) ([]int64, error) {
	var ids []int64
	err := db.Model(&model.CommunityUserBlock{}).
		Where("blocker_id = ? AND blocked_id IN ?", blockerID, userIDs).
		Pluck("blocked_id", &ids).Error
	return ids, err
}

// BlockersAmong returns which of userIDs have blocked blockedID.
func BlockersAmong(db *gorm.DB, blockedID int64, userIDs []int64) ([]int64, error) {
	var ids []int64
	err := db.Model(&model.CommunityUserBlock{}).
		Where("blocked_id = ? AND blocker_id IN ?", blockedID, userIDs).
		Pluck("blocker_id", &ids).Error
	return ids, err
}

func DeleteUserBlocks(db *gorm.DB, uid int64) error {
	return db.Where("blocker_id = ? OR blocked_id = ?", uid, uid).
		Delete(&model.CommunityUserBlock{}).Error
}

// LockUserPairTx serializes a follow and a block between the same two users.
// Without it a follow could check "no block" while the block's transaction
// deletes the (not yet visible) follow edge, and both would commit.
func LockUserPairTx(tx *gorm.DB, a, b int64) error {
	if a > b {
		a, b = b, a
	}
	return tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`,
		fmt.Sprintf("community:user-pair:%d:%d", a, b)).Error
}
