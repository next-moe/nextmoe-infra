package llm

import "strings"

const (
	ReasoningOff     = "off"
	ReasoningLow     = "low"
	ReasoningMedium  = "medium"
	ReasoningHigh    = "high"
	ReasoningDefault = "default"
)

const (
	ThinkingAuto       = "auto"
	ThinkingNone       = "none"
	ThinkingEffort     = "effort"
	ThinkingEffortNone = "effort_none"
	ThinkingTemplate   = "template"
	ThinkingToggle     = "thinking"
)

func sceneReasoning(sc *Scene, call string) string {
	if call != "" {
		return call
	}
	if sc.Reasoning == "" {
		return ReasoningOff
	}
	return sc.Reasoning
}

// Workers AI documents reasoning_effort "none" for glm-5.2 and deepseek-v4, and
// qwen3 there honours it too; xAI rejects "none" with a 400 but takes "low".
func thinkingStyle(r *Route) string {
	if r.Protocol != "" && r.Protocol != ProtocolChat && r.Protocol != ProtocolResponses {
		return ThinkingNone
	}
	if c := thinkingOrAuto(r.ThinkingControl); c != ThinkingAuto {
		return c
	}
	u := strings.ToLower(r.UpstreamID)
	family := func(names ...string) bool {
		for _, n := range names {
			if strings.Contains(u, n) {
				return true
			}
		}
		return false
	}
	switch {
	case !family("qwen3", "glm", "deepseek", "grok", "gpt-5", "o3", "o4", "kimi"):
		return ThinkingNone
	case strings.HasPrefix(u, "@cf/"):
		return ThinkingEffortNone
	case family("grok", "gpt-5", "o3", "o4"):
		return ThinkingEffort
	case family("qwen3"):
		return ThinkingTemplate
	case family("deepseek", "glm", "kimi"):
		return ThinkingToggle
	}
	return ThinkingNone
}

func reasoningFields(style, level, protocol string) map[string]any {
	if level == ReasoningDefault || level == "" {
		return nil
	}
	effort := level
	switch style {
	case ThinkingEffort:
		if level == ReasoningOff {
			effort = ReasoningLow
		}
	case ThinkingEffortNone:
		if level == ReasoningOff {
			effort = "none"
		}
	case ThinkingTemplate:
		if protocol != ProtocolChat {
			return nil
		}
		return map[string]any{"chat_template_kwargs": map[string]any{"enable_thinking": level != ReasoningOff}}
	case ThinkingToggle:
		if protocol != ProtocolChat {
			return nil
		}
		t := "enabled"
		if level == ReasoningOff {
			t = "disabled"
		}
		return map[string]any{"thinking": map[string]any{"type": t}}
	default:
		return nil
	}
	if protocol == ProtocolResponses {
		return map[string]any{"reasoning": map[string]any{"effort": effort}}
	}
	return map[string]any{"reasoning_effort": effort}
}
