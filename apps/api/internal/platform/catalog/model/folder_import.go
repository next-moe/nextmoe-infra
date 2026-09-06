package model

import "time"

const (
	FolderImportSiteForum = "forum"
	FolderImportSiteMoyu  = "moyu"
)

// CatalogUserFolderImport maps a source site's collection row to the catalog
// folder it was imported into. Without it a re-run cannot tell an already
// imported collection from a new one — (owner_uid, name) is not a key, three
// forum users hold two collections under one name — and the later read-cut and
// guarded retirement have nothing to diff the source against.
type CatalogUserFolderImport struct {
	Site      string    `gorm:"primaryKey;size:16" json:"site"`
	SourceID  int64     `gorm:"primaryKey;column:source_id" json:"source_id"`
	FolderID  int64     `gorm:"not null;index" json:"folder_id"`
	OwnerUID  int64     `gorm:"column:owner_uid;not null" json:"owner_uid"`
	CreatedAt time.Time `json:"created_at"`
}

func (CatalogUserFolderImport) TableName() string { return "catalog_user_folder_import" }
