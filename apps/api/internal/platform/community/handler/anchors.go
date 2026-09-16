package handler

import (
	"context"
	"net/http"
	"strconv"

	"api/internal/platform/community/dto"
	"api/internal/platform/community/model"
	"api/internal/platform/community/service"
	"api/pkg/errors"

	"github.com/danielgtaylor/huma/v2"
)

func (s *Server) registerAnchors(api huma.API) {
	tags := []string{"community-engagement"}
	huma.Register(api, huma.Operation{OperationID: "setAnchorNotification", Method: http.MethodPost, Path: "/api/v1/community/anchors/notification",
		Summary: "Set a user's notification level for an anchor (0=muted 1=normal 3=watching 4=watching first post)", Tags: tags}, s.setAnchorNotification)
	huma.Register(api, huma.Operation{OperationID: "anchorStates", Method: http.MethodPost, Path: "/api/v1/community/anchors/states",
		Summary: "Batch stored subscription state for a user over a set of anchors", Tags: tags}, s.anchorStates)
	huma.Register(api, huma.Operation{OperationID: "listAnchorSubscriptions", Method: http.MethodGet, Path: "/api/v1/community/users/{id}/anchor-subscriptions",
		Summary: "List a user's anchor subscriptions on this site, newest first", Tags: tags}, s.listAnchorSubscriptions)
}

type anchorNotificationInput struct{ Body dto.AnchorNotificationRequest }
type anchorSubscriptionOutput struct {
	Body Envelope[dto.AnchorSubscriptionView]
}

func (s *Server) setAnchorNotification(ctx context.Context, in *anchorNotificationInput) (*anchorSubscriptionOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	st, err := s.engagement.SetAnchorLevel(ctx, site, in.Body.UserID, in.Body.AnchorKind, in.Body.AnchorID, in.Body.Level)
	if err != nil {
		return nil, mapErr("set anchor notification", err)
	}
	return &anchorSubscriptionOutput{Body: okEnvelope(toAnchorSubscriptionViewFromState(st))}, nil
}

type anchorStatesInput struct{ Body dto.AnchorStatesRequest }
type anchorStatesOutput struct {
	Body Envelope[dto.AnchorStatesResponse]
}

func (s *Server) anchorStates(ctx context.Context, in *anchorStatesInput) (*anchorStatesOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	refs := make([]service.AnchorRef, len(in.Body.Anchors))
	for i, a := range in.Body.Anchors {
		refs[i] = service.AnchorRef{AnchorKind: a.AnchorKind, AnchorID: a.AnchorID}
	}
	rows, err := s.engagement.AnchorStates(site, in.Body.UserID, refs)
	if err != nil {
		return nil, mapErr("anchor states", err)
	}
	return &anchorStatesOutput{Body: okEnvelope(dto.AnchorStatesResponse{States: toAnchorSubscriptionViews(rows)})}, nil
}

type listAnchorSubscriptionsInput struct {
	ID         int64  `path:"id"`
	AnchorKind int16  `query:"anchor_kind" default:"-1" doc:"optional anchor-kind filter (0-4); -1 = all kinds"`
	Cursor     string `query:"cursor" doc:"opaque cursor from the previous page"`
	Limit      int    `query:"limit" doc:"page size (max 100, default 50)"`
}
type listAnchorSubscriptionsOutput struct {
	Body Envelope[dto.AnchorSubscriptionListResponse]
}

func (s *Server) listAnchorSubscriptions(ctx context.Context, in *listAnchorSubscriptionsInput) (*listAnchorSubscriptionsOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	afterID, he := parseAnchorCursor(in.Cursor)
	if he != nil {
		return nil, he
	}
	limit := clampLimit(in.Limit)
	rows, err := s.engagement.ListAnchorSubscriptions(site, in.ID, in.AnchorKind, afterID, limit)
	if err != nil {
		return nil, mapErr("list anchor subscriptions", err)
	}
	return &listAnchorSubscriptionsOutput{Body: okEnvelope(dto.AnchorSubscriptionListResponse{
		Subscriptions: toAnchorSubscriptionViews(rows), NextCursor: anchorSubsCursor(rows, limit),
	})}, nil
}

func parseAnchorCursor(cursor string) (int64, *houseError) {
	if cursor == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(cursor, 10, 64)
	if err != nil || id <= 0 {
		return 0, apiErrMsg(http.StatusBadRequest, errors.ErrInvalidParam, "malformed cursor")
	}
	return id, nil
}

func toAnchorSubscriptionViewFromState(st *service.AnchorState) dto.AnchorSubscriptionView {
	return dto.AnchorSubscriptionView{
		UserID: st.UserID, AnchorKind: st.AnchorKind, AnchorID: st.AnchorID,
		BoardID: boardIDOf(st.AnchorKind, st.AnchorID), NotificationLevel: st.NotificationLevel,
	}
}

func toAnchorSubscriptionView(row *model.CommunityAnchorUser) dto.AnchorSubscriptionView {
	return dto.AnchorSubscriptionView{
		UserID: row.UserID, AnchorKind: row.AnchorKind, AnchorID: row.AnchorID,
		BoardID: boardIDOf(row.AnchorKind, row.AnchorID), NotificationLevel: row.NotificationLevel,
	}
}

func toAnchorSubscriptionViews(rows []model.CommunityAnchorUser) []dto.AnchorSubscriptionView {
	out := make([]dto.AnchorSubscriptionView, len(rows))
	for i := range rows {
		out[i] = toAnchorSubscriptionView(&rows[i])
	}
	return out
}

func anchorSubsCursor(rows []model.CommunityAnchorUser, limit int) string {
	if len(rows) < limit || len(rows) == 0 {
		return ""
	}
	return strconv.FormatInt(rows[len(rows)-1].ID, 10)
}
