package model

import "time"

type CommunityThreadUser struct {
	ThreadID           int64      `gorm:"primaryKey;autoIncrement:false;column:thread_id" json:"thread_id"`
	UserID             int64      `gorm:"primaryKey;autoIncrement:false;index:idx_community_thread_user_user;column:user_id" json:"user_id"`
	LastReadPostNumber int32      `gorm:"not null;default:0;column:last_read_post_number" json:"last_read_post_number"`
	NotificationLevel  int16      `gorm:"not null;column:notification_level" json:"notification_level"`
	LastVisitedAt      *time.Time `gorm:"column:last_visited_at" json:"last_visited_at"`
}

func (CommunityThreadUser) TableName() string { return "community_thread_user" }
