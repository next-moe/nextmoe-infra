package llmsuggest

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type Options struct {
	Model         string
	Concurrency   int
	Limit         int
	DryRun        bool
	GoldSetPath   string
	Batch         bool
	Actor         int64
	MinConfidence float64
	Families      string
	Queue         string
}

const goldsetBatchSize = 10

const (
	namePairSystem = "You are a meticulous name-matching expert for Japanese ACG / visual-novel creator metadata. " +
		"Given two names, decide whether they denote the SAME person. " +
		"SAME includes: a Japanese kanji name and its Chinese (simplified or traditional) rendering; a name and its kana or romaji reading; " +
		"pen names or alternate 名義 of one creator; common abbreviations. " +
		"DIFFERENT includes two distinct people who merely share a surname or look similar. " +
		"If you genuinely cannot tell, answer \"unsure\". Keep the reason to one short clause."

	residueSystem = "You extract structured fields from a raw Bangumi wiki infobox that failed automatic parsing. " +
		"Return the infobox type and a flat list of {key, value} fields exactly as written — do NOT translate, invent, or normalize values. " +
		"For an array field, emit each item as a separate field reusing the same key. If a value is empty use an empty string."
)

var extractionSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"type": map[string]any{"type": "string"},
		"fields": map[string]any{"type": "array", "items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"key":   map[string]any{"type": "string"},
				"value": map[string]any{"type": "string"},
			},
			"required":             []string{"key", "value"},
			"additionalProperties": false,
		}},
	},
	"required":             []string{"type", "fields"},
	"additionalProperties": false,
}

func namePairUser(a, b string) string {
	return fmt.Sprintf("Name A: %s\nName B: %s", a, b)
}

func RunGoldset(ctx context.Context, db *gorm.DB, c *Client, opts Options) (judged, errs int, err error) {
	pairs, err := LoadGoldSet(opts.GoldSetPath)
	if err != nil {
		return 0, 0, fmt.Errorf("load gold set: %w", err)
	}
	if opts.DryRun {
		return dryRunNamePairs(ctx, c, pairs, opts.Limit)
	}
	pv := PromptNamePairV1
	if opts.Batch {
		pv = PromptNamePairV1B
	}
	done, err := loadDoneHashes(db, "src_llm.name_pair_judgment", opts.Model, pv, "task", "goldset")
	if err != nil {
		return 0, 0, err
	}
	var work []goldItem
	for _, p := range pairs {
		h := hashInput("goldset", p.A, p.B)
		if !done[h] {
			work = append(work, goldItem{p, h})
		}
	}
	if opts.Limit > 0 && len(work) > opts.Limit {
		work = work[:opts.Limit]
	}

	if opts.Batch {
		judged, errs = runGoldsetBatch(ctx, db, c, opts.Model, opts.Concurrency, work)
	} else {
		judged, errs = runGoldsetSingle(ctx, db, c, opts.Model, opts.Concurrency, work)
	}
	_ = recordRun(db, "goldset", opts.Model, pv,
		map[string]int{"judged": judged, "errors": errs, "total_pairs": len(pairs)}, time.Now(), "")
	return judged, errs, nil
}

type goldItem struct {
	pair GoldPair
	hash string
}

func newGoldRow(it goldItem, model, pv string) NamePairJudgment {
	return NamePairJudgment{
		Task: "goldset", InputHash: it.hash, Model: model, PromptVersion: pv,
		A: it.pair.A, B: it.pair.B, GoldLabel: it.pair.Label, SourceRule: it.pair.SourceRule,
	}
}

func runGoldsetSingle(ctx context.Context, db *gorm.DB, c *Client, model string, conc int, work []goldItem) (int, int) {
	var nJudged, nErrs atomic.Int64
	runPool(ctx, work, conc, func(ctx context.Context, it goldItem) {
		row := newGoldRow(it, model, PromptNamePairV1)
		v, jerr := judge(ctx, c, namePairSystem, namePairUser(it.pair.A, it.pair.B), 256)
		if jerr != nil {
			row.Error = truncate(jerr.Error(), 500)
			nErrs.Add(1)
		} else {
			row.Verdict, row.Reason, row.Confidence = v.Verdict, v.Reason, v.Confidence
			nJudged.Add(1)
		}
		if e := upsertJudgement(db, &row, "name_pair_judgment",
			[]string{"task", "input_hash", "model", "prompt_version"},
			[]string{"a", "b", "gold_label", "source_rule", "verdict", "reason", "confidence", "error", "created_at"}); e != nil {
			slog.Error("persist goldset judgment", "error", e)
		}
	})
	return int(nJudged.Load()), int(nErrs.Load())
}

func runGoldsetBatch(ctx context.Context, db *gorm.DB, c *Client, model string, conc int, work []goldItem) (int, int) {
	var chunks [][]goldItem
	for i := 0; i < len(work); i += goldsetBatchSize {
		end := min(i+goldsetBatchSize, len(work))
		chunks = append(chunks, work[i:end])
	}
	var nJudged, nErrs atomic.Int64
	runPool(ctx, chunks, conc, func(ctx context.Context, chunk []goldItem) {
		var sb strings.Builder
		sb.WriteString("Judge each numbered name pair independently. Return one result per pair tagged with its index.\n")
		for i, it := range chunk {
			fmt.Fprintf(&sb, "%d. A: %s | B: %s\n", i+1, it.pair.A, it.pair.B)
		}
		results, berr := judgeBatch(ctx, c, namePairSystem, sb.String(), len(chunk))
		for i, it := range chunk {
			row := newGoldRow(it, model, PromptNamePairV1B)
			if v, ok := results[i+1]; ok {
				row.Verdict, row.Reason, row.Confidence = v.Verdict, v.Reason, v.Confidence
				nJudged.Add(1)
			} else {
				msg := "missing from batch result"
				if berr != nil {
					msg = berr.Error()
				}
				row.Error = truncate(msg, 500)
				nErrs.Add(1)
			}
			if e := upsertJudgement(db, &row, "name_pair_judgment",
				[]string{"task", "input_hash", "model", "prompt_version"},
				[]string{"a", "b", "gold_label", "source_rule", "verdict", "reason", "confidence", "error", "created_at"}); e != nil {
				slog.Error("persist goldset batch judgment", "error", e)
			}
		}
	})
	return int(nJudged.Load()), int(nErrs.Load())
}

func dryRunNamePairs(ctx context.Context, c *Client, pairs []GoldPair, limit int) (int, int, error) {
	if limit <= 0 {
		limit = 5
	}
	for i, p := range pairs {
		if i >= limit {
			break
		}
		v, err := judge(ctx, c, namePairSystem, namePairUser(p.A, p.B), 256)
		if err != nil {
			fmt.Printf("[dry] %s ⇔ %s (gold=%s) ERROR: %v\n", p.A, p.B, p.Label, err)
			continue
		}
		fmt.Printf("[dry] %s ⇔ %s | gold=%s → verdict=%s conf=%.2f | %s\n", p.A, p.B, p.Label, v.Verdict, v.Confidence, v.Reason)
	}
	return 0, 0, nil
}

type residueRow struct {
	Table string
	ID    int64
	Raw   string
}

func RunResidue(ctx context.Context, db *gorm.DB, c *Client, opts Options) (done, errs int, err error) {
	var rows []residueRow
	for _, tbl := range []string{"subject", "person", "character"} {
		var rs []struct {
			ID  int64  `gorm:"column:id"`
			Raw string `gorm:"column:infobox_raw"`
		}
		if err := db.Raw(`SELECT id, infobox_raw FROM src_bangumi.` + tbl + ` WHERE parse_error <> '' ORDER BY id`).Scan(&rs).Error; err != nil {
			return 0, 0, err
		}
		for _, r := range rs {
			rows = append(rows, residueRow{Table: tbl, ID: r.ID, Raw: r.Raw})
		}
	}
	if opts.DryRun {
		return dryRunResidue(ctx, c, rows, opts.Limit)
	}
	doneSet, err := loadDoneHashes(db, "src_llm.infobox_extraction", opts.Model, PromptResidueV1, "", "")
	if err != nil {
		return 0, 0, err
	}
	var work []residueRow
	for _, r := range rows {
		if !doneSet[hashInput("residue", r.Table, fmt.Sprint(r.ID))] {
			work = append(work, r)
		}
	}
	if opts.Limit > 0 && len(work) > opts.Limit {
		work = work[:opts.Limit]
	}

	var nDone, nErrs atomic.Int64
	runPool(ctx, work, opts.Concurrency, func(ctx context.Context, r residueRow) {
		row := InfoboxExtraction{
			SrcTable: r.Table, SrcID: r.ID, Model: opts.Model, PromptVersion: PromptResidueV1,
			InputHash: hashInput("residue", r.Table, fmt.Sprint(r.ID)),
		}
		content, jerr := extractResidue(ctx, c, r.Raw)
		if jerr != nil {
			row.Error = truncate(jerr.Error(), 500)
			nErrs.Add(1)
		} else {
			row.Extracted = datatypes.JSON(content)
			nDone.Add(1)
		}
		if e := upsertJudgement(db, &row, "infobox_extraction",
			[]string{"input_hash", "model", "prompt_version"},
			[]string{"src_table", "src_id", "extracted", "error", "created_at"}); e != nil {
			slog.Error("persist residue extraction", "error", e, "table", r.Table, "id", r.ID)
		}
	})
	done, errs = int(nDone.Load()), int(nErrs.Load())
	_ = recordRun(db, "residue", opts.Model, PromptResidueV1,
		map[string]int{"done": done, "errors": errs, "total": len(rows)}, time.Now(), "")
	return done, errs, nil
}

func extractResidue(ctx context.Context, c *Client, raw string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		res, err := c.ChatJSON(ctx, residueSystem, "Raw infobox:\n"+truncate(raw, 4000), "infobox", extractionSchema, 1800)
		if err != nil {
			lastErr = err
			continue
		}
		if !json.Valid([]byte(res.Content)) {
			lastErr = fmt.Errorf("invalid json: %s", truncate(res.Content, 200))
			continue
		}
		return []byte(res.Content), nil
	}
	return nil, lastErr
}

func dryRunResidue(ctx context.Context, c *Client, rows []residueRow, limit int) (int, int, error) {
	if limit <= 0 {
		limit = 5
	}
	for i, r := range rows {
		if i >= limit {
			break
		}
		content, err := extractResidue(ctx, c, r.Raw)
		if err != nil {
			fmt.Printf("[dry] %s#%d EXTRACT ERROR: %v\n", r.Table, r.ID, err)
			continue
		}
		fmt.Printf("[dry] %s#%d → %s\n", r.Table, r.ID, truncate(string(content), 400))
	}
	return 0, 0, nil
}
