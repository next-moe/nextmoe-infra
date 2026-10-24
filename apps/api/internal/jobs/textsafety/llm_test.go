package textsafety

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
	got, err := bisect(t.Context(), []int{1, 2, 3, 4, 5, 6, 7, 8}, run, func(int) string { return "refused" })
	require.NoError(t, err)
	assert.Equal(t, []string{"ok", "ok", "ok", "ok", "refused", "refused", "ok", "ok"}, got)
	assert.LessOrEqual(t, calls, 9, "only the refused halves split further")
}

func TestBisectPassesOtherErrorsThrough(t *testing.T) {
	boom := errors.New("upstream down")
	_, err := bisect(t.Context(), []int{1, 2}, func(context.Context, []int) ([]string, error) {
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
	err := NewLLM(srv.URL, "t", "m").Ask(t.Context(), "x", &out)
	assert.ErrorIs(t, err, ErrBlocked)
	assert.Equal(t, 1, hits)
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
	llm := NewLLM(srv.URL, "t", "m")
	require.NoError(t, llm.Ask(t.Context(), "x", &out))
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
	err := NewLLM(srv.URL, "t", "m").Ask(t.Context(), "x", &out)
	assert.ErrorIs(t, err, ErrBlocked)
	assert.Equal(t, 2, hits)
}
