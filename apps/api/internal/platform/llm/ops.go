package llm

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

type RouteHealthView struct {
	RouteID     int64          `json:"route_id"`
	ModelKey    string         `json:"model_key"`
	Provider    string         `json:"provider"`
	UpstreamID  string         `json:"upstream_id"`
	Status      string         `json:"status" enum:"active,disabled"`
	State       string         `json:"state" enum:"healthy,cooling,limited,disabled"`
	Health      HealthState    `json:"health"`
	Load        RouteLoad      `json:"load"`
	MaxInFlight int            `json:"max_in_flight"`
	RPM         int            `json:"rpm"`
	LatencyMs   int            `json:"latency_ms" doc:"Recent first-byte (chat) or full (other kinds) time in this process; 0 until measured."`
	Hour        RouteHourStats `json:"last_hour"`
}

type RouteHourStats struct {
	Calls       int64      `json:"calls"`
	Errors      int64      `json:"errors"`
	P50Ms       int64      `json:"p50_ms"`
	P95Ms       int64      `json:"p95_ms"`
	LastError   string     `json:"last_error"`
	LastErrorAt *time.Time `json:"last_error_at"`
}

func (a *Admin) Health(ctx context.Context) ([]RouteHealthView, error) {
	if a.gw == nil {
		return nil, invalid("no gateway")
	}
	var rows []struct {
		ID, ProviderID, ModelID int64
		UpstreamID, Status      string
		MaxConcurrency, RPM     int
		ModelKey, Provider      string
	}
	if err := a.db.WithContext(ctx).Raw(`SELECT r.id, r.provider_id, r.model_id, r.upstream_id, r.status, r.max_concurrency, r.rpm,
			m.key AS model_key, p.name AS provider
		FROM llm_route r JOIN llm_model m ON m.id = r.model_id JOIN llm_provider p ON p.id = r.provider_id
		ORDER BY m.key, r.priority DESC, r.id`).Scan(&rows).Error; err != nil {
		return nil, err
	}
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	var hours []struct {
		RouteID     int64
		Calls       int64
		Errors      int64
		P50, P95    float64
		LastError   string
		LastErrorAt *time.Time
	}
	if err := a.db.WithContext(ctx).Raw(`SELECT route_id, count(*) AS calls, count(*) FILTER (WHERE NOT ok) AS errors,
			coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY duration_ms), 0) AS p50,
			coalesce(percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_ms), 0) AS p95,
			coalesce((array_agg(error_kind ORDER BY created_at DESC) FILTER (WHERE NOT ok))[1], '') AS last_error,
			max(created_at) FILTER (WHERE NOT ok) AS last_error_at
		FROM llm_attempt WHERE created_at > now() - interval '1 hour' AND route_id IN ? GROUP BY route_id`, ids).Scan(&hours).Error; err != nil {
		return nil, err
	}
	byRoute := map[int64]RouteHourStats{}
	for _, h := range hours {
		byRoute[h.RouteID] = RouteHourStats{Calls: h.Calls, Errors: h.Errors, P50Ms: int64(h.P50), P95Ms: int64(h.P95), LastError: h.LastError, LastErrorAt: h.LastErrorAt}
	}
	load := a.gw.coord.Load(ctx, ids)
	out := make([]RouteHealthView, len(rows))
	for i, r := range rows {
		v := RouteHealthView{RouteID: r.ID, ModelKey: r.ModelKey, Provider: r.Provider, UpstreamID: r.UpstreamID, Status: r.Status,
			Health: a.gw.health.state(r.ID), Load: load[r.ID], MaxInFlight: r.MaxConcurrency, RPM: r.RPM,
			LatencyMs: int(a.gw.stats.latencyMs(r.ID)), Hour: byRoute[r.ID]}
		switch {
		case r.Status != RouteActive:
			v.State = "disabled"
		case v.Health.CoolUntil != nil:
			v.State = "cooling"
		case (r.MaxConcurrency > 0 && v.Load.InFlight >= r.MaxConcurrency) || (r.RPM > 0 && v.Load.RPMUsed >= r.RPM):
			v.State = "limited"
		default:
			v.State = "healthy"
		}
		out[i] = v
	}
	return out, nil
}

func (a *Admin) ResetCooldown(ctx context.Context, routeID int64) error {
	if a.gw == nil {
		return invalid("no gateway")
	}
	var r Route
	if err := a.db.WithContext(ctx).First(&r, routeID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	a.gw.health.reset(routeID)
	a.gw.coord.ClearCool(ctx, routeID)
	a.changed(ctx, "cooldown_reset", r.UpstreamID, r.ID, r.ProviderID)
	return nil
}

type PlanStep struct {
	Order      int    `json:"order"`
	Chain      string `json:"chain" enum:"primary,fallback" doc:"Whether the model is the scene's primary or one of its fallbacks."`
	ModelKey   string `json:"model_key"`
	RouteID    int64  `json:"route_id"`
	Provider   string `json:"provider"`
	UpstreamID string `json:"upstream_id"`
	Priority   int    `json:"priority"`
	Weight     int    `json:"weight"`
	State      string `json:"state" enum:"ready,forced,cooling" doc:"forced: every route is cooling, so the one that recovers first is tried anyway; cooling appears only among the skipped."`
}

type ScenePlan struct {
	Scene    string     `json:"scene"`
	Strategy string     `json:"strategy" enum:"weighted,least_busy,latency,cost"`
	Steps    []PlanStep `json:"steps"`
	Skipped  []PlanStep `json:"skipped" doc:"Routes cooling down, not tried unless everything else is."`
}

func (a *Admin) PlanScene(ctx context.Context, key, affinity string) (ScenePlan, error) {
	if a.gw == nil {
		return ScenePlan{}, invalid("no gateway")
	}
	if err := a.gw.Refresh(ctx); err != nil {
		return ScenePlan{}, err
	}
	s := a.gw.snap.Load()
	sc := s.scenes[key]
	if sc == nil {
		return ScenePlan{}, ErrNotFound
	}
	out := ScenePlan{Scene: key, Strategy: sc.Strategy, Steps: []PlanStep{}, Skipped: []PlanStep{}}
	if out.Strategy == "" {
		out.Strategy = StrategyWeighted
	}
	step := func(c candidate, state string) PlanStep {
		chain := "fallback"
		if c.model.ID == sc.ModelID {
			chain = "primary"
		}
		name := ""
		if c.provider != nil {
			name = c.provider.Name
		}
		return PlanStep{Chain: chain, ModelKey: c.model.Key, RouteID: c.route.ID, Provider: name, UpstreamID: c.route.UpstreamID,
			Priority: c.route.Priority, Weight: c.route.Weight, State: state}
	}
	planned := s.plan(sc, affinity, a.gw.health, a.gw.stats)
	seen := map[int64]bool{}
	for i, c := range planned {
		st := "ready"
		if c.forced {
			st = "forced"
		}
		p := step(c, st)
		p.Order = i + 1
		out.Steps = append(out.Steps, p)
		seen[c.route.ID] = true
	}
	chain := append([]int64{sc.ModelID}, modelIDs(sc.FallbackModelIDs)...)
	for _, mid := range chain {
		for _, r := range s.routes[mid] {
			if seen[r.ID] {
				continue
			}
			if _, cooling := a.gw.health.blockedUntil(r.ID); cooling {
				out.Skipped = append(out.Skipped, step(candidate{route: r, model: s.models[mid], provider: s.providers[r.ProviderID]}, "cooling"))
			}
		}
	}
	return out, nil
}
