package handler

import (
	"context"
	stderrors "errors"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"api/internal/platform/telemetry/dto"
	telemetryPerm "api/internal/platform/telemetry/perm"
	"api/internal/platform/telemetry/store"
	"api/pkg/errors"
	"api/pkg/wireshape"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humafiber"
	"github.com/gofiber/fiber/v3"
)

type adminCtxKey string

const ctxKeyAdminRoles adminCtxKey = "telemetry_admin:roles"

func adminAuthBridge(ctx huma.Context, next func(huma.Context)) {
	if roles, ok := humafiber.Unwrap(ctx).Locals("user_roles").([]string); ok {
		ctx = huma.WithValue(ctx, ctxKeyAdminRoles, roles)
	}
	next(ctx)
}

func adminRoles(ctx context.Context) []string {
	roles, _ := ctx.Value(ctxKeyAdminRoles).([]string)
	return roles
}

type invalidator interface {
	Invalidate()
}

type AdminServer struct {
	store *store.Store
	keys  invalidator
}

func SetupAdmin(app *fiber.App, st *store.Store, keys invalidator) huma.API {
	InstallErrorEnvelope()

	cfg := huma.DefaultConfig("KUN Telemetry Admin API", "1.0.0")
	cfg.OpenAPIPath = ""
	cfg.DocsPath = ""
	cfg.SchemasPath = ""

	api := humafiber.New(app, cfg)
	api.UseMiddleware(adminAuthBridge)

	s := &AdminServer{store: st, keys: keys}
	s.register(api)
	wireshape.Publish(api.OpenAPI())
	return api
}

func (s *AdminServer) register(api huma.API) {
	tags := []string{"telemetry-admin"}
	huma.Register(api, huma.Operation{OperationID: "listTelemetryApps", Method: http.MethodGet, Path: "/api/v1/admin/telemetry/apps",
		Summary: "List telemetry apps including ingest keys", Tags: tags}, s.listApps)
	huma.Register(api, huma.Operation{OperationID: "createTelemetryApp", Method: http.MethodPost, Path: "/api/v1/admin/telemetry/apps",
		Summary: "Create a telemetry app and mint its public ingest key", Tags: tags}, s.createApp)
	huma.Register(api, huma.Operation{OperationID: "updateTelemetryApp", Method: http.MethodPatch, Path: "/api/v1/admin/telemetry/apps/{id}",
		Summary: "Update a telemetry app's display name or enabled flag", Tags: tags}, s.updateApp)
	huma.Register(api, huma.Operation{OperationID: "rotateTelemetryAppKey", Method: http.MethodPost, Path: "/api/v1/admin/telemetry/apps/{id}/rotate-key",
		Summary: "Replace an app's ingest key; the old key stops working in this process immediately", Tags: tags}, s.rotateKey)
	huma.Register(api, huma.Operation{OperationID: "listTelemetryDailyMetrics", Method: http.MethodGet, Path: "/api/v1/admin/telemetry/metrics/daily",
		Summary: "Daily session metrics for an app", Tags: tags}, s.listMetrics)
}

func (s *AdminServer) requireManage(ctx context.Context) error {
	if !telemetryPerm.Resolver.Can(adminRoles(ctx), telemetryPerm.Manage) {
		return apiErrMsg(http.StatusForbidden, errors.ErrForbidden, "this action requires telemetry.manage")
	}
	return nil
}

func (s *AdminServer) invalidate() {
	if s.keys != nil {
		s.keys.Invalidate()
	}
}

type listAppsInput struct{}
type listAppsOutput struct {
	Body Envelope[[]dto.AppView]
}

func (s *AdminServer) listApps(ctx context.Context, _ *listAppsInput) (*listAppsOutput, error) {
	rows, err := s.store.ListAppRows(ctx)
	if err != nil {
		return nil, mapAdminErr("list apps", err)
	}
	out := make([]dto.AppView, 0, len(rows))
	for _, r := range rows {
		out = append(out, dto.AppViewFrom(r))
	}
	return &listAppsOutput{Body: okEnvelope(out)}, nil
}

type createAppInput struct {
	Body dto.CreateAppRequest
}
type createAppOutput struct {
	Body Envelope[dto.AppView]
}

var serviceNameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)

func (s *AdminServer) createApp(ctx context.Context, in *createAppInput) (*createAppOutput, error) {
	if err := s.requireManage(ctx); err != nil {
		return nil, err
	}
	if !serviceNameRe.MatchString(in.Body.ServiceName) {
		return nil, apiErrMsg(http.StatusUnprocessableEntity, errors.ErrValidationFailed, "service_name must match ^[a-z][a-z0-9-]{1,62}$")
	}
	if in.Body.DisplayName == "" {
		return nil, apiErrMsg(http.StatusUnprocessableEntity, errors.ErrValidationFailed, "display_name is required")
	}
	row, err := s.store.CreateApp(ctx, in.Body.ServiceName, in.Body.DisplayName)
	if err != nil {
		return nil, mapAdminErr("create app", err)
	}
	s.invalidate()
	return &createAppOutput{Body: okEnvelope(dto.AppViewFrom(*row))}, nil
}

type updateAppInput struct {
	ID   int64 `path:"id"`
	Body dto.UpdateAppRequest
}
type updateAppOutput struct {
	Body Envelope[dto.AppView]
}

func (s *AdminServer) updateApp(ctx context.Context, in *updateAppInput) (*updateAppOutput, error) {
	if err := s.requireManage(ctx); err != nil {
		return nil, err
	}
	row, err := s.store.UpdateApp(ctx, in.ID, in.Body.DisplayName, in.Body.Enabled)
	if err != nil {
		return nil, mapAdminErr("update app", err)
	}
	s.invalidate()
	return &updateAppOutput{Body: okEnvelope(dto.AppViewFrom(*row))}, nil
}

type rotateKeyInput struct {
	ID int64 `path:"id"`
}
type rotateKeyOutput struct {
	Body Envelope[dto.AppView]
}

func (s *AdminServer) rotateKey(ctx context.Context, in *rotateKeyInput) (*rotateKeyOutput, error) {
	if err := s.requireManage(ctx); err != nil {
		return nil, err
	}
	row, err := s.store.RotateKey(ctx, in.ID)
	if err != nil {
		return nil, mapAdminErr("rotate key", err)
	}
	s.invalidate()
	return &rotateKeyOutput{Body: okEnvelope(dto.AppViewFrom(*row))}, nil
}

type listMetricsInput struct {
	AppID       int64  `query:"app_id" required:"true"`
	Environment string `query:"environment" default:"direct"`
	From        string `query:"from"`
	To          string `query:"to"`
}
type listMetricsOutput struct {
	Body Envelope[[]dto.DailyMetricView]
}

func (s *AdminServer) listMetrics(ctx context.Context, in *listMetricsInput) (*listMetricsOutput, error) {
	to := time.Now().UTC()
	if in.To != "" {
		t, err := time.Parse("2006-01-02", in.To)
		if err != nil {
			return nil, apiErrMsg(http.StatusUnprocessableEntity, errors.ErrValidationFailed, "to must be YYYY-MM-DD")
		}
		to = t
	}
	from := to.AddDate(0, 0, -29)
	if in.From != "" {
		t, err := time.Parse("2006-01-02", in.From)
		if err != nil {
			return nil, apiErrMsg(http.StatusUnprocessableEntity, errors.ErrValidationFailed, "from must be YYYY-MM-DD")
		}
		from = t
	}
	if from.After(to) {
		return nil, apiErrMsg(http.StatusUnprocessableEntity, errors.ErrValidationFailed, "from must not be after to")
	}
	if to.After(from.AddDate(0, 0, 400)) {
		return nil, apiErrMsg(http.StatusUnprocessableEntity, errors.ErrValidationFailed, "range must not exceed 400 days")
	}
	rows, err := s.store.ListDailyMetrics(ctx, in.AppID, in.Environment, from, to)
	if err != nil {
		return nil, mapAdminErr("list metrics", err)
	}
	if rows == nil {
		rows = []dto.DailyMetricView{}
	}
	return &listMetricsOutput{Body: okEnvelope(rows)}, nil
}

func mapAdminErr(op string, err error) *houseError {
	switch {
	case stderrors.Is(err, store.ErrNotFound):
		return apiErr(http.StatusNotFound, errors.ErrNotFound)
	case stderrors.Is(err, store.ErrConflict):
		return apiErrMsg(http.StatusConflict, errors.ErrOperationFailed, "service_name already exists")
	default:
		slog.Error("telemetry admin "+op, "err", err)
		return apiErr(http.StatusInternalServerError, errors.ErrInternalServer)
	}
}
