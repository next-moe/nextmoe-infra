package handler

import (
	"context"
	"net/http"

	"api/internal/platform/community/dto"
	"api/internal/platform/community/model"
	"api/internal/platform/community/service"

	"github.com/danielgtaylor/huma/v2"
)

func (s *Server) registerThreadModeration(api huma.API) {
	tags := []string{"community-moderation"}
	huma.Register(api, huma.Operation{OperationID: "moveTopic", Method: http.MethodPost, Path: "/api/v1/community/threads/{id}/move",
		Summary: "Move a topic to another board", Tags: tags}, s.moveTopic)
	huma.Register(api, huma.Operation{OperationID: "pinTopic", Method: http.MethodPost, Path: "/api/v1/community/threads/{id}/pin",
		Summary: "Pin a topic on its board or site-wide, optionally until a time; scope 0 unpins", Tags: tags}, s.pinTopic)
	huma.Register(api, huma.Operation{OperationID: "closeThread", Method: http.MethodPost, Path: "/api/v1/community/threads/{id}/close",
		Summary: "Close a thread to new posts, or reopen it", Tags: tags}, s.closeThread)
	huma.Register(api, huma.Operation{OperationID: "markAnswer", Method: http.MethodPost, Path: "/api/v1/community/threads/{id}/answer",
		Summary: "Mark the reply that answers a Q&A-board topic or a feedback thread (the thread's author, or a moderator)", Tags: tags}, s.markAnswer)
}

type moveTopicInput struct {
	ID   int64 `path:"id"`
	Body dto.MoveTopicRequest
}

func (s *Server) moveTopic(ctx context.Context, in *moveTopicInput) (*threadOutput, error) {
	return s.moderate(ctx, "move topic", func(ctx context.Context) (*model.CommunityThread, error) {
		return s.threads.Move(ctx, in.ID, in.Body.BoardID, in.Body.ActorID)
	})
}

type pinTopicInput struct {
	ID   int64 `path:"id"`
	Body dto.PinTopicRequest
}

func (s *Server) pinTopic(ctx context.Context, in *pinTopicInput) (*threadOutput, error) {
	return s.moderate(ctx, "pin topic", func(ctx context.Context) (*model.CommunityThread, error) {
		return s.threads.Pin(ctx, in.ID, in.Body.Scope, in.Body.Until, in.Body.ActorID)
	})
}

type closeThreadInput struct {
	ID   int64 `path:"id"`
	Body dto.CloseThreadRequest
}

func (s *Server) closeThread(ctx context.Context, in *closeThreadInput) (*threadOutput, error) {
	return s.moderate(ctx, "close thread", func(ctx context.Context) (*model.CommunityThread, error) {
		return s.threads.SetClosed(ctx, in.ID, in.Body.Closed, in.Body.ActorID)
	})
}

type markAnswerInput struct {
	ID   int64 `path:"id"`
	Body dto.MarkAnswerRequest
}

func (s *Server) markAnswer(ctx context.Context, in *markAnswerInput) (*threadOutput, error) {
	return s.moderate(ctx, "mark answer", func(ctx context.Context) (*model.CommunityThread, error) {
		return s.threads.SetAnswer(ctx, in.ID, in.Body.PostID, in.Body.ActorID, in.Body.AsModerator)
	})
}

func (s *Server) moderate(ctx context.Context, op string, act func(context.Context) (*model.CommunityThread, error)) (*threadOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	thread, err := act(service.WithCallerSite(ctx, site))
	if err != nil {
		return nil, mapErr(op, err)
	}
	return &threadOutput{Body: okEnvelope(dto.ThreadResponse{Thread: toThreadView(thread)})}, nil
}
