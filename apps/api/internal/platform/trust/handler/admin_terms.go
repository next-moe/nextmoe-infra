package handler

import (
	"context"
	"net/http"

	"api/internal/platform/trust/dto"
	"api/internal/platform/trust/model"
	"api/internal/platform/trust/service"

	"github.com/danielgtaylor/huma/v2"
)

func (s *AdminServer) registerTerms(api huma.API) {
	tags := []string{"trust-admin"}
	const note = "Requires trust.term_manage (admin/ren); a moderator is rejected 403. Terms are never hard-deleted — deprecate retires them."

	huma.Register(api, huma.Operation{OperationID: "listTrustTerms", Method: http.MethodGet, Path: "/api/v1/admin/trust/terms",
		Summary: "List Tier0 word-list terms (filter by site/kind; optionally include deprecated)", Tags: tags,
		Description: note}, s.listTerms)
	huma.Register(api, huma.Operation{OperationID: "createTrustTerm", Method: http.MethodPost, Path: "/api/v1/admin/trust/terms",
		Summary: "Register a Tier0 term (raw word is normalized server-side before storage)", Tags: tags,
		Description: note}, s.createTerm)
	huma.Register(api, huma.Operation{OperationID: "deprecateTrustTerm", Method: http.MethodPost, Path: "/api/v1/admin/trust/terms/{id}/deprecate",
		Summary: "Deprecate (retire) a Tier0 term — never a hard delete", Tags: tags,
		Description: note}, s.deprecateTerm)
}

type listTermsInput struct {
	Site              string `query:"site" doc:"filter to one site; empty = all (global + every site)"`
	Kind              int16  `query:"kind" default:"-1" doc:"0=suspect 1=banned; -1 = all"`
	Purpose           int16  `query:"purpose" default:"-1" doc:"0=abuse 1=compliance; -1 = all"`
	IncludeDeprecated bool   `query:"include_deprecated" doc:"include retired terms"`
	Q                 string `query:"q" doc:"substring search over the normalized term (the raw input is normalized the same way before matching)"`
	Page              int    `query:"page" doc:"1-based page number"`
	Limit             int    `query:"limit" doc:"terms per page (default 50, max 200)"`
}
type termsOutput struct {
	Body Envelope[dto.TermsResponse]
}

func (s *AdminServer) listTerms(ctx context.Context, in *listTermsInput) (*termsOutput, error) {
	if he := s.requireTermManage(ctx); he != nil {
		return nil, he
	}
	terms, total, err := s.terms.List(ctx, service.TermFilters{
		Site: in.Site, Kind: optionalFilter(in.Kind), Purpose: optionalFilter(in.Purpose),
		IncludeDeprecated: in.IncludeDeprecated,
		Query:             in.Q, Page: in.Page, Limit: in.Limit,
	})
	if err != nil {
		return nil, mapAdminErr("list terms", err)
	}
	return &termsOutput{Body: okEnvelope(dto.TermsResponse{Terms: toTermViews(terms), Total: total})}, nil
}

type createTermInput struct{ Body dto.CreateTermRequest }
type termOutput struct {
	Body Envelope[dto.TermView]
}

func (s *AdminServer) createTerm(ctx context.Context, in *createTermInput) (*termOutput, error) {
	if he := s.requireTermManage(ctx); he != nil {
		return nil, he
	}
	term, err := s.terms.Create(ctx, service.CreateTermParams{
		ActorID: adminIDFromCtx(ctx), Site: in.Body.Site, Term: in.Body.Term,
		Kind: in.Body.Kind, Purpose: in.Body.Purpose, Note: in.Body.Note,
	})
	if err != nil {
		return nil, mapAdminErr("create term", err)
	}
	return &termOutput{Body: okEnvelope(toTermView(*term))}, nil
}

type deprecateTermInput struct {
	ID int64 `path:"id"`
}

func (s *AdminServer) deprecateTerm(ctx context.Context, in *deprecateTermInput) (*termOutput, error) {
	if he := s.requireTermManage(ctx); he != nil {
		return nil, he
	}
	term, err := s.terms.Deprecate(ctx, adminIDFromCtx(ctx), in.ID)
	if err != nil {
		return nil, mapAdminErr("deprecate term", err)
	}
	return &termOutput{Body: okEnvelope(toTermView(*term))}, nil
}

func toTermView(t model.TrustTerm) dto.TermView {
	return dto.TermView{
		ID: t.ID, Site: t.Site, TermNorm: t.TermNorm, Kind: t.Kind, Purpose: t.Purpose,
		Note: t.Note, IsDeprecated: t.IsDeprecated, CreatedAt: t.CreatedAt,
	}
}

func toTermViews(ts []model.TrustTerm) []dto.TermView {
	out := make([]dto.TermView, 0, len(ts))
	for _, t := range ts {
		out = append(out, toTermView(t))
	}
	return out
}
