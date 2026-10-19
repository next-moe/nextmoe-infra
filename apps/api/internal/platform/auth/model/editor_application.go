package model

import "time"

const (
	EditorAppPending  = "pending"
	EditorAppApproved = "approved"
	EditorAppDeclined = "declined"
)

type EditorApplication struct {
	ID            uint       `gorm:"primaryKey" json:"id"`
	UserID        uint       `gorm:"not null;index" json:"user_id"`
	Status        string     `gorm:"size:16;not null;default:'pending'" json:"status"`
	Reason        string     `gorm:"type:text;not null;default:''" json:"reason"`
	ReviewerID    *uint      `gorm:"column:reviewer_id" json:"reviewer_id,omitempty"`
	ReviewedAt    *time.Time `json:"reviewed_at,omitempty"`
	DeclineReason string     `gorm:"type:text;not null;default:''" json:"decline_reason"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

func (EditorApplication) TableName() string { return "editor_applications" }
