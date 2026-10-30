package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"api/internal/platform/catalog/llmsuggest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTranslateAsksAgainWithOneRuneNamesMasked(t *testing.T) {
	zh := strings.NewReplacer("鈴", "铃", "を追う", "去追", "セーブ１２", "存档１２")
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req struct {
			Messages []struct{ Content string } `json:"messages"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		var in struct{ Strings []string }
		require.NoError(t, json.Unmarshal([]byte(req.Messages[1].Content), &in))
		out := make([]string, len(in.Strings))
		for i, s := range in.Strings {
			out[i] = zh.Replace(s)
		}
		content, _ := json.Marshal(map[string]any{"t": out})
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{
			{"message": map[string]any{"role": "assistant", "content": string(content)}, "finish_reason": "stop"},
		}})
	}))
	defer srv.Close()

	l := &llm{client: llmsuggest.NewClient(srv.URL, "m"), model: "m"}
	job := translateJob{Strings: []string{"鈴を追う", "鈴", "セーブ１２"}, KeepNames: []string{"鈴"}, Glossary: []glossaryTerm{}}
	out, st, err := translateOne(context.Background(), l, mustSchema(translateSchemaJSON), job, 1000)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"鈴を追う": "鈴去追", "鈴": "鈴", "セーブ１２": "存档１２"}, out)
	assert.Equal(t, 0, st.refused)
	assert.Equal(t, 2, calls, "only the refused strings are asked again")
}
