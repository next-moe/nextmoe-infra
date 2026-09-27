package repository

import (
	"time"

	"api/internal/platform/community/model"

	"gorm.io/gorm"
)

func GetActivitySetting(db *gorm.DB, userID int64) (*model.CommunityActivitySetting, error) {
	var rows []model.CommunityActivitySetting
	if err := db.Where("user_id = ?", userID).Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

func ActivitiesHidden(db *gorm.DB, userID int64) (bool, error) {
	s, err := GetActivitySetting(db, userID)
	return s != nil && s.Hidden, err
}

func UpsertActivitySettingTx(tx *gorm.DB, userID int64, hidden bool) (time.Time, error) {
	var at time.Time
	err := tx.Raw(`
		INSERT INTO community_activity_setting (user_id, hidden, updated_at)
		VALUES (?, ?, now())
		ON CONFLICT (user_id) DO UPDATE SET hidden = EXCLUDED.hidden, updated_at = now()
		RETURNING updated_at`, userID, hidden).Scan(&at).Error
	return at, err
}

func RetractFolloweeActivityTx(tx *gorm.DB, actorID int64) (int64, error) {
	res := tx.Exec(`
		UPDATE community_notification SET
		    item_count = 0, activity_id = NULL, read_at = COALESCE(read_at, now()),
		    seq = nextval('community_notification_seq'), updated_at = now()
		 WHERE kind = ? AND actor_id = ? AND item_count > 0`,
		model.NotificationKindFolloweeActivity, actorID)
	return res.RowsAffected, res.Error
}

func DeleteActivitySetting(db *gorm.DB, userID int64) error {
	return db.Where("user_id = ?", userID).Delete(&model.CommunityActivitySetting{}).Error
}
