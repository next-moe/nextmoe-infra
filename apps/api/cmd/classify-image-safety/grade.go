package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"api/internal/infrastructure/database"

	"gorm.io/gorm"
)

type gradeOptions struct {
	DSN         string
	BaseURL     string
	Variant     string
	Limit       int
	Batch       int
	Concurrency int
	Apply       bool
	ReadyWait   time.Duration
	Tagger      *wdClient
}

type gradeLabels struct {
	Provider string             `json:"provider"`
	Model    string             `json:"model"`
	Level    int                `json:"level"`
	Answers  map[string]bool    `json:"answers,omitempty"`
	Scores   map[string]float64 `json:"scores,omitempty"`
	At       time.Time          `json:"at"`
}

const providerWD = "wd-tagger"

func runGrade(ctx context.Context, o gradeOptions, w io.Writer) error {
	if o.DSN == "" {
		return fmt.Errorf("--dsn is required")
	}
	if o.BaseURL == "" {
		return fmt.Errorf("--base-url is required")
	}
	if o.Tagger == nil || o.Tagger.base == "" {
		return fmt.Errorf("--tagger-url is required")
	}
	if o.Batch <= 0 {
		o.Batch = 2000
	}
	if o.Concurrency <= 0 {
		o.Concurrency = 1
	}

	model, err := o.Tagger.waitReady(ctx, o.ReadyWait)
	if err != nil {
		return err
	}

	db, err := database.OpenJob(o.DSN)
	if err != nil {
		return err
	}
	if sqlDB, err := db.DB(); err == nil {
		defer sqlDB.Close()
	}

	var pending int64
	if err := db.WithContext(ctx).
		Raw(`SELECT count(*) FROM images WHERE review_labels -> 'grade' IS NULL`).
		Scan(&pending).Error; err != nil {
		return fmt.Errorf("count pending: %w", err)
	}
	fmt.Fprintf(w, "pending=%d apply=%v concurrency=%d tagger=%s\n", pending, o.Apply, o.Concurrency, model)

	fetch := &http.Client{Timeout: 60 * time.Second}
	var (
		mu                   sync.Mutex
		ok, bad, undecodable int
		byLevel              [4]int
		started              = time.Now()
		undecodableSample    []string
	)
	work := make(chan imageRow)
	var wg sync.WaitGroup
	for i := 0; i < o.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := range work {
				level, err := gradeOne(ctx, db, o, model, fetch, r)
				mu.Lock()
				switch {
				case errors.Is(err, errUndecodable):
					undecodable++
					if len(undecodableSample) < 10 {
						undecodableSample = append(undecodableSample, r.Hash)
					}
				case err != nil:
					bad++
					if bad%20 == 1 {
						fmt.Fprintf(w, "error hash=%s: %v\n", r.Hash, err)
					}
				default:
					ok++
					byLevel[level]++
				}
				if n := ok + bad + undecodable; n%1000 == 0 {
					fmt.Fprintf(w, "progress done=%d errors=%d rate=%.2f img/s\n",
						ok, bad, float64(n)/time.Since(started).Seconds())
				}
				mu.Unlock()
			}
		}()
	}

	cursor := ""
	processed := 0
feed:
	for {
		var rows []imageRow
		if err := db.WithContext(ctx).
			Raw(`SELECT hash, ext, width, height, size_bytes, variants::text AS variants
			       FROM images
			      WHERE review_labels -> 'grade' IS NULL AND hash > ?
			      ORDER BY hash
			      LIMIT ?`, cursor, o.Batch).
			Scan(&rows).Error; err != nil {
			close(work)
			wg.Wait()
			return fmt.Errorf("page images: %w", err)
		}
		if len(rows) == 0 {
			break
		}
		cursor = rows[len(rows)-1].Hash
		for _, r := range rows {
			if o.Limit > 0 && processed >= o.Limit {
				break feed
			}
			select {
			case work <- r:
				processed++
			case <-ctx.Done():
				break feed
			}
		}
	}
	close(work)
	wg.Wait()

	fmt.Fprintf(w, "grade complete done=%d level0=%d level1=%d level2=%d level3=%d errors=%d undecodable=%d elapsed=%s\n",
		ok, byLevel[0], byLevel[1], byLevel[2], byLevel[3], bad, undecodable, time.Since(started).Truncate(time.Second))
	if undecodable > 0 {
		fmt.Fprintf(w, "undecodable images stay ungraded and are retried next run: %v\n", undecodableSample)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if bad > 0 {
		return fmt.Errorf("%d images failed to grade", bad)
	}
	return nil
}

func gradeOne(ctx context.Context, db *gorm.DB, o gradeOptions, model string, fetch *http.Client, r imageRow) (int, error) {
	body, err := fetchImage(ctx, fetch, publicURL(o.BaseURL, r, o.Variant))
	if err != nil {
		return 0, err
	}
	scores, err := o.Tagger.tag(ctx, body)
	if err != nil {
		return 0, err
	}
	level, err := wdLevel(scores)
	if err != nil {
		return 0, err
	}
	kept := make(map[string]float64, len(scores.Rating)+len(scores.Tags))
	for k, v := range scores.Rating {
		kept[k] = v
	}
	for k, v := range scores.Tags {
		kept[k] = v
	}
	raw, err := json.Marshal(gradeLabels{
		Provider: providerWD,
		Model:    model,
		Level:    level,
		Scores:   kept,
		At:       time.Now().UTC(),
	})
	if err != nil {
		return 0, err
	}
	if !o.Apply {
		return level, nil
	}
	return level, db.WithContext(ctx).Exec(
		`UPDATE images
		    SET review_labels = jsonb_set(coalesce(review_labels, '{}'::jsonb), '{grade}', ?::jsonb)
		  WHERE hash = ? AND review_labels -> 'grade' IS NULL`, string(raw), r.Hash).Error
}

func fetchImage(ctx context.Context, c *http.Client, url string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		resp, err := c.Do(req)
		if err != nil {
			lastErr = err
		} else {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 24<<20))
			resp.Body.Close()
			switch {
			case resp.StatusCode == http.StatusOK && readErr == nil:
				return body, nil
			case resp.StatusCode == http.StatusNotFound:
				return nil, fmt.Errorf("fetch %s: http 404", url)
			case readErr != nil:
				lastErr = readErr
			default:
				lastErr = fmt.Errorf("fetch %s: http %d", url, resp.StatusCode)
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(1<<attempt) * time.Second):
		}
	}
	return nil, lastErr
}
