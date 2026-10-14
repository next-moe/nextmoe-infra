package dto

import "time"

type AdminArtifactRow struct {
	UUID           string    `json:"uuid"`
	Name           string    `json:"name"`
	FileKey        string    `json:"file_key"`
	FileSize       int64     `json:"file_size"`
	MimeType       string    `json:"mime_type"`
	SiteKey        string    `json:"site_key"`
	Status         string    `json:"status" enum:"uploading,ready,failed" doc:"Upload lifecycle status"`
	Public         bool      `json:"public"`
	UploaderSub    string    `json:"uploader_sub"`
	UploaderClient string    `json:"uploader_client"`
	Checksum       string    `json:"checksum"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type AdminArtifactList struct {
	Items []AdminArtifactRow `json:"items"`
	Total int64              `json:"total"`
	Page  int                `json:"page"`
	Limit int                `json:"limit"`
}

type AdminArtifactSiteStats struct {
	Count int64 `json:"count"`
	Bytes int64 `json:"bytes"`
}

type AdminArtifactStats struct {
	TotalCount  int64                             `json:"total_count"`
	TotalBytes  int64                             `json:"total_bytes"`
	Uploading   int64                             `json:"uploading"`
	Failed      int64                             `json:"failed"`
	SoftDeleted int64                             `json:"soft_deleted"`
	BySite      map[string]AdminArtifactSiteStats `json:"by_site,omitempty"`
}

type AdminArtifactDelete struct {
	UUID              string `json:"uuid"`
	SoftDeleted       bool   `json:"soft_deleted"`
	CompositesDeleted int    `json:"composites_deleted" doc:"Composites that served this artifact's bytes, deleted with it"`
}

type AdminArtifactReclaim struct {
	UUID      string `json:"uuid"`
	Reclaimed bool   `json:"reclaimed"`
}
