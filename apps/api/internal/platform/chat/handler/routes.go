package handler

import (
	"context"
	"net/http"
	"time"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/chat/dto"
	"api/internal/platform/chat/service"

	"github.com/danielgtaylor/huma/v2"
)

var (
	readErrs  = []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusUnprocessableEntity}
	writeErrs = []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity, http.StatusTooManyRequests}
)

func (h *Handler) register(api huma.API) {
	tag := []string{"chat"}

	huma.Register(api, huma.Operation{OperationID: "getChatState", Method: http.MethodGet, Path: "/v2/chat/state", Tags: tag, Errors: readErrs,
		Summary: "Unread counts and the caller's update-stream position; fetch once when a page opens"}, h.state)
	huma.Register(api, huma.Operation{OperationID: "listChatUpdates", Method: http.MethodGet, Path: "/v2/chat/updates", Tags: tag, Errors: readErrs,
		Summary: "The caller's update stream after a position, with the messages, conversations and users it names",
		Description: "Apply an update when its update_seq is exactly one past the last one applied; a bigger jump is a gap to fill from here. " +
			"too_long means the position is older than the kept stream (30 days): reload the conversation list and state instead."}, h.updates)
	huma.Register(api, huma.Operation{OperationID: "listChatConversations", Method: http.MethodGet, Path: "/v2/chat/conversations", Tags: tag, Errors: readErrs,
		Summary: "One folder of the caller's conversations, newest activity first; the inbox's first page starts with the pinned ones"}, h.conversations)
	huma.Register(api, huma.Operation{OperationID: "openChatDirect", Method: http.MethodPut, Path: "/v2/chat/direct/{user_id}", Tags: tag, Errors: writeErrs,
		Summary:     "Get or create the caller's direct conversation with a user",
		Description: "Idempotent and sends nothing; a conversation with no message stays out of both lists. Creating one applies the recipient's settings: straight to the inbox, into message requests, or refused."}, h.openDirect)
	huma.Register(api, huma.Operation{OperationID: "getChatConversation", Method: http.MethodGet, Path: "/v2/chat/conversations/{id}", Tags: tag, Errors: readErrs,
		Summary: "One conversation with its members and pinned messages"}, h.conversation)
	huma.Register(api, huma.Operation{OperationID: "listChatMessages", Method: http.MethodGet, Path: "/v2/chat/conversations/{id}/messages", Tags: tag, Errors: readErrs,
		Summary: "A page of the messages the caller can see, oldest first within the page; the newest page when no position is given"}, h.messages)
	huma.Register(api, huma.Operation{OperationID: "sendChatMessage", Method: http.MethodPost, Path: "/v2/chat/conversations/{id}/messages", Tags: tag, Errors: writeErrs,
		DefaultStatus: http.StatusCreated,
		Summary:       "Send a message; resending the same client_message_id returns the first attempt's message and writes nothing"}, h.send)
	huma.Register(api, huma.Operation{OperationID: "editChatMessage", Method: http.MethodPatch, Path: "/v2/chat/messages/{id}", Tags: tag, Errors: writeErrs,
		Summary: "Edit the caller's own message within 48 hours of sending"}, h.edit)
	huma.Register(api, huma.Operation{OperationID: "deleteChatMessages", Method: http.MethodPost, Path: "/v2/chat/conversations/{id}/messages/delete", Tags: tag, Errors: writeErrs,
		Summary: "Delete the caller's own messages for everyone, or hide any messages from the caller alone"}, h.deleteMessages)
	huma.Register(api, huma.Operation{OperationID: "setChatReaction", Method: http.MethodPut, Path: "/v2/chat/messages/{id}/reaction", Tags: tag, Errors: writeErrs,
		Summary: "Set the caller's reaction to a message, replacing any earlier one; null removes it"}, h.react)
	huma.Register(api, huma.Operation{OperationID: "readChatConversation", Method: http.MethodPost, Path: "/v2/chat/conversations/{id}/read", Tags: tag, Errors: writeErrs,
		Summary: "Move the caller's read position forward and clear a manual unread mark"}, h.read)
	huma.Register(api, huma.Operation{OperationID: "updateChatDialog", Method: http.MethodPatch, Path: "/v2/chat/conversations/{id}/me", Tags: tag, Errors: writeErrs,
		Summary: "Change the caller's own state of a conversation: mute, archive, pin, mark unread"}, h.updateDialog)
	huma.Register(api, huma.Operation{OperationID: "saveChatDraft", Method: http.MethodPut, Path: "/v2/chat/conversations/{id}/draft", Tags: tag, Errors: writeErrs,
		Summary: "Save what the caller is typing for their other devices; empty text and no reply clears it"}, h.saveDraft)
	huma.Register(api, huma.Operation{OperationID: "acceptChatRequest", Method: http.MethodPost, Path: "/v2/chat/conversations/{id}/accept", Tags: tag, Errors: writeErrs,
		DefaultStatus: http.StatusNoContent, Summary: "Move a message request into the inbox"}, h.accept)
	huma.Register(api, huma.Operation{OperationID: "clearChatHistory", Method: http.MethodPost, Path: "/v2/chat/conversations/{id}/clear-history", Tags: tag, Errors: writeErrs,
		DefaultStatus: http.StatusNoContent,
		Summary:       "Hide everything so far from the caller alone; with remove, the conversation also leaves the caller's lists until the next message (this is also how a request is deleted)"}, h.clearHistory)
	huma.Register(api, huma.Operation{OperationID: "pinChatMessage", Method: http.MethodPut, Path: "/v2/chat/conversations/{id}/pins/{seq}", Tags: tag, Errors: writeErrs,
		DefaultStatus: http.StatusNoContent, Summary: "Pin a message for everyone in the conversation; posts a service message"}, h.pin)
	huma.Register(api, huma.Operation{OperationID: "unpinChatMessage", Method: http.MethodDelete, Path: "/v2/chat/conversations/{id}/pins/{seq}", Tags: tag, Errors: writeErrs,
		DefaultStatus: http.StatusNoContent, Summary: "Unpin a message"}, h.unpin)
	huma.Register(api, huma.Operation{OperationID: "sendChatTyping", Method: http.MethodPost, Path: "/v2/chat/conversations/{id}/typing", Tags: tag, Errors: writeErrs,
		DefaultStatus: http.StatusNoContent,
		Summary:       "Tell the other members the caller is typing; repeat every 5 seconds while typing, receivers drop it after 6"}, h.typing)
	huma.Register(api, huma.Operation{OperationID: "reportChatMessage", Method: http.MethodPost, Path: "/v2/chat/messages/{id}/report", Tags: tag, Errors: writeErrs,
		Summary: "Report someone else's message; the message and up to 10 before it go to moderators"}, h.report)
	huma.Register(api, huma.Operation{OperationID: "getChatSettings", Method: http.MethodGet, Path: "/v2/chat/settings", Tags: tag, Errors: readErrs,
		Summary: "The caller's chat privacy settings"}, h.settings)
	huma.Register(api, huma.Operation{OperationID: "updateChatSettings", Method: http.MethodPatch, Path: "/v2/chat/settings", Tags: tag, Errors: writeErrs,
		Summary: "Change the caller's chat privacy settings"}, h.updateSettings)
	huma.Register(api, huma.Operation{OperationID: "listChatReactions", Method: http.MethodGet, Path: "/v2/chat/reactions", Tags: tag, Errors: readErrs,
		Summary: "The reaction vocabulary (the forum's, which is Telegram's default set)"}, h.reactions)
	huma.Register(api, huma.Operation{OperationID: "createChatRealtimeToken", Method: http.MethodPost, Path: "/v2/chat/realtime-token", Tags: tag, Errors: writeErrs,
		Summary:     "A short-lived token for the realtime connection",
		Description: "Connect a Centrifugo client to url with this token; it subscribes to the caller's own channel. Fetch a new one from here when the client asks for a refresh."}, h.realtimeToken)
}

func parseID(ctx context.Context, name, raw string) (int64, error) {
	id, ok := dto.ParseID(raw)
	if !ok {
		p := stamp(ctx, problem.New(problem.CodeInvalidParameter, "", "", name+" must be a positive decimal id."))
		p.Errors = []problem.FieldError{{Parameter: name, Reason: problem.ReasonInvalidFormat, Detail: "a positive decimal id"}}
		return 0, p
	}
	return id, nil
}

// IDPath and MessagePath are exported for huma: it drops the path and query
// parameters of an unexported embedded input struct, and every route that
// embedded them then answered 400 for a missing id.
type IDPath struct {
	ID string `path:"id" pattern:"^[0-9]+$" maxLength:"20" doc:"Conversation id."`
}

type MessagePath struct {
	ID string `path:"id" pattern:"^[0-9]+$" maxLength:"20" doc:"Message id."`
}

type stateOutput struct{ Body dto.State }

func (h *Handler) state(ctx context.Context, _ *struct{}) (*stateOutput, error) {
	a, err := actorFrom(ctx)
	if err != nil {
		return nil, err
	}
	st, err := h.opt.Chat.State(ctx, a)
	if err != nil {
		return nil, fail(ctx, "state", err)
	}
	return &stateOutput{Body: *st}, nil
}

type updatesInput struct {
	After int64 `query:"after" minimum:"0" doc:"the last update_seq the client has applied; 0 for none"`
	Limit int   `query:"limit" minimum:"0" maximum:"100" doc:"page size, default 50"`
}

type UpdatesBody struct {
	Object        string             `json:"object" enum:"update_list"`
	Updates       []dto.Update       `json:"updates"`
	Messages      []dto.Message      `json:"messages" doc:"the messages new_message and edit_message updates name, as the caller sees them now"`
	Conversations []dto.Conversation `json:"conversations" doc:"the conversations the updates name"`
	Users         []dto.User         `json:"users"`
	LastUpdateSeq int64              `json:"last_update_seq"`
	HasMore       bool               `json:"has_more" doc:"more updates follow; ask again after the last one"`
	TooLong       bool               `json:"too_long" doc:"the position is older than the kept stream: reload instead"`
}

type updatesOutput struct{ Body UpdatesBody }

func (h *Handler) updates(ctx context.Context, in *updatesInput) (*updatesOutput, error) {
	a, err := actorFrom(ctx)
	if err != nil {
		return nil, err
	}
	page, err := h.opt.Chat.Updates(ctx, a, in.After, in.Limit)
	if err != nil {
		return nil, fail(ctx, "updates", err)
	}
	return &updatesOutput{Body: UpdatesBody{
		Object: "update_list", Updates: page.Updates, Messages: page.Messages, Conversations: page.Conversations,
		Users: page.Users, LastUpdateSeq: page.LastUpdateSeq, HasMore: page.HasMore, TooLong: page.TooLong,
	}}, nil
}

type conversationsInput struct {
	Folder string `query:"folder" enum:"inbox,archive,requests" doc:"default inbox"`
	Cursor string `query:"cursor" maxLength:"512" doc:"next_cursor from the previous page"`
	Limit  int    `query:"limit" minimum:"0" maximum:"100" doc:"page size, default 50"`
}

type ConversationListBody struct {
	Object     string             `json:"object" enum:"list"`
	Items      []dto.Conversation `json:"items"`
	Users      []dto.User         `json:"users"`
	NextCursor *string            `json:"next_cursor,omitempty"`
}

type conversationsOutput struct{ Body ConversationListBody }

func (h *Handler) conversations(ctx context.Context, in *conversationsInput) (*conversationsOutput, error) {
	a, err := actorFrom(ctx)
	if err != nil {
		return nil, err
	}
	page, err := h.opt.Chat.ListConversations(ctx, a, in.Folder, in.Cursor, in.Limit)
	if err != nil {
		return nil, fail(ctx, "list conversations", err)
	}
	body := ConversationListBody{Object: "list", Items: page.Conversations, Users: page.Users}
	if body.Items == nil {
		body.Items = []dto.Conversation{}
	}
	if page.NextCursor != "" {
		body.NextCursor = &page.NextCursor
	}
	return &conversationsOutput{Body: body}, nil
}

type ConversationBody struct {
	dto.ConversationDetail
	Users []dto.User `json:"users"`
}

type conversationOutput struct{ Body ConversationBody }

type openDirectInput struct {
	UserID string `path:"user_id" pattern:"^[0-9]+$" maxLength:"20" doc:"The other person."`
}

func (h *Handler) openDirect(ctx context.Context, in *openDirectInput) (*conversationOutput, error) {
	a, err := actorFrom(ctx)
	if err != nil {
		return nil, err
	}
	peer, err := parseID(ctx, "user_id", in.UserID)
	if err != nil {
		return nil, err
	}
	res, err := h.opt.Chat.EnsureDirect(ctx, a, peer)
	if err != nil {
		return nil, fail(ctx, "open direct", err)
	}
	return &conversationOutput{Body: ConversationBody{ConversationDetail: res.Conversation, Users: res.Users}}, nil
}

func (h *Handler) conversation(ctx context.Context, in *IDPath) (*conversationOutput, error) {
	a, err := actorFrom(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseID(ctx, "id", in.ID)
	if err != nil {
		return nil, err
	}
	res, err := h.opt.Chat.Conversation(ctx, a, id)
	if err != nil {
		return nil, fail(ctx, "conversation", err)
	}
	return &conversationOutput{Body: ConversationBody{ConversationDetail: res.Conversation, Users: res.Users}}, nil
}

type messagesInput struct {
	IDPath
	BeforeSeq int64 `query:"before_seq" minimum:"0" doc:"messages before this seq"`
	AfterSeq  int64 `query:"after_seq" minimum:"0" doc:"messages after this seq"`
	AroundSeq int64 `query:"around_seq" minimum:"0" doc:"a page centred on this seq, for jumping to a reply or a mention"`
	Limit     int   `query:"limit" minimum:"0" maximum:"100" doc:"page size, default 50"`
}

type MessageListBody struct {
	Object        string        `json:"object" enum:"list"`
	Items         []dto.Message `json:"items"`
	Users         []dto.User    `json:"users"`
	HasMoreBefore bool          `json:"has_more_before"`
	HasMoreAfter  bool          `json:"has_more_after"`
}

type messagesOutput struct{ Body MessageListBody }

func optSeq(v int64) *int64 {
	if v <= 0 {
		return nil
	}
	return &v
}

func (h *Handler) messages(ctx context.Context, in *messagesInput) (*messagesOutput, error) {
	a, err := actorFrom(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseID(ctx, "id", in.ID)
	if err != nil {
		return nil, err
	}
	page, err := h.opt.Chat.Messages(ctx, a, id, service.MessageQuery{
		BeforeSeq: optSeq(in.BeforeSeq), AfterSeq: optSeq(in.AfterSeq), AroundSeq: optSeq(in.AroundSeq), Limit: in.Limit,
	})
	if err != nil {
		return nil, fail(ctx, "messages", err)
	}
	return &messagesOutput{Body: MessageListBody{Object: "list", Items: page.Messages, Users: page.Users,
		HasMoreBefore: page.HasMoreBefore, HasMoreAfter: page.HasMoreAfter}}, nil
}

type MediaInput struct {
	Type      string `json:"type" enum:"photo"`
	ImageHash string `json:"image_hash" pattern:"^[0-9a-f]{64}$" doc:"an image already uploaded to the image service"`
}

type QuoteInput struct {
	Text   string `json:"text" minLength:"1" doc:"the quoted part, exactly as it appears in the replied message"`
	Offset int    `json:"offset" minimum:"0" doc:"where it starts in the replied message, in UTF-16 code units"`
}

type ContextInput struct {
	Kind  string `json:"kind" maxLength:"64"`
	ID    string `json:"id" maxLength:"64"`
	Title string `json:"title" maxLength:"400"`
	URL   string `json:"url" maxLength:"2048" doc:"an https URL on the calling site's own host"`
}

type SendBody struct {
	ClientMessageID *string       `json:"client_message_id,omitempty" doc:"a UUID the client makes up; resending it returns the first attempt's message"`
	Text            string        `json:"text,omitempty" maxLength:"16384" doc:"at most 4096 UTF-16 code units after trimming; may be empty on a media message"`
	Entities        []dto.Entity  `json:"entities,omitempty" maxItems:"100"`
	Media           *MediaInput   `json:"media,omitempty"`
	MediaGroupID    *string       `json:"media_group_id,omitempty" pattern:"^[0-9]+$" maxLength:"20" doc:"the same value on every photo of one album"`
	ReplyToSeq      *int64        `json:"reply_to_seq,omitempty" minimum:"1"`
	ReplyQuote      *QuoteInput   `json:"reply_quote,omitempty" doc:"quote only part of the replied message"`
	Context         *ContextInput `json:"context,omitempty" doc:"a card pointing at the page the conversation is about"`
	Silent          bool          `json:"silent,omitempty" doc:"deliver without a notification sound"`
}

type sendInput struct {
	IDPath
	Body SendBody
}

type MessageBody struct {
	dto.Message
	Users []dto.User `json:"users"`
}

type messageOutput struct{ Body MessageBody }

func (h *Handler) send(ctx context.Context, in *sendInput) (*messageOutput, error) {
	a, err := actorFrom(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseID(ctx, "id", in.ID)
	if err != nil {
		return nil, err
	}
	b := in.Body
	sin := service.SendInput{
		ClientMessageID: b.ClientMessageID, Text: b.Text, Entities: dto.EntitiesIn(b.Entities),
		ReplyToSeq: b.ReplyToSeq, Silent: b.Silent,
	}
	if b.Media != nil {
		sin.Media = &service.MediaInput{Type: b.Media.Type, ImageHash: b.Media.ImageHash}
	}
	if b.MediaGroupID != nil {
		g, ok := dto.ParseID(*b.MediaGroupID)
		if !ok {
			return nil, fail(ctx, "send", &service.InvalidError{Field: "media_group_id", Reason: "must be a positive decimal id"})
		}
		sin.MediaGroupID = &g
	}
	if b.ReplyQuote != nil {
		sin.ReplyQuote = &service.QuoteInput{Text: b.ReplyQuote.Text, Offset: b.ReplyQuote.Offset}
	}
	if b.Context != nil {
		sin.Context = &service.ContextInput{Kind: b.Context.Kind, ID: b.Context.ID, Title: b.Context.Title, URL: b.Context.URL}
	}
	res, err := h.opt.Chat.Send(ctx, a, id, sin)
	if err != nil {
		return nil, fail(ctx, "send", err)
	}
	return &messageOutput{Body: MessageBody{Message: res.Message, Users: res.Users}}, nil
}

type editInput struct {
	MessagePath
	Body struct {
		Text     string       `json:"text" maxLength:"16384"`
		Entities []dto.Entity `json:"entities,omitempty" maxItems:"100"`
	}
}

func (h *Handler) edit(ctx context.Context, in *editInput) (*messageOutput, error) {
	a, err := actorFrom(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseID(ctx, "id", in.ID)
	if err != nil {
		return nil, err
	}
	res, err := h.opt.Chat.Edit(ctx, a, id, in.Body.Text, dto.EntitiesIn(in.Body.Entities))
	if err != nil {
		return nil, fail(ctx, "edit", err)
	}
	return &messageOutput{Body: MessageBody{Message: res.Message, Users: res.Users}}, nil
}

type deleteInput struct {
	IDPath
	Body struct {
		Seqs        []int64 `json:"seqs" minItems:"1" maxItems:"100"`
		ForEveryone bool    `json:"for_everyone" doc:"true: delete the caller's own messages for everyone; false: hide the messages from the caller alone"`
	}
}

type DeletedBody struct {
	Object      string  `json:"object" enum:"deleted_messages"`
	Seqs        []int64 `json:"seqs" doc:"the messages actually deleted or hidden; ones already gone are left out"`
	ForEveryone bool    `json:"for_everyone"`
}

type deletedOutput struct{ Body DeletedBody }

func (h *Handler) deleteMessages(ctx context.Context, in *deleteInput) (*deletedOutput, error) {
	a, err := actorFrom(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseID(ctx, "id", in.ID)
	if err != nil {
		return nil, err
	}
	done, err := h.opt.Chat.DeleteMessages(ctx, a, id, in.Body.Seqs, in.Body.ForEveryone)
	if err != nil {
		return nil, fail(ctx, "delete messages", err)
	}
	if done == nil {
		done = []int64{}
	}
	return &deletedOutput{Body: DeletedBody{Object: "deleted_messages", Seqs: done, ForEveryone: in.Body.ForEveryone}}, nil
}

type reactInput struct {
	MessagePath
	Body struct {
		Reaction *string `json:"reaction" doc:"a key from GET /v2/chat/reactions; null removes the caller's reaction"`
	}
}

type ReactionsBody struct {
	Object    string              `json:"object" enum:"message_reactions"`
	MessageID string              `json:"message_id"`
	Reactions []dto.ReactionCount `json:"reactions"`
}

type reactionsOutput struct{ Body ReactionsBody }

func (h *Handler) react(ctx context.Context, in *reactInput) (*reactionsOutput, error) {
	a, err := actorFrom(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseID(ctx, "id", in.ID)
	if err != nil {
		return nil, err
	}
	rs, err := h.opt.Chat.React(ctx, a, id, in.Body.Reaction)
	if err != nil {
		return nil, fail(ctx, "react", err)
	}
	return &reactionsOutput{Body: ReactionsBody{Object: "message_reactions", MessageID: in.ID, Reactions: rs}}, nil
}

type readInput struct {
	IDPath
	Body struct {
		MaxSeq int64 `json:"max_seq" minimum:"0" doc:"the newest message the caller has seen; a position behind the current one changes nothing"`
	}
}

type ReadBody struct {
	Object         string `json:"object" enum:"read_state"`
	ConversationID string `json:"conversation_id"`
	LastReadSeq    int64  `json:"last_read_seq"`
	UnreadCount    int32  `json:"unread_count"`
}

type readOutput struct{ Body ReadBody }

func (h *Handler) read(ctx context.Context, in *readInput) (*readOutput, error) {
	a, err := actorFrom(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseID(ctx, "id", in.ID)
	if err != nil {
		return nil, err
	}
	res, err := h.opt.Chat.Read(ctx, a, id, in.Body.MaxSeq)
	if err != nil {
		return nil, fail(ctx, "read", err)
	}
	return &readOutput{Body: ReadBody{Object: "read_state", ConversationID: in.ID, LastReadSeq: res.LastReadSeq, UnreadCount: res.UnreadCount}}, nil
}

type dialogInput struct {
	IDPath
	Body struct {
		Muted        *bool      `json:"muted,omitempty" doc:"true mutes, false unmutes"`
		MutedUntil   *time.Time `json:"muted_until,omitempty" doc:"with muted=true: until when; omit to mute until unmuted"`
		Archived     *bool      `json:"archived,omitempty"`
		Pinned       *bool      `json:"pinned,omitempty" doc:"at most 5 conversations can be pinned"`
		MarkedUnread *bool      `json:"marked_unread,omitempty"`
	}
}

type DialogBody struct {
	Object string `json:"object" enum:"dialog_state"`
	dto.DialogState
}

type dialogOutput struct{ Body DialogBody }

func (h *Handler) updateDialog(ctx context.Context, in *dialogInput) (*dialogOutput, error) {
	a, err := actorFrom(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseID(ctx, "id", in.ID)
	if err != nil {
		return nil, err
	}
	b := in.Body
	st, err := h.opt.Chat.UpdateDialog(ctx, a, id, service.DialogPatch{
		Muted: b.Muted, MutedUntil: b.MutedUntil, Archived: b.Archived, Pinned: b.Pinned, MarkedUnread: b.MarkedUnread,
	})
	if err != nil {
		return nil, fail(ctx, "update dialog", err)
	}
	return &dialogOutput{Body: DialogBody{Object: "dialog_state", DialogState: *st}}, nil
}

type draftInput struct {
	IDPath
	Body struct {
		Text       string       `json:"text" maxLength:"16384"`
		Entities   []dto.Entity `json:"entities,omitempty" maxItems:"100"`
		ReplyToSeq *int64       `json:"reply_to_seq,omitempty" minimum:"1"`
	}
}

type DraftBody struct {
	Object string     `json:"object" enum:"draft"`
	Draft  *dto.Draft `json:"draft" doc:"null when the draft was cleared"`
}

type draftOutput struct{ Body DraftBody }

func (h *Handler) saveDraft(ctx context.Context, in *draftInput) (*draftOutput, error) {
	a, err := actorFrom(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseID(ctx, "id", in.ID)
	if err != nil {
		return nil, err
	}
	d, err := h.opt.Chat.SaveDraft(ctx, a, id, in.Body.Text, dto.EntitiesIn(in.Body.Entities), in.Body.ReplyToSeq)
	if err != nil {
		return nil, fail(ctx, "save draft", err)
	}
	return &draftOutput{Body: DraftBody{Object: "draft", Draft: d}}, nil
}

func (h *Handler) accept(ctx context.Context, in *IDPath) (*struct{}, error) {
	a, err := actorFrom(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseID(ctx, "id", in.ID)
	if err != nil {
		return nil, err
	}
	if err := h.opt.Chat.Accept(ctx, a, id); err != nil {
		return nil, fail(ctx, "accept", err)
	}
	return nil, nil
}

type clearInput struct {
	IDPath
	Body *struct {
		Remove bool `json:"remove,omitempty" doc:"also take the conversation out of the caller's lists until the next message"`
	}
}

func (h *Handler) clearHistory(ctx context.Context, in *clearInput) (*struct{}, error) {
	a, err := actorFrom(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseID(ctx, "id", in.ID)
	if err != nil {
		return nil, err
	}
	remove := in.Body != nil && in.Body.Remove
	if err := h.opt.Chat.ClearHistory(ctx, a, id, remove); err != nil {
		return nil, fail(ctx, "clear history", err)
	}
	return nil, nil
}

type pinInput struct {
	IDPath
	Seq int64 `path:"seq" minimum:"1" doc:"The message's position in the conversation."`
}

func (h *Handler) pin(ctx context.Context, in *pinInput) (*struct{}, error) {
	return h.setPinned(ctx, in, true)
}

func (h *Handler) unpin(ctx context.Context, in *pinInput) (*struct{}, error) {
	return h.setPinned(ctx, in, false)
}

func (h *Handler) setPinned(ctx context.Context, in *pinInput, pinned bool) (*struct{}, error) {
	a, err := actorFrom(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseID(ctx, "id", in.ID)
	if err != nil {
		return nil, err
	}
	if err := h.opt.Chat.SetPinned(ctx, a, id, in.Seq, pinned); err != nil {
		return nil, fail(ctx, "pin", err)
	}
	return nil, nil
}

func (h *Handler) typing(ctx context.Context, in *IDPath) (*struct{}, error) {
	a, err := actorFrom(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseID(ctx, "id", in.ID)
	if err != nil {
		return nil, err
	}
	if err := h.opt.Chat.Typing(ctx, a, id); err != nil {
		return nil, fail(ctx, "typing", err)
	}
	return nil, nil
}

type reportInput struct {
	MessagePath
	Body struct {
		Reason string  `json:"reason" enum:"spam,harassment,sexual,violence,illegal,other"`
		Note   *string `json:"note,omitempty" maxLength:"2000" doc:"at most 500 characters"`
	}
}

type ReportBody struct {
	Object  string `json:"object" enum:"report"`
	ID      string `json:"id"`
	Created bool   `json:"created" doc:"false when the caller had already reported this message"`
}

type reportOutput struct{ Body ReportBody }

func (h *Handler) report(ctx context.Context, in *reportInput) (*reportOutput, error) {
	a, err := actorFrom(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseID(ctx, "id", in.ID)
	if err != nil {
		return nil, err
	}
	res, err := h.opt.Chat.Report(ctx, a, id, in.Body.Reason, in.Body.Note)
	if err != nil {
		return nil, fail(ctx, "report", err)
	}
	return &reportOutput{Body: ReportBody{Object: "report", ID: dto.ID(res.ReportID), Created: res.Created}}, nil
}

type settingsOutput struct{ Body dto.Settings }

func (h *Handler) settings(ctx context.Context, _ *struct{}) (*settingsOutput, error) {
	a, err := actorFrom(ctx)
	if err != nil {
		return nil, err
	}
	st, err := h.opt.Chat.Settings(ctx, a)
	if err != nil {
		return nil, fail(ctx, "settings", err)
	}
	return &settingsOutput{Body: *st}, nil
}

type settingsInput struct {
	Body struct {
		AllowIncoming     *string `json:"allow_incoming,omitempty" enum:"all,following,none"`
		AcceptRequests    *bool   `json:"accept_requests,omitempty"`
		AllowGroupInvites *string `json:"allow_group_invites,omitempty" enum:"all,following,none"`
	}
}

func (h *Handler) updateSettings(ctx context.Context, in *settingsInput) (*settingsOutput, error) {
	a, err := actorFrom(ctx)
	if err != nil {
		return nil, err
	}
	st, err := h.opt.Chat.UpdateSettings(ctx, a, service.SettingsPatch{
		AllowIncoming: in.Body.AllowIncoming, AcceptRequests: in.Body.AcceptRequests, AllowGroupInvites: in.Body.AllowGroupInvites,
	})
	if err != nil {
		return nil, fail(ctx, "update settings", err)
	}
	return &settingsOutput{Body: *st}, nil
}

type ReactionListBody struct {
	Object string               `json:"object" enum:"list"`
	Items  []dto.ReactionOption `json:"items"`
}

type reactionListOutput struct{ Body ReactionListBody }

func (h *Handler) reactions(ctx context.Context, _ *struct{}) (*reactionListOutput, error) {
	if _, err := actorFrom(ctx); err != nil {
		return nil, err
	}
	return &reactionListOutput{Body: ReactionListBody{Object: "list", Items: service.Reactions()}}, nil
}

type RealtimeTokenBody struct {
	Object    string    `json:"object" enum:"realtime_token"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	URL       string    `json:"url" doc:"the Centrifugo WebSocket endpoint"`
}

type realtimeTokenOutput struct{ Body RealtimeTokenBody }

func (h *Handler) realtimeToken(ctx context.Context, _ *struct{}) (*realtimeTokenOutput, error) {
	a, err := actorFrom(ctx)
	if err != nil {
		return nil, err
	}
	if h.opt.Tokens == nil || h.opt.RealtimeURL == "" {
		return nil, stamp(ctx, problem.New(problem.CodeServiceUnavailable, "", "", "realtime delivery is not configured; poll /v2/chat/updates."))
	}
	tok, exp, err := h.opt.Tokens.Issue(a.UserID, time.Now())
	if err != nil {
		return nil, fail(ctx, "realtime token", err)
	}
	return &realtimeTokenOutput{Body: RealtimeTokenBody{Object: "realtime_token", Token: tok, ExpiresAt: exp, URL: h.opt.RealtimeURL}}, nil
}
