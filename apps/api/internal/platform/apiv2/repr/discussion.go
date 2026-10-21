package repr

type DiscussionPost struct {
	_              struct{} `json:"-" additionalProperties:"true"`
	Object         string   `json:"object" enum:"discussion_post" doc:"Type discriminant. Always discussion_post."`
	ID             string   `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Post id."`
	AnchorKind     string   `json:"anchor_kind" pattern:"^[a-z_]{1,32}$" maxLength:"32" doc:"Object discriminant of what the thread hangs under: proposal, merge_request, and the kinds later request types register. Open vocabulary."`
	AnchorID       string   `json:"anchor_id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Id of the proposal or request."`
	AuthorUID      string   `json:"author_uid" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Central account id of the author."`
	Author         *UserRef `json:"author" doc:"Account behind author_uid. null only when the account directory could not be read."`
	AuthorReviewer bool     `json:"author_reviewer" doc:"true when the author could review this kind of anchor when posting."`
	State          string   `json:"state" enum:"visible,hidden,deleted" doc:"hidden: a discussion moderator hid it. deleted: its author deleted it."`
	Body           string   `json:"body" maxLength:"2000" doc:"Plain text; clients render a small markdown subset (bold, inline code, links, line breaks). Empty when deleted, and when hidden unless the caller moderates discussions. Must not be used as a discriminant."`
	ReplyToID      *string  `json:"reply_to_id" pattern:"^[0-9]+$" maxLength:"20" doc:"The post this one quotes. null when none."`
	MentionUIDs    []string `json:"mention_uids" doc:"Central account ids the author mentioned; each is notified. Empty array when none."`
	ModerationNote *string  `json:"moderation_note,omitempty" maxLength:"500" doc:"Present only to discussion moderators, on a post that was ever hidden or restored. Must not be used as a discriminant."`
	CreatedAt      string   `json:"created_at" format:"date-time" maxLength:"40" doc:"RFC 3339 UTC with the stored fractional seconds, so it can be sent back as since= without matching the same post again."`
	EditedAt       *string  `json:"edited_at" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC of the author's last edit. null when never edited."`
}

type DiscussionViewer struct {
	_           struct{} `json:"-" additionalProperties:"true"`
	CanPost     bool     `json:"can_post" doc:"true for any signed-in caller who can read the thread. Posting itself also needs a first-party site's token, checked on POST /v2/me/discussions."`
	CanModerate bool     `json:"can_moderate" doc:"true when the caller holds catalog.discussion.moderate."`
	Watching    bool     `json:"watching" doc:"true when new posts here reach the caller's notices."`
	Muted       bool     `json:"muted" doc:"true when the caller muted this thread; mentions still reach them."`
}

type DiscussionPage struct {
	_          struct{}          `json:"-" additionalProperties:"true"`
	Object     string            `json:"object" enum:"list" doc:"Type discriminant. Always list."`
	Items      []DiscussionPost  `json:"items" doc:"Posts oldest first. Empty array, never null."`
	NextCursor *string           `json:"next_cursor,omitempty" pattern:"^cur_[0-9]+$" maxLength:"32" doc:"Opaque cursor for the next, newer page. Omitted on the last page."`
	Total      int64             `json:"total" minimum:"0" doc:"Posts in the thread, deleted ones excluded."`
	OwnerUID   *string           `json:"owner_uid" pattern:"^[0-9]+$" maxLength:"20" doc:"Central account id of whoever filed the anchor: the proposer or the requester."`
	Viewer     *DiscussionViewer `json:"viewer,omitempty" doc:"Present when the call carries a user token."`
}

type DiscussionNotice struct {
	_             struct{}       `json:"-" additionalProperties:"true"`
	Object        string         `json:"object" enum:"discussion_notice" doc:"Type discriminant. Always discussion_notice."`
	Reason        string         `json:"reason" enum:"reply,mention" doc:"mention: the post names the caller. reply: a new post in a thread the caller watches."`
	ViewerIsOwner bool           `json:"viewer_is_owner" doc:"true when the caller filed the anchor; clients use it to pick which page to open."`
	Post          DiscussionPost `json:"post" doc:"The new post."`
}

type DiscussionWatch struct {
	_          struct{} `json:"-" additionalProperties:"true"`
	Object     string   `json:"object" enum:"discussion_watch" doc:"Type discriminant. Always discussion_watch."`
	AnchorKind string   `json:"anchor_kind" pattern:"^[a-z_]{1,32}$" maxLength:"32" doc:"Anchor kind."`
	AnchorID   string   `json:"anchor_id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Anchor id."`
	Watching   bool     `json:"watching" doc:"true when new posts reach the caller."`
	Muted      bool     `json:"muted" doc:"true when the caller muted the thread."`
}
