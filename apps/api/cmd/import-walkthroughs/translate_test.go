package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
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

func TestTranslateSplitsAChunkAnsweredInProse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct{ Content string } `json:"messages"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		var in struct{ Strings []string }
		require.NoError(t, json.Unmarshal([]byte(req.Messages[1].Content), &in))
		content := "我无法翻译这段内容。"
		if !slices.Contains(in.Strings, "拒否") {
			out := make([]string, len(in.Strings))
			for i, s := range in.Strings {
				out[i] = "zh:" + s
			}
			b, _ := json.Marshal(map[string]any{"t": out})
			content = string(b)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{
			{"message": map[string]any{"role": "assistant", "content": content}, "finish_reason": "stop"},
		}})
	}))
	defer srv.Close()

	l := &llm{client: llmsuggest.NewClient(srv.URL, "m"), model: "m"}
	job := translateJob{Strings: []string{"帰る", "拒否", "話す"}, KeepNames: []string{}, Glossary: []glossaryTerm{}}
	out, st, err := translateOne(context.Background(), l, mustSchema(translateSchemaJSON), job, 1000)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"帰る": "zh:帰る", "話す": "zh:話す"}, out)
	assert.Equal(t, 1, st.refused)
}

func TestChunkStringsCapsCountAndLength(t *testing.T) {
	long := strings.Repeat("あ", 1000)
	short := make([]string, 170)
	for i := range short {
		short[i] = "は"
	}
	sizes := func(chunks [][]string) []int {
		var n []int
		for _, c := range chunks {
			n = append(n, len(c))
		}
		return n
	}
	assert.Equal(t, []int{80, 80, 10}, sizes(chunkStrings(short)))
	assert.Equal(t, []int{1, 3}, sizes(chunkStrings([]string{long, long, "は", "は"})))
	assert.Equal(t, []int{1}, sizes(chunkStrings([]string{strings.Repeat("あ", 3000)})), "one over-long string is still asked")
}
