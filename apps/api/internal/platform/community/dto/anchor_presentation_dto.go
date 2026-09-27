package dto

import "time"

type AnchorPresentationItem struct {
	AnchorKind     int16  `json:"anchor_kind" enum:"1,2" doc:"1 = site_game, 2 = site_resource: the anchor this site's comment walls hang on"`
	AnchorID       string `json:"anchor_id" doc:"the anchor id exactly as the site sends it on POST /comments, e.g. 123 or rating:45; 1-128 characters"`
	Revision       int64  `json:"revision" doc:"unix microseconds at which the site read the state it is sending; a write whose revision is not greater than the stored one is ignored (stale)"`
	Removed        bool   `json:"removed,omitempty" doc:"true: the page is gone or no longer public, and every comment on it leaves the feed. Needs only anchor_kind, anchor_id and revision"`
	Title          string `json:"title,omitempty" doc:"the page's display name, e.g. the game's title; at most 200 characters. Becomes the title of every comment activity on the anchor"`
	URL            string `json:"url,omitempty" doc:"the page's canonical absolute https URL on one of the calling client's registered redirect hosts"`
	WorkID         *int64 `json:"work_id,omitempty" doc:"the catalog work the page is about"`
	CoverImageHash string `json:"cover_image_hash,omitempty" doc:"image service hash (64 lowercase hex) of the page's cover; every comment activity on the anchor carries it"`
	ContentLimit   string `json:"content_limit,omitempty" doc:"sfw | nsfw, the site's judgement of the page; missing = nsfw. A comment is nsfw when its page or its own rating is"`
}

type AnchorPresentationWriteRequest struct {
	Items []AnchorPresentationItem `json:"items" minItems:"1" maxItems:"100"`
}

type AnchorPresentationOutcome struct {
	AnchorKind int16  `json:"anchor_kind"`
	AnchorID   string `json:"anchor_id"`
	Outcome    string `json:"outcome" enum:"created,updated,removed,restored,stale,invalid"`
	Reason     string `json:"reason,omitempty" doc:"why an item is invalid"`
}

type AnchorPresentationWriteResponse struct {
	Results []AnchorPresentationOutcome `json:"results" doc:"one per item, in request order"`
}

type AnchorPresentationView struct {
	AnchorKind     int16      `json:"anchor_kind"`
	AnchorID       string     `json:"anchor_id"`
	Title          string     `json:"title"`
	URL            string     `json:"url"`
	WorkID         *int64     `json:"work_id"`
	CoverImageHash *string    `json:"cover_image_hash"`
	ContentLimit   string     `json:"content_limit" enum:"sfw,nsfw"`
	Revision       int64      `json:"revision"`
	Removed        bool       `json:"removed" doc:"a tombstone; its title, url and work_id are cleared"`
	UpdatedAt      time.Time  `json:"updated_at"`
	RemovedAt      *time.Time `json:"removed_at"`
}

type AnchorPresentationListResponse struct {
	Presentations []AnchorPresentationView `json:"presentations"`
	NextCursor    string                   `json:"next_cursor,omitempty" doc:"pass as cursor for the next page; empty = last page"`
}
