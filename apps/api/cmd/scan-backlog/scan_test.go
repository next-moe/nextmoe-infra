package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

type stubServer struct {
	*httptest.Server
	mu   sync.Mutex
	hits map[string]int
}

func newStub(t *testing.T) *stubServer {
	t.Helper()
	s := &stubServer{hits: map[string]int{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.Close)
	return s
}

func (s *stubServer) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	_ = json.Unmarshal(body, &req)
	user := ""
	for _, m := range req.Messages {
		if m.Role == "user" {
			user = m.Content
		}
	}

	s.mu.Lock()
	s.hits[user]++
	n := s.hits[user]
	s.mu.Unlock()

	if strings.Contains(user, "HTTPFAIL") {
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	if strings.Contains(user, "FLAKY") && n == 1 {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	flagged, score, cats := directive(user)
	vb, _ := json.Marshal(map[string]any{"flagged": flagged, "score": score, "categories": cats})
	_ = json.NewEncoder(w).Encode(map[string]any{
		"model": "stub-model",
		"choices": []any{map[string]any{
			"message": map[string]any{"role": "assistant", "content": string(vb)},
		}},
		"usage": map[string]any{"prompt_tokens": 3, "completion_tokens": 4},
	})
}

func (s *stubServer) hitCount(user string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits[user]
}

func directive(content string) (bool, float64, []string) {
	var flagged bool
	var score float64
	var cats []string
	for _, tok := range strings.Fields(content) {
		switch {
		case strings.HasPrefix(tok, "score="):
			fmt.Sscanf(tok[len("score="):], "%f", &score)
		case tok == "flag=1":
			flagged = true
		case strings.HasPrefix(tok, "cats="):
			for _, c := range strings.Split(tok[len("cats="):], "|") {
				if c != "" {
					cats = append(cats, c)
				}
			}
		}
	}
	return flagged, score, cats
}

func recLine(id, site, kind, text string) string {
	b, _ := json.Marshal(inputRecord{ID: id, Site: site, Kind: kind, Text: text})
	return string(b)
}

func readScored(t *testing.T, path string) ([]scoredRow, []errorRow) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open out %s: %v", path, err)
	}
	defer f.Close()
	var rows []scoredRow
	var errs []errorRow
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), scanBufMax)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var probe struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(line, &probe)
		if probe.Error != "" {
			var e errorRow
			_ = json.Unmarshal(line, &e)
			errs = append(errs, e)
			continue
		}
		var r scoredRow
		_ = json.Unmarshal(line, &r)
		rows = append(rows, r)
	}
	return rows, errs
}

func readWorklist(t *testing.T, path string) []worklistItem {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open worklist %s: %v", path, err)
	}
	defer f.Close()
	var items []worklistItem
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), scanBufMax)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var it worklistItem
		_ = json.Unmarshal(line, &it)
		items = append(items, it)
	}
	return items
}

func byID(rows []scoredRow) map[string]scoredRow {
	m := map[string]scoredRow{}
	for _, r := range rows {
		m[r.ID] = r
	}
	return m
}

func TestRunScoresCountsHistogramTop(t *testing.T) {
	stub := newStub(t)
	client := newClient(stub.URL, "", "stub-model")

	longBody := "score=0.95 flag=1 cats=abuse " + strings.Repeat("あ", 400)
	lines := []string{
		recLine("a", "forum", "topic", "score=0.05 flag=0 hello world"),
		recLine("b", "forum", "reply", "score=0.35 flag=0 meh"),
		recLine("c", "kungal", "topic", "score=0.95 flag=1 cats=abuse|spam you are trash"),
		recLine("d", "kungal", "reply", longBody),
		`{"id":"e"`,
		`{"id":"f","text":""}`,
		string([]byte{0x7b, 0xff, 0x7d}),
		"",
	}
	input := strings.NewReader(strings.Join(lines, "\n") + "\n")

	dir := t.TempDir()
	cfg := scanConfig{outPath: filepath.Join(dir, "out.jsonl"), model: "stub-model", workers: 2, topN: 2}
	var summary bytes.Buffer
	res, err := run(context.Background(), cfg, client, input, &summary)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if res.input.valid != 4 || res.input.badLines != 2 || res.input.invalidUTF8 != 1 {
		t.Fatalf("input stats = %+v, want valid=4 bad=2 invalidUTF8=1", res.input)
	}
	if res.enqueued != 4 || res.succeeded != 4 || res.failed != 0 || res.skippedResume != 0 {
		t.Fatalf("run counters = %+v, want enqueued=4 succeeded=4 failed=0 skipped=0", res)
	}

	rows, errs := readScored(t, cfg.outPath)
	if len(rows) != 4 || len(errs) != 0 {
		t.Fatalf("out rows=%d errs=%d, want 4/0", len(rows), len(errs))
	}
	m := byID(rows)
	if !m["c"].Flagged || !m["d"].Flagged || m["a"].Flagged || m["b"].Flagged {
		t.Errorf("flagged wrong: a=%v b=%v c=%v d=%v", m["a"].Flagged, m["b"].Flagged, m["c"].Flagged, m["d"].Flagged)
	}
	if m["a"].Score == nil || *m["a"].Score > 0.1 {
		t.Errorf("a score = %v, want ~0.05", m["a"].Score)
	}

	h, flagged := histogram(res.allScored)
	if h[0] != 1 || h[3] != 1 || h[9] != 2 {
		t.Errorf("histogram buckets = %v, want [0]=1 [3]=1 [9]=2", h)
	}
	if flagged != 2 {
		t.Errorf("flagged = %d, want 2", flagged)
	}
	if !strings.Contains(summary.String(), "flagged=2") {
		t.Errorf("summary missing flagged=2:\n%s", summary.String())
	}

	cats := topCategories(res.allScored, 10)
	if len(cats) == 0 || cats[0].name != "abuse" || cats[0].count != 2 {
		t.Errorf("top category = %+v, want abuse×2", cats)
	}

	items := readWorklist(t, worklistPathFor(cfg.outPath))
	if len(items) != 2 || items[0].ID != "c" || items[1].ID != "d" {
		t.Fatalf("worklist = %+v, want [c, d]", items)
	}
	if n := utf8.RuneCountInString(items[1].Text); n != worklistTextRunes {
		t.Errorf("worklist d text = %d runes, want %d (truncated)", n, worklistTextRunes)
	}
}

func TestRunResumeIdempotentWithLimit(t *testing.T) {
	stub := newStub(t)
	client := newClient(stub.URL, "", "stub-model")
	lines := []string{
		recLine("a", "s", "k", "score=0.1 alpha"),
		recLine("b", "s", "k", "score=0.2 beta"),
		recLine("c", "s", "k", "score=0.3 gamma"),
	}
	joined := strings.Join(lines, "\n") + "\n"
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.jsonl")

	res1, err := run(context.Background(),
		scanConfig{outPath: outPath, workers: 2, topN: 100, limit: 2},
		client, strings.NewReader(joined), io.Discard)
	if err != nil {
		t.Fatalf("run1: %v", err)
	}
	if res1.input.valid != 2 || res1.enqueued != 2 || res1.succeeded != 2 {
		t.Fatalf("run1 counters = %+v, want valid/enqueued/succeeded = 2", res1)
	}
	rows1, _ := readScored(t, outPath)
	if len(rows1) != 2 {
		t.Fatalf("run1 out rows = %d, want 2", len(rows1))
	}

	res2, err := run(context.Background(),
		scanConfig{outPath: outPath, workers: 2, topN: 100},
		client, strings.NewReader(joined), io.Discard)
	if err != nil {
		t.Fatalf("run2: %v", err)
	}
	if res2.skippedResume != 2 || res2.enqueued != 1 || res2.succeeded != 1 {
		t.Fatalf("run2 counters = %+v, want skipped=2 enqueued=1 succeeded=1", res2)
	}
	if len(res2.allScored) != 3 {
		t.Errorf("run2 allScored = %d, want 3", len(res2.allScored))
	}

	rows2, _ := readScored(t, outPath)
	counts := map[string]int{}
	for _, r := range rows2 {
		counts[r.ID]++
	}
	if len(counts) != 3 || counts["a"] != 1 || counts["b"] != 1 || counts["c"] != 1 {
		t.Fatalf("out id counts = %v, want each of a,b,c exactly once", counts)
	}
}

func TestRunRetryAndErrorRows(t *testing.T) {
	stub := newStub(t)
	client := newClient(stub.URL, "", "stub-model")
	flakyText := "score=0.4 FLAKY needs one retry"
	deadText := "score=0.9 HTTPFAIL always down"
	lines := []string{
		recLine("ok", "s", "k", "score=0.1 clean"),
		recLine("flaky", "s", "k", flakyText),
		recLine("dead", "s", "k", deadText),
	}
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.jsonl")

	res, err := run(context.Background(),
		scanConfig{outPath: outPath, workers: 1, topN: 100},
		client, strings.NewReader(strings.Join(lines, "\n")+"\n"), io.Discard)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.succeeded != 2 || res.failed != 1 {
		t.Fatalf("counters = %+v, want succeeded=2 failed=1", res)
	}

	rows, errs := readScored(t, outPath)
	if len(rows) != 2 || len(errs) != 1 {
		t.Fatalf("rows=%d errs=%d, want 2/1", len(rows), len(errs))
	}
	if errs[0].ID != "dead" || errs[0].Error == "" {
		t.Errorf("error row = %+v, want id=dead with a non-empty error", errs[0])
	}
	if got := stub.hitCount(flakyText); got != 2 {
		t.Errorf("flaky upstream hits = %d, want 2 (fail once then succeed)", got)
	}
	if got := stub.hitCount(deadText); got != 2 {
		t.Errorf("dead upstream hits = %d, want 2 (initial + one retry)", got)
	}
	m := byID(rows)
	if _, ok := m["flaky"]; !ok {
		t.Errorf("flaky not scored despite retry recovery")
	}
}

func TestRunDupInputIds(t *testing.T) {
	stub := newStub(t)
	client := newClient(stub.URL, "", "stub-model")
	lines := []string{
		recLine("x", "s", "k", "score=0.5 first"),
		recLine("x", "s", "k", "score=0.9 dup same id"),
		recLine("y", "s", "k", "score=0.2 other"),
	}
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.jsonl")

	res, err := run(context.Background(),
		scanConfig{outPath: outPath, workers: 2, topN: 100},
		client, strings.NewReader(strings.Join(lines, "\n")+"\n"), io.Discard)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.input.valid != 3 || res.dupInput != 1 || res.enqueued != 2 || res.succeeded != 2 {
		t.Fatalf("counters = %+v, want valid=3 dup=1 enqueued=2 succeeded=2", res)
	}
	rows, _ := readScored(t, outPath)
	if len(rows) != 2 {
		t.Fatalf("out rows = %d, want 2 (x once, y once)", len(rows))
	}
}
