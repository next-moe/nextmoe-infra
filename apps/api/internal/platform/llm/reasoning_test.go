package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestThinkingStyleGuessesFromTheUpstream(t *testing.T) {
	for upstream, want := range map[string]string{
		"@cf/qwen/qwen3-30b-a3b-fp8":         ThinkingEffortNone,
		"@cf/zai-org/glm-5.2":                ThinkingEffortNone,
		"@cf/deepseek-ai/deepseek-v4-flash":  ThinkingEffortNone,
		"grok-4.7":                           ThinkingEffort,
		"Qwen/Qwen3-235B-A22B":               ThinkingTemplate,
		"deepseek-chat":                      ThinkingToggle,
		"@cf/meta/llama-3.3-70b-instruct":    ThinkingNone,
		"@cf/qwen/qwen3-embedding-0.6b-test": ThinkingEffortNone,
	} {
		if got := thinkingStyle(&Route{UpstreamID: upstream}); got != want {
			t.Errorf("%s: got %s, want %s", upstream, got, want)
		}
	}
	for _, r := range []Route{{UpstreamID: "@cf/qwen/qwen3-embedding-0.6b", Protocol: ProtocolEmbeddings}, {UpstreamID: "grok-imagine-image", Protocol: ProtocolImages}} {
		if got := thinkingStyle(&r); got != ThinkingNone {
			t.Errorf("%s over %s must not be thinking-controlled, got %s", r.UpstreamID, r.Protocol, got)
		}
	}
	if got := thinkingStyle(&Route{UpstreamID: "grok-4.7", ThinkingControl: ThinkingNone}); got != ThinkingNone {
		t.Errorf("an explicit control must win over the guess, got %s", got)
	}
}

func TestReasoningFieldsPerStyle(t *testing.T) {
	cases := []struct {
		style, level, protocol string
		want                   map[string]any
	}{
		{ThinkingEffort, ReasoningOff, ProtocolChat, map[string]any{"reasoning_effort": "low"}},
		{ThinkingEffort, ReasoningHigh, ProtocolChat, map[string]any{"reasoning_effort": "high"}},
		{ThinkingEffortNone, ReasoningOff, ProtocolChat, map[string]any{"reasoning_effort": "none"}},
		{ThinkingEffort, ReasoningOff, ProtocolResponses, map[string]any{"reasoning": map[string]any{"effort": "low"}}},
		{ThinkingTemplate, ReasoningOff, ProtocolChat, map[string]any{"chat_template_kwargs": map[string]any{"enable_thinking": false}}},
		{ThinkingTemplate, ReasoningMedium, ProtocolChat, map[string]any{"chat_template_kwargs": map[string]any{"enable_thinking": true}}},
		{ThinkingTemplate, ReasoningOff, ProtocolResponses, nil},
		{ThinkingToggle, ReasoningOff, ProtocolChat, map[string]any{"thinking": map[string]any{"type": "disabled"}}},
		{ThinkingNone, ReasoningOff, ProtocolChat, nil},
		{ThinkingEffort, ReasoningDefault, ProtocolChat, nil},
	}
	for _, c := range cases {
		if got := reasoningFields(c.style, c.level, c.protocol); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s/%s/%s: got %v, want %v", c.style, c.level, c.protocol, got, c.want)
		}
	}
}

func TestStreamSendsTheSceneReasoningAndCallerExtraWins(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		bodies = append(bodies, b)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sse(`{"choices":[{"delta":{"content":"ok"}}]}`, "[DONE]")))
	}))
	t.Cleanup(srv.Close)
	g := testGateway(t, srv.URL)
	s := g.snap.Load()
	s.routes[1][0].UpstreamID = "@cf/qwen/qwen3-30b-a3b-fp8"

	if _, err := g.Stream(context.Background(), Call{Scene: "s", Messages: []Message{{Role: RoleUser, Content: "hi"}}}, nil); err != nil {
		t.Fatal(err)
	}
	s.scenes["s"].Reasoning = ReasoningHigh
	if _, err := g.Stream(context.Background(), Call{Scene: "s", Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Extra: map[string]json.RawMessage{"reasoning_effort": json.RawMessage(`"max"`)}}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Stream(context.Background(), Call{Scene: "s", Reasoning: ReasoningDefault, Messages: []Message{{Role: RoleUser, Content: "hi"}}}, nil); err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 3 {
		t.Fatalf("got %d upstream calls", len(bodies))
	}
	if bodies[0]["reasoning_effort"] != "none" {
		t.Errorf("an unset scene level must turn thinking off, body %v", bodies[0])
	}
	if bodies[1]["reasoning_effort"] != "max" {
		t.Errorf("the caller's own reasoning_effort must win, body %v", bodies[1])
	}
	if _, set := bodies[2]["reasoning_effort"]; set {
		t.Errorf("default must send nothing, body %v", bodies[2])
	}
}
