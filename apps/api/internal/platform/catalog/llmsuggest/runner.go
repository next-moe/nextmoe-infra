package llmsuggest

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"gorm.io/gorm"
)

type verdictResult struct {
	Verdict    string  `json:"verdict"`
	Reason     string  `json:"reason"`
	Confidence float64 `json:"confidence"`
}

var verdictSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"verdict":    map[string]any{"type": "string", "enum": []string{VerdictSame, VerdictDifferent, VerdictUnsure}},
		"reason":     map[string]any{"type": "string"},
		"confidence": map[string]any{"type": "number", "minimum": 0, "maximum": 1},
	},
	"required":             []string{"verdict", "reason", "confidence"},
	"additionalProperties": false,
}

func judge(ctx context.Context, c *Client, system, user string, maxTokens int) (verdictResult, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		res, err := c.ChatJSON(ctx, system, user, "verdict", verdictSchema, maxTokens)
		if err != nil {
			lastErr = err
			continue
		}
		var v verdictResult
		if err := json.Unmarshal([]byte(res.Content), &v); err != nil {
			lastErr = fmt.Errorf("unmarshal verdict: %w (raw: %s)", err, truncate(res.Content, 200))
			continue
		}
		switch v.Verdict {
		case VerdictSame, VerdictDifferent, VerdictUnsure:
			return v, nil
		default:
			lastErr = fmt.Errorf("verdict out of enum: %q", v.Verdict)
		}
	}
	return verdictResult{}, lastErr
}

var batchSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"results": map[string]any{"type": "array", "items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"index":      map[string]any{"type": "integer"},
				"verdict":    map[string]any{"type": "string", "enum": []string{VerdictSame, VerdictDifferent, VerdictUnsure}},
				"reason":     map[string]any{"type": "string"},
				"confidence": map[string]any{"type": "number"},
			},
			"required":             []string{"index", "verdict", "reason", "confidence"},
			"additionalProperties": false,
		}},
	},
	"required":             []string{"results"},
	"additionalProperties": false,
}

func judgeBatch(ctx context.Context, c *Client, system, user string, n int) (map[int]verdictResult, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		res, err := c.ChatJSON(ctx, system, user, "batch", batchSchema, 120+n*90)
		if err != nil {
			lastErr = err
			continue
		}
		var parsed struct {
			Results []struct {
				Index int `json:"index"`
				verdictResult
			} `json:"results"`
		}
		if err := json.Unmarshal([]byte(res.Content), &parsed); err != nil {
			lastErr = fmt.Errorf("unmarshal batch: %w", err)
			continue
		}
		out := map[int]verdictResult{}
		for _, r := range parsed.Results {
			switch r.Verdict {
			case VerdictSame, VerdictDifferent, VerdictUnsure:
				out[r.Index] = r.verdictResult
			}
		}
		return out, nil
	}
	return nil, lastErr
}

func runPool[T any](ctx context.Context, items []T, concurrency int, fn func(context.Context, T)) {
	if concurrency < 1 {
		concurrency = 1
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for _, it := range items {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(item T) {
			defer wg.Done()
			defer func() { <-sem }()
			fn(ctx, item)
		}(it)
	}
	wg.Wait()
}

// loadDoneHashes lists the inputs that already carry an answer. A row whose
// model call failed is not one of them. The 2026-08/09 gateway 429 storm wrote
// 3,340 workpair and 2,263 ref failures as rows, and because this set ignored
// error, every later run skipped exactly the inputs still waiting for a verdict:
// the queue reported itself drained while 72% of the needs_manual duplicate
// backlog had never actually been judged.
func loadDoneHashes(db *gorm.DB, table, model, promptVersion, taskCol, task string) (map[string]bool, error) {
	q := db.Table(table).Where("model = ? AND prompt_version = ? AND error = ''", model, promptVersion)
	if taskCol != "" {
		q = q.Where(taskCol+" = ?", task)
	}
	var hashes []string
	if err := q.Pluck("input_hash", &hashes).Error; err != nil {
		return nil, err
	}
	done := make(map[string]bool, len(hashes))
	for _, h := range hashes {
		done[h] = true
	}
	return done, nil
}

func recordRun(db *gorm.DB, task, model, promptVersion string, counts any, startedAt any, notes string) error {
	cj, _ := json.Marshal(counts)
	return db.Exec(
		`INSERT INTO src_llm.run (task, model, prompt_version, counts, notes, started_at) VALUES (?,?,?,?::jsonb,?,?)`,
		task, model, promptVersion, string(cj), notes, startedAt,
	).Error
}
