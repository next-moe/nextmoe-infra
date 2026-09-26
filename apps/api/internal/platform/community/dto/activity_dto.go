package dto

import "time"

type ActivityWriteItem struct {
	Key            string     `json:"key" doc:"stable key of this activity within the site, e.g. topic:123; 1-128 of A-Z a-z 0-9 . _ : -"`
	ActorID        int64      `json:"actor_id" doc:"the author's NextMoe uid"`
	Revision       int64      `json:"revision" doc:"unix microseconds at which the site read the state it is sending; a write whose revision is not greater than the stored one is ignored (stale)"`
	Removed        bool       `json:"removed,omitempty" doc:"true sends a tombstone: the content was hidden, deleted, made non-public or its author purged. A tombstone needs only key, actor_id and revision"`
	Verb           string     `json:"verb,omitempty" doc:"publish | reply | comment | rate | like | edit"`
	ObjectKind     string     `json:"object_kind,omitempty" doc:"the site's own type name, 1-32 of a-z 0-9 _; community does not interpret it"`
	ObjectLabel    string     `json:"object_label,omitempty" doc:"display name of the type for any site to render, at most 16 characters, e.g. Galgame 资源"`
	Title          string     `json:"title,omitempty" doc:"at most 200 characters; the site truncates"`
	Excerpt        string     `json:"excerpt,omitempty" doc:"plain text, at most 300 characters"`
	URL            string     `json:"url,omitempty" doc:"absolute https URL on one of the calling client's registered redirect hosts"`
	CoverImageHash string     `json:"cover_image_hash,omitempty" doc:"image service hash (64 lowercase hex)"`
	WorkID         *int64     `json:"work_id,omitempty" doc:"the catalog work this activity is about"`
	ContentLimit   string     `json:"content_limit,omitempty" doc:"sfw | nsfw, the site's judgement of this item; missing = nsfw"`
	Notify         bool       `json:"notify,omitempty" doc:"notify the author's followers (kind 10); only with verb publish. Sent only when the key is new, the item is live and occurred_at is within 24 hours"`
	OccurredAt     *time.Time `json:"occurred_at,omitempty" doc:"when the content was made (not when it is pushed); required unless removed"`
}

type ActivityWriteRequest struct {
	Items []ActivityWriteItem `json:"items" minItems:"1" maxItems:"100"`
}

type ActivityWriteOutcome struct {
	Key     string `json:"key"`
	Outcome string `json:"outcome" enum:"created,updated,removed,restored,stale,invalid"`
	Reason  string `json:"reason,omitempty" doc:"why an item is invalid"`
}

type ActivityWriteResponse struct {
	Results []ActivityWriteOutcome `json:"results" doc:"one per item, in request order"`
}

type SiteActivityView struct {
	ID             int64      `json:"id"`
	Key            string     `json:"key"`
	ActorID        int64      `json:"actor_id"`
	Verb           string     `json:"verb"`
	ObjectKind     string     `json:"object_kind"`
	ObjectLabel    string     `json:"object_label"`
	Title          string     `json:"title"`
	Excerpt        string     `json:"excerpt"`
	URL            string     `json:"url"`
	CoverImageHash *string    `json:"cover_image_hash"`
	WorkID         *int64     `json:"work_id"`
	ContentLimit   string     `json:"content_limit" enum:"sfw,nsfw"`
	Notify         bool       `json:"notify"`
	OccurredAt     time.Time  `json:"occurred_at"`
	Revision       int64      `json:"revision"`
	Removed        bool       `json:"removed" doc:"a tombstone; its title, excerpt, url, cover_image_hash and work_id are cleared"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	RemovedAt      *time.Time `json:"removed_at"`
}

type SiteActivityListResponse struct {
	Activities []SiteActivityView `json:"activities"`
	NextCursor string             `json:"next_cursor,omitempty" doc:"pass as cursor for the next page; empty = last page"`
}

type ActivityItemView struct {
	ID             int64     `json:"id"`
	Site           string    `json:"site"`
	Key            string    `json:"key"`
	ActorID        int64     `json:"actor_id"`
	Verb           string    `json:"verb"`
	ObjectKind     string    `json:"object_kind"`
	ObjectLabel    string    `json:"object_label"`
	Title          string    `json:"title"`
	Excerpt        string    `json:"excerpt"`
	URL            string    `json:"url"`
	CoverImageHash *string   `json:"cover_image_hash"`
	WorkID         *int64    `json:"work_id"`
	ContentLimit   string    `json:"content_limit" enum:"sfw,nsfw"`
	OccurredAt     time.Time `json:"occurred_at"`
}

type ActivityGroupView struct {
	ID          int64              `json:"id"`
	Site        string             `json:"site"`
	ActorID     int64              `json:"actor_id"`
	Verb        string             `json:"verb"`
	ObjectKind  string             `json:"object_kind"`
	ObjectLabel string             `json:"object_label"`
	Day         string             `json:"day" doc:"the Beijing calendar day the group covers, YYYY-MM-DD"`
	ItemCount   int                `json:"item_count" doc:"live items in the group, in the requested content_limit view"`
	LatestAt    time.Time          `json:"latest_at" doc:"occurred_at of the group's newest item; a group moves to the top when it gains one"`
	Items       []ActivityItemView `json:"items" doc:"the newest items, at most 3; the rest via /activity-groups/{id}/items"`
}

type ActivityGroupListResponse struct {
	Groups     []ActivityGroupView `json:"groups"`
	NextCursor string              `json:"next_cursor,omitempty" doc:"pass as cursor for the next (older) page; empty = last page"`
}

type ActivityItemListResponse struct {
	Items      []ActivityItemView `json:"items"`
	NextCursor string             `json:"next_cursor,omitempty"`
}

type ActivityUnseenResponse struct {
	UnseenCount int        `json:"unseen_count" doc:"groups that moved since the later of seen_at and when the follow began; counted to 100, and 100 means 100 or more"`
	SeenAt      *time.Time `json:"seen_at" doc:"null until the first mark"`
}

type ActivitySeenRequest struct {
	At *time.Time `json:"at,omitempty" doc:"the latest_at of the first group in the response the user was shown, before the site filtered anything out; omitted = now. Clamped to now; the mark never moves back"`
}

type ActivitySeenResponse struct {
	SeenAt time.Time `json:"seen_at"`
}
