package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func scoresOf(rating [4]float64, tags map[string]float64) wdScores {
	s := wdScores{Rating: map[string]float64{}, Tags: map[string]float64{}}
	for i, name := range wdRatingLevels {
		s.Rating[name] = rating[i]
	}
	for _, family := range [][]string{wdActTags, wdNudeTags, wdCensorTags} {
		for _, name := range family {
			s.Tags[name] = 0
		}
	}
	for k, v := range tags {
		s.Tags[k] = v
	}
	return s
}

func TestWDLevel(t *testing.T) {
	cases := []struct {
		name   string
		rating [4]float64
		tags   map[string]float64
		want   int
	}{
		{"title screen", [4]float64{0.93, 0.06, 0.01, 0}, nil, 0},
		{"swimsuit", [4]float64{0.10, 0.80, 0.09, 0.01}, map[string]float64{"bikini": 0.9}, 1},
		{"lewd without a nudity tag", [4]float64{0.01, 0.3, 0.65, 0.04}, nil, 2},
		{"explicit rating", [4]float64{0, 0.03, 0.09, 0.88}, nil, 3},
		// A 2026-10-02 hand regrade (level 3) that the rating alone calls sensitive.
		{"mosaic over a rating of sensitive", [4]float64{0.0718, 0.5948, 0.1247, 0.0268},
			map[string]float64{"mosaic_censoring": 0.8896}, 3},
		{"nipples lift a general rating to nude", [4]float64{0.6, 0.3, 0.08, 0.02}, map[string]float64{"nipples": 0.5}, 2},
		{"nipples never lower an explicit rating", [4]float64{0, 0, 0.2, 0.8}, map[string]float64{"nipples": 0.9}, 3},
		{"act tag", [4]float64{0.1, 0.6, 0.25, 0.05}, map[string]float64{"penis": 0.4}, 3},
		{"tags under their cut-offs", [4]float64{0.1, 0.8, 0.08, 0.02},
			map[string]float64{"nipples": 0.34, "sex": 0.34, "mosaic_censoring": 0.49}, 1},
	}
	for _, c := range cases {
		got, err := wdLevel(scoresOf(c.rating, c.tags))
		if err != nil || got != c.want {
			t.Errorf("%s: level %d, %v; want %d", c.name, got, err, c.want)
		}
	}

	incomplete := scoresOf([4]float64{1, 0, 0, 0}, nil)
	delete(incomplete.Tags, "mosaic_censoring")
	if _, err := wdLevel(incomplete); err == nil {
		t.Error("a response without a tag the rule reads must not grade")
	}
}

func TestWDClientRetriesServerErrorsOnly(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		switch string(body) {
		case "flaky":
			if n == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			_ = json.NewEncoder(w).Encode(scoresOf([4]float64{1, 0, 0, 0}, nil))
		case "broken":
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"error":"undecodable image"}`))
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()
	c := newWDClient(srv.URL)
	ctx := context.Background()

	if _, err := c.tag(ctx, []byte("flaky")); err != nil || calls.Load() != 2 {
		t.Fatalf("flaky: %v after %d calls", err, calls.Load())
	}
	calls.Store(0)
	if _, err := c.tag(ctx, []byte("broken")); !errors.Is(err, errUndecodable) || calls.Load() != 1 {
		t.Fatalf("broken: %v after %d calls", err, calls.Load())
	}
	calls.Store(0)
	if _, err := c.tag(ctx, []byte("bad")); err == nil || calls.Load() != 1 {
		t.Fatalf("bad request: %v after %d calls", err, calls.Load())
	}
}

func fakeTagger(t *testing.T, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			_, _ = w.Write([]byte(`{"model":"wd@test"}`))
			return
		}
		calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		s := scoresOf([4]float64{0.9, 0.1, 0, 0}, nil)
		switch string(body) {
		case "explicit":
			s = scoresOf([4]float64{0, 0.1, 0.2, 0.7}, map[string]float64{"sex": 0.9})
		case "undecodable":
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		s.Model = "wd@test"
		_ = json.NewEncoder(w).Encode(s)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func nameServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		file := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		_, _ = w.Write([]byte(strings.TrimLeft(strings.TrimSuffix(file, ".webp"), "0")))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func padHash(name string) string { return strings.Repeat("0", 64-len(name)) + name }

func TestTagWritesScoresAndResumesWhereItStopped(t *testing.T) {
	dir := t.TempDir()
	in, out := filepath.Join(dir, "pending.csv"), filepath.Join(dir, "scores.jsonl")
	if err := os.WriteFile(in, []byte(padHash("safe")+",webp\n"+padHash("explicit")+",webp\n"+padHash("undecodable")+",webp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	o := tagOptions{In: in, Out: out, BaseURL: nameServer(t).URL, Concurrency: 2, ReadyWait: 5 * time.Second,
		Tagger: newWDClient(fakeTagger(t, &calls).URL)}
	if err := runTag(context.Background(), o, io.Discard); err != nil {
		t.Fatalf("tag: %v", err)
	}
	recs, err := readTagRecords(out)
	if err != nil || len(recs) != 2 || calls.Load() != 3 {
		t.Fatalf("first run: %d records, %v, %d tagger calls", len(recs), err, calls.Load())
	}
	for _, r := range recs {
		if r.Model != "wd@test" || len(r.Rating) != 4 {
			t.Errorf("record %+v", r)
		}
	}

	calls.Store(0)
	if err := runTag(context.Background(), o, io.Discard); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if recs, _ := readTagRecords(out); len(recs) != 2 || calls.Load() != 1 {
		t.Fatalf("a rerun re-tagged finished images: %d records, %d calls (only the undecodable one should be retried)", len(recs), calls.Load())
	}
}

func TestApplyGradesOnlyWhatItMayReplace(t *testing.T) {
	db := imagesTestDB(t)
	ctx := context.Background()
	pending := seedImage(t, db, "pending", "")
	moondream := seedImage(t, db, "moondream", machineSafe)
	human := seedImage(t, db, "human", `{"grade":{"provider":"human","model":"","level":1,"at":"2026-10-11T00:00:00Z"}}`)
	already := seedImage(t, db, "already", `{"grade":{"provider":"wd-tagger","model":"wd@old","level":0,"at":"2026-10-11T00:00:00Z"}}`)

	explicit := scoresOf([4]float64{0, 0.1, 0.2, 0.7}, map[string]float64{"sex": 0.9})
	explicit.Model = "wd@test"
	var lines []string
	for _, h := range []string{pending, moondream, human, already, padHash("gone")} {
		b, _ := json.Marshal(tagRecord{Hash: h, wdScores: explicit})
		lines = append(lines, string(b))
	}
	in := filepath.Join(t.TempDir(), "scores.jsonl")
	if err := os.WriteFile(in, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	recs, err := readTagRecords(in)
	if err != nil {
		t.Fatal(err)
	}

	st, err := applyGrades(ctx, db, recs, applyOptions{})
	if err != nil || st.written != 1 || st.missing != 1 || st.keptHuman != 1 || st.keptWD != 1 || st.keptMachine != 1 {
		t.Fatalf("dry run: %+v, %v", st, err)
	}
	if _, ok := labelsOf(t, db, pending)["grade"]; ok {
		t.Fatal("a dry run wrote a grade")
	}

	if _, err := applyGrades(ctx, db, recs, applyOptions{Apply: true}); err != nil {
		t.Fatal(err)
	}
	if g := labelsOf(t, db, pending)["grade"]; g.Provider != providerWD || g.Model != "wd@test" || g.Level != 3 || len(g.Scores) != 20 {
		t.Fatalf("pending: %+v", g)
	}
	if g := labelsOf(t, db, moondream)["grade"]; g.Provider != providerMoondream {
		t.Fatalf("a moondream grade was replaced without -replace-machine: %+v", g)
	}

	st, err = applyGrades(ctx, db, recs, applyOptions{Apply: true, ReplaceMachine: true})
	if err != nil || st.written != 1 || st.hidden != 1 {
		t.Fatalf("replace: %+v, %v", st, err)
	}
	if g := labelsOf(t, db, moondream)["grade"]; g.Provider != providerWD || g.Level != 3 {
		t.Fatalf("moondream grade not replaced: %+v", g)
	}
	var kept struct {
		Provider string `gorm:"column:provider"`
		Reason   string `gorm:"column:reason"`
	}
	if err := db.Raw(`SELECT review_labels->'grade_moondream'->>'provider' AS provider, review_labels->>'manual_reason' AS reason
	                    FROM images WHERE hash = ?`, moondream).Scan(&kept).Error; err != nil || kept.Provider != providerMoondream || kept.Reason != "kept" {
		t.Fatalf("the moondream grade or another key was lost: %+v, %v", kept, err)
	}
	if g := labelsOf(t, db, human)["grade"]; g.Provider != providerHuman || g.Level != 1 {
		t.Fatalf("a human grade was touched: %+v", g)
	}
	if g := labelsOf(t, db, already)["grade"]; g.Model != "wd@old" {
		t.Fatalf("an existing tagger grade was overwritten: %+v", g)
	}
}
