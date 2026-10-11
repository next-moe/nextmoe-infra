package dto

import "time"

type NotificationView struct {
	ID              int64      `json:"id"`
	UserID          int64      `json:"user_id"`
	Kind            int16      `json:"kind" doc:"1=replied 2=mentioned 3=posted 4=thread_created 5=liked 6=answer_accepted 7=feedback_status 8=followed 9=followee_thread_created 10=followee_activity 11=notice; kinds 8, 10 and 11 name no thread (thread_id 0, empty anchor)"`
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

	Activity *NotificationActivityView `json:"activity,omitempty" doc:"kind 10 only: the newest live activity the row counts. Absent when item_count is 0, which retracts the row"`
	Notice   *NotificationNoticeView   `json:"notice,omitempty" doc:"kind 11 only: the newest notice the row folds; item_count is how many notices of this topic and ref arrived since the row was last read"`
}

type NotificationNoticeView struct {
	ID       int64     `json:"id"`
	Topic    string    `json:"topic" doc:"what raised it, e.g. shop_restock"`
	Ref      string    `json:"ref" doc:"the subject within the topic, e.g. offer:7; one unread row per topic and ref"`
	Title    string    `json:"title" doc:"the line to show, already written for the recipient"`
	URL      string    `json:"url" doc:"absolute URL to open, or empty"`
	RaisedAt time.Time `json:"raised_at"`
}

type NotificationActivityView struct {
	ID           int64     `json:"id"`
	Site         string    `json:"site"`
	Key          string    `json:"key"`
	Verb         string    `json:"verb"`
	ObjectKind   string    `json:"object_kind"`
	ObjectLabel  string    `json:"object_label"`
	Title        string    `json:"title"`
	URL          string    `json:"url"`
	ContentLimit string    `json:"content_limit" enum:"sfw,nsfw"`
	OccurredAt   time.Time `json:"occurred_at"`
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
