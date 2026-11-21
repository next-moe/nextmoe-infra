package intromt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"sync"
	"time"
	"unicode/utf8"

	"api/internal/platform/catalog/introguard"
	"api/internal/platform/catalog/repository"

	"gorm.io/gorm"
)

const (
	langZhHans     = "zh-Hans"
	workIntroTable = "catalog_work_intro"
)

type decision int

const (
	decInsert decision = iota
	decRetrans
	decSkipSame
)

// force exists because the prompt is not part of the hash: after a prompt
// rewrite every row still hashes as current, so nothing would be redone.
func decide(c candidate, force bool) (decision, string) {
	hash := hashCandidate(c.JaText, c.Gloss)
	if c.MZhID == nil {
		return decInsert, hash
	}
	if !force && c.MZhSrcHash != nil && *c.MZhSrcHash == hash {
		return decSkipSame, hash
	}
	return decRetrans, hash
}

func hashCandidate(text string, gloss Glossary) string {
	if len(gloss) == 0 {
		return hashSource(text)
	}
	return hashSource(text + "\x00" + gloss.Canonical())
}

func hashSource(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

type runner struct {
	db       *gorm.DB
	tr       Translator
	stats    *Stats
	force    bool
	rejected introguard.Rejections
	mu       sync.Mutex
	touched  []int64
}

func (r *runner) markTouched(workID int64) {
	r.mu.Lock()
	r.touched = append(r.touched, workID)
	r.mu.Unlock()
}

func (r *runner) touch(ctx context.Context) error {
	return repository.TouchWorks(ctx, r.db, r.touched)
}

func (r *runner) inc(n *int) {
	r.mu.Lock()
	*n++
	r.mu.Unlock()
}

func (r *runner) process(ctx context.Context, cands []candidate, apply bool, delay time.Duration, workers int) {
	if !apply || workers <= 1 {
		for i, c := range cands {
			if ctx.Err() != nil {
				return
			}
			r.handle(ctx, c, apply, delay, i)
		}
		return
	}
	ch := make(chan candidate)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for c := range ch {
				if ctx.Err() != nil {
					continue
				}
				r.handle(ctx, c, apply, delay, 1)
			}
		})
	}
	for _, c := range cands {
		if ctx.Err() != nil {
			break
		}
		ch <- c
	}
	close(ch)
	wg.Wait()
}

func (r *runner) handle(ctx context.Context, c candidate, apply bool, delay time.Duration, idx int) {
	if c.NoSource {
		r.clearIfMachine(ctx, c, apply)
		return
	}
	dec, hash := decide(c, r.force)
	if dec == decSkipSame && c.MZhText != nil && introguard.Reject(c.JaText, *c.MZhText) != "" {
		r.inc(&r.stats.WouldHeal)
		dec = decRetrans
	}
	if dec != decSkipSame && r.rejected.Has(c.WorkID, hash) {
		r.inc(&r.stats.SkipRejected)
		return
	}
	switch dec {
	case decSkipSame:
		r.inc(&r.stats.SkipUnchanged)
		r.pruneIfStray(ctx, c, apply)
		return
	case decRetrans:
		r.inc(&r.stats.WouldRetranslate)
	case decInsert:
		r.inc(&r.stats.WouldInsert)
	}
	if c.MZhStrays {
		r.inc(&r.stats.WouldPrune)
	}

	sample := r.beginSample(c, dec)
	if !apply {
		r.finishSample(sample, "", "")
		return
	}

	if delay > 0 && idx > 0 {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}

	zh, mtModel, err := r.tr.Translate(ctx, c.JaText, c.Gloss)
	if err != nil {
		r.inc(&r.stats.Errors)
		slog.Warn("translate failed", "work", c.WorkID, "err", err)
		return
	}
	if zh == "" {
		r.inc(&r.stats.Errors)
		slog.Warn("translate returned empty — refusing to write an empty machine row", "work", c.WorkID)
		return
	}
	if collapsed(c.JaText, zh) {
		r.inc(&r.stats.Collapsed)
		slog.Warn("translation collapsed a long source into almost nothing — keeping the previous row",
			"work", c.WorkID, "src_runes", utf8.RuneCountInString(c.JaText), "zh", zh)
		return
	}

	if reason := introguard.Reject(c.JaText, zh); reason != "" {
		r.reject(ctx, c, hash, reason, mtModel, zh)
		return
	}

	rows, pruned, err := r.writeMachine(ctx, c, zh, hash, mtModel)
	if err != nil {
		r.inc(&r.stats.Errors)
		slog.Warn("write machine intro", "work", c.WorkID, "err", err)
		return
	}
	if rows == 0 {
		r.inc(&r.stats.Refused)
		slog.Warn("refused to overwrite a source intro row", "work", c.WorkID, "source_id", c.JaSourceID)
		return
	}
	if pruned > 0 {
		r.inc(&r.stats.Pruned)
	}
	r.markTouched(c.WorkID)
	if dec == decRetrans {
		r.inc(&r.stats.Retranslated)
	} else {
		r.inc(&r.stats.Inserted)
	}
	r.finishSample(sample, zh, mtModel)
}

// reject records why the model's answer was refused and clears the work's
// machine rows: the row on this source is junk (the heal path) or translates a
// text the source no longer holds, and the memo keeps the next nights from
// asking again.
func (r *runner) reject(ctx context.Context, c candidate, hash, reason, mtModel, zh string) {
	r.inc(&r.stats.Rejected)
	slog.Warn("translation refused — not a translation of the source",
		"work", c.WorkID, "reason", reason, "src_runes", utf8.RuneCountInString(c.JaText), "zh", firstRunes(zh, 120))
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := introguard.RecordRejection(ctx, tx, workIntroTable, c.WorkID, c.JaSourceID, hash, reason, mtModel, zh); err != nil {
			return err
		}
		n, err := r.deleteMachine(tx, c.WorkID)
		if n > 0 {
			r.inc(&r.stats.Cleared)
			r.markTouched(c.WorkID)
		}
		return err
	})
	if err != nil {
		r.inc(&r.stats.Errors)
		slog.Warn("record refused translation", "work", c.WorkID, "err", err)
	}
}

func (r *runner) clearIfMachine(ctx context.Context, c candidate, apply bool) {
	r.inc(&r.stats.Untranslatable)
	if !c.HasMachine {
		return
	}
	r.inc(&r.stats.WouldClear)
	if !apply {
		return
	}
	n, err := r.deleteMachine(r.db.WithContext(ctx), c.WorkID)
	if err != nil {
		r.inc(&r.stats.Errors)
		slog.Warn("clear machine intros of an untranslatable work", "work", c.WorkID, "err", err)
		return
	}
	if n > 0 {
		r.inc(&r.stats.Cleared)
		r.markTouched(c.WorkID)
	}
}

func (r *runner) deleteMachine(db *gorm.DB, workID int64) (int64, error) {
	res := db.Exec(`DELETE FROM catalog_work_intro WHERE work_id = ? AND lang = ? AND provenance = 1`, workID, langZhHans)
	return res.RowsAffected, res.Error
}

func (r *runner) pruneIfStray(ctx context.Context, c candidate, apply bool) {
	if !c.MZhStrays {
		return
	}
	r.inc(&r.stats.WouldPrune)
	if !apply {
		return
	}
	n, err := r.deleteStrays(r.db.WithContext(ctx), c.WorkID, c.JaSourceID)
	if err != nil {
		r.inc(&r.stats.Errors)
		slog.Warn("prune stray machine intros", "work", c.WorkID, "err", err)
		return
	}
	if n > 0 {
		r.inc(&r.stats.Pruned)
		r.markTouched(c.WorkID)
	}
}

func (r *runner) writeMachine(ctx context.Context, c candidate, zh, hash, mtModel string) (rows, pruned int64, err error) {
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var e error
		rows, e = r.upsert(tx, c, zh, hash, mtModel)
		if e != nil {
			return e
		}
		if rows == 0 {
			return nil
		}
		pruned, e = r.deleteStrays(tx, c.WorkID, c.JaSourceID)
		return e
	})
	return rows, pruned, err
}

func (r *runner) upsert(db *gorm.DB, c candidate, zh, hash, mtModel string) (int64, error) {
	res := db.Exec(`
		INSERT INTO catalog_work_intro
			(work_id, lang, intro, source_id, provenance, src_hash, mt_model, created_at, updated_at)
		VALUES (?, ?, ?, ?, 1, ?, ?, now(), now())
		ON CONFLICT (work_id, lang, source_id) DO UPDATE
			SET intro = EXCLUDED.intro,
				src_hash = EXCLUDED.src_hash,
				mt_model = EXCLUDED.mt_model,
				updated_at = now()
			WHERE catalog_work_intro.provenance = 1`,
		c.WorkID, langZhHans, zh, c.JaSourceID, hash, mtModel)
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

func (r *runner) deleteStrays(db *gorm.DB, workID int64, keepSource int16) (int64, error) {
	res := db.Exec(`
		DELETE FROM catalog_work_intro
		WHERE work_id = ? AND lang = ? AND provenance = 1 AND source_id <> ?`,
		workID, langZhHans, keepSource)
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

// Prompt rule 2 lets the model drop whole blocks that are not description, so
// it can also drop everything. Shortness alone is not the signal — 529 machine
// rows are legitimately under 20 characters — so the guard only fires when a
// long source produced almost nothing, and it leaves the previous row in place
// so the work comes back as a candidate on the next pass.
const (
	collapseSourceRunes = 100
	collapseOutputRunes = 20
)

func collapsed(src, zh string) bool {
	return utf8.RuneCountInString(src) >= collapseSourceRunes && utf8.RuneCountInString(zh) < collapseOutputRunes
}
