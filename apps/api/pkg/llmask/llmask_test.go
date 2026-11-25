package llmask

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBisectIsolatesRefusedItems(t *testing.T) {
	calls := 0
	run := func(_ context.Context, b []int) ([]string, error) {
		calls++
		if slices.Contains(b, 5) || slices.Contains(b, 6) {
			return nil, ErrBlocked
		}
		out := make([]string, len(b))
		for i := range b {
			out[i] = "ok"
		}
		return out, nil
	}
	got, err := Bisect(t.Context(), []int{1, 2, 3, 4, 5, 6, 7, 8}, run, func(int) string { return "refused" })
	require.NoError(t, err)
	assert.Equal(t, []string{"ok", "ok", "ok", "ok", "refused", "refused", "ok", "ok"}, got)
	assert.LessOrEqual(t, calls, 9, "only the refused halves split further")
}

func TestBisectPassesOtherErrorsThrough(t *testing.T) {
	boom := errors.New("upstream down")
	_, err := Bisect(t.Context(), []int{1, 2}, func(context.Context, []int) ([]string, error) {
		return nil, boom
	}, func(int) string { return "refused" })
	assert.ErrorIs(t, err, boom)
}

func TestAskMapsUnprocessableToErrBlockedWithoutRetry(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":{"message":"Request blocked","type":"content_blocked"}}`))
	}))
	defer srv.Close()
	var out map[string]any
	_, err := New(srv.URL, "t", "m").Ask(t.Context(), "x", &out)
	assert.ErrorIs(t, err, ErrBlocked)
	assert.Equal(t, 1, hits)
}

func TestAskMapsTheGatewaysContentFilterToErrBlocked(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"request blocked","type":"invalid_request_error","code":"content_filtered"}}`))
	}))
	defer srv.Close()
	var out map[string]any
	_, err := New(srv.URL, "t", "m").Ask(t.Context(), "x", &out)
	assert.ErrorIs(t, err, ErrBlocked)
	assert.Equal(t, 1, hits)
}

func TestAskReturnsATruncatedReplyAsErrTruncatedWithoutRetry(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"items\":["},"finish_reason":"length"}],"usage":{"prompt_tokens":10,"completion_tokens":8192}}`))
	}))
	defer srv.Close()
	var out map[string]any
	llm := New(srv.URL, "t", "m")
	_, err := llm.Ask(t.Context(), "x", &out)
	assert.ErrorIs(t, err, ErrTruncated)
	assert.Equal(t, 1, hits)
	calls, _, outTok := llm.Usage()
	assert.Equal(t, []int64{1, 8192}, []int64{calls, outTok}, "a cut-off call still spent its tokens")
}

func TestBisectSplitsTruncatedBatchesAndLeavesALoneTruncatedItemPending(t *testing.T) {
	run := func(_ context.Context, b []int) ([]string, error) {
		if len(b) > 2 || slices.Contains(b, 3) {
			return nil, ErrTruncated
		}
		out := make([]string, len(b))
		for i := range b {
			out[i] = "ok"
		}
		return out, nil
	}
	got, err := Bisect(t.Context(), []int{1, 2, 3, 4}, run, func(int) string { return "refused" })
	require.NoError(t, err)
	assert.Equal(t, []string{"ok", "ok", "ok"}, got)
}

func TestAskDecodesTheJSONObjectInsideProse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"Sure:\n` + "```json" + `\n{\"items\":[{\"id\":1}]}\n` + "```" + `"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2}}`))
	}))
	defer srv.Close()
	var out struct {
		Items []struct {
			ID int `json:"id"`
		} `json:"items"`
	}
	llm := New(srv.URL, "t", "m")
	served, err := llm.Ask(t.Context(), "x", &out)
	require.NoError(t, err)
	assert.Equal(t, "m", served, "a reply without a model names the requested one")
	assert.Equal(t, 1, out.Items[0].ID)
	calls, in, outTok := llm.Usage()
	assert.Equal(t, []int64{1, 10, 2}, []int64{calls, in, outTok})
}

func TestAskTreatsRepeatedProseAsARefusal(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"I can't help with that."},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()
	var out map[string]any
	_, err := New(srv.URL, "t", "m").Ask(t.Context(), "x", &out)
	assert.ErrorIs(t, err, ErrBlocked)
	assert.Equal(t, 2, hits)
}

func TestAskReportsTheModelTheGatewayServedFrom(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"model":"grok-4.7:low:fast","choices":[{"message":{"role":"assistant","content":"{}"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()
	var out map[string]any
	served, err := New(srv.URL, "t", "catalog.text-safety.title").Ask(t.Context(), "x", &out)
	require.NoError(t, err)
	assert.Equal(t, "grok-4.7:low:fast", served)
}
