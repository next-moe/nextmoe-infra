package hihyou

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"api/internal/platform/news/model"
	"api/pkg/config"
)

type PollOpts struct {
	// Lookback is how far back an issue is still re-read. Rows that already
	// exist come out unchanged, but a picture that failed to upload last time
	// gets another try, and an issue that was rate-limited or quarantined keeps
	// failing the run for this long instead of forever.
	Lookback    time.Duration
	Gap         time.Duration
	Concurrency int
}

func DefaultPollOpts() PollOpts {
	return PollOpts{Lookback: 28 * 24 * time.Hour, Gap: 30 * time.Second, Concurrency: 8}
}

// Poll is the scheduled refresh: it reads the first page of the column index,
// imports every weekly published inside the lookback straight from bilibili
// without a corpus on disk, and ends with the standing release.
func Poll(ctx context.Context, cfg *config.Config, opts PollOpts) (map[string]any, error) {
	db, closeDB, err := openNewsDB(cfg, "")
	if err != nil {
		return nil, err
	}
	defer closeDB()
	w := newWriter(cfg, db, Opts{Apply: true, Concurrency: opts.Concurrency})
	if err := w.seedSource(ctx); err != nil {
		return nil, err
	}
	client, err := NewClient()
	if err != nil {
		return nil, err
	}
	return w.poll(ctx, client, opts, time.Now())
}

func (w *writer) poll(ctx context.Context, client *Client, opts PollOpts, now time.Time) (map[string]any, error) {
	if err := client.Warm(ctx); err != nil {
		return nil, fmt.Errorf("bilibili warm-up: %w", err)
	}
	entries, _, _, err := client.Index(ctx, 1, 30)
	if err != nil {
		return nil, fmt.Errorf("column index: %w", err)
	}

	cutoff := now.Add(-opts.Lookback).Unix()
	var due []IndexEntry
	for _, e := range entries {
		if _, ok := IssueNumber(e.Title); ok && e.PublishTime >= cutoff {
			due = append(due, e)
		}
	}
	sort.Slice(due, func(i, j int) bool { return due[i].PublishTime < due[j].PublishTime })

	st := &stats{}
	var issues []int
	var problems []string
	for _, e := range due {
		if err := sleep(ctx, opts.Gap); err != nil {
			return nil, err
		}
		a, _, err := client.Article(ctx, e.ID)
		if err != nil {
			problems = append(problems, fmt.Sprintf("cv%d: %v", e.ID, err))
			continue
		}
		seg := Segment(a)
		issues = append(issues, seg.IssueNo)
		if fail := Gate(seg); len(fail) > 0 {
			problems = append(problems, fmt.Sprintf("issue %d quarantined: %s", seg.IssueNo, strings.Join(fail, "; ")))
			continue
		}
		if err := w.applyIssue(ctx, seg, time.Unix(a.Data.PublishTime, 0).UTC(), st); err != nil {
			return nil, err
		}
	}

	released, err := w.release(ctx, model.SystemActorUID)
	if err != nil {
		return nil, fmt.Errorf("standing release: %w", err)
	}

	sum := map[string]any{
		"issues":          issues,
		"created":         st.created,
		"updated":         st.updated,
		"unchanged":       st.unchanged,
		"dropped_no_body": st.droppedEmpty,
		"truncated":       st.truncated,
		"images_uploaded": st.imagesUp,
		"images_failed":   st.imagesFail,
		"released":        released,
		"rate_limited":    client.RateLimitedCount(),
	}
	if len(problems) > 0 {
		slog.Warn("hihyou poll: issues left for the next run", "summary", sum, "problems", problems)
		return sum, fmt.Errorf("%d issue(s) not imported: %s", len(problems), strings.Join(problems, " | "))
	}
	return sum, nil
}
