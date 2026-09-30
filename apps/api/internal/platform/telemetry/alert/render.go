package alert

import (
	"encoding/json"
	"fmt"
	"html"
	"strconv"
	"strings"

	"api/internal/platform/telemetry/model"
)

const defaultAdminBase = "https://admin.nextmoe.dev"
const subjectPrefix = "[NextMoe 监测]"

func trimAdminBase(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return defaultAdminBase
	}
	return base
}

func RenderImmediate(a model.Alert, appName, adminBase string) Notification {
	heading := a.Title
	if heading == "" {
		heading = a.Rule
	}
	name := appName
	if name == "" {
		name = "engine"
	}
	return Notification{
		Subject: fmt.Sprintf("%s %s %s", subjectPrefix, name, heading),
		Heading: heading,
		HTML:    renderAlertHTML(a, adminBase),
	}
}

func RenderDigest(appName, adminBase string, alerts []model.Alert) Notification {
	name := appName
	if name == "" {
		name = "engine"
	}
	grouped := groupByRule(alerts)
	var b strings.Builder
	for _, g := range grouped {
		for _, a := range g {
			b.WriteString(renderAlertHTML(a, adminBase))
		}
	}
	return Notification{
		Subject: fmt.Sprintf("%s %s %s", subjectPrefix, name, "摘要"),
		Heading: "摘要",
		HTML:    b.String(),
	}
}

func groupByRule(alerts []model.Alert) [][]model.Alert {
	order := make([]string, 0)
	by := map[string][]model.Alert{}
	for _, a := range alerts {
		if _, ok := by[a.Rule]; !ok {
			order = append(order, a.Rule)
		}
		by[a.Rule] = append(by[a.Rule], a)
	}
	out := make([][]model.Alert, 0, len(order))
	for _, r := range order {
		out = append(out, by[r])
	}
	return out
}

func renderAlertHTML(a model.Alert, adminBase string) string {
	base := trimAdminBase(adminBase)
	facts := parseFacts(a.Facts)
	sentence := html.EscapeString(chineseSentence(a.Rule, facts))
	href := html.EscapeString(alertURL(base, a, facts))
	var b strings.Builder
	b.WriteString(fmt.Sprintf(
		`<p style="margin:0 0 10px; font-size:14px; color:#3e4c59;">%s</p>`, sentence))
	for _, line := range topPathLines(facts) {
		b.WriteString(fmt.Sprintf(
			`<p style="margin:0 0 6px; font-size:13px; color:#3e4c59;">%s</p>`, html.EscapeString(line)))
	}
	b.WriteString(fmt.Sprintf(
		`<p style="margin:0 0 16px; font-size:13px;"><a href="%s">在管理后台查看</a></p>`, href))
	return b.String()
}

func topPathLines(facts map[string]any) []string {
	raw, ok := facts["top"]
	if !ok {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, fmt.Sprintf("%s %s %s (%d)",
			factString(m, "method"), factString(m, "path"), factString(m, "status"), factInt(m, "count")))
	}
	return out
}

func alertURL(base string, a model.Alert, facts map[string]any) string {
	switch a.Rule {
	case model.RuleContractNew, model.RuleIssueNew, model.RuleIssueRegressed:
		return base + "/telemetry/issues/" + strconv.FormatInt(factInt(facts, "issue_id"), 10)
	case model.RuleCrashRate, model.RuleAnrRate, model.RuleCrashRegression, model.RuleServerFaults:
		return base + "/telemetry?app=" + strconv.FormatInt(a.AppID, 10)
	default:
		return base + "/telemetry/apps"
	}
}

func chineseSentence(rule string, facts map[string]any) string {
	switch rule {
	case model.RuleContractNew:
		return fmt.Sprintf("发现新的契约不一致：%s（首次版本 %s）。",
			factString(facts, "title"), factString(facts, "first_version"))
	case model.RuleIssueNew:
		return fmt.Sprintf("新问题（%s）：%s，位置 %s，首次版本 %s。",
			factString(facts, "kind"), factString(facts, "title"),
			factString(facts, "culprit"), factString(facts, "first_version"))
	case model.RuleIssueRegressed:
		return fmt.Sprintf("问题复发：%s，最近版本 %s。",
			factString(facts, "title"), factString(facts, "last_version"))
	case model.RuleCrashRate:
		return fmt.Sprintf("版本 %s 崩溃率为 %.2f%%（%d/%d），超过阈值 %.2f%%。",
			factString(facts, "version"), factFloat(facts, "rate")*100,
			factInt(facts, "crashed"), factInt(facts, "sessions"),
			factFloat(facts, "threshold")*100)
	case model.RuleAnrRate:
		return fmt.Sprintf("版本 %s ANR 率为 %.2f%%（%d/%d），超过阈值 %.2f%%。",
			factString(facts, "version"), factFloat(facts, "rate")*100,
			factInt(facts, "crashed"), factInt(facts, "sessions"),
			factFloat(facts, "threshold")*100)
	case model.RuleCrashRegression:
		return fmt.Sprintf("版本 %s 崩溃率 %.2f%% 相对上一版本 %s 的 %.2f%% 明显升高。",
			factString(facts, "version"), factFloat(facts, "rate")*100,
			factString(facts, "previous_version"), factFloat(facts, "previous_rate")*100)
	case model.RuleServerFaults:
		return fmt.Sprintf("最近一小时服务端故障 %d 次。", factInt(facts, "count"))
	case model.RuleSymbolsMissing:
		return fmt.Sprintf("版本 %s 缺少符号 %s，已有 %d 条崩溃在等待。",
			factString(facts, "version"), factString(facts, "need"), factInt(facts, "count"))
	case model.RuleSilentApp:
		return fmt.Sprintf("应用已超过 %d 小时无上报（近期日均会话 %.0f）。",
			factInt(facts, "hours"), factFloat(facts, "baseline"))
	case model.RuleEngineFetchFailed:
		return fmt.Sprintf("引擎符号获取失败：%s / %s：%s。",
			factString(facts, "revision"), factString(facts, "variant"), factString(facts, "last_error"))
	default:
		return aRuleFallback(rule)
	}
}

func aRuleFallback(rule string) string { return "监测告警：" + rule }

func parseFacts(raw []byte) map[string]any {
	out := map[string]any{}
	if len(raw) == 0 {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	return out
}

func factString(m map[string]any, k string) string {
	v, ok := m[k]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

func factInt(m map[string]any, k string) int64 {
	v, ok := m[k]
	if !ok || v == nil {
		return 0
	}
	switch t := v.(type) {
	case float64:
		return int64(t)
	case int64:
		return t
	case int:
		return int64(t)
	case json.Number:
		n, _ := t.Int64()
		return n
	default:
		n, _ := strconv.ParseInt(fmt.Sprint(t), 10, 64)
		return n
	}
}

func factFloat(m map[string]any, k string) float64 {
	v, ok := m[k]
	if !ok || v == nil {
		return 0
	}
	switch t := v.(type) {
	case float64:
		return t
	case int64:
		return float64(t)
	case int:
		return float64(t)
	case json.Number:
		n, _ := t.Float64()
		return n
	default:
		n, _ := strconv.ParseFloat(fmt.Sprint(t), 64)
		return n
	}
}
