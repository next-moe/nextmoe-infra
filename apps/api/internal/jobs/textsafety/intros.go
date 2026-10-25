package textsafety

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// The bars a text must clear to be shown on the sfw shelf. A verdict below the
// bar is treated as unsafe, so doubt costs a redaction, never a leak.
const (
	safeIntroConfidence = 0.85
	minKeptShare        = 0.3
	minKeptRunes        = 40
)

type IntroStats struct {
	Pending, Failed int
	ByMode          map[string]int
}

var introClasses = map[string]int16{
	"safe": model.IntroSafetySafe, "mild": model.IntroSafetyMild, "explicit": model.IntroSafetyExplicit,
}

var sfwModes = map[string]int16{
	"original": model.IntroSFWOriginal, "redacted": model.IntroSFWRedacted,
	"rewritten": model.IntroSFWRewritten, "dropped": model.IntroSFWDropped,
}

type introVerdict struct {
	Class      int16
	Confidence float64
	Drop       []int
}

func (v introVerdict) safe() bool {
	return v.Class == model.IntroSafetySafe && v.Confidence >= safeIntroConfidence
}

type introReply struct {
	Items []struct {
		ID         int     `json:"id"`
		Class      string  `json:"class"`
		Confidence float64 `json:"confidence"`
		Drop       []int   `json:"drop"`
	} `json:"items"`
}

type rewriteReply struct {
	Items []struct {
		ID    int    `json:"id"`
		Intro string `json:"intro"`
	} `json:"items"`
}

func JudgeIntros(ctx context.Context, db *gorm.DB, llm *LLM, opts JudgeOpts, pending []IntroInput, kind IntroKind) (*IntroStats, error) {
	if opts.Limit > 0 && len(pending) > opts.Limit {
		pending = pending[:opts.Limit]
	}
	st := &IntroStats{Pending: len(pending), ByMode: map[string]int{}}
	var mu sync.Mutex
	batches := pack(pending, func(in IntroInput) int { return runeLen(in.Intro) }, 5000, 20)
	parallel(ctx, batches, opts.Concurrency, func(ctx context.Context, batch []IntroInput) {
		rows, err := bisect(ctx, batch, func(ctx context.Context, b []IntroInput) ([]model.CatalogWorkIntroSafety, error) {
			return judgeIntroBatch(ctx, llm, b, kind)
		}, func(in IntroInput) model.CatalogWorkIntroSafety {
			return model.CatalogWorkIntroSafety{
				IntroHash: in.Hash, Verdict: model.IntroSafetyExplicit, Confidence: 1,
				SFWMode: model.IntroSFWDropped, Model: llm.Model(), PromptVersion: kind.promptVersion,
			}
		})
		if err == nil && opts.Sink == nil && len(rows) > 0 {
			err = db.WithContext(ctx).Table(kind.table).Clauses(clause.OnConflict{UpdateAll: true}).Create(&rows).Error
		}
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			slog.Warn("intro batch failed", "size", len(batch), "error", err)
			st.Failed += len(batch)
			return
		}
		st.Failed += len(batch) - len(rows)
		byHash := make(map[string]IntroInput, len(batch))
		for _, in := range batch {
			byHash[in.Hash] = in
		}
		for _, r := range rows {
			st.ByMode[classKey(sfwModes, r.SFWMode)]++
			if opts.Sink != nil {
				in := byHash[r.IntroHash]
				opts.Sink(map[string]any{
					"lang": in.Lang, "title": in.Title, "intro": in.Intro, "class": classKey(introClasses, r.Verdict),
					"confidence": r.Confidence, "mode": classKey(sfwModes, r.SFWMode), "sfw_intro": r.SFWIntro,
				})
			}
		}
	})
	return st, ctx.Err()
}

// judgeIntroBatch runs one batch through classify → redact → verify, then
// rewrite → verify for what redaction could not save. An intro whose call
// failed is left out and stays pending for the next run.
func judgeIntroBatch(ctx context.Context, llm *LLM, batch []IntroInput, kind IntroKind) ([]model.CatalogWorkIntroSafety, error) {
	verdicts, err := classifyIntros(ctx, llm, kind, introTexts(batch))
	if err != nil {
		return nil, err
	}
	out := make([]*model.CatalogWorkIntroSafety, len(batch))
	var redactIdx []int
	var redacted []string
	var rewriteIdx []int
	for i, in := range batch {
		v, ok := verdicts[i]
		if !ok {
			continue
		}
		row := &model.CatalogWorkIntroSafety{
			IntroHash: in.Hash, Verdict: v.Class, Confidence: float32(v.Confidence),
			Model: llm.Model(), PromptVersion: kind.promptVersion,
		}
		out[i] = row
		if v.safe() {
			row.SFWMode = model.IntroSFWOriginal
			continue
		}
		text, kept := redact(splitSentences(in.Intro), v.Drop)
		if len(v.Drop) == 0 || kept < minKeptRunes || float64(kept) < minKeptShare*float64(visibleRunes(in.Intro)) {
			rewriteIdx = append(rewriteIdx, i)
			continue
		}
		redactIdx = append(redactIdx, i)
		redacted = append(redacted, text)
	}

	if len(redacted) > 0 {
		checks, err := classifyIntros(ctx, llm, kind, redacted)
		if err != nil {
			return nil, err
		}
		for j, i := range redactIdx {
			if v, ok := checks[j]; ok && v.safe() {
				out[i].SFWMode, out[i].SFWIntro = model.IntroSFWRedacted, redacted[j]
			} else {
				rewriteIdx = append(rewriteIdx, i)
			}
		}
	}

	if len(rewriteIdx) > 0 {
		rewrites, err := rewriteIntros(ctx, llm, kind, batch, rewriteIdx)
		if err != nil {
			return nil, err
		}
		var texts []string
		var idx []int
		for j, i := range rewriteIdx {
			if t := strings.TrimSpace(rewrites[j]); t != "" {
				texts = append(texts, t)
				idx = append(idx, i)
			} else {
				out[i].SFWMode = model.IntroSFWDropped
			}
		}
		if len(texts) > 0 {
			checks, err := classifyIntros(ctx, llm, kind, texts)
			if err != nil {
				return nil, err
			}
			for j, i := range idx {
				if v, ok := checks[j]; ok && v.safe() {
					out[i].SFWMode, out[i].SFWIntro = model.IntroSFWRewritten, texts[j]
				} else {
					out[i].SFWMode = model.IntroSFWDropped
				}
			}
		}
	}

	rows := make([]model.CatalogWorkIntroSafety, 0, len(out))
	for _, r := range out {
		if r != nil {
			rows = append(rows, *r)
		}
	}
	return rows, nil
}

func introTexts(batch []IntroInput) []string {
	out := make([]string, len(batch))
	for i, in := range batch {
		out[i] = in.Intro
	}
	return out
}

// classifyIntros returns verdicts keyed by the index into texts; a text the
// reply skipped or garbled is absent.
func classifyIntros(ctx context.Context, llm *LLM, kind IntroKind, texts []string) (map[int]introVerdict, error) {
	segs := make([][]string, len(texts))
	var b strings.Builder
	for i, t := range texts {
		segs[i] = splitSentences(t)
		fmt.Fprintf(&b, "=== id %d ===\n%s\n", i+1, numberedText(segs[i]))
	}
	var reply introReply
	if err := llm.Ask(ctx, kind.classify+b.String(), &reply); err != nil {
		return nil, err
	}
	out := make(map[int]introVerdict, len(texts))
	for _, it := range reply.Items {
		cls, ok := introClasses[strings.ToLower(strings.TrimSpace(it.Class))]
		i := it.ID - 1
		if !ok || i < 0 || i >= len(texts) {
			continue
		}
		if _, dup := out[i]; dup {
			continue
		}
		var drop []int
		for _, n := range it.Drop {
			if n >= 1 && n <= len(segs[i]) {
				drop = append(drop, n)
			}
		}
		out[i] = introVerdict{Class: cls, Confidence: it.Confidence, Drop: drop}
	}
	return out, nil
}

func rewriteIntros(ctx context.Context, llm *LLM, kind IntroKind, batch []IntroInput, idx []int) ([]string, error) {
	items := make([]any, len(idx))
	for j, i := range idx {
		items[j] = map[string]any{"id": j + 1, "title": batch[i].Title, "lang": batch[i].Lang, "description": batch[i].Intro}
	}
	var reply rewriteReply
	if err := llm.Ask(ctx, kind.rewrite+jsonLines(items), &reply); err != nil {
		return nil, err
	}
	out := make([]string, len(idx))
	for _, it := range reply.Items {
		if it.ID >= 1 && it.ID <= len(idx) && out[it.ID-1] == "" {
			out[it.ID-1] = it.Intro
		}
	}
	return out, nil
}
