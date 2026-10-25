package main

import (
	"api/internal/platform/catalog/walkthrough/extract"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"api/internal/platform/catalog/walkthrough"

	"gorm.io/gorm"
)

type extractOpts struct {
	Workers   int
	MaxTokens int
	Redo      bool
}

func runExtract(ctx context.Context, src *gorm.DB, l *llm, entries []sourceEntry, o extractOpts) error {
	done, err := settledExtractions(ctx, src)
	if err != nil {
		return err
	}
	var todo []sourceEntry
	for _, e := range entries {
		if o.Redo || done[e.ID] != hashText(chartText(e.BodyHTML)) {
			todo = append(todo, e)
		}
	}
	fmt.Printf("\n=== import-walkthroughs extract model=%s prompt=%s candidates=%d todo=%d ===\n",
		l.model, extract.PromptVersion, len(entries), len(todo))

	schema := mustSchema(extract.SchemaJSON)
	counts := map[string]*atomic.Int64{}
	for _, v := range []string{extract.VerdictOK, extract.VerdictNoChart, extract.VerdictReview, extract.VerdictRejected, extract.VerdictError} {
		counts[v] = &atomic.Int64{}
	}
	var finished atomic.Int64
	sem := make(chan struct{}, max(o.Workers, 1))
	var wg sync.WaitGroup
	for _, e := range todo {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(e sourceEntry) {
			defer wg.Done()
			defer func() { <-sem }()
			verdict, err := extractOne(ctx, src, l, schema, e, o.MaxTokens)
			if err != nil {
				slog.Warn("extract failed", "entry", e.ID, "title", e.Title, "err", err)
			}
			counts[verdict].Add(1)
			if n := finished.Add(1); n%25 == 0 || n == int64(len(todo)) {
				slog.Info("progress", "done", n, "of", len(todo), "ok", counts[extract.VerdictOK].Load(),
					"review", counts[extract.VerdictReview].Load(), "rejected", counts[extract.VerdictRejected].Load(),
					"no_chart", counts[extract.VerdictNoChart].Load(), "error", counts[extract.VerdictError].Load())
			}
		}(e)
	}
	wg.Wait()
	fmt.Printf("ok=%d review=%d rejected=%d no_chart=%d error=%d\n", counts[extract.VerdictOK].Load(), counts[extract.VerdictReview].Load(),
		counts[extract.VerdictRejected].Load(), counts[extract.VerdictNoChart].Load(), counts[extract.VerdictError].Load())
	return ctx.Err()
}

func settledExtractions(ctx context.Context, src *gorm.DB) (map[int64]string, error) {
	var rows []struct {
		EntryID   int64
		InputHash string
	}
	err := src.WithContext(ctx).Table("wt_stage.extraction").
		Where("prompt_version = ? AND verdict IN ?", extract.PromptVersion, []string{extract.VerdictOK, extract.VerdictNoChart, extract.VerdictReview}).
		Scan(&rows).Error
	out := make(map[int64]string, len(rows))
	for _, r := range rows {
		out[r.EntryID] = r.InputHash
	}
	return out, err
}

func extractOne(ctx context.Context, src *gorm.DB, l *llm, schema map[string]any, e sourceEntry, maxTokens int) (string, error) {
	text := chartText(e.BodyHTML)
	start := time.Now()
	var got extract.Extracted
	res, err := l.chatJSON(ctx, extract.System, text, "walkthrough", schema, maxTokens, &got)
	m := extract.Metrics{Seconds: time.Since(start).Seconds(), CompletionTokens: res.CompletionTokens}
	row := extractionRow{EntryID: e.ID, Model: l.model, InputHash: hashText(text)}
	if err != nil {
		row.Verdict, row.Error = extract.VerdictError, err.Error()
		return row.Verdict, storeExtraction(ctx, src, row, m)
	}
	row.Raw = res.Content
	row.Verdict, row.Chart = extract.Settle(text, got, &m)
	return row.Verdict, storeExtraction(ctx, src, row, m)
}

type extractionRow struct {
	EntryID   int64
	Model     string
	InputHash string
	Verdict   string
	Chart     *walkthrough.Chart
	Raw       string
	Error     string
}

func storeExtraction(ctx context.Context, src *gorm.DB, r extractionRow, m extract.Metrics) error {
	var chartJSON, rawJSON any
	if r.Chart != nil {
		b, err := json.Marshal(r.Chart)
		if err != nil {
			return err
		}
		chartJSON = string(b)
	}
	if r.Raw != "" {
		rawJSON = r.Raw
	}
	mj, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return src.WithContext(ctx).Exec(`
INSERT INTO wt_stage.extraction (entry_id, prompt_version, model, input_hash, verdict, chart, raw, metrics, error, created_at)
VALUES (?, ?, ?, ?, ?, ?::jsonb, ?::jsonb, ?::jsonb, ?, now())
ON CONFLICT (entry_id, prompt_version) DO UPDATE SET
  model = EXCLUDED.model, input_hash = EXCLUDED.input_hash, verdict = EXCLUDED.verdict,
  chart = EXCLUDED.chart, raw = EXCLUDED.raw, metrics = EXCLUDED.metrics, error = EXCLUDED.error, created_at = now()`,
		r.EntryID, extract.PromptVersion, r.Model, r.InputHash, r.Verdict, chartJSON, rawJSON, string(mj), r.Error).Error
}

func runRegate(ctx context.Context, src *gorm.DB, entries []sourceEntry) error {
	byID := make(map[int64]sourceEntry, len(entries))
	for _, e := range entries {
		byID[e.ID] = e
	}
	var rows []struct {
		EntryID   int64
		Model     string
		InputHash string
		Raw       string
		Metrics   string
	}
	if err := src.WithContext(ctx).Table("wt_stage.extraction").
		Select("entry_id, model, input_hash, raw::text AS raw, metrics::text AS metrics").
		Where("prompt_version = ? AND raw IS NOT NULL", extract.PromptVersion).Scan(&rows).Error; err != nil {
		return err
	}
	counts := map[string]int{}
	for _, r := range rows {
		e, ok := byID[r.EntryID]
		if !ok {
			continue
		}
		text := chartText(e.BodyHTML)
		if hashText(text) != r.InputHash {
			continue
		}
		var got extract.Extracted
		if err := json.Unmarshal([]byte(r.Raw), &got); err != nil {
			return fmt.Errorf("entry %d raw: %w", r.EntryID, err)
		}
		var prev extract.Metrics
		_ = json.Unmarshal([]byte(r.Metrics), &prev)
		m := extract.Metrics{Seconds: prev.Seconds, CompletionTokens: prev.CompletionTokens}
		row := extractionRow{EntryID: r.EntryID, Model: r.Model, InputHash: r.InputHash, Raw: r.Raw}
		row.Verdict, row.Chart = extract.Settle(text, got, &m)
		if err := storeExtraction(ctx, src, row, m); err != nil {
			return err
		}
		counts[row.Verdict]++
	}
	fmt.Printf("regated=%d ok=%d review=%d rejected=%d no_chart=%d\n", counts[extract.VerdictOK]+counts[extract.VerdictReview]+counts[extract.VerdictRejected]+counts[extract.VerdictNoChart], counts[extract.VerdictOK], counts[extract.VerdictReview], counts[extract.VerdictRejected], counts[extract.VerdictNoChart])
	return nil
}
