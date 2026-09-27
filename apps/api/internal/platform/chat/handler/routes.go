package handler

import (
	"context"
	"net/http"

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
	huma.Register(api, huma.Operation{OperationID: "uploadChatImage", Method: http.MethodPost, Path: "/v2/chat/images", Tags: tag, Errors: writeErrs,
		DefaultStatus: http.StatusCreated,
		Summary:       "Upload a photo for a message; send its image_hash as media.image_hash",
		Description:   "multipart/form-data with one part named file. Chat keeps the photos it hosts alive; a hash from anywhere else is refused when sent."}, h.uploadImage)
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
