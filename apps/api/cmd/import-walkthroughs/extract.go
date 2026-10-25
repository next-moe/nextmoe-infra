package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"api/internal/platform/catalog/walkthrough"

	"gorm.io/gorm"
)

const (
	verdictOK       = "ok"
	verdictNoChart  = "no_chart"
	verdictReview   = "review"
	verdictRejected = "rejected"
	verdictError    = "error"
)

type extractMetrics struct {
	Seconds           float64  `json:"seconds"`
	CompletionTokens  int      `json:"completion_tokens"`
	Coverage          float64  `json:"coverage"`
	Missed            []string `json:"missed"`
	EndMarkers        int      `json:"end_markers"`
	EndingsReached    int      `json:"endings_reached"`
	NotVerbatim       []string `json:"not_verbatim"`
	DroppedConditions []string `json:"dropped_conditions"`
	Problems          []string `json:"problems"`
}

var (
	endMarker   = regexp.MustCompile(`(END|ＥＮＤ|エンド)\s*[0-9０-９]*$`)
	chartStart  = regexp.MustCompile(`(START|【セーブ|◆セーブ|はじめから)`)
	markerLine  = regexp.MustCompile(`(セーブ|START|RESTART|から開始|END|ＥＮＤ|回収|終了|━)`)
	whitespaces = regexp.MustCompile(`\s+`)
)

func squash(s string) string { return whitespaces.ReplaceAllString(s, "") }

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
		l.model, extractPromptVersion, len(entries), len(todo))

	schema := mustSchema(extractSchemaJSON)
	counts := map[string]*atomic.Int64{}
	for _, v := range []string{verdictOK, verdictNoChart, verdictReview, verdictRejected, verdictError} {
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
				slog.Info("progress", "done", n, "of", len(todo), "ok", counts[verdictOK].Load(),
					"review", counts[verdictReview].Load(), "rejected", counts[verdictRejected].Load(),
					"no_chart", counts[verdictNoChart].Load(), "error", counts[verdictError].Load())
			}
		}(e)
	}
	wg.Wait()
	fmt.Printf("ok=%d review=%d rejected=%d no_chart=%d error=%d\n", counts[verdictOK].Load(), counts[verdictReview].Load(),
		counts[verdictRejected].Load(), counts[verdictNoChart].Load(), counts[verdictError].Load())
	return ctx.Err()
}

func settledExtractions(ctx context.Context, src *gorm.DB) (map[int64]string, error) {
	var rows []struct {
		EntryID   int64
		InputHash string
	}
	err := src.WithContext(ctx).Table("wt_stage.extraction").
		Where("prompt_version = ? AND verdict IN ?", extractPromptVersion, []string{verdictOK, verdictNoChart, verdictReview}).
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
	var got extracted
	res, err := l.chatJSON(ctx, extractSystem, text, "walkthrough", schema, maxTokens, &got)
	m := extractMetrics{Seconds: time.Since(start).Seconds(), CompletionTokens: res.CompletionTokens}
	row := extractionRow{EntryID: e.ID, Model: l.model, InputHash: hashText(text)}
	if err != nil {
		row.Verdict, row.Error = verdictError, err.Error()
		return row.Verdict, storeExtraction(ctx, src, row, m)
	}
	row.Raw = res.Content
	row.Verdict, row.Chart = settle(text, got, &m)
	return row.Verdict, storeExtraction(ctx, src, row, m)
}

func settle(text string, got extracted, m *extractMetrics) (string, *walkthrough.Chart) {
	if got.NoChart {
		return verdictNoChart, nil
	}
	chart := got.chart()
	walkthrough.Normalize(&chart)
	m.DroppedConditions = dropUnquotedConditions(text, &chart)
	quoteCharacterNames(text, &chart)
	return gate(text, chart, m), &chart
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

func gate(text string, c walkthrough.Chart, m *extractMetrics) string {
	m.Problems = walkthrough.Validate(c)
	m.NotVerbatim = notVerbatim(text, c)
	m.Coverage, m.Missed = coverage(text, c)
	m.EndMarkers = countEndMarkers(text)
	for _, s := range c.Segments {
		if s.To.NodeType == walkthrough.NodeEnding {
			m.EndingsReached++
		}
	}
	switch {
	case len(m.Problems) > 0 || len(m.NotVerbatim) > 0:
		return verdictRejected
	case m.Coverage < 0.9 || m.EndingsReached < m.EndMarkers:
		return verdictReview
	default:
		return verdictOK
	}
}

func notVerbatim(text string, c walkthrough.Chart) []string {
	src := squash(text)
	var bad []string
	check := func(s string) {
		if t := squash(s); t != "" && !strings.Contains(src, t) {
			bad = append(bad, s)
		}
	}
	for _, s := range c.Segments {
		for _, st := range s.Steps {
			if st.Text != nil {
				check(*st.Text)
			}
			for _, a := range st.Alt {
				check(a)
			}
		}
	}
	for _, e := range c.Endings {
		check(e.Name)
	}
	for _, s := range c.Sections {
		check(s.Title)
	}
	for _, ch := range c.Characters {
		check(ch.Name)
	}
	return bad
}

func dropUnquotedConditions(text string, c *walkthrough.Chart) []string {
	src := squash(text)
	var dropped []string
	for i := range c.Segments {
		if cond := c.Segments[i].Condition; cond != nil && !strings.Contains(src, squash(*cond)) {
			dropped = append(dropped, *cond)
			c.Segments[i].Condition = nil
		}
	}
	return dropped
}

func quoteCharacterNames(text string, c *walkthrough.Chart) {
	src := squash(text)
	for i := range c.Characters {
		ch := &c.Characters[i]
		if strings.Contains(src, squash(ch.Name)) {
			continue
		}
		for j, a := range ch.Aliases {
			if strings.Contains(src, squash(a)) {
				ch.Aliases[j] = ch.Name
				ch.Name = a
				break
			}
		}
	}
}

func chartRegion(text string) []string {
	lines := strings.Split(text, "\n")
	first, last := -1, -1
	for i, l := range lines {
		if first < 0 && chartStart.MatchString(l) {
			first = i
		}
		if endMarker.MatchString(strings.TrimSpace(l)) {
			last = i
		}
	}
	if first < 0 || last < first {
		return nil
	}
	return lines[first : last+1]
}

func countEndMarkers(text string) int {
	n := 0
	for _, l := range chartRegion(text) {
		if endMarker.MatchString(strings.TrimSpace(l)) {
			n++
		}
	}
	return n
}

func coverage(text string, c walkthrough.Chart) (float64, []string) {
	var known []string
	add := func(s string) {
		if t := squash(s); t != "" {
			known = append(known, t)
		}
	}
	for _, s := range c.Segments {
		for _, st := range s.Steps {
			if st.Text != nil {
				add(*st.Text)
			}
			for _, a := range st.Alt {
				add(a)
			}
		}
		if s.Condition != nil {
			add(*s.Condition)
		}
	}
	for _, s := range c.Sections {
		add(s.Title)
	}
	for _, ch := range c.Characters {
		add(ch.Name)
	}
	for _, e := range c.Endings {
		add(e.Name)
	}
	for _, e := range c.Entries {
		add(e.Label)
	}
	var total int
	var missed []string
	for _, raw := range chartRegion(text) {
		l := strings.TrimSpace(raw)
		if l == "" || markerLine.MatchString(l) || strings.Contains(l, "│") || strings.HasPrefix(l, "※") ||
			strings.HasPrefix(l, "■") || strings.HasPrefix(l, "[") {
			continue
		}
		total++
		sl := squash(l)
		hit := false
		for _, k := range known {
			if strings.Contains(sl, k) || strings.Contains(k, sl) {
				hit = true
				break
			}
		}
		if !hit {
			missed = append(missed, l)
		}
	}
	if total == 0 {
		return 1, nil
	}
	if len(missed) > 8 {
		missed = missed[:8]
	}
	return 1 - float64(len(missed))/float64(total), missed
}

func storeExtraction(ctx context.Context, src *gorm.DB, r extractionRow, m extractMetrics) error {
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
		r.EntryID, extractPromptVersion, r.Model, r.InputHash, r.Verdict, chartJSON, rawJSON, string(mj), r.Error).Error
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
		Where("prompt_version = ? AND raw IS NOT NULL", extractPromptVersion).Scan(&rows).Error; err != nil {
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
		var got extracted
		if err := json.Unmarshal([]byte(r.Raw), &got); err != nil {
			return fmt.Errorf("entry %d raw: %w", r.EntryID, err)
		}
		var prev extractMetrics
		_ = json.Unmarshal([]byte(r.Metrics), &prev)
		m := extractMetrics{Seconds: prev.Seconds, CompletionTokens: prev.CompletionTokens}
		row := extractionRow{EntryID: r.EntryID, Model: r.Model, InputHash: r.InputHash, Raw: r.Raw}
		row.Verdict, row.Chart = settle(text, got, &m)
		if err := storeExtraction(ctx, src, row, m); err != nil {
			return err
		}
		counts[row.Verdict]++
	}
	fmt.Printf("regated=%d ok=%d review=%d rejected=%d no_chart=%d\n", counts[verdictOK]+counts[verdictReview]+counts[verdictRejected]+counts[verdictNoChart], counts[verdictOK], counts[verdictReview], counts[verdictRejected], counts[verdictNoChart])
	return nil
}
