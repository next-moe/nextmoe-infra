package textsafety

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"sync"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type JudgeOpts struct {
	Limit       int
	Concurrency int
	// Sink, when set, receives every verdict instead of the database (a
	// calibration run against a sample).
	Sink func(any)
}

type TitleStats struct {
	Pending, Judged, Failed int
	ByClass                 map[string]int
}

var titleClasses = map[string]int16{
	"safe": model.TitleSafetySafe, "suggestive": model.TitleSafetySuggestive, "explicit": model.TitleSafetyExplicit,
}

type titleReply struct {
	Items []struct {
		ID         int     `json:"id"`
		Class      string  `json:"class"`
		Confidence float64 `json:"confidence"`
	} `json:"items"`
}

func JudgeTitles(ctx context.Context, db *gorm.DB, llm *LLM, opts JudgeOpts) (*TitleStats, error) {
	works, err := loadWorkTitles(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("load titles: %w", err)
	}
	var done []string
	if err := db.WithContext(ctx).Model(&model.CatalogWorkTitleSafety{}).Pluck("title_hash", &done).Error; err != nil {
		return nil, fmt.Errorf("load title verdicts: %w", err)
	}
	judged := make(map[string]bool, len(done))
	for _, h := range done {
		judged[h] = true
	}
	var todo []workTitles
	for _, w := range works {
		if !judged[w.Hash] || opts.Sink != nil {
			judged[w.Hash] = true
			todo = append(todo, w)
		}
	}
	if opts.Sink != nil {
		rand.New(rand.NewPCG(1, 2)).Shuffle(len(todo), func(i, j int) { todo[i], todo[j] = todo[j], todo[i] })
	}
	if opts.Limit > 0 && len(todo) > opts.Limit {
		todo = todo[:opts.Limit]
	}
	st := &TitleStats{Pending: len(todo), ByClass: map[string]int{}}
	var mu sync.Mutex
	batches := pack(todo, func(w workTitles) int { return runeLen(strings.Join(w.Names, " ")) }, 6000, 100)
	parallel(ctx, batches, opts.Concurrency, func(ctx context.Context, batch []workTitles) {
		rows, err := bisect(ctx, batch, func(ctx context.Context, b []workTitles) ([]model.CatalogWorkTitleSafety, error) {
			return judgeTitleBatch(ctx, llm, b)
		}, func(w workTitles) model.CatalogWorkTitleSafety {
			return model.CatalogWorkTitleSafety{
				TitleHash: w.Hash, Verdict: model.TitleSafetyExplicit, Confidence: 1,
				Model: llm.Model(), PromptVersion: TitlePromptVersion,
			}
		})
		if err == nil && opts.Sink == nil && len(rows) > 0 {
			err = db.WithContext(ctx).Clauses(clause.OnConflict{UpdateAll: true}).Create(&rows).Error
		}
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			slog.Warn("title batch failed", "size", len(batch), "error", err)
			st.Failed += len(batch)
			return
		}
		st.Failed += len(batch) - len(rows)
		st.Judged += len(rows)
		for _, r := range rows {
			st.ByClass[classKey(titleClasses, r.Verdict)]++
			if opts.Sink != nil {
				opts.Sink(map[string]any{"names": namesFor(batch, r.TitleHash), "class": classKey(titleClasses, r.Verdict), "confidence": r.Confidence})
			}
		}
	})
	return st, ctx.Err()
}

func judgeTitleBatch(ctx context.Context, llm *LLM, batch []workTitles) ([]model.CatalogWorkTitleSafety, error) {
	items := make([]any, len(batch))
	for i, w := range batch {
		items[i] = map[string]any{"id": i + 1, "names": w.Names}
	}
	var reply titleReply
	if err := llm.Ask(ctx, titlePrompt+jsonLines(items), &reply); err != nil {
		return nil, err
	}
	seen := map[int]bool{}
	var out []model.CatalogWorkTitleSafety
	for _, it := range reply.Items {
		cls, ok := titleClasses[strings.ToLower(strings.TrimSpace(it.Class))]
		if !ok || it.ID < 1 || it.ID > len(batch) || seen[it.ID] {
			continue
		}
		seen[it.ID] = true
		out = append(out, model.CatalogWorkTitleSafety{
			TitleHash: batch[it.ID-1].Hash, Verdict: cls, Confidence: float32(it.Confidence),
			Model: llm.Model(), PromptVersion: TitlePromptVersion,
		})
	}
	return out, nil
}

func namesFor(batch []workTitles, hash string) []string {
	for _, w := range batch {
		if w.Hash == hash {
			return w.Names
		}
	}
	return nil
}

func classKey(m map[string]int16, v int16) string {
	for k, c := range m {
		if c == v {
			return k
		}
	}
	return "?"
}
