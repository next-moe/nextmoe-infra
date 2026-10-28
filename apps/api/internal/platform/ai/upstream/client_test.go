package upstream

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"api/internal/platform/llm"
)

type fakeGateway struct {
	ready    bool
	streamFn func(llm.Call) (llm.Result, error)
	modBody  string
	modErr   error
	lastCall llm.Call
	lastReq  llm.Request
}

func (f *fakeGateway) Ready(string) (string, bool) { return "glm-5.2", f.ready }

func (f *fakeGateway) Stream(_ context.Context, call llm.Call, _ func(llm.Event)) (llm.Result, error) {
	f.lastCall = call
	return f.streamFn(call)
}

func (f *fakeGateway) Moderate(_ context.Context, r llm.Request) (llm.Response, error) {
	f.lastReq = r
	return llm.Response{Body: []byte(f.modBody)}, f.modErr
}

func TestChatJSONGoesThroughTheSceneAsBufferedJSON(t *testing.T) {
	gw := &fakeGateway{ready: true, streamFn: func(llm.Call) (llm.Result, error) {
		return llm.Result{Text: " {\"label\":\"safe\"} ", Model: "@cf/zai-org/glm-5.2", Usage: llm.Usage{InputTokens: 9, OutputTokens: 4}}, nil
	}}
	c := NewClient(gw, llm.SceneModerationLLM)
	if !c.Configured() || c.Model() != "glm-5.2" {
		t.Fatal("a ready scene is configured and reports its primary model")
	}
	res, err := c.ChatJSON(context.Background(), "sys", "user", 64)
	if err != nil || res.Content != `{"label":"safe"}` || res.Channel != "@cf/zai-org/glm-5.2" || res.PromptTokens != 9 || res.CompletionTokens != 4 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if call := gw.lastCall; call.Scene != llm.SceneModerationLLM || !call.Buffered || !call.JSON || call.MaxTokens != 64 || string(call.Extra["temperature"]) != "0" {
		t.Fatalf("call = %+v", call)
	}
}

func TestChatJSONRateLimitIsTyped(t *testing.T) {
	gw := &fakeGateway{ready: true, streamFn: func(llm.Call) (llm.Result, error) {
		return llm.Result{}, &llm.Error{Kind: llm.ErrKindRateLimit, Status: 429, Msg: `{"errors":[{"code":3021}]}`}
	}}
	_, err := NewClient(gw, llm.SceneModerationLLM).ChatJSON(context.Background(), "s", "u", 8)
	if !IsRateLimited(err) || !strings.HasPrefix(err.Error(), "upstream http 429: ") {
		t.Fatalf("a 429 must stay typed and keep its log text: %v", err)
	}
}

func TestChatJSONOtherStatusIsNotRateLimited(t *testing.T) {
	gw := &fakeGateway{ready: true, streamFn: func(llm.Call) (llm.Result, error) {
		return llm.Result{}, &llm.Error{Kind: llm.ErrKindAuth, Status: 401, Msg: "unauthorized"}
	}}
	_, err := NewClient(gw, llm.SceneModerationLLM).ChatJSON(context.Background(), "s", "u", 8)
	// Cloudflare has been seen answering 401 when merely overloaded. Retrying
	// that would mask a genuine credential failure, so it must not be retried.
	if err == nil || IsRateLimited(err) {
		t.Fatalf("401 must not count as rate limited: %v", err)
	}
}

func TestOmniParsesTheFirstResult(t *testing.T) {
	gw := &fakeGateway{ready: true, modBody: `{"model":"omni-moderation-2024-09-26","results":[{"flagged":true,"categories":{"sexual":true},"category_scores":{"sexual":0.91}}]}`}
	c := NewOmniClient(gw, llm.SceneModerationOmni)
	res, err := c.Moderate(context.Background(), "text")
	if err != nil || !res.Flagged || !res.Categories["sexual"] || res.CategoryScores["sexual"] != 0.91 || res.Channel != "omni-moderation-2024-09-26" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	var input string
	if json.Unmarshal(gw.lastReq.Body["input"], &input) != nil || input != "text" || gw.lastReq.Scene != llm.SceneModerationOmni {
		t.Fatalf("request = %+v", gw.lastReq)
	}
	gw.modBody = `{"results":[]}`
	if _, err := c.Moderate(context.Background(), "x"); err == nil {
		t.Fatal("no results must be an error")
	}
	if (&OmniClient{gw: &fakeGateway{}, scene: "x"}).Configured() {
		t.Fatal("a scene without routes is not configured")
	}
}
