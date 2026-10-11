package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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

func TestRunGradeWritesTaggerGradesToUngradedImagesOnly(t *testing.T) {
	db := imagesTestDB(t)
	ctx := context.Background()
	safe := seedImage(t, db, "safe", "")
	explicit := seedImage(t, db, "explicit", "")
	undecodable := seedImage(t, db, "undecodable", "")
	human := seedImage(t, db, "human", `{"grade":{"provider":"human","model":"","level":1,"at":"2026-10-11T00:00:00Z"}}`)

	images := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimLeft(strings.TrimSuffix(r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:], ".webp"), "0")
		_, _ = w.Write([]byte(name))
	}))
	defer images.Close()
	const model = "SmilingWolf/wd-vit-tagger-v3@test"
	tagger := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			_, _ = w.Write([]byte(`{"model":"` + model + `"}`))
			return
		}
		body, _ := io.ReadAll(r.Body)
		switch string(body) {
		case "safe":
			_ = json.NewEncoder(w).Encode(scoresOf([4]float64{0.9, 0.1, 0, 0}, nil))
		case "explicit":
			_ = json.NewEncoder(w).Encode(scoresOf([4]float64{0, 0.1, 0.2, 0.7}, map[string]float64{"sex": 0.9}))
		case "undecodable":
			w.WriteHeader(http.StatusUnprocessableEntity)
		default:
			t.Errorf("the tagger was asked about %q", body)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer tagger.Close()

	opts := gradeOptions{DSN: os.Getenv("TEST_IMAGES_DSN"), BaseURL: images.URL, Concurrency: 2, ReadyWait: 5 * time.Second, Tagger: newWDClient(tagger.URL)}
	if err := runGrade(ctx, opts, io.Discard); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if _, ok := labelsOf(t, db, safe)["grade"]; ok {
		t.Fatal("a dry run wrote a grade")
	}

	opts.Apply = true
	if err := runGrade(ctx, opts, io.Discard); err != nil {
		t.Fatalf("apply: %v", err)
	}
	for hash, want := range map[string]int{safe: 0, explicit: 3} {
		g := labelsOf(t, db, hash)["grade"]
		if g.Provider != providerWD || g.Model != model || g.Level != want || len(g.Scores) != 20 {
			t.Errorf("%s: grade %+v, want level %d from %s", hash, g, want, providerWD)
		}
	}
	if _, ok := labelsOf(t, db, undecodable)["grade"]; ok {
		t.Error("an image the tagger could not decode got a grade")
	}
	if g := labelsOf(t, db, human)["grade"]; g.Provider != providerHuman || g.Level != 1 {
		t.Errorf("a human grade was touched: %+v", g)
	}
}

func TestRunGradeFailsWhenTheTaggerNeverComesUp(t *testing.T) {
	err := runGrade(context.Background(), gradeOptions{
		DSN: "unused", BaseURL: "http://unused", ReadyWait: time.Millisecond,
		Tagger: newWDClient("http://127.0.0.1:1"),
	}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "not ready") {
		t.Fatalf("err = %v", err)
	}
}
