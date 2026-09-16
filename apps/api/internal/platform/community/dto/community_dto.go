package dto

import "time"

type ThreadView struct {
	ID                int64      `json:"id"`
	Site              string     `json:"site"`
	Kind              int16      `json:"kind"`
	AnchorKind        int16      `json:"anchor_kind"`
	AnchorID          string     `json:"anchor_id"`
	Title             *string    `json:"title,omitempty"`
	HeaderImageHashes []string   `json:"header_image_hashes,omitempty" doc:"Header image hashes as accepted at topic creation; empty for none"`
	ContentRating     int16      `json:"content_rating"`
	Status            int16      `json:"status"`
	FbStatus          *int16     `json:"fb_status,omitempty"`
	FbResponse        *string    `json:"fb_response,omitempty"`
	AnswerPostID      *int64     `json:"answer_post_id,omitempty"`
	MergedIntoID      *int64     `json:"merged_into_id,omitempty"`
	PostsCount        int32      `json:"posts_count"`
	ParticipantsCount int32      `json:"participants_count"`
	HighestPostNumber int32      `json:"highest_post_number"`
	LastPostedAt      *time.Time `json:"last_posted_at,omitempty"`
	CreatedBy         int64      `json:"created_by"`
	CreatedAt         time.Time  `json:"created_at"`
	OpeningStatus     *int16     `json:"opening_status,omitempty"`
	OpeningAuthorID   *int64     `json:"opening_author_id,omitempty"`
	BoardID           *int64     `json:"board_id,omitempty" doc:"the board a topic lives on (its anchor_id as a number)"`
	PinScope          int16      `json:"pin_scope,omitempty" doc:"0=not pinned 1=pinned on its board 2=pinned on its board and site-wide; a lapsed pin reads 0"`
	PinnedAt          *time.Time `json:"pinned_at,omitempty"`
	PinnedUntil       *time.Time `json:"pinned_until,omitempty"`
}

type PostView struct {
	ID                int64      `json:"id"`
	ThreadID          int64      `json:"thread_id"`
	PostNumber        int32      `json:"post_number"`
	RootPostID        *int64     `json:"root_post_id,omitempty"`
	ReplyToPostID     *int64     `json:"reply_to_post_id,omitempty"`
	TargetUserID      *int64     `json:"target_user_id,omitempty"`
	AuthorID          int64      `json:"author_id"`
	ContentRaw        string     `json:"content_raw"`
	ContentHTML       string     `json:"content_html"`
	ContentRating     int16      `json:"content_rating"`
	Status            int16      `json:"status"`
	EditedAt          *time.Time `json:"edited_at,omitempty"`
	EditedByModerator bool       `json:"edited_by_moderator,omitempty" doc:"true when the latest edit was a mod-actor edit (as_moderator)"`
	CreatedAt         time.Time  `json:"created_at"`

	ReactionCount int32 `json:"reaction_count" doc:"likes on this post (reaction kind 0)"`
	ViewerReacted bool  `json:"viewer_reacted,omitempty" doc:"whether viewer_id has liked this post; always false when the request named no viewer"`
}

type CommentsResolveRequest struct {
	AnchorKind    int16  `json:"anchor_kind" doc:"0=board 1=site_game 2=site_resource 3=catalog_work 4=catalog_person"`
	AnchorID      string `json:"anchor_id"`
	ContentRating int16  `json:"content_rating" doc:"0=all 1=r15 2=r18 (inherited from the anchor)"`
}

type CommentRequest struct {
	AnchorKind    int16  `json:"anchor_kind" doc:"1=site_game 2=site_resource 3=catalog_work 4=catalog_person"`
	AnchorID      string `json:"anchor_id"`
	ContentRating int16  `json:"content_rating" doc:"0=all 1=r15 2=r18 (inherited from the anchor); applied only when this comment is the one that creates the thread"`
	AuthorID      int64  `json:"author_id"`
	Body          string `json:"body" doc:"markdown source"`
	RootPostID    *int64 `json:"root_post_id,omitempty"`
	ReplyToPostID *int64 `json:"reply_to_post_id,omitempty"`
	TargetUserID  *int64 `json:"target_user_id,omitempty"`
}

type PostsResolveRequest struct {
	IDs      []int64 `json:"ids" doc:"post ids to hydrate (max 100; deduped; only visible posts return)"`
	ViewerID int64   `json:"viewer_id,omitempty" doc:"fill viewer_reacted for this user; 0 = no viewer"`
}

type OpenTopicRequest struct {
	AuthorID          int64    `json:"author_id"`
	BoardID           int64    `json:"board_id,omitempty" doc:"the board to open the topic on"`
	AnchorID          string   `json:"anchor_id,omitempty" deprecated:"true" doc:"deprecated: the board's id or slug; send board_id instead"`
	AsModerator       bool     `json:"as_moderator,omitempty" doc:"the site vouches author_id is its moderator: required on an announcement board, and exempt from the board's topic trust level"`
	Title             string   `json:"title"`
	ContentRating     int16    `json:"content_rating"`
	Body              string   `json:"body" doc:"markdown source of the opening post"`
	HeaderImageHashes []string `json:"header_image_hashes,omitempty"`
}

type OpenFeedbackRequest struct {
	AuthorID      int64  `json:"author_id"`
	AnchorKind    int16  `json:"anchor_kind"`
	AnchorID      string `json:"anchor_id"`
	Title         string `json:"title"`
	ContentRating int16  `json:"content_rating"`
	Body          string `json:"body"`
}

type ReplyRequest struct {
	AuthorID      int64  `json:"author_id"`
	Body          string `json:"body"`
	RootPostID    *int64 `json:"root_post_id,omitempty"`
	ReplyToPostID *int64 `json:"reply_to_post_id,omitempty"`
	TargetUserID  *int64 `json:"target_user_id,omitempty"`
}

type EditPostRequest struct {
	AuthorID    int64  `json:"author_id" doc:"the acting user (the post author, or the moderator when as_moderator)"`
	Body        string `json:"body" doc:"new markdown source; re-cooked + sanitized on write"`
	AsModerator bool   `json:"as_moderator,omitempty" doc:"mod-actor variant: skip the author match; the site vouches author_id is its moderator"`
}

type ReactionToggleRequest struct {
	UserID int64 `json:"user_id"`
	Kind   int16 `json:"kind" doc:"reaction kind (0=like)"`
}

type FlagRequest struct {
	FlaggerID int64   `json:"flagger_id"`
	Reason    *int16  `json:"reason,omitempty"`
	Note      *string `json:"note,omitempty"`
}

type FeedbackStatusRequest struct {
	FbStatus    int16   `json:"fb_status"`
	ResponderID int64   `json:"responder_id"`
	Response    *string `json:"response,omitempty"`
}

type FeedbackMergeRequest struct {
	IntoID int64 `json:"into_id"`
}

type ThreadWithPosts struct {
	Thread     ThreadView `json:"thread"`
	Posts      []PostView `json:"posts"`
	NextCursor string     `json:"next_cursor,omitempty" doc:"post_number to pass as after for the next page; empty = last page"`
}

type CommentsPage struct {
	Thread     *ThreadView `json:"thread,omitempty" doc:"absent until the anchor's first comment creates the thread; posts is then empty too"`
	Posts      []PostView  `json:"posts"`
	NextCursor string      `json:"next_cursor,omitempty" doc:"post_number to pass as after for the next page; empty = last page"`
}

type ThreadListResponse struct {
	Threads    []ThreadView `json:"threads"`
	NextCursor string       `json:"next_cursor,omitempty"`
}

type PostListResponse struct {
	Posts      []PostView `json:"posts"`
	NextCursor string     `json:"next_cursor,omitempty"`
}

type ThreadResponse struct {
	Thread ThreadView `json:"thread"`
	Post   *PostView  `json:"post,omitempty" doc:"the post this call wrote, when it wrote one"`
}

type PostThreadContext struct {
	ThreadID   int64   `json:"thread_id"`
	Title      *string `json:"title,omitempty" doc:"thread title (NULL for a comments thread)"`
	AnchorKind int16   `json:"anchor_kind" doc:"0=board 1=site_game 2=site_resource 3=catalog_work 4=catalog_person"`
	AnchorID   string  `json:"anchor_id"`
	BoardID    *int64  `json:"board_id,omitempty" doc:"the board a topic lives on (its anchor_id as a number)"`
}

type AuthorPostView struct {
	Post   PostView          `json:"post"`
	Thread PostThreadContext `json:"thread"`
}

type AuthorPostsResponse struct {
	Posts      []AuthorPostView `json:"posts"`
	NextCursor string           `json:"next_cursor,omitempty" doc:"post id to pass as after for the next (older) page; empty = last page"`
}

type ThreadReadRequest struct {
	UserID             int64 `json:"user_id"`
	LastReadPostNumber int32 `json:"last_read_post_number" doc:"how far the user has read; clamped to the thread's highest post number and never walked backwards"`
}

type ThreadNotificationRequest struct {
	UserID int64 `json:"user_id"`
	Level  int16 `json:"level" doc:"0=muted 1=normal 2=tracking 3=watching"`
}

type ThreadStatesRequest struct {
	UserID    int64   `json:"user_id"`
	ThreadIDs []int64 `json:"thread_ids" doc:"threads to report on (max 100); a thread the user never touched carries no row and is simply absent from the response"`
}

type ThreadUserView struct {
	ThreadID           int64 `json:"thread_id"`
	UserID             int64 `json:"user_id"`
	LastReadPostNumber int32 `json:"last_read_post_number"`
	HighestPostNumber  int32 `json:"highest_post_number"`
	UnreadCount        int32 `json:"unread_count" doc:"highest_post_number - last_read_post_number; a tombstone keeps its number, so a deleted post still counts as unread"`
	NotificationLevel  int16 `json:"notification_level" doc:"0=muted 1=normal 2=tracking 3=watching"`
}

type ThreadStatesResponse struct {
	States []ThreadUserView `json:"states"`
}

type UnreadThreadView struct {
	Thread ThreadView     `json:"thread"`
	State  ThreadUserView `json:"state"`
}

type UnreadListResponse struct {
	Threads    []UnreadThreadView `json:"threads"`
	NextCursor string             `json:"next_cursor,omitempty"`
	Total      int64              `json:"total" doc:"the user's threads carrying unread posts on this site, muted excluded — the red-dot number"`
}

type PostFeedResponse struct {
	Posts      []AuthorPostView `json:"posts"`
	NextCursor string           `json:"next_cursor,omitempty" doc:"opaque cursor for the next (older) page; empty = last page"`
}

type PostsResolveResponse struct {
	Posts []AuthorPostView `json:"posts"`
}

type AuthorStat struct {
	AuthorID     int64 `json:"author_id"`
	VisiblePosts int64 `json:"visible_posts"`
}

type AuthorStatsResponse struct {
	Stats []AuthorStat `json:"stats"`
}

type PurgeResponse struct {
	PostsPurged       int64 `json:"posts_purged" doc:"posts tombstoned + content-scrubbed this run"`
	ReactionsDeleted  int64 `json:"reactions_deleted" doc:"reaction rows the author left that were deleted this run"`
	ReadStatesDeleted int64 `json:"read_states_deleted" doc:"read/subscription rows (which threads they opened, how far they read) deleted this run"`
}

type PostResponse struct {
	Post PostView `json:"post"`
}

type ReactionToggleResponse struct {
	Added         bool   `json:"added" doc:"the acting user's new state: true = now reacted (the viewer_reacted a read face would report for them)"`
	ReactionCount int32  `json:"reaction_count" doc:"the post's like count after this toggle, the same number a read face reports"`
	AuthorID      int64  `json:"author_id" doc:"the post's author (the like-notification recipient)"`
	ThreadID      int64  `json:"thread_id"`
	AnchorKind    int16  `json:"anchor_kind"`
	AnchorID      string `json:"anchor_id"`
}

type OKResponse struct {
	OK bool `json:"ok"`
}

type ActivityReceiptRequest struct {
	UserID           int64  `json:"user_id"`
	TopicsEntered    int32  `json:"topics_entered"`
	PostsRead        int32  `json:"posts_read"`
	ReadTimeS        int32  `json:"read_time_s"`
	DaysVisited      int32  `json:"days_visited"`
	WindowActiveDays *int32 `json:"window_active_days,omitempty"`
}

type SetBoostRequest struct {
	UserID int64 `json:"user_id"`
	Boost  int16 `json:"boost" doc:"0=none 1=veteran 2=creator 3=staff"`
}

type TrustView struct {
	UserID                  int64  `json:"user_id"`
	Level                   int16  `json:"level"`
	TopicsEntered           *int32 `json:"topics_entered,omitempty"`
	PostsRead               *int32 `json:"posts_read,omitempty"`
	ReadTimeS               *int32 `json:"read_time_s,omitempty"`
	DaysVisited             *int32 `json:"days_visited,omitempty"`
	LikesGiven              *int32 `json:"likes_given,omitempty"`
	LikesReceived           *int32 `json:"likes_received,omitempty"`
	FlagsAgreed             *int32 `json:"flags_agreed,omitempty"`
	FlagsDisagreed          *int32 `json:"flags_disagreed,omitempty"`
	FirstPostsHeldRemaining int32  `json:"first_posts_held_remaining"`
	GrantedBoost            *int16 `json:"granted_boost,omitempty"`
}

type ReviewItemView struct {
	ID        int64  `json:"id"`
	Site      string `json:"site,omitempty"`
	PostID    *int64 `json:"post_id,omitempty"`
	ThreadID  *int64 `json:"thread_id,omitempty" doc:"the subject post's thread (deep-link target)"`
	AuthorID  *int64 `json:"author_id,omitempty" doc:"the subject post's author"`
	Source    *int16 `json:"source,omitempty" doc:"0=flags 1=first_post_hold 2=suspect_words 3=external"`
	Status    int16  `json:"status" doc:"0=pending 1=approved 2=rejected"`
	DecidedBy *int64 `json:"decided_by,omitempty"`
}

type ReviewListResponse struct {
	Items []ReviewItemView `json:"items"`
}

type ReviewDecisionRequest struct {
	DecidedBy int64 `json:"decided_by"`
}
