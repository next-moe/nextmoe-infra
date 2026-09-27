package dto

import (
	"encoding/json"
	"strconv"
	"time"

	"api/internal/platform/chat/content"
)

func ID(n int64) string { return strconv.FormatInt(n, 10) }

func IDPtr(n *int64) *string {
	if n == nil {
		return nil
	}
	s := ID(*n)
	return &s
}

func ParseID(s string) (int64, bool) {
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil && n > 0
}

type Entity struct {
	Type     string  `json:"type" enum:"bold,italic,underline,strikethrough,spoiler,code,pre,blockquote,text_link,mention,url" doc:"url entities are added by the server; one sent by a client is dropped"`
	Offset   int     `json:"offset" minimum:"0" doc:"start, in UTF-16 code units"`
	Length   int     `json:"length" minimum:"1" doc:"length, in UTF-16 code units"`
	UserID   *string `json:"user_id,omitempty" pattern:"^[0-9]+$" doc:"mention only: the mentioned user"`
	URL      *string `json:"url,omitempty" doc:"text_link only: an absolute http or https URL"`
	Language *string `json:"language,omitempty" doc:"pre only: the code block's language"`
}

func EntitiesOut(es []content.Entity) []Entity {
	out := make([]Entity, len(es))
	for i, e := range es {
		out[i] = Entity{Type: e.Type, Offset: e.Offset, Length: e.Length, UserID: IDPtr(e.UserID), URL: e.URL, Language: e.Language}
	}
	return out
}

func EntitiesIn(es []Entity) []content.Entity {
	if es == nil {
		return nil
	}
	out := make([]content.Entity, len(es))
	for i, e := range es {
		out[i] = content.Entity{Type: e.Type, Offset: e.Offset, Length: e.Length, URL: e.URL, Language: e.Language}
		if e.UserID != nil {
			if id, ok := ParseID(*e.UserID); ok {
				out[i].UserID = &id
			}
		}
	}
	return out
}

type Media struct {
	Type      string `json:"type" enum:"photo" doc:"photo for now; sticker and file follow"`
	ImageHash string `json:"image_hash" doc:"the image service hash"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Thumbhash string `json:"thumbhash,omitempty"`
}

type ReplyPreview struct {
	Seq       int64    `json:"seq"`
	SenderID  string   `json:"sender_id"`
	Text      string   `json:"text" doc:"cut to 120 UTF-16 code units"`
	Entities  []Entity `json:"entities" doc:"only the entities that fall entirely inside text"`
	MediaType *string  `json:"media_type"`
	Deleted   bool     `json:"deleted" doc:"the replied message is gone for the caller: deleted for everyone, or hidden or cleared by the caller; text is empty"`
}

type Quote struct {
	Text     string   `json:"text"`
	Entities []Entity `json:"entities"`
	Offset   int      `json:"offset" doc:"where the quote starts in the replied message, in UTF-16 code units"`
}

type ContextCard struct {
	Site  string `json:"site"`
	Kind  string `json:"kind" doc:"what the card points at on that site, e.g. patch, resource, topic"`
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

type ServiceAction struct {
	Type    string   `json:"type" enum:"group_created,members_added,member_left,member_removed,title_changed,photo_changed,message_pinned,joined_by_link"`
	UserIDs []string `json:"user_ids,omitempty"`
	UserID  *string  `json:"user_id,omitempty"`
	Title   *string  `json:"title,omitempty"`
	Seq     *int64   `json:"seq,omitempty"`
}

type ReactionCount struct {
	Reaction string `json:"reaction"`
	Count    int    `json:"count"`
	Reacted  bool   `json:"reacted" doc:"the caller is one of the reactors"`
}

type Message struct {
	Object          string          `json:"object" enum:"message"`
	ID              string          `json:"id"`
	ConversationID  string          `json:"conversation_id"`
	Seq             int64           `json:"seq" doc:"position in the conversation, from 1 with no gaps; a deleted message keeps its number"`
	SenderID        string          `json:"sender_id"`
	Kind            string          `json:"kind" enum:"message,service"`
	Text            string          `json:"text"`
	Entities        []Entity        `json:"entities"`
	Media           *Media          `json:"media"`
	MediaGroupID    *string         `json:"media_group_id" doc:"an album: the messages sent together share it"`
	ReplyTo         *ReplyPreview   `json:"reply_to"`
	ReplyQuote      *Quote          `json:"reply_quote"`
	ServiceAction   *ServiceAction  `json:"service_action"`
	Context         *ContextCard    `json:"context"`
	Reactions       []ReactionCount `json:"reactions" doc:"count descending, then first reacted first"`
	ClientMessageID *string         `json:"client_message_id" doc:"the sender's own idempotency key; null on anyone else's messages"`
	Silent          bool            `json:"silent"`
	PinnedAt        *time.Time      `json:"pinned_at"`
	EditedAt        *time.Time      `json:"edited_at"`
	CreatedAt       time.Time       `json:"created_at"`
}

type Draft struct {
	Text       string    `json:"text"`
	Entities   []Entity  `json:"entities"`
	ReplyToSeq *int64    `json:"reply_to_seq"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type DialogState struct {
	Role              string     `json:"role" enum:"owner,admin,member"`
	Accepted          bool       `json:"accepted" doc:"false = this conversation is in the caller's message requests"`
	LastReadSeq       int64      `json:"last_read_seq"`
	UnreadCount       int32      `json:"unread_count"`
	MarkedUnread      bool       `json:"marked_unread"`
	ClearedThroughSeq int64      `json:"cleared_through_seq"`
	VisibleFromSeq    int64      `json:"visible_from_seq"`
	MutedUntil        *time.Time `json:"muted_until" doc:"null = not muted; 9999-12-31 = muted until unmuted"`
	Archived          bool       `json:"archived"`
	PinnedRank        *int16     `json:"pinned_rank" doc:"null = not pinned; a higher rank sits higher"`
	Draft             *Draft     `json:"draft"`
}

type Conversation struct {
	Object         string      `json:"object" enum:"conversation"`
	ID             string      `json:"id"`
	Kind           string      `json:"kind" enum:"direct,group"`
	Title          *string     `json:"title"`
	About          *string     `json:"about"`
	PhotoImageHash *string     `json:"photo_image_hash"`
	PeerID         *string     `json:"peer_id" doc:"direct only: the other person"`
	MemberCount    int32       `json:"member_count"`
	LastSeq        int64       `json:"last_seq"`
	PeerReadSeq    int64       `json:"peer_read_seq" doc:"how far the others have read: direct = the peer's position (0 while the caller's request is pending), group = the furthest any other member has read"`
	CreatedAt      time.Time   `json:"created_at"`
	Me             DialogState `json:"me"`
	LastMessage    *Message    `json:"last_message" doc:"the newest message the caller can see"`
}

type Member struct {
	UserID   string    `json:"user_id"`
	Role     string    `json:"role" enum:"owner,admin,member"`
	JoinedAt time.Time `json:"joined_at"`
}

type ConversationDetail struct {
	Conversation
	Members    []Member `json:"members"`
	PinnedSeqs []int64  `json:"pinned_seqs" doc:"pinned messages, most recently pinned first"`
}

type User struct {
	Object  string `json:"object" enum:"user"`
	ID      string `json:"id"`
	Name    string `json:"name"`
	Avatar  string `json:"avatar"`
	Deleted bool   `json:"deleted" doc:"the account was deleted; name and avatar are empty, show a placeholder"`
}

type Update struct {
	Object         string          `json:"object" enum:"update"`
	UpdateSeq      int64           `json:"update_seq" doc:"position in the caller's own update stream, from 1 with no gaps"`
	Kind           string          `json:"kind" enum:"new_message,edit_message,delete_messages,read_inbox,read_outbox,message_reactions,pinned_messages,dialog,hide_messages,clear_history,member,conversation"`
	ConversationID string          `json:"conversation_id"`
	Data           json.RawMessage `json:"data" doc:"kind-specific; see the chat contract"`
	CreatedAt      time.Time       `json:"created_at"`
}

type State struct {
	Object                  string `json:"object" enum:"chat_state"`
	LastUpdateSeq           int64  `json:"last_update_seq"`
	UnreadConversationCount int64  `json:"unread_conversation_count" doc:"accepted, unmuted conversations with anything unread"`
	UnreadMessageCount      int64  `json:"unread_message_count" doc:"unread messages across accepted, unmuted conversations"`
	RequestCount            int64  `json:"request_count" doc:"conversations waiting in message requests"`
}

type Settings struct {
	Object            string `json:"object" enum:"chat_settings"`
	AllowIncoming     string `json:"allow_incoming" enum:"all,following,none" doc:"whose direct messages go straight to the inbox: anyone, only people the user follows, or nobody"`
	AcceptRequests    bool   `json:"accept_requests" doc:"whether people outside allow_incoming may send a message request"`
	AllowGroupInvites string `json:"allow_group_invites" enum:"all,following,none" doc:"who may add the user to a group directly; others' invites become requests"`
}

type ReactionOption struct {
	Key   string `json:"key"`
	Emoji string `json:"emoji"`
	Label string `json:"label"`
}
