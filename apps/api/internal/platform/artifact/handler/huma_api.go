package handler

import (
	"context"
	stderrors "errors"
	"log/slog"
	"net/http"

	"api/internal/platform/artifact/dto"
	artMW "api/internal/platform/artifact/middleware"
	"api/internal/platform/artifact/service"
	"api/internal/platform/settings/keys"
	siteModel "api/internal/platform/site/model"
	"api/pkg/errors"
	"api/pkg/utils"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humafiber"
	"github.com/gofiber/fiber/v3"
)

type ctxKey string

const (
	ctxKeySite    ctxKey = "artifact:site_key"
	ctxKeyClient  ctxKey = "artifact:oauth_client"
	ctxKeyUserSub ctxKey = "artifact:user_sub"
	ctxKeyMethod  ctxKey = "artifact:auth_method"
)

func AuthBridge(ctx huma.Context, next func(huma.Context)) {
	fc := humafiber.Unwrap(ctx)
	ctx = huma.WithValue(ctx, ctxKeySite, artMW.SiteKeyFromCtx(fc))
	ctx = huma.WithValue(ctx, ctxKeyClient, artMW.ClientFromCtx(fc))
	ctx = huma.WithValue(ctx, ctxKeyUserSub, artMW.UserSubFromCtx(fc))
	ctx = huma.WithValue(ctx, ctxKeyMethod, artMW.AuthMethodFromCtx(fc))
	next(ctx)
}

func siteFromCtx(ctx context.Context) string {
	s, _ := ctx.Value(ctxKeySite).(string)
	return s
}

func clientFromCtx(ctx context.Context) *siteModel.OAuthClient {
	c, _ := ctx.Value(ctxKeyClient).(*siteModel.OAuthClient)
	return c
}

func userSubFromCtx(ctx context.Context) string {
	s, _ := ctx.Value(ctxKeyUserSub).(string)
	return s
}

func byUserToken(ctx context.Context) bool {
	m, _ := ctx.Value(ctxKeyMethod).(string)
	return m == artMW.MethodJWT
}

// A user token exists to upload from the browser, yet until 2026-09 it also
// listed, downloaded and deleted every artifact of its site: only the site key
// was checked. Site-wide operations take the site's server credential alone.
func siteWideSite(ctx context.Context) (string, error) {
	if byUserToken(ctx) {
		return "", apiErr(http.StatusForbidden, errors.ErrArtifactForbidden)
	}
	site := siteFromCtx(ctx)
	if site == "" {
		return "", apiErr(http.StatusUnauthorized, errors.ErrArtifactUnauthorized)
	}
	return site, nil
}

func uploadOwner(ctx context.Context) string {
	if byUserToken(ctx) {
		return userSubFromCtx(ctx)
	}
	return ""
}

func uploadEnabled(ctx context.Context) bool {
	if client := clientFromCtx(ctx); client != nil && client.SiteID != nil {
		return keys.ArtifactUploadEnabled.ForSite(*client.SiteID)
	}
	return keys.ArtifactUploadEnabled.Get()
}

type uuidInput struct {
	UUID string `path:"uuid" doc:"Artifact UUID"`
}

type artifactOutput struct {
	Body Envelope[*dto.ArtifactResponse]
}

type listInput struct {
	Page     int `query:"page" doc:"1-based page number"`
	PageSize int `query:"page_size" doc:"Items per page (max 100)"`
}

type listData struct {
	Items []dto.ArtifactResponse `json:"items"`
	Total int64                  `json:"total"`
}

type listOutput struct {
	Body Envelope[listData]
}

type downloadOutput struct {
	Body Envelope[*dto.DownloadResponse]
}

type resumeOutput struct {
	Body Envelope[*dto.ResumeUploadResponse]
}

type deleteData struct {
	UUID    string `json:"uuid"`
	Deleted bool   `json:"deleted"`
}

type deleteOutput struct {
	Body Envelope[deleteData]
}

type initInput struct {
	Body dto.InitUploadRequest
}

type initOutput struct {
	Body Envelope[*dto.InitUploadResponse]
}

type completeInput struct {
	UUID string `path:"uuid" doc:"Artifact UUID"`
	Body dto.CompleteUploadRequest
}

type HumaServer struct {
	svc *service.Service
}

func Setup(app *fiber.App, svc *service.Service) huma.API {
	InstallErrorEnvelope()

	cfg := huma.DefaultConfig("KUN Artifact Service", "1.0.0")
	cfg.OpenAPIPath = ""
	cfg.DocsPath = ""
	cfg.SchemasPath = ""

	api := humafiber.New(app, cfg)
	api.UseMiddleware(AuthBridge)

	s := &HumaServer{svc: svc}
	s.register(api)
	return api
}

func (s *HumaServer) register(api huma.API) {
	tags := []string{"artifacts"}
	huma.Register(api, huma.Operation{
		OperationID: "listArtifacts", Method: http.MethodGet, Path: "/api/v1/artifacts",
		Summary: "List the site's artifacts (paginated)", Tags: tags,
	}, s.list)
	huma.Register(api, huma.Operation{
		OperationID: "getArtifact", Method: http.MethodGet, Path: "/api/v1/artifacts/{uuid}",
		Summary: "Get artifact metadata", Tags: tags,
	}, s.get)
	huma.Register(api, huma.Operation{
		OperationID: "downloadArtifact", Method: http.MethodGet, Path: "/api/v1/artifacts/{uuid}/download",
		Summary: "Issue a download URL (presigned, or Worker URL for public)", Tags: tags,
	}, s.download)
	huma.Register(api, huma.Operation{
		OperationID: "deleteArtifact", Method: http.MethodDelete, Path: "/api/v1/artifacts/{uuid}",
		Summary: "Soft-delete an artifact (GC reclaims after TTL)", Tags: tags,
	}, s.delete)
	huma.Register(api, huma.Operation{
		OperationID: "initUpload", Method: http.MethodPost, Path: "/api/v1/artifacts",
		Summary: "Init an upload; returns presigned URL(s)", Tags: tags,
	}, s.initUpload)
	huma.Register(api, huma.Operation{
		OperationID: "completeUpload", Method: http.MethodPost, Path: "/api/v1/artifacts/{uuid}/complete",
		Summary: "Finalize an upload (verify size, optional manifest)", Tags: tags,
	}, s.completeUpload)
	huma.Register(api, huma.Operation{
		OperationID: "resumeUpload", Method: http.MethodGet, Path: "/api/v1/artifacts/{uuid}/resume",
		Summary: "Resume an interrupted upload (lists stored parts, re-presigns the missing ones)", Tags: tags,
	}, s.resumeUpload)
}

func (s *HumaServer) list(ctx context.Context, in *listInput) (*listOutput, error) {
	site, err := siteWideSite(ctx)
	if err != nil {
		return nil, err
	}
	p := utils.Pagination{Page: in.Page, PageSize: in.PageSize}
	p.Normalize()
	items, total, err := s.svc.List(ctx, site, p.Offset(), p.Limit())
	if err != nil {
		slog.Error("artifact list", "site", site, "err", err)
		return nil, apiErr(http.StatusInternalServerError, errors.ErrArtifactStoreFailed)
	}
	return &listOutput{Body: okEnvelope(listData{Items: items, Total: total})}, nil
}

func (s *HumaServer) get(ctx context.Context, in *uuidInput) (*artifactOutput, error) {
	site, err := siteWideSite(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := s.svc.Get(ctx, in.UUID, site)
	if err != nil {
		if stderrors.Is(err, service.ErrNotFound) {
			return nil, apiErr(http.StatusNotFound, errors.ErrArtifactNotFound)
		}
		slog.Error("artifact get", "uuid", in.UUID, "site", site, "err", err)
		return nil, apiErr(http.StatusInternalServerError, errors.ErrArtifactStoreFailed)
	}
	return &artifactOutput{Body: okEnvelope(resp)}, nil
}

func (s *HumaServer) download(ctx context.Context, in *uuidInput) (*downloadOutput, error) {
	site, err := siteWideSite(ctx)
	if err != nil {
		return nil, err
	}
	client := clientFromCtx(ctx)
	if client == nil {
		return nil, apiErr(http.StatusUnauthorized, errors.ErrArtifactUnauthorized)
	}
	resp, err := s.svc.Download(ctx, in.UUID, site, client.ArtifactCDNBase)
	if err != nil {
		if stderrors.Is(err, service.ErrNotFound) {
			return nil, apiErr(http.StatusNotFound, errors.ErrArtifactNotFound)
		}
		slog.Error("artifact download", "uuid", in.UUID, "site", site, "err", err)
		return nil, apiErr(http.StatusInternalServerError, errors.ErrArtifactStoreFailed)
	}
	return &downloadOutput{Body: okEnvelope(resp)}, nil
}

func (s *HumaServer) resumeUpload(ctx context.Context, in *uuidInput) (*resumeOutput, error) {
	if !uploadEnabled(ctx) {
		return nil, apiErr(http.StatusServiceUnavailable, errors.ErrArtifactUploadDisabled)
	}
	site := siteFromCtx(ctx)
	if site == "" {
		return nil, apiErr(http.StatusUnauthorized, errors.ErrArtifactUnauthorized)
	}
	resp, err := s.svc.ResumeUpload(ctx, in.UUID, site, uploadOwner(ctx))
	if err != nil {
		switch {
		case stderrors.Is(err, service.ErrNotFound):
			return nil, apiErr(http.StatusNotFound, errors.ErrArtifactNotFound)
		case stderrors.Is(err, service.ErrNotResumable):
			return nil, apiErr(http.StatusConflict, errors.ErrArtifactBadRequest)
		case stderrors.Is(err, service.ErrBadRequest):
			return nil, apiErr(http.StatusBadRequest, errors.ErrArtifactBadRequest)
		default:
			slog.Error("artifact resume", "uuid", in.UUID, "site", site, "err", err)
			return nil, apiErr(http.StatusInternalServerError, errors.ErrArtifactStoreFailed)
		}
	}
	return &resumeOutput{Body: okEnvelope(resp)}, nil
}

func (s *HumaServer) delete(ctx context.Context, in *uuidInput) (*deleteOutput, error) {
	site, err := siteWideSite(ctx)
	if err != nil {
		return nil, err
	}
	ok, err := s.svc.Delete(ctx, in.UUID, site)
	if err != nil {
		slog.Error("artifact delete", "uuid", in.UUID, "site", site, "err", err)
		return nil, apiErr(http.StatusInternalServerError, errors.ErrArtifactStoreFailed)
	}
	if !ok {
		return nil, apiErr(http.StatusNotFound, errors.ErrArtifactNotFound)
	}
	return &deleteOutput{Body: okEnvelope(deleteData{UUID: in.UUID, Deleted: true})}, nil
}

func (s *HumaServer) initUpload(ctx context.Context, in *initInput) (*initOutput, error) {
	if !uploadEnabled(ctx) {
		return nil, apiErr(http.StatusServiceUnavailable, errors.ErrArtifactUploadDisabled)
	}
	site := siteFromCtx(ctx)
	client := clientFromCtx(ctx)
	if site == "" || client == nil {
		return nil, apiErr(http.StatusUnauthorized, errors.ErrArtifactUnauthorized)
	}
	if err := utils.Validate(&in.Body); err != nil {
		return nil, apiErr(http.StatusBadRequest, errors.ErrArtifactBadRequest)
	}

	uploaderSub := userSubFromCtx(ctx)
	if uploaderSub == "" {
		uploaderSub = in.Body.UploaderSub
	}

	resp, err := s.svc.InitUpload(ctx, in.Body, service.InitParams{
		Site:           site,
		UploaderSub:    uploaderSub,
		UploaderClient: client.ID,
		MaxFileSize:    client.ArtifactMaxFileSize,
		QuotaCount:     client.ArtifactQuotaDaily,
		QuotaBytes:     client.ArtifactQuotaBytesDaily,
		AllowedMime:    client.ArtifactAllowedMimes(),
	})
	if err != nil {
		switch {
		case stderrors.Is(err, service.ErrTooBig):
			return nil, apiErr(http.StatusRequestEntityTooLarge, errors.ErrArtifactTooBig)
		case stderrors.Is(err, service.ErrMIMEDenied):
			return nil, apiErr(http.StatusBadRequest, errors.ErrArtifactMIMEDenied)
		case stderrors.Is(err, service.ErrQuotaCount), stderrors.Is(err, service.ErrQuotaBytes):
			return nil, apiErr(http.StatusTooManyRequests, errors.ErrArtifactQuotaExceeded)
		default:
			slog.Error("artifact init", "site", site, "client_id", client.ID, "err", err)
			return nil, apiErr(http.StatusInternalServerError, errors.ErrArtifactStoreFailed)
		}
	}
	return &initOutput{Body: okEnvelope(resp)}, nil
}

func (s *HumaServer) completeUpload(ctx context.Context, in *completeInput) (*artifactOutput, error) {
	if !uploadEnabled(ctx) {
		return nil, apiErr(http.StatusServiceUnavailable, errors.ErrArtifactUploadDisabled)
	}
	site := siteFromCtx(ctx)
	if site == "" {
		return nil, apiErr(http.StatusUnauthorized, errors.ErrArtifactUnauthorized)
	}
	if err := utils.Validate(&in.Body); err != nil {
		return nil, apiErr(http.StatusBadRequest, errors.ErrArtifactBadRequest)
	}

	resp, err := s.svc.CompleteUpload(ctx, in.UUID, site, uploadOwner(ctx), in.Body)
	if err != nil {
		switch {
		case stderrors.Is(err, service.ErrNotFound):
			return nil, apiErr(http.StatusNotFound, errors.ErrArtifactNotFound)
		case stderrors.Is(err, service.ErrBadRequest):
			return nil, apiErr(http.StatusBadRequest, errors.ErrArtifactBadRequest)
		case stderrors.Is(err, service.ErrSizeMismatch):
			return nil, apiErr(http.StatusBadRequest, errors.ErrArtifactSizeMismatch)
		default:
			slog.Error("artifact complete", "uuid", in.UUID, "site", site, "err", err)
			return nil, apiErr(http.StatusInternalServerError, errors.ErrArtifactStoreFailed)
		}
	}
	return &artifactOutput{Body: okEnvelope(resp)}, nil
}
