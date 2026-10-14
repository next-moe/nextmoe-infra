package handler

import (
	"context"
	stderrors "errors"
	"log/slog"
	"net/http"

	"api/internal/platform/artifact/dto"
	"api/internal/platform/artifact/service"
	"api/pkg/errors"
	"api/pkg/utils"

	"github.com/danielgtaylor/huma/v2"
)

const CompositeBodyLimit = 16 << 20

type compositeIDInput struct {
	ID string `path:"id" pattern:"^[0-9a-f]{32}$" doc:"Composite id"`
}

type createCompositeInput struct {
	Body dto.CreateCompositeRequest
}

type compositeOutput struct {
	Body Envelope[*dto.CompositeResponse]
}

type compositeListData struct {
	Items []dto.CompositeResponse `json:"items"`
	Total int64                   `json:"total"`
}

type compositeListOutput struct {
	Body Envelope[compositeListData]
}

type compositeDeleteData struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
}

type compositeDeleteOutput struct {
	Body Envelope[compositeDeleteData]
}

func (s *HumaServer) registerComposites(api huma.API) {
	tags := []string{"composites"}
	huma.Register(api, huma.Operation{
		OperationID: "createComposite", Method: http.MethodPost, Path: "/api/v1/artifacts/composites",
		Summary:      "Create a composite: one download spliced at serve time from byte ranges of the site's artifacts plus inline bytes (idempotent)",
		Tags:         tags,
		MaxBodyBytes: CompositeBodyLimit,
	}, s.createComposite)
	huma.Register(api, huma.Operation{
		OperationID: "listComposites", Method: http.MethodGet, Path: "/api/v1/artifacts/composites",
		Summary: "List the site's composites (paginated, newest first)", Tags: tags,
	}, s.listComposites)
	huma.Register(api, huma.Operation{
		OperationID: "getComposite", Method: http.MethodGet, Path: "/api/v1/artifacts/composites/{id}",
		Summary: "Get a composite", Tags: tags,
	}, s.getComposite)
	huma.Register(api, huma.Operation{
		OperationID: "deleteComposite", Method: http.MethodDelete, Path: "/api/v1/artifacts/composites/{id}",
		Summary: "Delete a composite: its URL stops serving and its artifacts are unpinned", Tags: tags,
	}, s.deleteComposite)
}

func (s *HumaServer) createComposite(ctx context.Context, in *createCompositeInput) (*compositeOutput, error) {
	site, err := siteWideSite(ctx)
	if err != nil {
		return nil, err
	}
	if !uploadEnabled(ctx) {
		return nil, apiErr(http.StatusServiceUnavailable, errors.ErrArtifactUploadDisabled)
	}
	client := clientFromCtx(ctx)
	resp, err := s.composites.Create(ctx, in.Body, service.CompositeParams{
		Site:    site,
		Client:  client.ID,
		CDNBase: client.ArtifactCDNBase,
	})
	if err != nil {
		if stderrors.Is(err, service.ErrInvalidPlan) {
			return nil, apiErrMsg(http.StatusUnprocessableEntity, errors.ErrArtifactInvalid, err.Error())
		}
		slog.Error("composite create", "site", site, "err", err)
		return nil, apiErr(http.StatusInternalServerError, errors.ErrArtifactStoreFailed)
	}
	return &compositeOutput{Body: okEnvelope(resp)}, nil
}

func (s *HumaServer) listComposites(ctx context.Context, in *listInput) (*compositeListOutput, error) {
	site, err := siteWideSite(ctx)
	if err != nil {
		return nil, err
	}
	p := utils.Pagination{Page: in.Page, PageSize: in.PageSize}
	p.Normalize()
	items, total, err := s.composites.List(ctx, site, clientFromCtx(ctx).ArtifactCDNBase, p.Offset(), p.Limit())
	if err != nil {
		slog.Error("composite list", "site", site, "err", err)
		return nil, apiErr(http.StatusInternalServerError, errors.ErrArtifactStoreFailed)
	}
	return &compositeListOutput{Body: okEnvelope(compositeListData{Items: items, Total: total})}, nil
}

func (s *HumaServer) getComposite(ctx context.Context, in *compositeIDInput) (*compositeOutput, error) {
	site, err := siteWideSite(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := s.composites.Get(ctx, in.ID, site, clientFromCtx(ctx).ArtifactCDNBase)
	if err != nil {
		if stderrors.Is(err, service.ErrNotFound) {
			return nil, apiErr(http.StatusNotFound, errors.ErrArtifactNotFound)
		}
		slog.Error("composite get", "id", in.ID, "site", site, "err", err)
		return nil, apiErr(http.StatusInternalServerError, errors.ErrArtifactStoreFailed)
	}
	return &compositeOutput{Body: okEnvelope(resp)}, nil
}

func (s *HumaServer) deleteComposite(ctx context.Context, in *compositeIDInput) (*compositeDeleteOutput, error) {
	site, err := siteWideSite(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.composites.Delete(ctx, in.ID, site); err != nil {
		if stderrors.Is(err, service.ErrNotFound) {
			return nil, apiErr(http.StatusNotFound, errors.ErrArtifactNotFound)
		}
		slog.Error("composite delete", "id", in.ID, "site", site, "err", err)
		return nil, apiErr(http.StatusInternalServerError, errors.ErrArtifactStoreFailed)
	}
	return &compositeDeleteOutput{Body: okEnvelope(compositeDeleteData{ID: in.ID, Deleted: true})}, nil
}
