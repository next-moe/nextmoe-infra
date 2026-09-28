package service

import (
	"context"

	"api/internal/platform/ai/dto"
	"api/internal/platform/ai/model"

	"gorm.io/gorm"
)

type StatsService struct {
	db *gorm.DB
}

func NewStatsService(db *gorm.DB) *StatsService { return &StatsService{db: db} }

var windowIntervals = map[string]string{
	"24h": "24 hours",
	"7d":  "7 days",
	"30d": "30 days",
}

func resolveWindow(window string) (string, string) {
	if iv, ok := windowIntervals[window]; ok {
		return window, iv
	}
	return "24h", windowIntervals["24h"]
}

func (s *StatsService) Summary(ctx context.Context, window string) (dto.UsageSummary, error) {
	resolved, interval := resolveWindow(window)

	rows := []dto.SummaryRow{}
	err := s.db.WithContext(ctx).
		Table("ai_usage").
		Select(`site, route, channel,
			COUNT(*) AS calls,
			COALESCE(SUM(prompt_tokens), 0) AS prompt_tokens,
			COALESCE(SUM(completion_tokens), 0) AS completion_tokens,
			COALESCE(SUM(cost_micro), 0) AS cost_micro,
			COUNT(*) FILTER (WHERE status = ?) AS ok,
			COUNT(*) FILTER (WHERE status = ?) AS upstream_error,
			COUNT(*) FILTER (WHERE status = ?) AS budget_denied,
			COUNT(*) FILTER (WHERE status = ?) AS degraded,
			COUNT(*) FILTER (WHERE status = ?) AS truncated`,
			model.StatusOK, model.StatusUpstreamError, model.StatusBudgetDenied,
			model.StatusDegraded, model.StatusTruncated).
		Where("created_at >= now() - CAST(? AS interval)", interval).
		Group("site, route, channel").
		Order("calls DESC, site, route, channel").
		Scan(&rows).Error
	if err != nil {
		return dto.UsageSummary{}, err
	}

	return dto.UsageSummary{Window: resolved, Overview: overviewOf(rows), Rows: rows}, nil
}

func overviewOf(rows []dto.SummaryRow) dto.UsageOverview {
	var o dto.UsageOverview
	for _, r := range rows {
		o.Calls += r.Calls
		o.PromptTokens += r.PromptTokens
		o.CompletionTokens += r.CompletionTokens
		o.CostMicro += r.CostMicro
		o.OK += r.OK
		o.UpstreamError += r.UpstreamError
		o.BudgetDenied += r.BudgetDenied
		o.Degraded += r.Degraded
		o.Truncated += r.Truncated
	}
	if o.Calls > 0 {
		o.ErrorRate = float64(o.Calls-o.OK) / float64(o.Calls)
	}
	return o
}

func (s *StatsService) Daily(ctx context.Context, days int) (dto.DailySeries, error) {
	if days < 1 {
		days = 1
	}
	points := []dto.DailyPoint{}
	err := s.db.WithContext(ctx).
		Table("ai_usage").
		Select(`to_char(date_trunc('day', created_at), 'YYYY-MM-DD') AS day, route,
			COUNT(*) AS calls,
			COALESCE(SUM(cost_micro), 0) AS cost_micro`).
		Where("created_at >= date_trunc('day', now()) - (? * interval '1 day')", days-1).
		Group("day, route").
		Order("day, route").
		Scan(&points).Error
	if err != nil {
		return dto.DailySeries{}, err
	}
	return dto.DailySeries{Days: days, Points: points}, nil
}
