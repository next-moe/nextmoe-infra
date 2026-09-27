package dto

import "time"

type FollowResult struct {
	FollowerID int64  `json:"follower_id"`
	FolloweeID int64  `json:"followee_id"`
	Following  bool   `json:"following" doc:"always true after this call"`
	Created    bool   `json:"created" doc:"false when the follow already existed"`
	Notify     string `json:"notify" enum:"all,feed" doc:"how the follower hears about the followee's new work: all = notifications and the feed, feed = the feed only"`
}

type FollowNotifyRequest struct {
	Notify string `json:"notify" enum:"all,feed" doc:"all = notifications and the feed; feed = the feed only"`
}

type FollowNotifyResult struct {
	FollowerID int64  `json:"follower_id"`
	FolloweeID int64  `json:"followee_id"`
	Notify     string `json:"notify" enum:"all,feed"`
}

type UnfollowResult struct {
	FollowerID int64 `json:"follower_id"`
	FolloweeID int64 `json:"followee_id"`
	Following  bool  `json:"following" doc:"always false after this call"`
	Deleted    bool  `json:"deleted" doc:"false when there was no follow to remove"`
}

type FollowView struct {
	UserID     int64      `json:"user_id" doc:"the follower (on a followers list) or the followed user (on a following list)"`
	FollowedAt *time.Time `json:"followed_at" doc:"null when the follow was imported from a site that never recorded when it was made"`
}

type FollowListResponse struct {
	Users      []FollowView `json:"users"`
	NextCursor string       `json:"next_cursor,omitempty" doc:"pass as cursor for the next (older) page; empty = last page"`
}

type FollowStatesRequest struct {
	ViewerID int64   `json:"viewer_id,omitempty" doc:"the signed-in user the relation flags are about; 0 = anonymous"`
	UserIDs  []int64 `json:"user_ids" maxItems:"100" doc:"users to report on (max 100); answered in request order, deduplicated"`
}

type FollowStateView struct {
	UserID         int64   `json:"user_id"`
	FollowersCount int64   `json:"followers_count"`
	FollowingCount int64   `json:"following_count"`
	ViewerFollows  bool    `json:"viewer_follows" doc:"viewer_id follows this user"`
	FollowsViewer  bool    `json:"follows_viewer" doc:"this user follows viewer_id"`
	ViewerBlocks   bool    `json:"viewer_blocks" doc:"viewer_id has blocked this user"`
	BlocksViewer   bool    `json:"blocks_viewer" doc:"this user has blocked viewer_id"`
	ViewerNotify   *string `json:"viewer_notify" enum:"all,feed" doc:"how viewer_id hears about this user's new work; null when viewer_id does not follow them"`
}

type FollowStatesResponse struct {
	States []FollowStateView `json:"states"`
}

type BlockResult struct {
	BlockerID int64 `json:"blocker_id"`
	BlockedID int64 `json:"blocked_id"`
	Blocking  bool  `json:"blocking" doc:"always true after this call"`
	Created   bool  `json:"created" doc:"false when the block already existed"`
}

type UnblockResult struct {
	BlockerID int64 `json:"blocker_id"`
	BlockedID int64 `json:"blocked_id"`
	Blocking  bool  `json:"blocking" doc:"always false after this call"`
	Deleted   bool  `json:"deleted" doc:"false when there was no block to remove"`
}

type BlockView struct {
	UserID    int64     `json:"user_id" doc:"the blocked user"`
	BlockedAt time.Time `json:"blocked_at"`
}

type BlockListResponse struct {
	Users      []BlockView `json:"users"`
	NextCursor string      `json:"next_cursor,omitempty" doc:"pass as cursor for the next (older) page; empty = last page"`
}
