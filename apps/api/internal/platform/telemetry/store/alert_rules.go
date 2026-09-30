package store

import (
	"context"
	"fmt"
	"time"

	"api/internal/platform/telemetry/model"
)

const newIssueWindow = 7 * 24 * time.Hour

func (s *Store) ruleContractNew(ctx context.Context, now time.Time, _ map[int64]model.AlertSettings) ([]model.Alert, error) {
	type row struct {
		ID           int64
		AppID        int64
		Title        string
		FirstVersion string
	}
	var rows []row
	if err := s.db.WithContext(ctx).Raw(`
		SELECT i.id, i.app_id, i.title, i.first_version
		  FROM telemetry_issue i
		  JOIN telemetry_app a ON a.id = i.app_id AND a.enabled
		 WHERE i.kind = ?
		   AND i.created_at > ?`,
		model.KindContract, now.Add(-newIssueWindow),
	).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("rule contract_new: %w", err)
	}
	out := make([]model.Alert, 0, len(rows))
	for _, r := range rows {
		a, err := candidate(r.AppID, model.RuleContractNew, fmt.Sprintf("issue:%d", r.ID),
			model.UrgencyImmediate, "契约不一致", map[string]any{
				"issue_id": r.ID, "title": r.Title, "first_version": r.FirstVersion,
			}, now)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

func (s *Store) ruleIssueNew(ctx context.Context, now time.Time, _ map[int64]model.AlertSettings) ([]model.Alert, error) {
	type row struct {
		ID           int64
		AppID        int64
		Kind         string
		Title        string
		Culprit      string
		FirstVersion string
	}
	var rows []row
	if err := s.db.WithContext(ctx).Raw(`
		SELECT i.id, i.app_id, i.kind, i.title, i.culprit, i.first_version
		  FROM telemetry_issue i
		  JOIN telemetry_app a ON a.id = i.app_id AND a.enabled
		 WHERE i.kind IN (?, ?, ?, ?)
		   AND i.created_at > ?`,
		model.KindException, model.KindJava, model.KindNative, model.KindANR, now.Add(-newIssueWindow),
	).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("rule issue_new: %w", err)
	}
	out := make([]model.Alert, 0, len(rows))
	for _, r := range rows {
		a, err := candidate(r.AppID, model.RuleIssueNew, fmt.Sprintf("issue:%d", r.ID),
			model.UrgencyDigest, "新问题", map[string]any{
				"issue_id": r.ID, "kind": r.Kind, "title": r.Title,
				"culprit": r.Culprit, "first_version": r.FirstVersion,
			}, now)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

func (s *Store) ruleIssueRegressed(ctx context.Context, now time.Time, _ map[int64]model.AlertSettings) ([]model.Alert, error) {
	type row struct {
		ID          int64
		AppID       int64
		Title       string
		LastVersion string
		LastSeenDay time.Time
	}
	var rows []row
	if err := s.db.WithContext(ctx).Raw(`
		SELECT i.id, i.app_id, i.title, i.last_version, i.last_seen_day
		  FROM telemetry_issue i
		  JOIN telemetry_app a ON a.id = i.app_id AND a.enabled
		 WHERE i.regressed AND i.status = ?`,
		model.IssueOpen,
	).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("rule issue_regressed: %w", err)
	}
	out := make([]model.Alert, 0, len(rows))
	for _, r := range rows {
		day := r.LastSeenDay.UTC().Format("2006-01-02")
		a, err := candidate(r.AppID, model.RuleIssueRegressed, fmt.Sprintf("issue:%d:%s", r.ID, day),
			model.UrgencyDigest, "问题复发", map[string]any{
				"issue_id": r.ID, "title": r.Title, "last_version": r.LastVersion,
			}, now)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

type versionRate struct {
	AppID          int64  `gorm:"column:app_id"`
	ServiceVersion string `gorm:"column:service_version"`
	Sessions       int64  `gorm:"column:sessions"`
	Crashed        int64  `gorm:"column:crashed"`
	ANR            int64  `gorm:"column:anr"`
}

func (s *Store) recentVersionRates(ctx context.Context, from, to time.Time) ([]versionRate, error) {
	var rows []versionRate
	if err := s.db.WithContext(ctx).Raw(`
		SELECT m.app_id, m.service_version,
		       SUM(m.sessions)::bigint AS sessions,
		       SUM(m.crashed_sessions)::bigint AS crashed,
		       SUM(m.anr_sessions)::bigint AS anr
		  FROM telemetry_daily_metric m
		  JOIN telemetry_app a ON a.id = m.app_id AND a.enabled
		 WHERE m.environment = 'direct'
		   AND m.day BETWEEN ?::date AND ?::date
		 GROUP BY m.app_id, m.service_version`,
		from.Format("2006-01-02"), to.Format("2006-01-02"),
	).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func rate(num, den int64) float64 {
	if den == 0 {
		return 0
	}
	return float64(num) / float64(den)
}

func (s *Store) ruleCrashRate(ctx context.Context, now time.Time, settings map[int64]model.AlertSettings) ([]model.Alert, error) {
	today := dateUTC(now)
	rows, err := s.recentVersionRates(ctx, today.AddDate(0, 0, -1), today)
	if err != nil {
		return nil, fmt.Errorf("rule crash_rate: %w", err)
	}
	day := today.Format("2006-01-02")
	var out []model.Alert
	for _, r := range rows {
		st, ok := settings[r.AppID]
		if !ok || r.Sessions < int64(st.MinSessions) {
			continue
		}
		rt := rate(r.Crashed, r.Sessions)
		if rt <= st.CrashRate {
			continue
		}
		a, err := candidate(r.AppID, model.RuleCrashRate, "crash_rate:"+r.ServiceVersion+":"+day,
			model.UrgencyImmediate, "崩溃率偏高", map[string]any{
				"version": r.ServiceVersion, "sessions": r.Sessions, "crashed": r.Crashed,
				"rate": rt, "threshold": st.CrashRate,
			}, now)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

func (s *Store) ruleAnrRate(ctx context.Context, now time.Time, settings map[int64]model.AlertSettings) ([]model.Alert, error) {
	today := dateUTC(now)
	rows, err := s.recentVersionRates(ctx, today.AddDate(0, 0, -1), today)
	if err != nil {
		return nil, fmt.Errorf("rule anr_rate: %w", err)
	}
	day := today.Format("2006-01-02")
	var out []model.Alert
	for _, r := range rows {
		st, ok := settings[r.AppID]
		if !ok || r.Sessions < int64(st.MinSessions) {
			continue
		}
		rt := rate(r.ANR, r.Sessions)
		if rt <= st.AnrRate {
			continue
		}
		a, err := candidate(r.AppID, model.RuleAnrRate, "anr_rate:"+r.ServiceVersion+":"+day,
			model.UrgencyImmediate, "ANR 率偏高", map[string]any{
				"version": r.ServiceVersion, "sessions": r.Sessions, "crashed": r.ANR,
				"rate": rt, "threshold": st.AnrRate,
			}, now)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

func (s *Store) ruleCrashRegression(ctx context.Context, now time.Time, settings map[int64]model.AlertSettings) ([]model.Alert, error) {
	today := dateUTC(now)
	vFrom, vTo := today.AddDate(0, 0, -1), today
	pFrom, pTo := today.AddDate(0, 0, -14), today.AddDate(0, 0, -2)
	type pair struct {
		AppID  int64  `gorm:"column:app_id"`
		V      string `gorm:"column:v"`
		P      string `gorm:"column:p"`
		VSess  int64  `gorm:"column:v_sess"`
		VCrash int64  `gorm:"column:v_crash"`
		PSess  int64  `gorm:"column:p_sess"`
		PCrash int64  `gorm:"column:p_crash"`
	}
	var rows []pair
	if err := s.db.WithContext(ctx).Raw(`
		WITH first_days AS (
			SELECT app_id, service_version, MIN(day) AS first_day
			  FROM telemetry_daily_metric
			 WHERE environment = 'direct'
			 GROUP BY app_id, service_version
		),
		v AS (
			SELECT m.app_id, m.service_version,
			       SUM(m.sessions)::bigint AS sessions,
			       SUM(m.crashed_sessions)::bigint AS crashed
			  FROM telemetry_daily_metric m
			  JOIN telemetry_app a ON a.id = m.app_id AND a.enabled
			 WHERE m.environment = 'direct'
			   AND m.day BETWEEN ?::date AND ?::date
			 GROUP BY m.app_id, m.service_version
		),
		p_window AS (
			SELECT m.app_id, m.service_version,
			       SUM(m.sessions)::bigint AS sessions,
			       SUM(m.crashed_sessions)::bigint AS crashed
			  FROM telemetry_daily_metric m
			 WHERE m.environment = 'direct'
			   AND m.day BETWEEN ?::date AND ?::date
			 GROUP BY m.app_id, m.service_version
		)
		SELECT v.app_id,
		       v.service_version AS v,
		       prev.service_version AS p,
		       v.sessions AS v_sess,
		       v.crashed AS v_crash,
		       p_window.sessions AS p_sess,
		       p_window.crashed AS p_crash
		  FROM v
		  JOIN first_days fv ON fv.app_id = v.app_id AND fv.service_version = v.service_version
		  JOIN LATERAL (
		        SELECT fd.service_version
		          FROM first_days fd
		         WHERE fd.app_id = v.app_id
		           AND fd.service_version <> v.service_version
		           AND fd.first_day < fv.first_day
		         ORDER BY fd.first_day DESC, fd.service_version DESC
		         LIMIT 1
		  ) prev ON true
		  JOIN p_window ON p_window.app_id = v.app_id AND p_window.service_version = prev.service_version`,
		vFrom.Format("2006-01-02"), vTo.Format("2006-01-02"),
		pFrom.Format("2006-01-02"), pTo.Format("2006-01-02"),
	).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("rule crash_regression: %w", err)
	}
	day := today.Format("2006-01-02")
	var out []model.Alert
	for _, r := range rows {
		st, ok := settings[r.AppID]
		if !ok || r.VSess < int64(st.MinSessions) || r.PSess < int64(st.MinSessions) {
			continue
		}
		vRate := rate(r.VCrash, r.VSess)
		pRate := rate(r.PCrash, r.PSess)
		limit := pRate * st.RegressionFactor
		if alt := pRate + st.RegressionMinDelta; alt > limit {
			limit = alt
		}
		if vRate <= limit {
			continue
		}
		a, err := candidate(r.AppID, model.RuleCrashRegression, "crash_regression:"+r.V+":"+day,
			model.UrgencyImmediate, "崩溃率回升", map[string]any{
				"version": r.V, "previous_version": r.P,
				"sessions": r.VSess, "previous_sessions": r.PSess,
				"rate": vRate, "previous_rate": pRate,
			}, now)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

type serverFaultHit struct {
	AppID  int64  `gorm:"column:app_id"`
	Method string `gorm:"column:method"`
	Path   string `gorm:"column:path"`
	Status string `gorm:"column:status"`
	N      int    `gorm:"column:n"`
}

func (s *Store) ruleServerFaults(ctx context.Context, now time.Time, settings map[int64]model.AlertSettings) ([]model.Alert, error) {
	var hits []serverFaultHit
	if err := s.db.WithContext(ctx).Raw(`
		SELECT e.app_id,
		       COALESCE(e.attributes->>'http.request.method','') AS method,
		       COALESCE(e.attributes->>'url.path','') AS path,
		       COALESCE(e.attributes->>'http.response.status_code','') AS status,
		       COUNT(*)::int AS n
		  FROM telemetry_event e
		  JOIN telemetry_app a ON a.id = e.app_id AND a.enabled
		 WHERE e.event_name = 'exception'
		   AND e.attributes->>'app.fault' = 'server'
		   AND e.event_time > ? AND e.event_time <= ?
		 GROUP BY e.app_id, method, path, status`,
		now.Add(-time.Hour), now,
	).Scan(&hits).Error; err != nil {
		return nil, fmt.Errorf("rule server_faults: %w", err)
	}
	type agg struct {
		n    int
		tops []serverFaultHit
	}
	byApp := map[int64]*agg{}
	for _, h := range hits {
		a := byApp[h.AppID]
		if a == nil {
			a = &agg{}
			byApp[h.AppID] = a
		}
		a.n += h.N
		a.tops = append(a.tops, h)
	}
	key := "server:" + now.Format("2006010215")
	var out []model.Alert
	for appID, a := range byApp {
		st, ok := settings[appID]
		if !ok || a.n <= st.ServerFaultsPerHour {
			continue
		}
		tops := topServerPaths(a.tops, 5)
		items := make([]map[string]any, 0, len(tops))
		for _, t := range tops {
			items = append(items, map[string]any{
				"method": t.Method, "path": t.Path, "status": t.Status, "count": t.N,
			})
		}
		al, err := candidate(appID, model.RuleServerFaults, key, model.UrgencyDigest, "服务端故障",
			map[string]any{"count": a.n, "top": items}, now)
		if err != nil {
			return nil, err
		}
		out = append(out, al)
	}
	return out, nil
}

func topServerPaths(hits []serverFaultHit, n int) []serverFaultHit {
	sorted := append([]serverFaultHit(nil), hits...)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].N > sorted[i].N ||
				(sorted[j].N == sorted[i].N && (sorted[j].Path < sorted[i].Path ||
					(sorted[j].Path == sorted[i].Path && sorted[j].Method < sorted[i].Method))) {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	if len(sorted) > n {
		sorted = sorted[:n]
	}
	return sorted
}

func (s *Store) ruleSymbolsMissing(ctx context.Context, now time.Time, _ map[int64]model.AlertSettings) ([]model.Alert, error) {
	today := dateUTC(now)
	cutoff := today.AddDate(0, 0, -1).Format("2006-01-02")
	type row struct {
		AppID   int64
		Version string
		Need    string
		N       int
	}
	var rows []row
	if err := s.db.WithContext(ctx).Raw(`
		SELECT c.app_id, c.service_version AS version, n.need, COUNT(*)::int AS n
		  FROM telemetry_crash c
		  JOIN telemetry_event e ON e.event_day = c.event_day AND e.record_uid = c.record_uid
		  JOIN telemetry_app a ON a.id = c.app_id AND a.enabled
		  CROSS JOIN LATERAL unnest(string_to_array(btrim(c.needs), ' ')) AS n(need)
		 WHERE c.status = ?
		   AND c.event_day <= ?::date
		   AND e.environment = 'direct'
		   AND c.needs <> ''
		   AND n.need <> ''
		 GROUP BY c.app_id, c.service_version, n.need
		HAVING COUNT(*) >= 5`,
		model.CrashWaitingSymbols, cutoff,
	).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("rule symbols_missing: %w", err)
	}
	var out []model.Alert
	for _, r := range rows {
		a, err := candidate(r.AppID, model.RuleSymbolsMissing,
			"symbols_missing:"+r.Version+":"+r.Need,
			model.UrgencyImmediate, "符号缺失", map[string]any{
				"version": r.Version, "need": r.Need, "count": r.N,
			}, now)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

func (s *Store) ruleSilentApp(ctx context.Context, now time.Time, settings map[int64]model.AlertSettings) ([]model.Alert, error) {
	today := dateUTC(now)
	d7 := today.AddDate(0, 0, -7).Format("2006-01-02")
	d1 := today.AddDate(0, 0, -1).Format("2006-01-02")
	type row struct {
		AppID int64
		Avg   float64
	}
	var rows []row
	if err := s.db.WithContext(ctx).Raw(`
		SELECT m.app_id, SUM(m.sessions)::double precision / 7.0 AS avg
		  FROM telemetry_daily_metric m
		  JOIN telemetry_app a ON a.id = m.app_id AND a.enabled
		 WHERE m.environment = 'direct'
		   AND m.day BETWEEN ?::date AND ?::date
		 GROUP BY m.app_id`,
		d7, d1,
	).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("rule silent_app: %w", err)
	}
	hour := now.UTC().Hour()
	var out []model.Alert
	for _, r := range rows {
		st, ok := settings[r.AppID]
		if !ok || r.Avg < float64(st.SilentMinDailySessions) {
			continue
		}
		var n int64
		if err := s.db.WithContext(ctx).Raw(`
			SELECT COUNT(*) FROM telemetry_event
			 WHERE app_id = ?
			   AND event_day >= ?::date
			   AND event_time > ?`,
			r.AppID, d1, now.Add(-time.Duration(st.SilentHours)*time.Hour),
		).Scan(&n).Error; err != nil {
			return nil, fmt.Errorf("rule silent_app events: %w", err)
		}
		if n > 0 {
			continue
		}
		idx := 0
		if st.SilentHours > 0 {
			idx = hour / st.SilentHours
		}
		a, err := candidate(r.AppID, model.RuleSilentApp,
			fmt.Sprintf("silent:%s:%d", now.Format("20060102"), idx),
			model.UrgencyImmediate, "应用静默", map[string]any{
				"baseline": r.Avg, "hours": st.SilentHours,
			}, now)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

func (s *Store) ruleEngineFetchFailed(ctx context.Context, now time.Time, _ map[int64]model.AlertSettings) ([]model.Alert, error) {
	type row struct {
		EngineRevision string
		Variant        string
		LastError      string
	}
	var rows []row
	if err := s.db.WithContext(ctx).Raw(`
		SELECT engine_revision, variant, last_error
		  FROM telemetry_engine_symbol
		 WHERE status = 'failed'`,
	).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("rule engine_fetch_failed: %w", err)
	}
	out := make([]model.Alert, 0, len(rows))
	for _, r := range rows {
		a, err := candidate(0, model.RuleEngineFetchFailed,
			"engine:"+r.EngineRevision+":"+r.Variant,
			model.UrgencyImmediate, "引擎符号失败", map[string]any{
				"revision": r.EngineRevision, "variant": r.Variant, "last_error": r.LastError,
			}, now)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}
