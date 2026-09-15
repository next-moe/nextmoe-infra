package handler

import (
	"context"
	"net/http"

	"api/internal/platform/community/dto"
	"api/internal/platform/community/repository"
	"api/internal/platform/community/service"
	"api/pkg/errors"
)

type threadReadInput struct {
	ID   int64 `path:"id"`
	Body dto.ThreadReadRequest
}

type threadUserOutput struct {
	Body Envelope[dto.ThreadUserView]
}

func (s *Server) markThreadRead(ctx context.Context, in *threadReadInput) (*threadUserOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	ctx = service.WithCallerSite(ctx, site)
	state, err := s.engagement.MarkRead(ctx, in.ID, in.Body.UserID, in.Body.LastReadPostNumber)
	if err != nil {
		return nil, mapErr("mark thread read", err)
	}
	return &threadUserOutput{Body: okEnvelope(toThreadUserView(state))}, nil
}

type threadNotificationInput struct {
	ID   int64 `path:"id"`
	Body dto.ThreadNotificationRequest
}

func (s *Server) setThreadNotification(ctx context.Context, in *threadNotificationInput) (*threadUserOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	ctx = service.WithCallerSite(ctx, site)
	state, err := s.engagement.SetNotificationLevel(ctx, in.ID, in.Body.UserID, in.Body.Level)
	if err != nil {
		return nil, mapErr("set thread notification", err)
	}
	return &threadUserOutput{Body: okEnvelope(toThreadUserView(state))}, nil
}

type threadStatesInput struct{ Body dto.ThreadStatesRequest }
type threadStatesOutput struct {
	Body Envelope[dto.ThreadStatesResponse]
}

func (s *Server) threadStates(ctx context.Context, in *threadStatesInput) (*threadStatesOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	ids, he := dedupeIDs(in.Body.ThreadIDs)
	if he != nil {
		return nil, he
	}
	states, err := s.engagement.States(site, in.Body.UserID, ids)
	if err != nil {
		return nil, mapErr("thread states", err)
	}
	return &threadStatesOutput{Body: okEnvelope(dto.ThreadStatesResponse{States: toThreadUserViews(states)})}, nil
}

type unreadListInput struct {
	ID     int64  `path:"id"`
	Cursor string `query:"cursor" doc:"opaque cursor from the previous page"`
	Limit  int    `query:"limit" doc:"page size (max 100, default 50)"`
}
type unreadListOutput struct {
	Body Envelope[dto.UnreadListResponse]
}

func (s *Server) listUnread(ctx context.Context, in *unreadListInput) (*unreadListOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	cursor, err := decodeThreadCursor(in.Cursor)
	if err != nil || (cursor.ID != 0 && cursor.Sort != repository.ThreadSortActivity) {
		return nil, apiErrMsg(http.StatusBadRequest, errors.ErrInvalidParam, "malformed cursor")
	}
	limit := clampLimit(in.Limit)
	rows, total, err := s.engagement.ListUnread(site, in.ID, cursor, limit)
	if err != nil {
		return nil, mapErr("list unread", err)
	}
	return &unreadListOutput{Body: okEnvelope(dto.UnreadListResponse{
		Threads: toUnreadThreadViews(rows, in.ID), NextCursor: unreadPageCursor(rows, limit), Total: total,
	})}, nil
}
