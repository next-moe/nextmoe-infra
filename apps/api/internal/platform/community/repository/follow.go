package repository

import (
	"api/internal/platform/community/model"

	"gorm.io/gorm"
)

func InsertFollow(db *gorm.DB, site string, followerID, followeeID int64) (int64, error) {
	var id int64
	err := db.Raw(`
		INSERT INTO community_user_follow (follower_id, followee_id, origin_site, created_at)
		VALUES (?, ?, ?, now())
		ON CONFLICT (follower_id, followee_id) DO NOTHING
		RETURNING id`, followerID, followeeID, site).Scan(&id).Error
	return id, err
}

func CountFollowing(db *gorm.DB, followerID int64) (int64, error) {
	var n int64
	err := db.Model(&model.CommunityUserFollow{}).Where("follower_id = ?", followerID).Count(&n).Error
	return n, err
}

func DeleteFollow(db *gorm.DB, followerID, followeeID int64) (bool, error) {
	res := db.Where("follower_id = ? AND followee_id = ?", followerID, followeeID).
		Delete(&model.CommunityUserFollow{})
	return res.RowsAffected > 0, res.Error
}

func GetFollowTx(tx *gorm.DB, followerID, followeeID int64) (*model.CommunityUserFollow, error) {
	var row model.CommunityUserFollow
	err := tx.Where("follower_id = ? AND followee_id = ?", followerID, followeeID).
		Take(&row).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func ListNotifiedFollowerIDsTx(tx *gorm.DB, followeeID int64) ([]int64, error) {
	var ids []int64
	err := tx.Model(&model.CommunityUserFollow{}).
		Where("followee_id = ? AND notify_level = ?", followeeID, model.FollowNotifyAll).
		Pluck("follower_id", &ids).Error
	return ids, err
}

func GetFollowNotifyLevel(db *gorm.DB, followerID, followeeID int64) (int16, bool, error) {
	var levels []int16
	err := db.Model(&model.CommunityUserFollow{}).
		Where("follower_id = ? AND followee_id = ?", followerID, followeeID).
		Pluck("notify_level", &levels).Error
	if err != nil || len(levels) == 0 {
		return 0, false, err
	}
	return levels[0], true, nil
}

func UpdateFollowNotifyLevel(db *gorm.DB, followerID, followeeID int64, level int16) (bool, error) {
	res := db.Model(&model.CommunityUserFollow{}).
		Where("follower_id = ? AND followee_id = ?", followerID, followeeID).
		Update("notify_level", level)
	return res.RowsAffected > 0, res.Error
}

func ListFollowers(db *gorm.DB, userID, beforeID int64, limit int) ([]model.CommunityUserFollow, error) {
	q := db.Model(&model.CommunityUserFollow{}).Where("followee_id = ?", userID)
	if beforeID > 0 {
		q = q.Where("id < ?", beforeID)
	}
	var rows []model.CommunityUserFollow
	err := q.Order("id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

func ListFollowing(db *gorm.DB, userID, beforeID int64, limit int) ([]model.CommunityUserFollow, error) {
	q := db.Model(&model.CommunityUserFollow{}).Where("follower_id = ?", userID)
	if beforeID > 0 {
		q = q.Where("id < ?", beforeID)
	}
	var rows []model.CommunityUserFollow
	err := q.Order("id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

func FollowerCounts(db *gorm.DB, userIDs []int64) (map[int64]int64, error) {
	return groupedFollowCounts(db, "followee_id", userIDs)
}

func FollowingCounts(db *gorm.DB, userIDs []int64) (map[int64]int64, error) {
	return groupedFollowCounts(db, "follower_id", userIDs)
}

func groupedFollowCounts(db *gorm.DB, column string, userIDs []int64) (map[int64]int64, error) {
	type row struct {
		UserID int64 `gorm:"column:user_id"`
		N      int64 `gorm:"column:n"`
	}
	var rows []row
	err := db.Model(&model.CommunityUserFollow{}).
		Select(column+" AS user_id, COUNT(*) AS n").
		Where(column+" IN ?", userIDs).
		Group(column).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[int64]int64, len(rows))
	for _, r := range rows {
		out[r.UserID] = r.N
	}
	return out, nil
}

func FollowingAmong(db *gorm.DB, followerID int64, userIDs []int64) (map[int64]int16, error) {
	var rows []model.CommunityUserFollow
	err := db.Select("followee_id", "notify_level").
		Where("follower_id = ? AND followee_id IN ?", followerID, userIDs).
		Find(&rows).Error
	out := make(map[int64]int16, len(rows))
	for _, r := range rows {
		out[r.FolloweeID] = r.NotifyLevel
	}
	return out, err
}

func FollowersAmong(db *gorm.DB, followeeID int64, userIDs []int64) ([]int64, error) {
	var ids []int64
	err := db.Model(&model.CommunityUserFollow{}).
		Where("followee_id = ? AND follower_id IN ?", followeeID, userIDs).
		Pluck("follower_id", &ids).Error
	return ids, err
}

func DeleteUserFollows(db *gorm.DB, uid int64) error {
	return db.Where("follower_id = ? OR followee_id = ?", uid, uid).
		Delete(&model.CommunityUserFollow{}).Error
}
