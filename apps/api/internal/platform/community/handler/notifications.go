package handler

import (
	"context"
	"net/http"
	"strconv"

	"api/internal/platform/community/dto"
	"api/internal/platform/community/model"
	"api/pkg/errors"

	"github.com/danielgtaylor/huma/v2"
)

func (s *Server) registerNotifications(api huma.API) {
	tags := []string{"community-notifications"}
	huma.Register(api, huma.Operation{OperationID: "listNotifications", Method: http.MethodGet, Path: "/api/v1/community/users/{id}/notifications",
		Summary: "List a user's notifications on this site, newest first", Tags: tags}, s.listNotifications)
	huma.Register(api, huma.Operation{OperationID: "markNotificationsRead", Method: http.MethodPost, Path: "/api/v1/community/users/{id}/notifications/read",
		Summary: "Mark a user's notifications on this site read (by ids, or all)", Tags: tags}, s.markNotificationsRead)
	huma.Register(api, huma.Operation{OperationID: "notificationFeed", Method: http.MethodGet, Path: "/api/v1/community/notifications/feed",
		Summary: "A site's notification feed in seq order, for a site that mirrors into its own inbox", Tags: tags}, s.notificationFeed)
}

type listNotificationsInput struct {
	ID         int64  `path:"id"`
	Cursor     string `query:"cursor" doc:"opaque cursor from the previous page (the last row's seq)"`
	Limit      int    `query:"limit" doc:"page size (max 100, default 50)"`
	UnreadOnly bool   `query:"unread_only"`
}
type listNotificationsOutput struct {
	Body Envelope[dto.NotificationListResponse]
}

func (s *Server) listNotifications(ctx context.Context, in *listNotificationsInput) (*listNotificationsOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	cursor, he := parseNotificationCursor(in.Cursor)
	if he != nil {
		return nil, he
	}
	limit := clampLimit(in.Limit)
	rows, unread, err := s.notify.List(site, in.ID, cursor, in.UnreadOnly, limit)
	if err != nil {
		return nil, mapErr("list notifications", err)
	}
	views, err := s.notificationViews(rows)
	if err != nil {
		return nil, mapErr("list notification activities", err)
	}
	return &listNotificationsOutput{Body: okEnvelope(dto.NotificationListResponse{
		Notifications: views, NextCursor: notificationsCursor(rows, limit), UnreadCount: unread,
	})}, nil
}

type markNotificationsReadInput struct {
	ID   int64 `path:"id"`
	Body dto.MarkNotificationsReadRequest
}
type markNotificationsReadOutput struct {
	Body Envelope[dto.MarkNotificationsReadResponse]
}

func (s *Server) markNotificationsRead(ctx context.Context, in *markNotificationsReadInput) (*markNotificationsReadOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	marked, unread, err := s.notify.MarkRead(site, in.ID, in.Body.IDs, in.Body.All)
	if err != nil {
		return nil, mapErr("mark notifications read", err)
	}
	return &markNotificationsReadOutput{Body: okEnvelope(dto.MarkNotificationsReadResponse{
		Marked: marked, UnreadCount: unread,
	})}, nil
}

type notificationFeedInput struct {
	After int64 `query:"after" doc:"return rows with seq greater than this; 0 = from the start"`
	Limit int   `query:"limit" doc:"page size (default 100, max 500)"`
}
type notificationFeedOutput struct {
	Body Envelope[dto.NotificationFeedResponse]
}

func (s *Server) notificationFeed(ctx context.Context, in *notificationFeedInput) (*notificationFeedOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	rows, next, err := s.notify.Feed(site, in.After, in.Limit)
	if err != nil {
		return nil, mapErr("notification feed", err)
	}
	views, err := s.notificationViews(rows)
	if err != nil {
		return nil, mapErr("notification feed activities", err)
	}
	return &notificationFeedOutput{Body: okEnvelope(dto.NotificationFeedResponse{
		Notifications: views, NextAfter: next,
	})}, nil
}

func parseNotificationCursor(cursor string) (int64, *houseError) {
	if cursor == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(cursor, 10, 64)
	if err != nil || id <= 0 {
		return 0, apiErrMsg(http.StatusBadRequest, errors.ErrInvalidParam, "malformed cursor")
	}
	return id, nil
}

func notificationsCursor(rows []model.CommunityNotification, limit int) string {
	if len(rows) < limit || len(rows) == 0 {
		return ""
	}
	return strconv.FormatInt(rows[len(rows)-1].Seq, 10)
}

func toNotificationView(n *model.CommunityNotification) dto.NotificationView {
	return dto.NotificationView{
		ID: n.ID, UserID: n.UserID, Kind: n.Kind, ThreadID: n.ThreadID,
		AnchorKind: n.AnchorKind, AnchorID: n.AnchorID, BoardID: boardIDOf(n.AnchorKind, n.AnchorID),
		PostID: n.PostID, PostNumber: n.PostNumber, FirstPostNumber: n.FirstPostNumber,
		ActorID: n.ActorID, ActorCount: n.ActorCount, ItemCount: n.ItemCount,
		ReadAt: n.ReadAt, CreatedAt: n.CreatedAt, UpdatedAt: n.UpdatedAt, Seq: n.Seq,
	}
}

func (s *Server) notificationViews(rows []model.CommunityNotification) ([]dto.NotificationView, error) {
	activities, err := s.notify.Activities(rows)
	if err != nil {
		return nil, err
	}
	out := make([]dto.NotificationView, len(rows))
	for i := range rows {
		out[i] = toNotificationView(&rows[i])
		if rows[i].ActivityID == nil {
			continue
		}
		if a, ok := activities[*rows[i].ActivityID]; ok {
			out[i].Activity = &dto.NotificationActivityView{
				ID: a.ID, Site: a.Site, Key: a.Key, Verb: verbName(a.Verb), ObjectKind: a.ObjectKind,
				ObjectLabel: a.ObjectLabel, Title: a.Title, URL: a.URL,
				ContentLimit: contentLimitName(a.ContentLimit), OccurredAt: a.OccurredAt,
			}
		}
	}
	return out, nil
}
