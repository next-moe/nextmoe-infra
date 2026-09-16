package dto

import "time"

type BoardStats struct {
	TopicsCount     int64      `json:"topics_count" doc:"topics a reader can see: live threads whose opening post is visible"`
	PostsCount      int64      `json:"posts_count" doc:"the sum of those topics' posts_count (post numbers allocated, tombstones included)"`
	LastPostedAt    *time.Time `json:"last_posted_at,omitempty"`
	LastThreadID    *int64     `json:"last_thread_id,omitempty" doc:"the most recently active of those topics"`
	LastThreadTitle *string    `json:"last_thread_title,omitempty"`
}

type BoardView struct {
	ID                 int64      `json:"id"`
	Site               string     `json:"site"`
	ParentID           *int64     `json:"parent_id,omitempty" doc:"the top-level board this one sits under; boards nest one level"`
	Slug               string     `json:"slug"`
	Name               string     `json:"name"`
	Description        *string    `json:"description,omitempty"`
	Icon               *string    `json:"icon,omitempty" doc:"an emoji, an icon name or an image hash; the site decides how to render it"`
	Color              *string    `json:"color,omitempty" doc:"a palette token the site maps onto its theme"`
	Position           int32      `json:"position" doc:"order among its siblings, ascending"`
	Format             int16      `json:"format" doc:"0=discussion 1=qa 2=announcement"`
	Status             int16      `json:"status" doc:"0=active 1=archived (readable; no new topics or replies)"`
	ContentRating      int16      `json:"content_rating" doc:"the floor for its topics' rating: 0=all 1=r15 2=r18"`
	TopicMinTrustLevel int16      `json:"topic_min_trust_level"`
	ReplyMinTrustLevel int16      `json:"reply_min_trust_level"`
	TopicTemplate      *string    `json:"topic_template,omitempty" doc:"markdown the composer starts from"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	Stats              BoardStats `json:"stats"`
}

type BoardListResponse struct {
	Boards []BoardView `json:"boards" doc:"display order: each top-level board followed by its sub-boards, each group by position"`
}

type BoardResponse struct {
	Board BoardView `json:"board"`
}

type CreateBoardRequest struct {
	ActorID            int64   `json:"actor_id" doc:"the site admin making the change; recorded in the audit log"`
	ParentID           int64   `json:"parent_id,omitempty" doc:"a top-level board to nest this one under; 0 = top level"`
	Slug               string  `json:"slug" maxLength:"64" pattern:"^[a-z0-9]+(?:-[a-z0-9]+)*$" doc:"URL key, unique on the site: lowercase letters, digits and single hyphens, and not all digits"`
	Name               string  `json:"name" minLength:"1" maxLength:"64"`
	Description        *string `json:"description,omitempty" maxLength:"1000"`
	Icon               *string `json:"icon,omitempty" maxLength:"64"`
	Color              *string `json:"color,omitempty" maxLength:"32"`
	TopicTemplate      *string `json:"topic_template,omitempty" maxLength:"10000"`
	Format             int16   `json:"format,omitempty" enum:"0,1,2" doc:"0=discussion 1=qa (a topic can mark its answer) 2=announcement (only moderators open topics)"`
	ContentRating      int16   `json:"content_rating,omitempty" enum:"0,1,2" doc:"the floor for its topics' rating: 0=all 1=r15 2=r18"`
	TopicMinTrustLevel int16   `json:"topic_min_trust_level,omitempty" minimum:"0" maximum:"3" doc:"trust level needed to open a topic; moderators are exempt"`
	ReplyMinTrustLevel int16   `json:"reply_min_trust_level,omitempty" minimum:"0" maximum:"3" doc:"trust level needed to reply to its topics"`
}

type UpdateBoardRequest struct {
	ActorID            int64   `json:"actor_id" doc:"the site admin making the change; recorded in the audit log"`
	ParentID           *int64  `json:"parent_id,omitempty" doc:"move under this top-level board; 0 = move to the top level"`
	Slug               *string `json:"slug,omitempty" maxLength:"64" pattern:"^[a-z0-9]+(?:-[a-z0-9]+)*$"`
	Name               *string `json:"name,omitempty" minLength:"1" maxLength:"64"`
	Description        *string `json:"description,omitempty" maxLength:"1000" doc:"an empty string clears it"`
	Icon               *string `json:"icon,omitempty" maxLength:"64" doc:"an empty string clears it"`
	Color              *string `json:"color,omitempty" maxLength:"32" doc:"an empty string clears it"`
	TopicTemplate      *string `json:"topic_template,omitempty" maxLength:"10000" doc:"an empty string clears it"`
	Format             *int16  `json:"format,omitempty" enum:"0,1,2"`
	Status             *int16  `json:"status,omitempty" enum:"0,1" doc:"0=active 1=archived"`
	ContentRating      *int16  `json:"content_rating,omitempty" enum:"0,1,2" doc:"applies to topics opened from now on; existing topics keep theirs"`
	TopicMinTrustLevel *int16  `json:"topic_min_trust_level,omitempty" minimum:"0" maximum:"3"`
	ReplyMinTrustLevel *int16  `json:"reply_min_trust_level,omitempty" minimum:"0" maximum:"3"`
}

type ReorderBoardsRequest struct {
	ActorID  int64   `json:"actor_id" doc:"the site admin making the change; recorded in the audit log"`
	ParentID int64   `json:"parent_id,omitempty" doc:"whose sub-boards to order; 0 = the top level"`
	BoardIDs []int64 `json:"board_ids" doc:"every board under that parent, exactly once, in the new order"`
}

type MoveTopicRequest struct {
	ActorID int64 `json:"actor_id" doc:"the moderator; recorded in the audit log"`
	BoardID int64 `json:"board_id" doc:"the board to move the topic to; its rating floor applies, a higher rating is kept"`
}

type PinTopicRequest struct {
	ActorID int64      `json:"actor_id" doc:"the moderator; recorded in the audit log"`
	Scope   int16      `json:"scope" enum:"0,1,2" doc:"0=unpin 1=pinned on its board 2=pinned on its board and on the site's topic listing"`
	Until   *time.Time `json:"until,omitempty" doc:"the pin lapses after this time; absent = until unpinned"`
}

type CloseThreadRequest struct {
	ActorID int64 `json:"actor_id" doc:"the moderator; recorded in the audit log"`
	Closed  bool  `json:"closed" doc:"true closes the thread to new posts, false reopens it"`
}

type MarkAnswerRequest struct {
	ActorID     int64 `json:"actor_id" doc:"the thread's author, or a moderator with as_moderator"`
	PostID      int64 `json:"post_id" doc:"the reply that answers the thread; 0 clears the mark"`
	AsModerator bool  `json:"as_moderator,omitempty" doc:"the site vouches actor_id is its moderator"`
}
