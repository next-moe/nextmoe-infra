package llmsuggest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// statusLLM answers with status(call) until it returns 200, then a verdict.
func statusLLM(t *testing.T, status func(call int) int) (*Client, *atomic.Int64) {
	t.Helper()
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(calls.Add(1)) - 1
		if code := status(n); code != http.StatusOK {
			w.WriteHeader(code)
			_, _ = w.Write([]byte(`{"error":{"message":"openai_error"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]any{"content": `{"verdict":"same","reason":"r","confidence":0.9}`},
				"finish_reason": "stop",
			}},
		})
	}))
	t.Cleanup(srv.Close)
	return NewClient(srv.URL, "mock-model"), &calls
}

func TestJudgeRidesOutARateLimit(t *testing.T) {
	c, calls := statusLLM(t, func(call int) int {
		if call < 2 {
			return http.StatusTooManyRequests
		}
		return http.StatusOK
	})
	start := time.Now()
	v, err := judge(t.Context(), c, "sys", "user", 64)
	require.NoError(t, err, "a 429 is the upstream asking to wait, not a verdict")
	assert.Equal(t, VerdictSame, v.Verdict)
	assert.Equal(t, int64(3), calls.Load())
	assert.GreaterOrEqual(t, time.Since(start), judgeBackoffMin,
		"retrying a rate limit without pausing is what produced 3,341 lost judgements")
}

func TestJudgeStopsOnARefusalTheGatewayWillRepeat(t *testing.T) {
	c, calls := statusLLM(t, func(int) int { return http.StatusBadRequest })
	_, err := judge(t.Context(), c, "sys", "user", 64)
	require.Error(t, err)
	assert.Equal(t, int64(1), calls.Load(), "an unsupported model is not worth judgeAttempts calls")
	assert.Contains(t, err.Error(), "vllm http 400")
}

func TestJudgeRetriesAMalformedCompletionWithoutWaiting(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(calls.Add(1)) - 1
		body := `not json`
		if n > 0 {
			body = `{"verdict":"different","reason":"r","confidence":0.8}`
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": body}, "finish_reason": "stop"}},
		})
	}))
	t.Cleanup(srv.Close)
	start := time.Now()
	v, err := judge(t.Context(), NewClient(srv.URL, "mock-model"), "sys", "user", 64)
	require.NoError(t, err)
	assert.Equal(t, VerdictDifferent, v.Verdict)
	assert.Less(t, time.Since(start), judgeBackoffMin, "only the upstream earns a pause")
}

func TestWaitBeforeRetryHonoursCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := waitBeforeRetry(ctx, 1, &StatusError{Status: http.StatusTooManyRequests})
	assert.ErrorIs(t, err, context.Canceled)
}

func TestTransientSeparatesTheUpstreamFromTheRequest(t *testing.T) {
	assert.True(t, transient(&StatusError{Status: http.StatusTooManyRequests}))
	assert.True(t, transient(&StatusError{Status: http.StatusBadGateway}))
	assert.True(t, transient(context.DeadlineExceeded), "a dead connection never reached a status line")
	assert.False(t, transient(&StatusError{Status: http.StatusBadRequest}))
	assert.False(t, transient(nil))
}
