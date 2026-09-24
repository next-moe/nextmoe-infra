package repository

import (
	"time"

	"api/internal/platform/community/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TrustRepository struct{ db *gorm.DB }

func NewTrustRepository(db *gorm.DB) *TrustRepository { return &TrustRepository{db: db} }

func (r *TrustRepository) GetTrust(userID int64) (*model.CommunityTrust, error) {
	var t model.CommunityTrust
	err := r.db.Where("user_id = ?", userID).First(&t).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func GetOrCreateTrustTx(tx *gorm.DB, userID int64) (*model.CommunityTrust, error) {
	var t model.CommunityTrust
	err := tx.Where("user_id = ?", userID).First(&t).Error
	if err == nil {
		return &t, nil
	}
	if err != gorm.ErrRecordNotFound {
		return nil, err
	}
	t = model.CommunityTrust{UserID: userID, Level: model.TrustLevelNew}
	if err := tx.Create(&t).Error; err != nil {
		if reread := tx.Where("user_id = ?", userID).First(&t).Error; reread == nil {
			return &t, nil
		}
		return nil, err
	}
	return &t, nil
}

func GetTrustTx(tx *gorm.DB, userID int64) (*model.CommunityTrust, error) {
	var t model.CommunityTrust
	err := tx.Where("user_id = ?", userID).First(&t).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

type MeteringDelta struct {
	TopicsEntered int32
	PostsRead     int32
	ReadTimeS     int32
	DaysVisited   int32
}

func ApplyMeteringTx(tx *gorm.DB, userID int64, d MeteringDelta) error {
	return tx.Model(&model.CommunityTrust{}).Where("user_id = ?", userID).Updates(map[string]any{
		"topics_entered": gorm.Expr("COALESCE(topics_entered, 0) + ?", d.TopicsEntered),
		"posts_read":     gorm.Expr("COALESCE(posts_read, 0) + ?", d.PostsRead),
		"read_time_s":    gorm.Expr("COALESCE(read_time_s, 0) + ?", d.ReadTimeS),
		"days_visited":   gorm.Expr("COALESCE(days_visited, 0) + ?", d.DaysVisited),
		"updated_at":     time.Now(),
	}).Error
}

func AdjustLikesTx(tx *gorm.DB, userID int64, given, received int32) error {
	return tx.Model(&model.CommunityTrust{}).Where("user_id = ?", userID).Updates(map[string]any{
		"likes_given":    gorm.Expr("GREATEST(COALESCE(likes_given, 0) + ?, 0)", given),
		"likes_received": gorm.Expr("GREATEST(COALESCE(likes_received, 0) + ?, 0)", received),
		"updated_at":     time.Now(),
	}).Error
}

func SetLevelTx(tx *gorm.DB, userID int64, level int16) error {
	return tx.Model(&model.CommunityTrust{}).Where("user_id = ?", userID).
		Updates(map[string]any{"level": level, "updated_at": time.Now()}).Error
}

func SetBoostTx(tx *gorm.DB, userID int64, boost int16) error {
	return tx.Model(&model.CommunityTrust{}).Where("user_id = ?", userID).
		Updates(map[string]any{"granted_boost": boost, "updated_at": time.Now()}).Error
}

func IncFlagAccuracyTx(tx *gorm.DB, userID int64, agreed bool) error {
	col := "flags_disagreed"
	if agreed {
		col = "flags_agreed"
	}
	return tx.Model(&model.CommunityTrust{}).Where("user_id = ?", userID).
		Update(col, gorm.Expr("COALESCE("+col+", 0) + 1")).Error
}

func DecrementHoldTx(tx *gorm.DB, userID int64) error {
	return tx.Model(&model.CommunityTrust{}).
		Where("user_id = ? AND first_posts_held_remaining > 0", userID).
		UpdateColumn("first_posts_held_remaining", gorm.Expr("first_posts_held_remaining - 1")).Error
}

func ClearHoldsTx(tx *gorm.DB, userID int64) error {
	return tx.Model(&model.CommunityTrust{}).Where("user_id = ?", userID).
		UpdateColumn("first_posts_held_remaining", 0).Error
}

func AddReactionTx(tx *gorm.DB, postID, userID int64, kind int16) (bool, error) {
	res := tx.Clauses(clause.OnConflict{DoNothing: true}).
		Create(&model.CommunityReaction{PostID: postID, UserID: userID, Kind: kind})
	return res.RowsAffected > 0, res.Error
}

func RemoveReactionTx(tx *gorm.DB, postID, userID int64, kind int16) (bool, error) {
	res := tx.Where("post_id = ? AND user_id = ? AND kind = ?", postID, userID, kind).
		Delete(&model.CommunityReaction{})
	return res.RowsAffected > 0, res.Error
}

func ToggleReactionTx(tx *gorm.DB, postID, userID int64, kind int16) (bool, error) {
	res := tx.Where("post_id = ? AND user_id = ? AND kind = ?", postID, userID, kind).
		Delete(&model.CommunityReaction{})
	if res.Error != nil {
		return false, res.Error
	}
	if res.RowsAffected > 0 {
		return false, nil
	}
	if err := tx.Create(&model.CommunityReaction{PostID: postID, UserID: userID, Kind: kind}).Error; err != nil {
		return false, err
	}
	return true, nil
}
