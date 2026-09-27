package handler

import (
	"context"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"

	"api/internal/platform/community/dto"
	"api/internal/platform/community/repository"
	"api/internal/platform/community/service"
	"api/pkg/errors"

	"github.com/danielgtaylor/huma/v2"
)

func (s *Server) registerAnchorPresentations(api huma.API) {
	tags := []string{"community-activities"}
	huma.Register(api, huma.Operation{OperationID: "writeAnchorPresentations", Method: http.MethodPut, Path: "/api/v1/community/anchor-presentations",
		Summary: "Push up to 100 of this site's comment-wall pages (title, URL, work, content limit) under the revision rule; one outcome per item", Tags: tags}, s.writeAnchorPresentations)
	huma.Register(api, huma.Operation{OperationID: "listAnchorPresentations", Method: http.MethodGet, Path: "/api/v1/community/anchor-presentations",
		Summary: "This site's stored anchor presentations in anchor order, tombstones included: the reconciliation read", Tags: tags}, s.listAnchorPresentations)
}

type writeAnchorPresentationsInput struct {
	Body dto.AnchorPresentationWriteRequest
}
type writeAnchorPresentationsOutput struct {
	Body Envelope[dto.AnchorPresentationWriteResponse]
}

func (s *Server) writeAnchorPresentations(ctx context.Context, in *writeAnchorPresentationsInput) (*writeAnchorPresentationsOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	items := make([]service.AnchorPresentationInput, len(in.Body.Items))
	for i, it := range in.Body.Items {
		items[i] = service.AnchorPresentationInput{
			AnchorKind: it.AnchorKind, AnchorID: it.AnchorID, Title: it.Title, URL: it.URL,
			WorkID: it.WorkID, ContentLimit: it.ContentLimit, Revision: it.Revision, Removed: it.Removed,
		}
	}
	results, err := s.activities.WritePresentations(ctx, site, activityURLHosts(clientFromCtx(ctx)), items)
	if err != nil {
		return nil, mapErr("write anchor presentations", err)
	}
	out := make([]dto.AnchorPresentationOutcome, len(results))
	for i, r := range results {
		out[i] = dto.AnchorPresentationOutcome{AnchorKind: r.AnchorKind, AnchorID: r.AnchorID, Outcome: r.Outcome, Reason: r.Reason}
	}
	return &writeAnchorPresentationsOutput{Body: okEnvelope(dto.AnchorPresentationWriteResponse{Results: out})}, nil
}

type listAnchorPresentationsInput struct {
	Cursor string `query:"cursor" doc:"opaque cursor from the previous page"`
	Limit  int    `query:"limit" doc:"page size (max 1000, default 1000)"`
}
type listAnchorPresentationsOutput struct {
	Body Envelope[dto.AnchorPresentationListResponse]
}

func (s *Server) listAnchorPresentations(ctx context.Context, in *listAnchorPresentationsInput) (*listAnchorPresentationsOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	after, he := decodePresentationCursor(in.Cursor)
	if he != nil {
		return nil, he
	}
	rows, limit, err := s.activities.ListPresentations(site, after, in.Limit)
	if err != nil {
		return nil, mapErr("list anchor presentations", err)
	}
	views := make([]dto.AnchorPresentationView, len(rows))
	for i := range rows {
		r := &rows[i]
		views[i] = dto.AnchorPresentationView{
			AnchorKind: r.AnchorKind, AnchorID: r.AnchorID, Title: r.Title, URL: r.URL, WorkID: r.WorkID,
			ContentLimit: contentLimitName(r.ContentLimit), Revision: r.Revision, Removed: r.RemovedAt != nil,
			UpdatedAt: r.UpdatedAt, RemovedAt: r.RemovedAt,
		}
	}
	next := ""
	if len(rows) > 0 && len(rows) == limit {
		last := rows[len(rows)-1]
		next = base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(int(last.AnchorKind)) + ":" + last.AnchorID))
	}
	return &listAnchorPresentationsOutput{Body: okEnvelope(dto.AnchorPresentationListResponse{Presentations: views, NextCursor: next})}, nil
}

func decodePresentationCursor(s string) (*repository.AnchorPresentationCursor, *houseError) {
	if s == "" {
		return nil, nil
	}
	bad := apiErrMsg(http.StatusBadRequest, errors.ErrInvalidParam, "malformed cursor")
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, bad
	}
	kind, id, ok := strings.Cut(string(raw), ":")
	k, err := strconv.ParseInt(kind, 10, 16)
	if !ok || err != nil || id == "" {
		return nil, bad
	}
	return &repository.AnchorPresentationCursor{AnchorKind: int16(k), AnchorID: id}, nil
}
