package model

import (
	"time"

	"gorm.io/datatypes"
)

type CommunityAnchorPresentation struct {
	Site           string     `gorm:"primaryKey;column:site" json:"site"`
	AnchorKind     int16      `gorm:"primaryKey;autoIncrement:false;column:anchor_kind" json:"anchor_kind"`
	AnchorID       string     `gorm:"primaryKey;column:anchor_id" json:"anchor_id"`
	Title          string     `gorm:"not null;column:title" json:"title"`
	URL            string     `gorm:"not null;column:url" json:"url"`
	WorkID         *int64     `gorm:"column:work_id" json:"work_id"`
	CoverImageHash *string    `gorm:"column:cover_image_hash" json:"cover_image_hash"`
	ContentLimit   int16      `gorm:"not null;column:content_limit" json:"content_limit"`
	Revision       int64      `gorm:"not null;column:revision" json:"revision"`
	RemovedAt      *time.Time `gorm:"column:removed_at" json:"removed_at"`
	CreatedAt      time.Time  `gorm:"not null;column:created_at;autoCreateTime:false" json:"created_at"`
	UpdatedAt      time.Time  `gorm:"not null;column:updated_at;autoUpdateTime:false" json:"updated_at"`
}

func (CommunityAnchorPresentation) TableName() string { return "community_anchor_presentation" }

type CommunityActivitySite struct {
	Site        string         `gorm:"primaryKey;column:site" json:"site"`
	Enabled     bool           `gorm:"not null;column:enabled" json:"enabled"`
	ThreadURL   string         `gorm:"not null;column:thread_url" json:"thread_url"`
	Rules       datatypes.JSON `gorm:"type:jsonb;not null;column:rules" json:"rules"`
	NotifyAfter time.Time      `gorm:"not null;column:notify_after" json:"notify_after"`
	UpdatedAt   time.Time      `gorm:"not null;column:updated_at;autoUpdateTime:false" json:"updated_at"`
}

func (CommunityActivitySite) TableName() string { return "community_activity_site" }

type CommunityActivityProjection struct {
	PostID     int64     `gorm:"primaryKey;autoIncrement:false;column:post_id" json:"post_id"`
	EnqueuedAt time.Time `gorm:"not null;column:enqueued_at" json:"enqueued_at"`
}

func (CommunityActivityProjection) TableName() string { return "community_activity_projection" }
