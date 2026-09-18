package entityintromt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"sync"
	"time"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

const langZhHans = "zh-Hans"

const touchChunk = 1000

type decision int

const (
	decInsert decision = iota
	decRetrans
	decSkipSame
)

// force exists because the prompt is not part of the hash: after a prompt
// rewrite every row still hashes as current, so nothing would be redone.
func decide(c candidate, force bool) (decision, string) {
	hash := hashCandidate(c.Text, c.Gloss)
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
	db      *gorm.DB
	tr      Translator
	lane    laneDef
	force   bool
	stats   *LaneStats
	mu      sync.Mutex
	touched []int64
}

func (r *runner) markTouched(entityID int64) {
	r.mu.Lock()
	r.touched = append(r.touched, entityID)
	r.mu.Unlock()
}

func (r *runner) touch(ctx context.Context) error {
	if len(r.touched) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(r.touched))
	ids := make([]int64, 0, len(r.touched))
	for _, id := range r.touched {
		if id == 0 {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	for start := 0; start < len(ids); start += touchChunk {
		end := min(start+touchChunk, len(ids))
		if err := r.db.WithContext(ctx).
			Exec(`UPDATE `+r.lane.entityTable+` SET updated_at = now() WHERE id IN (?)`, ids[start:end]).
			Error; err != nil {
			return err
		}
	}
	return nil
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
	// A source with no substance cannot yield a non-empty translation — the
	// empty-output guard below would refuse it after a wasted LLM call, and
	// fill-missing would then retry it EVERY sweep. Skip it up front (same
	// decision dry and apply).
	if substanceRunes(c.Text) < minSourceRunes {
		r.inc(&r.stats.SkipShortSource)
		return
	}
	dec, hash := decide(c, r.force)
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

	zh, mtModel, err := r.tr.Translate(ctx, c.Text, SourceLang(c.SrcLang), c.Gloss)
	if err != nil {
		r.inc(&r.stats.Errors)
		slog.Warn("translate failed", "lane", r.lane.key, "entity", c.EntityID, "err", err)
		return
	}
	if zh == "" {
		r.inc(&r.stats.Errors)
		slog.Warn("translate returned empty — refusing to write an empty machine row", "lane", r.lane.key, "entity", c.EntityID)
		return
	}

	rows, pruned, err := r.writeMachine(ctx, c, zh, hash, mtModel)
	if err != nil {
		r.inc(&r.stats.Errors)
		slog.Warn("write machine intro", "lane", r.lane.key, "entity", c.EntityID, "err", err)
		return
	}
	if rows == 0 {
		r.inc(&r.stats.Refused)
		slog.Warn("refused to overwrite a source intro row", "lane", r.lane.key, "entity", c.EntityID, "source_id", c.SourceID)
		return
	}
	if pruned > 0 {
		r.inc(&r.stats.Pruned)
	}
	r.markTouched(c.EntityID)
	if dec == decRetrans {
		r.inc(&r.stats.Retranslated)
	} else {
		r.inc(&r.stats.Inserted)
	}
	r.finishSample(sample, zh, mtModel)
}

func (r *runner) pruneIfStray(ctx context.Context, c candidate, apply bool) {
	if !c.MZhStrays {
		return
	}
	r.inc(&r.stats.WouldPrune)
	if !apply {
		return
	}
	n, err := r.deleteStrays(r.db.WithContext(ctx), c.EntityID, c.SourceID)
	if err != nil {
		r.inc(&r.stats.Errors)
		slog.Warn("prune stray machine intros", "lane", r.lane.key, "entity", c.EntityID, "err", err)
		return
	}
	if n > 0 {
		r.inc(&r.stats.Pruned)
		r.markTouched(c.EntityID)
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
		pruned, e = r.deleteStrays(tx, c.EntityID, c.SourceID)
		return e
	})
	return rows, pruned, err
}

func (r *runner) upsert(db *gorm.DB, c candidate, zh, hash, mtModel string) (int64, error) {
	t, id := r.lane.introTable, r.lane.idCol
	res := db.Exec(`
		INSERT INTO `+t+`
			(`+id+`, lang, intro, source_id, provenance, src_hash, mt_model, created_at, updated_at)
		VALUES (?, ?, ?, ?, 1, ?, ?, now(), now())
		ON CONFLICT (`+id+`, lang, source_id) DO UPDATE
			SET intro = EXCLUDED.intro,
				src_hash = EXCLUDED.src_hash,
				mt_model = EXCLUDED.mt_model,
				updated_at = now()
			WHERE `+t+`.provenance = 1`,
		c.EntityID, langZhHans, zh, c.SourceID, hash, mtModel)
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

// The derived rows are extract-char-intros output, which the character read
// path prefers over this job's rows; they are not strays.
func (r *runner) deleteStrays(db *gorm.DB, entityID int64, keepSource int16) (int64, error) {
	t, id := r.lane.introTable, r.lane.idCol
	res := db.Exec(`
		DELETE FROM `+t+`
		WHERE `+id+` = ? AND lang = ? AND provenance = 1 AND source_id <> ? AND source_id <> ?`,
		entityID, langZhHans, keepSource, model.SourceDerived)
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

func (r *runner) beginSample(c candidate, dec decision) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.stats.Samples) >= maxSamples {
		return -1
	}
	r.stats.Samples = append(r.stats.Samples, Sample{
		Lane: r.lane.key, EntityID: c.EntityID, Decision: decisionName(dec),
		SrcLang: c.SrcLang, Src: c.Text, Gloss: c.Gloss,
	})
	return len(r.stats.Samples) - 1
}

func (r *runner) finishSample(i int, zh, mtModel string) {
	if i < 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stats.Samples[i].Zh = zh
	r.stats.Samples[i].MTModel = mtModel
}

func decisionName(d decision) string {
	switch d {
	case decInsert:
		return "insert"
	case decRetrans:
		return "retranslate"
	default:
		return "skip_unchanged"
	}
}
