package model

import "time"

type ModerationQueue struct {
	ID         int64      `gorm:"primaryKey" json:"id"`
	Hash       string     `gorm:"type:char(64);not null;index" json:"hash"`
	Site       string     `gorm:"size:32;not null" json:"site"`
	Attempts   int        `gorm:"not null;default:0" json:"attempts"`
	LastError  string     `gorm:"type:text" json:"last_error,omitempty"`
	EnqueuedAt time.Time  `gorm:"not null;default:now();index" json:"enqueued_at"`
	PickupAt   *time.Time `gorm:"index" json:"pickup_at,omitempty"`
}

func (ModerationQueue) TableName() string {
	return "image_moderation_queue"
}
