package hihyou

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"api/internal/platform/news/model"
	"api/pkg/config"
)

type PollOpts struct {
	// Lookback is how long a weekly that has no rows yet is still looked for;
	// a rate-limited or quarantined issue fails the run for this long, not forever.
	Lookback time.Duration
	// Recheck is how long a stored weekly is still re-read, which is what gives
	// a picture that failed to upload another try.
	Recheck time.Duration
	// The first run from production (2026-09-29) lost 2 of 4 articles to -509
	// at a 30s gap, so a rate-limited article gets more passes after a cooldown,
	// the same recovery shape Harvest measured.
	Passes      int
	Gap         time.Duration
	Cooldown    time.Duration
	Concurrency int
}

func DefaultPollOpts() PollOpts {
	return PollOpts{
		Lookback: 28 * 24 * time.Hour, Recheck: 7 * 24 * time.Hour,
		Passes: 3, Gap: 30 * time.Second, Cooldown: 10 * time.Minute, Concurrency: 8,
	}
}

// Poll is the scheduled refresh: it reads the first page of the column index,
// imports the recent weeklies straight from bilibili without a corpus on disk,
// and ends with the standing release.
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
	var weeklies []IndexEntry
	for _, e := range entries {
		if _, ok := IssueNumber(e.Title); ok {
			weeklies = append(weeklies, e)
		}
	}
	stored, err := w.storedIssues(ctx, weeklies)
	if err != nil {
		return nil, err
	}

	var due []IndexEntry
	for _, e := range weeklies {
		window := opts.Lookback
		if stored[e.ID] {
			window = opts.Recheck
		}
		if e.PublishTime >= now.Add(-window).Unix() {
			due = append(due, e)
		}
	}
	sort.Slice(due, func(i, j int) bool {
		if stored[due[i].ID] != stored[due[j].ID] {
			return !stored[due[i].ID]
		}
		return due[i].PublishTime < due[j].PublishTime
	})

	st := &stats{}
	var issues []int
	var problems []string
	for pass := 1; pass <= max(opts.Passes, 1) && len(due) > 0; pass++ {
		if pass > 1 {
			if err := sleep(ctx, opts.Cooldown); err != nil {
				return nil, err
			}
			if err := client.Warm(ctx); err != nil {
				return nil, fmt.Errorf("bilibili warm-up: %w", err)
			}
		}
		var limited []IndexEntry
		for _, e := range due {
			if err := sleep(ctx, opts.Gap); err != nil {
				return nil, err
			}
			a, _, err := client.Article(ctx, e.ID)
			if errors.Is(err, ErrRateLimited) {
				limited = append(limited, e)
				continue
			}
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
		due = limited
	}
	for _, e := range due {
		problems = append(problems, fmt.Sprintf("cv%d: %v", e.ID, ErrRateLimited))
	}

	released, err := w.release(ctx, model.SystemActorUID)
	if err != nil {
		return nil, fmt.Errorf("standing release: %w", err)
	}

	sort.Ints(issues)
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

func (w *writer) storedIssues(ctx context.Context, weeklies []IndexEntry) (map[int64]bool, error) {
	out := map[int64]bool{}
	if len(weeklies) == 0 {
		return out, nil
	}
	byPrefix := make(map[string]int64, len(weeklies))
	prefixes := make([]string, 0, len(weeklies))
	for _, e := range weeklies {
		p := fmt.Sprintf("cv%d", e.ID)
		byPrefix[p] = e.ID
		prefixes = append(prefixes, p)
	}
	var have []string
	if err := w.db.WithContext(ctx).Model(&model.NewsItem{}).
		Where("source_key = ? AND split_part(external_id, '#', 1) IN ?", model.SourceKeyHihyou, prefixes).
		Distinct().Pluck("split_part(external_id, '#', 1)", &have).Error; err != nil {
		return nil, err
	}
	for _, p := range have {
		out[byPrefix[p]] = true
	}
	return out, nil
}
