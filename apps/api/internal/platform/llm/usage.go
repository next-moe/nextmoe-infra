package llm

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

const (
	attemptRetention = 30 * 24 * time.Hour
	rollupLookback   = 3 * time.Hour
)

func (a *Admin) RollupUsage(ctx context.Context) error {
	return a.db.WithContext(ctx).Exec(`INSERT INTO llm_usage_hourly
			(hour, scene, route_id, access_key_id, provider_id, model_key, calls, errors, input_tokens, output_tokens, cost_nano, duration_ms)
		SELECT date_trunc('hour', created_at), scene, route_id, access_key_id, max(provider_id), max(model_key),
			count(*), count(*) FILTER (WHERE NOT ok), coalesce(sum(input_tokens), 0), coalesce(sum(output_tokens), 0),
			coalesce(sum(cost_nano), 0), coalesce(sum(duration_ms), 0)
		FROM llm_attempt
		WHERE created_at >= date_trunc('hour', now() - ?::interval) AND created_at < date_trunc('hour', now())
		GROUP BY 1, 2, 3, 4
		ON CONFLICT (hour, scene, route_id, access_key_id) DO UPDATE SET
			provider_id = excluded.provider_id, model_key = excluded.model_key, calls = excluded.calls, errors = excluded.errors,
			input_tokens = excluded.input_tokens, output_tokens = excluded.output_tokens, cost_nano = excluded.cost_nano,
			duration_ms = excluded.duration_ms`, fmt.Sprintf("%d seconds", int(rollupLookback.Seconds()))).Error
}

func (a *Admin) PruneAttempts(ctx context.Context) (int64, error) {
	var total int64
	for {
		res := a.db.WithContext(ctx).Exec(`DELETE FROM llm_attempt WHERE id IN (
			SELECT id FROM llm_attempt WHERE created_at < ? LIMIT 5000)`, a.now().Add(-attemptRetention))
		if res.Error != nil {
			return total, res.Error
		}
		total += res.RowsAffected
		if res.RowsAffected < 5000 {
			return total, nil
		}
	}
}

func (a *Admin) RunUsageJobs(ctx context.Context) {
	lastPrune := time.Time{}
	for {
		if err := a.RollupUsage(ctx); err != nil {
			slog.Warn("llm usage: rollup", "err", err)
		}
		if a.now().Sub(lastPrune) > 24*time.Hour {
			if n, err := a.PruneAttempts(ctx); err != nil {
				slog.Warn("llm usage: prune attempts", "err", err)
			} else if n > 0 {
				slog.Info("llm usage: pruned attempts", "rows", n)
			}
			lastPrune = a.now()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Minute):
		}
	}
}

type UsageRow struct {
	Key          string `json:"key" doc:"The group value: a provider, route or access key id, a scene key or a model key."`
	Label        string `json:"label"`
	Calls        int64  `json:"calls"`
	Errors       int64  `json:"errors"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	CostNano     int64  `json:"cost_nano"`
	AvgMs        int64  `json:"avg_ms"`
}

type UsageDay struct {
	Day      string `json:"day" doc:"YYYY-MM-DD in China Standard Time."`
	Calls    int64  `json:"calls"`
	Errors   int64  `json:"errors"`
	CostNano int64  `json:"cost_nano"`
}

type UsageReport struct {
	Days  int        `json:"days"`
	Group string     `json:"group"`
	Rows  []UsageRow `json:"rows"`
	Daily []UsageDay `json:"daily"`
	Total UsageRow   `json:"total"`
}

var usageGroups = map[string][2]string{
	"provider": {"u.provider_id::text", "coalesce(p.name, u.provider_id::text)"},
	"route":    {"u.route_id::text", "coalesce(u.model_key || ' · ' || p.name, u.route_id::text)"},
	"scene":    {"u.scene", "u.scene"},
	"key":      {"u.access_key_id::text", "CASE WHEN u.access_key_id = 0 THEN '内部调用（ai / agent）' ELSE coalesce(k.name, u.access_key_id::text) END"},
	"model":    {"u.model_key", "u.model_key"},
}

const usageSource = `(
		SELECT hour AS at, scene, route_id, access_key_id, provider_id, model_key, calls, errors, input_tokens, output_tokens, cost_nano, duration_ms
		FROM llm_usage_hourly WHERE hour >= ? AND hour < date_trunc('hour', now())
		UNION ALL
		SELECT created_at, scene, route_id, access_key_id, provider_id, model_key, 1, CASE WHEN ok THEN 0 ELSE 1 END,
			input_tokens, output_tokens, cost_nano, duration_ms
		FROM llm_attempt WHERE created_at >= greatest(date_trunc('hour', now()), ?)
	) u
	LEFT JOIN llm_provider p ON p.id = u.provider_id
	LEFT JOIN llm_access_key k ON k.id = u.access_key_id`

func (a *Admin) Usage(ctx context.Context, days int, group string) (UsageReport, error) {
	g, ok := usageGroups[group]
	if !ok {
		return UsageReport{}, invalid("group must be provider, route, scene, key or model")
	}
	since := a.now().In(budgetZone)
	since = time.Date(since.Year(), since.Month(), since.Day(), 0, 0, 0, 0, budgetZone).AddDate(0, 0, -(days - 1))
	out := UsageReport{Days: days, Group: group, Rows: []UsageRow{}, Daily: []UsageDay{}}
	db := a.db.WithContext(ctx)
	if err := db.Raw(`SELECT `+g[0]+` AS key, max(`+g[1]+`) AS label, sum(u.calls)::bigint AS calls, sum(u.errors)::bigint AS errors,
			sum(u.input_tokens)::bigint AS input_tokens, sum(u.output_tokens)::bigint AS output_tokens, sum(u.cost_nano)::bigint AS cost_nano,
			CASE WHEN sum(u.calls) > 0 THEN (sum(u.duration_ms) / sum(u.calls))::bigint ELSE 0 END AS avg_ms
		FROM `+usageSource+` GROUP BY 1 ORDER BY cost_nano DESC, calls DESC`, since, since).Scan(&out.Rows).Error; err != nil {
		return out, err
	}
	if err := db.Raw(`SELECT to_char(u.at AT TIME ZONE 'Asia/Shanghai', 'YYYY-MM-DD') AS day, sum(u.calls)::bigint AS calls,
			sum(u.errors)::bigint AS errors, sum(u.cost_nano)::bigint AS cost_nano
		FROM `+usageSource+` GROUP BY 1 ORDER BY 1`, since, since).Scan(&out.Daily).Error; err != nil {
		return out, err
	}
	for _, r := range out.Rows {
		out.Total.Calls += r.Calls
		out.Total.Errors += r.Errors
		out.Total.InputTokens += r.InputTokens
		out.Total.OutputTokens += r.OutputTokens
		out.Total.CostNano += r.CostNano
	}
	out.Total.Key, out.Total.Label = "total", "合计"
	return out, nil
}
