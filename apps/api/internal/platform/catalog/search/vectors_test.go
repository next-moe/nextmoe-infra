package search

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkCardText(t *testing.T) {
	in := WorkDocInput{
		DisplayName: "千恋＊万花",
		Titles: []WorkDocTitle{
			{Lang: "ja", Title: "千恋＊万花"},
			{Lang: "zh-Hans", Title: "千恋万花"},
		},
		Intros: []WorkDocIntro{
			{Lang: "en", Text: "english intro"},
			{Lang: "ja", Text: "  日本語の紹介  "},
			{Lang: "zh-Hans", Text: "中文简介"},
			{Lang: "zh-Hant", Text: "第二条中文简介"},
		},
	}
	got := WorkCardText(in, []string{"巫女", "妖怪"})
	want := "千恋＊万花 / 千恋万花\n巫女, 妖怪\n中文简介\n日本語の紹介\nenglish intro"
	if got != want {
		t.Fatalf("card text:\n got %q\nwant %q", got, want)
	}
}

func TestWorkCardTextCaps(t *testing.T) {
	in := WorkDocInput{DisplayName: "x", Intros: []WorkDocIntro{{Lang: "ja", Text: strings.Repeat("あ", cardIntroRunes+50)}}}
	tags := make([]string, cardMaxTags+5)
	for i := range tags {
		tags[i] = "t"
	}
	got := WorkCardText(in, tags)
	if n := strings.Count(got, "t"); n != cardMaxTags {
		t.Fatalf("tags kept = %d, want %d", n, cardMaxTags)
	}
	if n := strings.Count(got, "あ"); n != cardIntroRunes {
		t.Fatalf("intro runes kept = %d, want %d", n, cardIntroRunes)
	}
}

func TestCharacterCardText(t *testing.T) {
	d := EntityDoc{NameJa: "朝武芳乃", Latin: "Tomotake Yoshino", AliasesZh: []string{"芳乃"}, AliasesJa: []string{"朝武芳乃"}}
	got := CharacterCardText(CharacterCard{
		Doc:    d,
		Works:  []string{"千恋＊万花"},
		Traits: []string{"巫女", "黒髪"},
		Intros: []WorkDocIntro{{Lang: "ja", Text: "神社の巫女。"}},
	})
	want := "朝武芳乃 / Tomotake Yoshino / 芳乃\n千恋＊万花\n巫女, 黒髪\n神社の巫女。"
	if got != want {
		t.Fatalf("card text:\n got %q\nwant %q", got, want)
	}
}

func TestCardHashTracksModelAndText(t *testing.T) {
	base := CardHash("v1", "m1", "text")
	if base == CardHash("v1", "m2", "text") || base == CardHash("v1", "m1", "text2") || base == CardHash("v2", "m1", "text") {
		t.Fatal("hash must change with version, model and text")
	}
	if base != CardHash("v1", "m1", "text") {
		t.Fatal("hash must be deterministic")
	}
}

func decodeJSON(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestVectorFiltersClearEmptyValues(t *testing.T) {
	work := decodeJSON(t, NewWorkVectorDoc(EntityDoc{ID: "w1", CatalogID: 1}).Filters)
	for _, k := range []string{"embed_model", "text_hash", "vector", "id"} {
		if _, ok := work[k]; ok {
			t.Errorf("filter update must not carry %q", k)
		}
	}
	if v, ok := work["tag_ids"].([]any); !ok || len(v) != 0 {
		t.Errorf("empty tag_ids must be sent as [] to clear the old value, got %#v", work["tag_ids"])
	}
	if v, ok := work["content_limit"]; !ok || v != nil {
		t.Errorf("empty content_limit must be sent as null, got %#v", v)
	}
	char := decodeJSON(t, NewCharacterVectorDoc(EntityDoc{ID: "c1", CatalogID: 1}).Filters)
	if v, ok := char["trait_ids_sfw"].([]any); !ok || len(v) != 0 {
		t.Errorf("empty trait_ids_sfw must be sent as [], got %#v", char["trait_ids_sfw"])
	}
	if v, ok := char["gender"]; !ok || v != nil {
		t.Errorf("unknown gender must be sent as null, got %#v", v)
	}
}

func TestVectorDocMarshalMergesStampsAndFilters(t *testing.T) {
	d := NewWorkVectorDoc(EntityDoc{ID: "w7", CatalogID: 7, OLang: "ja"})
	d.EmbedModel, d.TextHash, d.Vector = "m", "h", []float32{0.5}
	got := decodeJSON(t, d)
	if got["id"] != "w7" || got["catalog_id"] != float64(7) || got["embed_model"] != "m" || got["text_hash"] != "h" || got["olang"] != "ja" {
		t.Fatalf("marshalled doc = %v", got)
	}
	if v, ok := got["vector"].([]any); !ok || len(v) != 1 {
		t.Fatalf("vector = %#v", got["vector"])
	}
}

func TestOpenAIEmbedderOrdersByIndexAndRetries429(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, `{"errors":[{"code":3040}]}`, http.StatusTooManyRequests)
			return
		}
		if r.URL.Path != "/embeddings" || r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"index":1,"embedding":[2]},{"index":0,"embedding":[1]}]}`))
	}))
	defer srv.Close()
	e := NewOpenAIEmbedder(srv.URL+"/", "tok", "m")
	e.retry = []time.Duration{time.Millisecond}
	got, err := e.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || got[0][0] != 1 || got[1][0] != 2 {
		t.Fatalf("calls=%d vectors=%v", calls.Load(), got)
	}
}

func TestOpenAIEmbedderRejectsShortReply(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[1]}]}`))
	}))
	defer srv.Close()
	if _, err := NewOpenAIEmbedder(srv.URL, "tok", "m").Embed(context.Background(), []string{"a", "b"}); err == nil {
		t.Fatal("want an error when fewer vectors than inputs come back")
	}
}

func TestOpenAIEmbedderHalvesOversizedBatch(t *testing.T) {
	var sizes []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		sizes = append(sizes, len(req.Input))
		if len(req.Input) > 2 {
			http.Error(w, `{"errors":[{"message":"AiError: AiError: input too big (x)","code":3030}]}`, http.StatusBadRequest)
			return
		}
		data := make([]map[string]any, len(req.Input))
		for i, s := range req.Input {
			data[i] = map[string]any{"index": i, "embedding": []float32{float32(len(s))}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer srv.Close()
	got, err := NewOpenAIEmbedder(srv.URL, "tok", "m").Embed(context.Background(), []string{"a", "bb", "ccc", "dddd", "eeeee"})
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range got {
		if int(v[0]) != i+1 {
			t.Fatalf("vector %d = %v, order lost across the split", i, v)
		}
	}
	if sizes[0] != 5 {
		t.Fatalf("first request must carry the whole batch, sizes=%v", sizes)
	}
}

func TestOpenAIEmbedderDoesNotRetry4xx(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer srv.Close()
	e := NewOpenAIEmbedder(srv.URL, "tok", "m")
	e.retry = []time.Duration{time.Millisecond, time.Millisecond}
	if _, err := e.Embed(context.Background(), []string{"a"}); err == nil || calls.Load() != 1 {
		t.Fatalf("err=%v calls=%d, want one call and an error", err, calls.Load())
	}
}
