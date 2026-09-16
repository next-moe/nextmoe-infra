package dto

type AnchorRef struct {
	AnchorKind int16  `json:"anchor_kind" doc:"0=board 1=site_game 2=site_resource 3=catalog_work 4=catalog_person"`
	AnchorID   string `json:"anchor_id"`
}

type AnchorNotificationRequest struct {
	UserID     int64  `json:"user_id"`
	AnchorKind int16  `json:"anchor_kind" doc:"0=board 1=site_game 2=site_resource 3=catalog_work 4=catalog_person"`
	AnchorID   string `json:"anchor_id"`
	Level      int16  `json:"level" doc:"0=muted 1=normal 3=watching 4=watching first post; 2 (tracking) is thread-only"`
}

type AnchorStatesRequest struct {
	UserID  int64       `json:"user_id"`
	Anchors []AnchorRef `json:"anchors" doc:"anchors to report on (max 100); a normal (unsubscribed) anchor is simply absent from the response"`
}

type AnchorSubscriptionView struct {
	UserID            int64  `json:"user_id"`
	AnchorKind        int16  `json:"anchor_kind" doc:"0=board 1=site_game 2=site_resource 3=catalog_work 4=catalog_person"`
	AnchorID          string `json:"anchor_id"`
	BoardID           *int64 `json:"board_id,omitempty" doc:"the board this subscription names (its anchor_id as a number)"`
	NotificationLevel int16  `json:"notification_level" doc:"0=muted 1=normal 3=watching 4=watching first post; 2 (tracking) is thread-only"`
}

type AnchorStatesResponse struct {
	States []AnchorSubscriptionView `json:"states"`
}

type AnchorSubscriptionListResponse struct {
	Subscriptions []AnchorSubscriptionView `json:"subscriptions"`
	NextCursor    string                   `json:"next_cursor,omitempty" doc:"id of the last row, passed as cursor for the next (older) page; empty = last page"`
}
