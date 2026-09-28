package handler

import (
	"context"
	stderrors "errors"
	"log/slog"
	"net/http"

	"api/internal/platform/ai/dto"
	"api/internal/platform/ai/service"
	"api/pkg/errors"
	"api/pkg/wireshape"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humafiber"
	"github.com/gofiber/fiber/v3"
)

type AdminServer struct {
	stats   *service.StatsService
	budgets *service.BudgetService
}

func SetupAdmin(app *fiber.App, stats *service.StatsService, budgets *service.BudgetService) huma.API {
	InstallErrorEnvelope()

	cfg := huma.DefaultConfig("KUN AI Admin API", "1.0.1")
	cfg.OpenAPIPath = ""
	cfg.DocsPath = ""
	cfg.SchemasPath = ""

	api := humafiber.New(app, cfg)

	s := &AdminServer{stats: stats, budgets: budgets}
	s.register(api)
	wireshape.Publish(api.OpenAPI())
	return api
}

func (s *AdminServer) register(api huma.API) {
	tags := []string{"ai-admin"}

	huma.Register(api, huma.Operation{OperationID: "getAIUsageSummary", Method: http.MethodGet, Path: "/api/v1/admin/ai/usage/summary",
		Summary: "Usage summary aggregated by site×route×channel (with a totalled overview + status distribution)", Tags: tags}, s.usageSummary)
	huma.Register(api, huma.Operation{OperationID: "getAIUsageDaily", Method: http.MethodGet, Path: "/api/v1/admin/ai/usage/daily",
		Summary: "Day×route calls/cost trend series", Tags: tags}, s.usageDaily)
	huma.Register(api, huma.Operation{OperationID: "listAIBudgets", Method: http.MethodGet, Path: "/api/v1/admin/ai/budgets",
		Summary: "List the per-route (optionally per-site) daily budget-fuse config", Tags: tags}, s.listBudgets)
	huma.Register(api, huma.Operation{OperationID: "upsertAIBudget", Method: http.MethodPut, Path: "/api/v1/admin/ai/budgets",
		Summary: "Set or clear a per-route (optionally per-site) daily cost cap (null cap = clear)", Tags: tags}, s.upsertBudget)
}

type usageSummaryInput struct {
	Window string `query:"window" enum:"24h,7d,30d" default:"24h" doc:"aggregation window"`
}
type usageSummaryOutput struct {
	Body Envelope[dto.UsageSummary]
}

func (s *AdminServer) usageSummary(ctx context.Context, in *usageSummaryInput) (*usageSummaryOutput, error) {
	summary, err := s.stats.Summary(ctx, in.Window)
	if err != nil {
		return nil, mapAdminErr("ai usage summary", err)
	}
	return &usageSummaryOutput{Body: okEnvelope(summary)}, nil
}

type usageDailyInput struct {
	Days int `query:"days" default:"14" minimum:"1" maximum:"90" doc:"number of calendar days (inclusive of today)"`
}
type usageDailyOutput struct {
	Body Envelope[dto.DailySeries]
}

func (s *AdminServer) usageDaily(ctx context.Context, in *usageDailyInput) (*usageDailyOutput, error) {
	series, err := s.stats.Daily(ctx, in.Days)
	if err != nil {
		return nil, mapAdminErr("ai usage daily", err)
	}
	return &usageDailyOutput{Body: okEnvelope(series)}, nil
}

type listBudgetsInput struct{}
type listBudgetsOutput struct {
	Body Envelope[[]dto.BudgetView]
}

func (s *AdminServer) listBudgets(ctx context.Context, _ *listBudgetsInput) (*listBudgetsOutput, error) {
	rows, err := s.budgets.List(ctx)
	if err != nil {
		return nil, mapAdminErr("list ai budgets", err)
	}
	return &listBudgetsOutput{Body: okEnvelope(rows)}, nil
}

type upsertBudgetInput struct {
	Body dto.UpsertBudgetRequest
}
type upsertBudgetOutput struct {
	Body Envelope[dto.BudgetView]
}

func (s *AdminServer) upsertBudget(ctx context.Context, in *upsertBudgetInput) (*upsertBudgetOutput, error) {
	view, err := s.budgets.Upsert(ctx, in.Body.Route, in.Body.Site, in.Body.DailyCostCapMicro)
	if err != nil {
		return nil, mapAdminErr("upsert ai budget", err)
	}
	return &upsertBudgetOutput{Body: okEnvelope(view)}, nil
}

func mapAdminErr(op string, err error) *houseError {
	switch {
	case stderrors.Is(err, service.ErrUnknownRoute):
		return apiErrMsg(http.StatusBadRequest, errors.ErrValidationFailed, err.Error())
	default:
		slog.Error("ai admin "+op, "err", err)
		return apiErr(http.StatusInternalServerError, errors.ErrInternalServer)
	}
}
