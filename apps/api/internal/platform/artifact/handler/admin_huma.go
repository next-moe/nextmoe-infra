package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"api/internal/platform/artifact/dto"
	"api/internal/platform/artifact/perm"
	"api/internal/platform/artifact/service"
	apperr "api/pkg/errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humafiber"
	"github.com/gofiber/fiber/v3"
)

type adminCtxKey string

const ctxKeyAdminRoles adminCtxKey = "artifact_admin:roles"

func AdminAuthBridge(ctx huma.Context, next func(huma.Context)) {
	fc := humafiber.Unwrap(ctx)
	if roles, ok := fc.Locals("user_roles").([]string); ok {
		ctx = huma.WithValue(ctx, ctxKeyAdminRoles, roles)
	}
	next(ctx)
}

func adminRoles(ctx context.Context) []string {
	roles, _ := ctx.Value(ctxKeyAdminRoles).([]string)
	return roles
}

type AdminHumaServer struct {
	h *AdminHandler
}

func SetupAdmin(app *fiber.App, h *AdminHandler) huma.API {
	InstallErrorEnvelope()

	cfg := huma.DefaultConfig("KUN Artifact Admin API", "1.0.0")
	cfg.OpenAPIPath = ""
	cfg.DocsPath = ""
	cfg.SchemasPath = ""

	api := humafiber.New(app, cfg)
	api.UseMiddleware(AdminAuthBridge)

	s := &AdminHumaServer{h: h}
	s.register(api)
	return api
}

func (s *AdminHumaServer) register(api huma.API) {
	tags := []string{"artifact-admin"}
	huma.Register(api, huma.Operation{
		OperationID: "adminListArtifacts", Method: http.MethodGet, Path: "/api/v1/admin/artifact/list",
		Summary: "List artifacts (paginated, filterable) — requires the ren role", Tags: tags,
	}, s.list)
	huma.Register(api, huma.Operation{
		OperationID: "adminArtifactStats", Method: http.MethodGet, Path: "/api/v1/admin/artifact/stats",
		Summary: "Aggregate artifact usage stats (admin dashboard)", Tags: tags,
	}, s.stats)
	huma.Register(api, huma.Operation{
		OperationID: "adminDeleteArtifact", Method: http.MethodDelete, Path: "/api/v1/admin/artifact/{uuid}",
		Summary: "Soft-delete an artifact — requires the ren role", Tags: tags,
	}, s.delete)
	huma.Register(api, huma.Operation{
		OperationID: "adminReclaimArtifact", Method: http.MethodPost, Path: "/api/v1/admin/artifact/{uuid}/reclaim",
		Summary: "Reclaim an interrupted upload (abort multipart + delete) — requires the ren role", Tags: tags,
	}, s.reclaim)
}

type adminListInput struct {
	Site        string `query:"site" doc:"Filter by site key"`
	Status      string `query:"status" enum:"uploading,ready,failed" doc:"Filter by upload status"`
	UploaderSub string `query:"uploader_sub" doc:"Filter by uploader subject"`
	Search      string `query:"search" doc:"Match filename substring or exact UUID"`
	From        string `query:"from" doc:"created_at lower bound (RFC3339)"`
	To          string `query:"to" doc:"created_at upper bound (RFC3339)"`
	Page        int    `query:"page" doc:"1-based page number"`
	Limit       int    `query:"limit" doc:"Items per page (max 200)"`
}

type adminListOutput struct {
	Body Envelope[dto.AdminArtifactList]
}

type adminStatsOutput struct {
	Body Envelope[dto.AdminArtifactStats]
}

type adminUUIDInput struct {
	UUID string `path:"uuid" doc:"Artifact UUID"`
}

type adminDeleteOutput struct {
	Body Envelope[dto.AdminArtifactDelete]
}

type adminReclaimOutput struct {
	Body Envelope[dto.AdminArtifactReclaim]
}

func (s *AdminHumaServer) requireRen(ctx context.Context) error {
	if !perm.Resolver.Can(adminRoles(ctx), perm.FilesManage) {
		return apiErr(http.StatusForbidden, apperr.ErrForbidden)
	}
	return nil
}

func (s *AdminHumaServer) list(ctx context.Context, in *adminListInput) (*adminListOutput, error) {
	if err := s.requireRen(ctx); err != nil {
		return nil, err
	}
	f := AdminListFilters{
		Site: in.Site, Status: in.Status, UploaderSub: in.UploaderSub,
		Search: in.Search, Page: in.Page, Limit: in.Limit,
	}
	if in.From != "" {
		t, err := time.Parse(time.RFC3339, in.From)
		if err != nil {
			return nil, apiErr(http.StatusBadRequest, apperr.ErrBadRequest)
		}
		f.From = &t
	}
	if in.To != "" {
		t, err := time.Parse(time.RFC3339, in.To)
		if err != nil {
			return nil, apiErr(http.StatusBadRequest, apperr.ErrBadRequest)
		}
		f.To = &t
	}
	data, err := s.h.List(ctx, f)
	if err != nil {
		if errors.Is(err, errAdminBadRequest) {
			return nil, apiErr(http.StatusBadRequest, apperr.ErrBadRequest)
		}
		slog.Error("artifact admin list", "err", err)
		return nil, apiErr(http.StatusInternalServerError, apperr.ErrInternalServer)
	}
	return &adminListOutput{Body: okEnvelope(data)}, nil
}

func (s *AdminHumaServer) stats(ctx context.Context, _ *struct{}) (*adminStatsOutput, error) {
	data, err := s.h.Stats(ctx)
	if err != nil {
		slog.Error("artifact admin stats", "err", err)
		return nil, apiErr(http.StatusInternalServerError, apperr.ErrInternalServer)
	}
	return &adminStatsOutput{Body: okEnvelope(data)}, nil
}

func (s *AdminHumaServer) delete(ctx context.Context, in *adminUUIDInput) (*adminDeleteOutput, error) {
	if err := s.requireRen(ctx); err != nil {
		return nil, err
	}
	removed, err := s.h.Delete(ctx, in.UUID)
	if err != nil {
		switch {
		case errors.Is(err, errAdminBadRequest):
			return nil, apiErr(http.StatusBadRequest, apperr.ErrBadRequest)
		case errors.Is(err, errAdminNotFound):
			return nil, apiErr(http.StatusNotFound, apperr.ErrNotFound)
		case errors.Is(err, service.ErrStorageUnconfigured):
			return nil, apiErrMsg(http.StatusServiceUnavailable, apperr.ErrInternalServer, "artifact storage not configured; cannot take down the composites serving this artifact")
		default:
			slog.Error("artifact admin delete", "uuid", in.UUID, "err", err)
			return nil, apiErr(http.StatusInternalServerError, apperr.ErrInternalServer)
		}
	}
	return &adminDeleteOutput{Body: okEnvelope(dto.AdminArtifactDelete{UUID: in.UUID, SoftDeleted: true, CompositesDeleted: removed})}, nil
}

func (s *AdminHumaServer) reclaim(ctx context.Context, in *adminUUIDInput) (*adminReclaimOutput, error) {
	if err := s.requireRen(ctx); err != nil {
		return nil, err
	}
	if err := s.h.Reclaim(ctx, in.UUID); err != nil {
		switch {
		case errors.Is(err, errReclaimUnavailable):
			return nil, apiErrMsg(http.StatusServiceUnavailable, apperr.ErrOperationFailed, err.Error())
		case errors.Is(err, errAdminBadRequest):
			return nil, apiErr(http.StatusBadRequest, apperr.ErrBadRequest)
		case errors.Is(err, errAdminNotFound):
			return nil, apiErr(http.StatusNotFound, apperr.ErrNotFound)
		case errors.Is(err, errReclaimNotUploading),
			errors.Is(err, errReclaimActive),
			errors.Is(err, errReclaimRaced):
			return nil, apiErrMsg(http.StatusConflict, apperr.ErrOperationFailed, err.Error())
		default:
			slog.Error("artifact admin reclaim", "uuid", in.UUID, "err", err)
			return nil, apiErr(http.StatusInternalServerError, apperr.ErrInternalServer)
		}
	}
	return &adminReclaimOutput{Body: okEnvelope(dto.AdminArtifactReclaim{UUID: in.UUID, Reclaimed: true})}, nil
}
