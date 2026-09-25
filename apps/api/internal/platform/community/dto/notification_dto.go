package dto

import "time"

type NotificationView struct {
	ID              int64      `json:"id"`
	UserID          int64      `json:"user_id"`
	Kind            int16      `json:"kind" doc:"1=replied 2=mentioned 3=posted 4=thread_created 5=liked 6=answer_accepted 7=feedback_status 8=followed 9=followee_thread_created; kind 8 names no thread (thread_id 0, empty anchor)"`
	ThreadID        int64      `json:"thread_id"`
	AnchorKind      int16      `json:"anchor_kind"`
	AnchorID        string     `json:"anchor_id"`
	BoardID         *int64     `json:"board_id,omitempty" doc:"the board this notification names (its anchor_id as a number)"`
	PostID          *int64     `json:"post_id"`
	PostNumber      *int32     `json:"post_number"`
	FirstPostNumber *int32     `json:"first_post_number"`
	ActorID         *int64     `json:"actor_id,omitempty"`
	ActorCount      int        `json:"actor_count"`
	ItemCount       int        `json:"item_count"`
	ReadAt          *time.Time `json:"read_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	Seq             int64      `json:"seq"`
}

type NotificationListResponse struct {
	Notifications []NotificationView `json:"notifications"`
	NextCursor    string             `json:"next_cursor,omitempty"`
	UnreadCount   int64              `json:"unread_count"`
}

type MarkNotificationsReadRequest struct {
	IDs []int64 `json:"ids,omitempty" maxItems:"100"`
	All bool    `json:"all,omitempty"`
}

type MarkNotificationsReadResponse struct {
	Marked      int64 `json:"marked"`
	UnreadCount int64 `json:"unread_count"`
}

type NotificationFeedResponse struct {
	Notifications []NotificationView `json:"notifications"`
	NextAfter     int64              `json:"next_after"`
}
