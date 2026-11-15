package model

import (
	"time"

	"gorm.io/datatypes"
)

// CatalogActivityItem is the feed item a user's catalog action should have in
// community now, one row per community key. The pusher sends a row whenever
// revision is ahead of pushed_revision.
type CatalogActivityItem struct {
	Key            string         `gorm:"primaryKey;column:key" json:"key"`
	ActorUID       int64          `gorm:"not null;index:idx_catalog_activity_item_actor,priority:1;column:actor_uid" json:"actor_uid"`
	WorkID         *int64         `gorm:"index:idx_catalog_activity_item_actor,priority:2;column:work_id" json:"work_id"`
	FolderID       *int64         `gorm:"index;column:folder_id" json:"folder_id"`
	Item           datatypes.JSON `gorm:"type:jsonb;not null;column:item" json:"item"`
	Baseline       *int           `gorm:"column:baseline" json:"baseline"`
	Removed        bool           `gorm:"not null;column:removed" json:"removed"`
	Revision       int64          `gorm:"not null;column:revision" json:"revision"`
	PushedRevision int64          `gorm:"not null;default:0;column:pushed_revision" json:"pushed_revision"`
	UpdatedAt      time.Time      `gorm:"not null;column:updated_at;autoUpdateTime:false" json:"updated_at"`
}

func (CatalogActivityItem) TableName() string { return "catalog_activity_item" }
