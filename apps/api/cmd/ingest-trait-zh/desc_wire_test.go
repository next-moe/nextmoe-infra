package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDescHTTPTranslatorWire(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/chat/completions", r.URL.Path)
		assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		body, _ := io.ReadAll(r.Body)
		require.NoError(t, json.Unmarshal(body, &got))
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"该角色有呆毛。\n\n第二段。\n"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	tr := newDescHTTPTranslator(srv.URL, "tok", "glm-5.2", 4096)
	require.True(t, tr.Configured())

	zh, err := tr.Translate(context.Background(), "Ahoge", "This character has ahoge.\n\nSecond paragraph.", []glossPair{
		{"Ahoge", "呆毛"},
	})
	require.NoError(t, err)
	assert.Equal(t, "该角色有呆毛。\n\n第二段。", zh)

	assert.Equal(t, "glm-5.2", got["model"])
	assert.Equal(t, float64(0), got["temperature"])
	assert.Equal(t, float64(4096), got["max_tokens"])
	kw, ok := got["chat_template_kwargs"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, false, kw["enable_thinking"])

	msgs, ok := got["messages"].([]any)
	require.True(t, ok)
	require.Len(t, msgs, 2)
	sys := msgs[0].(map[string]any)
	user := msgs[1].(map[string]any)
	assert.Equal(t, DescribeSystemPrompt, sys["content"])
	assert.Equal(t, describeUserMessage("Ahoge", "This character has ahoge.\n\nSecond paragraph.", []glossPair{{"Ahoge", "呆毛"}}), user["content"])
}

func TestDescHTTPTranslatorRetry429ThenSuccess(t *testing.T) {
	orig := descRetryBackoff
	descRetryBackoff = []time.Duration{time.Millisecond}
	t.Cleanup(func() { descRetryBackoff = orig })

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		if hits == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"译文"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	zh, err := newDescHTTPTranslator(srv.URL, "tok", "m", 64).Translate(context.Background(), "Ahoge", "x", nil)
	require.NoError(t, err)
	assert.Equal(t, "译文", zh)
	assert.Equal(t, 2, hits)
}

func TestDescHTTPTranslatorNoRetryOn400(t *testing.T) {
	orig := descRetryBackoff
	descRetryBackoff = []time.Duration{time.Millisecond}
	t.Cleanup(func() { descRetryBackoff = orig })

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad"}}`))
	}))
	defer srv.Close()

	_, err := newDescHTTPTranslator(srv.URL, "tok", "m", 64).Translate(context.Background(), "Ahoge", "x", nil)
	require.Error(t, err)
	assert.Equal(t, 1, hits)
}

func TestDescHTTPTranslatorLengthIsError(t *testing.T) {
	orig := descRetryBackoff
	descRetryBackoff = []time.Duration{time.Millisecond}
	t.Cleanup(func() { descRetryBackoff = orig })

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"半"},"finish_reason":"length"}]}`))
	}))
	defer srv.Close()

	_, err := newDescHTTPTranslator(srv.URL, "tok", "m", 64).Translate(context.Background(), "Ahoge", "x", nil)
	require.ErrorContains(t, err, "finish_reason")
	assert.Equal(t, 1, hits)
}
