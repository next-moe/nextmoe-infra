package alert

import (
	"encoding/json"
	"strings"
	"testing"

	"api/internal/platform/telemetry/model"

	"gorm.io/datatypes"
)

func TestSettingsDefaults(t *testing.T) {
	var nilScan model.AlertSettings
	if err := nilScan.Scan(nil); err != nil {
		t.Fatal(err)
	}
	d := model.DefaultAlertSettings()
	if nilScan != d {
		t.Fatalf("NULL scan: %+v want %+v", nilScan, d)
	}

	var empty model.AlertSettings
	if err := json.Unmarshal([]byte(`{}`), &empty); err != nil {
		t.Fatal(err)
	}
	if empty != d {
		t.Fatalf("{}: %+v want %+v", empty, d)
	}

	var partial model.AlertSettings
	if err := json.Unmarshal([]byte(`{"crash_rate":0.02}`), &partial); err != nil {
		t.Fatal(err)
	}
	if partial.CrashRate != 0.02 {
		t.Fatalf("crash_rate=%v", partial.CrashRate)
	}
	if partial.AnrRate != d.AnrRate || partial.MinSessions != d.MinSessions ||
		partial.RegressionFactor != d.RegressionFactor || partial.SilentHours != d.SilentHours ||
		partial.ServerFaultsPerHour != d.ServerFaultsPerHour || partial.SilentMinDailySessions != d.SilentMinDailySessions ||
		partial.RegressionMinDelta != d.RegressionMinDelta {
		t.Fatalf("partial defaults missing: %+v", partial)
	}

	var fromBytes model.AlertSettings
	if err := fromBytes.Scan([]byte(`{"min_sessions":10}`)); err != nil {
		t.Fatal(err)
	}
	if fromBytes.MinSessions != 10 || fromBytes.CrashRate != d.CrashRate {
		t.Fatalf("scan partial: %+v", fromBytes)
	}
}

func TestSettingsValidation(t *testing.T) {
	ok := model.DefaultAlertSettings()
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		mut  func(*model.AlertSettings)
		want string
	}{
		{func(s *model.AlertSettings) { s.CrashRate = 0 }, "crash_rate"},
		{func(s *model.AlertSettings) { s.CrashRate = 1 }, "crash_rate"},
		{func(s *model.AlertSettings) { s.CrashRate = -0.1 }, "crash_rate"},
		{func(s *model.AlertSettings) { s.AnrRate = 0 }, "anr_rate"},
		{func(s *model.AlertSettings) { s.AnrRate = 1 }, "anr_rate"},
		{func(s *model.AlertSettings) { s.MinSessions = 0 }, "min_sessions"},
		{func(s *model.AlertSettings) { s.MinSessions = 1_000_001 }, "min_sessions"},
		{func(s *model.AlertSettings) { s.RegressionFactor = 0.999 }, "regression_factor"},
		{func(s *model.AlertSettings) { s.RegressionFactor = 100.1 }, "regression_factor"},
		{func(s *model.AlertSettings) { s.RegressionMinDelta = -0.0001 }, "regression_min_delta"},
		{func(s *model.AlertSettings) { s.RegressionMinDelta = 1.0001 }, "regression_min_delta"},
		{func(s *model.AlertSettings) { s.ServerFaultsPerHour = 0 }, "server_faults_per_hour"},
		{func(s *model.AlertSettings) { s.ServerFaultsPerHour = 1_000_001 }, "server_faults_per_hour"},
		{func(s *model.AlertSettings) { s.SilentHours = 0 }, "silent_hours"},
		{func(s *model.AlertSettings) { s.SilentHours = 169 }, "silent_hours"},
		{func(s *model.AlertSettings) { s.SilentMinDailySessions = 0 }, "silent_min_daily_sessions"},
		{func(s *model.AlertSettings) { s.SilentMinDailySessions = 1_000_001 }, "silent_min_daily_sessions"},
	}
	for _, c := range cases {
		s := model.DefaultAlertSettings()
		c.mut(&s)
		err := s.Validate()
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err=%v", c.want, err)
		}
	}
	s := model.DefaultAlertSettings()
	s.CrashRate = 1e-9
	s.AnrRate = 0.999
	s.MinSessions = 1
	s.RegressionFactor = 1
	s.RegressionMinDelta = 0
	s.ServerFaultsPerHour = 1
	s.SilentHours = 1
	s.SilentMinDailySessions = 1
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	s.MinSessions = 1_000_000
	s.RegressionFactor = 100
	s.RegressionMinDelta = 1
	s.ServerFaultsPerHour = 1_000_000
	s.SilentHours = 168
	s.SilentMinDailySessions = 1_000_000
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
}

func factsJSON(t *testing.T, v any) datatypes.JSON {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return datatypes.JSON(b)
}

func TestRenderEscapes(t *testing.T) {
	issue := model.Alert{
		AppID: 1,
		Rule:  model.RuleIssueNew,
		Title: "新问题",
		Facts: factsJSON(t, map[string]any{
			"issue_id": 7, "kind": "java", "title": `<script>alert(1)</script>`,
			"culprit": "pkg.Foo", "first_version": "1.0",
		}),
	}
	n := RenderImmediate(issue, "kungal-app", "https://admin.nextmoe.dev")
	if strings.Contains(n.HTML, `<script>alert(1)</script>`) {
		t.Fatalf("script unescaped: %s", n.HTML)
	}
	if !strings.Contains(n.HTML, `&lt;script&gt;alert(1)&lt;/script&gt;`) {
		t.Fatalf("escaped script missing: %s", n.HTML)
	}

	server := model.Alert{
		AppID: 1,
		Rule:  model.RuleServerFaults,
		Title: "服务端故障",
		Facts: factsJSON(t, map[string]any{
			"count": 9,
			"top": []map[string]any{
				{"method": "GET", "path": `/v1/foo"bar`, "status": "500", "count": 9},
			},
		}),
	}
	n = RenderImmediate(server, "kungal-app", "https://admin.nextmoe.dev")
	if strings.Contains(n.HTML, `/v1/foo"bar`) {
		t.Fatalf("quote unescaped: %s", n.HTML)
	}
	if !strings.Contains(n.HTML, htmlQuoteNeedle()) {
		t.Fatalf("escaped quote missing: %s", n.HTML)
	}
}

func htmlQuoteNeedle() string {
	return `/v1/foo&#34;bar`
}

func TestRenderLinks(t *testing.T) {
	base := "https://admin.nextmoe.dev"
	cases := []struct {
		rule string
		app  int64
		fact map[string]any
		want string
	}{
		{model.RuleContractNew, 1, map[string]any{"issue_id": 42, "title": "t", "first_version": "1"}, "/telemetry/issues/42"},
		{model.RuleIssueNew, 1, map[string]any{"issue_id": 7, "kind": "java", "title": "t", "culprit": "c", "first_version": "1"}, "/telemetry/issues/7"},
		{model.RuleIssueRegressed, 1, map[string]any{"issue_id": 9, "title": "t", "last_version": "2"}, "/telemetry/issues/9"},
		{model.RuleCrashRate, 5, map[string]any{"version": "1", "sessions": 1, "crashed": 1, "rate": 1.0, "threshold": 0.01}, "/telemetry?app=5"},
		{model.RuleAnrRate, 5, map[string]any{"version": "1", "sessions": 1, "crashed": 1, "rate": 1.0, "threshold": 0.01}, "/telemetry?app=5"},
		{model.RuleCrashRegression, 5, map[string]any{"version": "2", "previous_version": "1", "sessions": 1, "previous_sessions": 1, "rate": 1.0, "previous_rate": 0.1}, "/telemetry?app=5"},
		{model.RuleServerFaults, 5, map[string]any{"count": 1}, "/telemetry?app=5"},
		{model.RuleSymbolsMissing, 5, map[string]any{"version": "1", "need": "dart:aa", "count": 5}, "/telemetry/apps"},
		{model.RuleSilentApp, 5, map[string]any{"baseline": 50.0, "hours": 6}, "/telemetry/apps"},
		{model.RuleEngineFetchFailed, 0, map[string]any{"revision": "r", "variant": "v", "last_error": "e"}, "/telemetry/apps"},
	}
	for _, c := range cases {
		a := model.Alert{AppID: c.app, Rule: c.rule, Title: "t", Facts: factsJSON(t, c.fact)}
		n := RenderImmediate(a, "kungal-app", base)
		if !strings.Contains(n.HTML, c.want) {
			t.Errorf("%s: want %s in %s", c.rule, c.want, n.HTML)
		}
		if !strings.Contains(n.HTML, `href="`+base+c.want+`"`) && !strings.Contains(n.HTML, `href="`+htmlEscapeURL(base+c.want)+`"`) {
			t.Errorf("%s: href missing %s in %s", c.rule, c.want, n.HTML)
		}
	}
}

func htmlEscapeURL(s string) string { return s }

func TestDigestGrouping(t *testing.T) {
	alerts := []model.Alert{
		{AppID: 1, Rule: model.RuleIssueNew, Title: "新问题", Facts: factsJSON(t, map[string]any{"issue_id": 1, "kind": "java", "title": "A", "culprit": "c", "first_version": "1"})},
		{AppID: 1, Rule: model.RuleIssueRegressed, Title: "问题复发", Facts: factsJSON(t, map[string]any{"issue_id": 2, "title": "B", "last_version": "2"})},
		{AppID: 1, Rule: model.RuleIssueNew, Title: "新问题", Facts: factsJSON(t, map[string]any{"issue_id": 3, "kind": "anr", "title": "C", "culprit": "c", "first_version": "1"})},
	}
	n := RenderDigest("kungal-app", "https://admin.nextmoe.dev", alerts)
	if n.Heading != "摘要" {
		t.Fatalf("heading=%q", n.Heading)
	}
	if !strings.Contains(n.Subject, subjectPrefix) || !strings.Contains(n.Subject, "kungal-app") {
		t.Fatalf("subject=%q", n.Subject)
	}
	iA := strings.Index(n.HTML, "A")
	iC := strings.Index(n.HTML, "C")
	iB := strings.Index(n.HTML, "B")
	if iA < 0 || iC < 0 || iB < 0 {
		t.Fatalf("missing titles: %s", n.HTML)
	}
	if !(iA < iC && iC < iB) {
		t.Fatalf("grouping order A,C then B: %d %d %d html=%s", iA, iC, iB, n.HTML)
	}
	g := groupByRule(alerts)
	if len(g) != 2 || len(g[0]) != 2 || g[0][0].Rule != model.RuleIssueNew || g[1][0].Rule != model.RuleIssueRegressed {
		t.Fatalf("groups=%+v", g)
	}
}
