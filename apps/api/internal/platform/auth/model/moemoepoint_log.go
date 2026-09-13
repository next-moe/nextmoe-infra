package model

import "time"

const (
	MoemoepointReasonAdminGrant      = "admin_grant"
	MoemoepointReasonAdminDeduct     = "admin_deduct"
	MoemoepointReasonMigration       = "migration"
	MoemoepointReasonContentApproved = "content_approved"
	MoemoepointReasonContentRemoved  = "content_removed"
	MoemoepointReasonDailyCheckin    = "daily_checkin"
	MoemoepointReasonLiked           = "liked"
	MoemoepointReasonRegisterGift    = "register_gift"
	MoemoepointReasonNameChange      = "name_change"
)

func IsValidMoemoepointReason(r string) bool {
	switch r {
	case MoemoepointReasonAdminGrant, MoemoepointReasonAdminDeduct,
		MoemoepointReasonMigration, MoemoepointReasonContentApproved,
		MoemoepointReasonContentRemoved, MoemoepointReasonDailyCheckin,
		MoemoepointReasonLiked, MoemoepointReasonRegisterGift,
		MoemoepointReasonNameChange:
		return true
	}
	return false
}

type MoemoepointLog struct {
	ID             int64     `gorm:"primaryKey" json:"id"`
	UserID         uint      `gorm:"not null;index" json:"user_id"`
	Delta          int       `gorm:"not null" json:"delta"`
	Reason         string    `gorm:"size:40;not null;index" json:"reason"`
	SourceApp      string    `gorm:"size:32;not null" json:"source_app"`
	Ref            string    `gorm:"size:80;default:''" json:"ref"`
	ActorUserID    uint      `gorm:"not null;default:0" json:"actor_user_id"`
	IdempotencyKey string    `gorm:"size:128;not null;uniqueIndex" json:"-"`
	Note           string    `gorm:"size:255;default:''" json:"note"`
	CreatedAt      time.Time `json:"created_at"`
}

func (MoemoepointLog) TableName() string {
	return "moemoepoint_log"
}
