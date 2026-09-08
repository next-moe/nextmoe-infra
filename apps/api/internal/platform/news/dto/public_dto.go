package dto

import "time"

type PublicNewsSource struct {
	Key          string `json:"key"`
	DisplayName  string `json:"display_name"`
	HomepageURL  string `json:"homepage_url"`
	Attribution  string `json:"attribution"`
	ColumnURL    string `json:"column_url"`
	PublisherUID int64  `json:"publisher_uid"`
}

// PublicNewsItem carries its Source and SourceURL on EVERY item rather than once
// per envelope, and neither is omitempty. Attribution was the only condition
// Galgame 批评 attached, and click-through to the original was 月幕's; a consumer
// that renders a single item, or mixes sources into one stream, must still be
// able to honour both from the item alone.
type PublicNewsItem struct {
	ID          int64            `json:"id"`
	Source      PublicNewsSource `json:"source"`
	Lane        string           `json:"lane"`
	SourceURL   string           `json:"source_url"`
	Title       string           `json:"title"`
	Preview     string           `json:"preview"`
	BannerURL   string           `json:"banner_url,omitempty"`
	BannerHash  string           `json:"banner_hash,omitempty"`
	Images      []string         `json:"images,omitempty"`
	PublishedAt time.Time        `json:"published_at"`
	WorkIDs     []int64          `json:"work_ids,omitempty"`
}

type PublicNewsFeedData struct {
	Items      []PublicNewsItem `json:"items"`
	Count      int64            `json:"count"`
	NextCursor *string          `json:"next_cursor,omitempty"`
}

type PublicNewsSourcesData struct {
	Sources []PublicNewsSource `json:"sources"`
}
