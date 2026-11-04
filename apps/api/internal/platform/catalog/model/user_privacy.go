package model

import "time"

type CatalogUserPrivacy struct {
	ActorUID              int64     `gorm:"column:actor_uid;primaryKey;autoIncrement:false" json:"actor_uid"`
	PlayRecordsPrivate    bool      `gorm:"not null" json:"play_records_private"`
	BrowsingHistoryOff    bool      `gorm:"not null;default:false" json:"browsing_history_off"`
	BrowsingHistoryShared bool      `gorm:"not null;default:false" json:"browsing_history_shared"`
	UpdatedAt             time.Time `json:"updated_at"`
}

func (CatalogUserPrivacy) TableName() string { return "catalog_user_privacy" }
