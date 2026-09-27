package handler

import (
	"context"
	"time"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/chat/dto"
	"api/internal/platform/chat/service"
)

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
