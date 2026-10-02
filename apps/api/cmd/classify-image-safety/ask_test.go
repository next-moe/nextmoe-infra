package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeWorkersAI answers the question per image: the test images carry their
// own name as their bytes, so the data URI says which image was asked about.
func fakeWorkersAI(t *testing.T, answer func(image string) (status int, body string)) (*moondreamClient, *[]string) {
	t.Helper()
	var (
		mu    sync.Mutex
		asked []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Image string `json:"image"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		name := decodeDataURI(t, req.Image)
		mu.Lock()
		asked = append(asked, name)
		mu.Unlock()
		status, body := answer(name)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	c := newMoondreamClient("acct", "token", "model")
	c.apiRoot = srv.URL
	return c, &asked
}

func sse(answer string) string {
	return fmt.Sprintf("data: {\"output\":[{\"answer\":%q}],\"usage\":{\"neurons\":41}}\n\ndata: [DONE]\n\n", answer)
}

func decodeDataURI(t *testing.T, uri string) string {
	t.Helper()
	_, b64, ok := strings.Cut(uri, ";base64,")
	if !ok {
		t.Fatalf("not a data URI: %q", uri)
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("decode data URI: %v", err)
	}
	return string(raw)
}

func imageHost(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSuffix(filepath.Base(r.URL.Path), ".webp")
		_, _ = io.WriteString(w, name)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func askFiles(t *testing.T, hashes ...string) (in, out string) {
	t.Helper()
	dir := t.TempDir()
	in, out = filepath.Join(dir, "in.jsonl"), filepath.Join(dir, "out.jsonl")
	var b bytes.Buffer
	for _, h := range hashes {
		fmt.Fprintf(&b, "{\"hash\":%q,\"ext\":\"webp\"}\n", h)
	}
	if err := os.WriteFile(in, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return in, out
}

func readAnswers(t *testing.T, path string) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		var rec askRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("answer line %q: %v", line, err)
		}
		got[rec.Hash] = rec.Answer
	}
	return got
}

func TestRunAsk_RecordsEachAnswerAndResumesWithoutAskingTwice(t *testing.T) {
	in, out := askFiles(t, "aaaa1", "bbbb2", "cccc3")
	client, asked := fakeWorkersAI(t, func(image string) (int, string) {
		if image == "bbbb2" {
			return http.StatusOK, sse("Yes.")
		}
		return http.StatusOK, sse("no")
	})
	o := askOptions{In: in, Out: out, Question: "mosaic?", BaseURL: imageHost(t), Concurrency: 2, Client: client}
	if err := runAsk(context.Background(), o, io.Discard); err != nil {
		t.Fatalf("runAsk: %v", err)
	}
	got := readAnswers(t, out)
	if len(got) != 3 || got["aaaa1"] || !got["bbbb2"] || got["cccc3"] {
		t.Fatalf("answers = %v, want only bbbb2 yes", got)
	}

	*asked = nil
	if err := runAsk(context.Background(), o, io.Discard); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if len(*asked) != 0 {
		t.Fatalf("the second run asked %v again", *asked)
	}
}

func TestRunAsk_AUsedUpAllocationStopsTheRunAndWritesNoAnswerForIt(t *testing.T) {
	in, out := askFiles(t, "aaaa1", "bbbb2")
	client, _ := fakeWorkersAI(t, func(image string) (int, string) {
		if image == "bbbb2" {
			return http.StatusTooManyRequests, `{"errors":[{"message":"you have used up your daily free allocation"}]}`
		}
		return http.StatusOK, sse("no")
	})
	o := askOptions{In: in, Out: out, Question: "mosaic?", BaseURL: imageHost(t), Concurrency: 1, Client: client}
	err := runAsk(context.Background(), o, io.Discard)
	if !errors.Is(err, errDailyQuota) {
		t.Fatalf("err = %v, want the daily allocation error", err)
	}
	if got := readAnswers(t, out); len(got) != 1 || got["aaaa1"] {
		t.Fatalf("answers = %v, want only aaaa1 = no; an unanswered image must stay unanswered", got)
	}
}
