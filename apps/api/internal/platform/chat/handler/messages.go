package handler

import (
	"context"

	"api/internal/platform/chat/dto"
	"api/internal/platform/chat/service"
)

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
