package llmsuggest

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

type workTitleEv struct {
	Title    string `json:"title"`
	Lang     string `json:"lang"`
	Official bool   `json:"official"`
	Norm     string `json:"-"`
}

type workSideDossier struct {
	ID             int64         `json:"id"`
	DisplayName    string        `json:"display_name"`
	Titles         []workTitleEv `json:"titles"`
	MediumKey      string        `json:"medium_key"`
	OLang          string        `json:"olang"`
	ContentRating  int16         `json:"content_rating"`
	MinReleaseYear *int          `json:"min_release_year,omitempty"`
	Refs           []string      `json:"refs"`
	Site           string        `json:"site,omitempty"`
	ClaimState     *int16        `json:"claim_state,omitempty"`
	Labels         []string      `json:"labels"`
}

// WorksHolding is how many live works hold this identifier, and it is what
// separates an exclusive product page from a studio's twitter account. Without
// it both arrive as one indistinguishable "shared ref" line.
type sharedRefEv struct {
	Ref          string `json:"ref"`
	WorksHolding int    `json:"works_holding"`
}

type workPairDossier struct {
	A                workSideDossier `json:"a"`
	B                workSideDossier `json:"b"`
	SharedRefs       []sharedRefEv   `json:"shared_refs"`
	SharedTitleNorms int             `json:"shared_title_norms"`
}

type workPairItem struct {
	AID, BID int64
	Hash     string
	Dossier  workPairDossier
}

func RunQueueWorkPair(ctx context.Context, db *gorm.DB, c *Client, opts Options) (judged, errs int, err error) {
	var skippedCross int
	items, skippedCross, err := loadWorkPairQueue(db)
	if err != nil {
		return 0, 0, err
	}
	if opts.DryRun {
		_, _, dryErr := dryRunWorkPairs(ctx, c, items, opts.Limit)
		fmt.Printf("[dry] workpair candidates=%d skipped_cross_medium=%d\n", len(items), skippedCross)
		return 0, 0, dryErr
	}
	done, err := loadDoneHashes(db, "src_llm.queue_verdict", opts.Model, PromptWorkPair, "queue", QueueWorkPair)
	if err != nil {
		return 0, 0, err
	}
	var work []workPairItem
	for _, it := range items {
		if !done[it.Hash] {
			work = append(work, it)
		}
	}
	if opts.Limit > 0 && len(work) > opts.Limit {
		work = work[:opts.Limit]
	}

	var nJudged, nErrs atomic.Int64
	runPool(ctx, work, opts.Concurrency, func(ctx context.Context, it workPairItem) {
		raw, _ := json.Marshal(it.Dossier)
		row := QueueVerdict{
			Queue: QueueWorkPair, Lane: LaneLLM,
			EntityType: model.EntityTypeWork, AID: it.AID, BID: it.BID,
			InputHash: it.Hash, Model: opts.Model, PromptVersion: PromptWorkPair,
			Evidence: raw,
		}
		v, jerr := judge(ctx, c, workPairSystem, workPairUser(raw), 512)
		if jerr != nil {
			row.Error = truncate(jerr.Error(), 500)
			nErrs.Add(1)
		} else {
			row.Verdict, row.Reason, row.Confidence = v.Verdict, v.Reason, v.Confidence
			nJudged.Add(1)
		}
		persistQueueVerdict(db, &row)
	})
	judged, errs = int(nJudged.Load()), int(nErrs.Load())
	_ = recordRun(db, "queue-workpair", opts.Model, PromptWorkPair,
		map[string]int{"judged": judged, "errors": errs, "total_same_medium": len(items),
			"skipped_cross_medium": skippedCross, "todo": len(work)}, time.Now(), "")
	return judged, errs, nil
}

func workPairUser(raw []byte) string {
	return "Judge whether these two catalog works are the same visual novel.\n" + string(raw)
}

func dryRunWorkPairs(ctx context.Context, c *Client, items []workPairItem, limit int) (int, int, error) {
	limit = dryLimit(limit)
	for i, it := range items {
		if i >= limit {
			break
		}
		raw, _ := json.Marshal(it.Dossier)
		v, err := judge(ctx, c, workPairSystem, workPairUser(raw), 512)
		if err != nil {
			fmt.Printf("[dry] workpair %d⇔%d ERROR: %v\n", it.AID, it.BID, err)
			continue
		}
		fmt.Printf("[dry] workpair %d⇔%d %q ⇔ %q → %s conf=%.2f | %s\n",
			it.AID, it.BID, it.Dossier.A.DisplayName, it.Dossier.B.DisplayName, v.Verdict, v.Confidence, v.Reason)
	}
	return 0, 0, nil
}

func loadWorkPairQueue(db *gorm.DB) ([]workPairItem, int, error) {
	var rows []struct {
		AID  int64 `gorm:"column:a_id"`
		BID  int64 `gorm:"column:b_id"`
		AMed int16 `gorm:"column:a_med"`
		BMed int16 `gorm:"column:b_med"`
	}
	// Both endpoints must still exist. A merge soft-deletes the work it
	// retires, and the candidate row outlives it whenever the pair that
	// executed was a different one, so an undecided candidate can name a work
	// that is gone. Judging those costs a model call and then fails at apply:
	// on 2026-09-14, 22 of the 22 verdicts left to apply were this, 12 raising
	// "candidate not found" and 10 "source and target resolve to the same
	// entity". assertEntityAlive tests deleted_at and not status, so this
	// matches it: a quarantined work is still mergeable, and merging it is how
	// it gets released.
	//
	// Deferred is loaded too: 164 deferred work candidates on 2026-09-18 were
	// never judged again, and a pair kept apart has to come back the night a
	// corroborator appears. uq_queue_verdict on (queue, input_hash, model,
	// prompt_version) plus loadDoneHashes means an already-judged pair costs
	// no model call.
	if err := db.Raw(`SELECT c.a_id, c.b_id, wa.medium_id AS a_med, wb.medium_id AS b_med
		FROM catalog_match_candidate c
		JOIN catalog_work wa ON wa.id = c.a_id AND wa.deleted_at IS NULL
		JOIN catalog_work wb ON wb.id = c.b_id AND wb.deleted_at IS NULL
		WHERE c.entity_type = ? AND c.status IN (?, ?, ?)
		ORDER BY c.a_id, c.b_id`,
		model.EntityTypeWork, model.CandidateStatusPending, model.CandidateStatusNeedsManual, model.CandidateStatusDeferred).
		Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	cross := 0
	var pairs [][2]int64
	seen := map[int64]struct{}{}
	var ids []int64
	for _, r := range rows {
		if r.AMed != r.BMed {
			cross++
			continue
		}
		pairs = append(pairs, [2]int64{r.AID, r.BID})
		for _, id := range []int64{r.AID, r.BID} {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	sides, err := loadWorkSides(db, ids)
	if err != nil {
		return nil, cross, err
	}
	out, err := assembleWorkPairs(db, pairs, sides)
	if err != nil {
		return nil, cross, err
	}
	return out, cross, nil
}

func assembleWorkPairs(db *gorm.DB, pairs [][2]int64, sides map[int64]workSideDossier) ([]workPairItem, error) {
	shared, err := loadSharedRefFanOut(db, pairs)
	if err != nil {
		return nil, err
	}
	out := make([]workPairItem, 0, len(pairs))
	for _, p := range pairs {
		a, aok := sides[p[0]]
		b, bok := sides[p[1]]
		if !aok || !bok {
			continue
		}
		refs := shared[p]
		if refs == nil {
			refs = []sharedRefEv{}
		}
		out = append(out, workPairItem{
			AID: p[0], BID: p[1], Hash: workPairHash(p[0], p[1]),
			Dossier: workPairDossier{A: a, B: b, SharedRefs: refs, SharedTitleNorms: sharedNorms(a.Titles, b.Titles)},
		})
	}
	return out, nil
}

func sharedNorms(a, b []workTitleEv) int {
	set := map[string]struct{}{}
	for _, t := range a {
		if t.Norm != "" {
			set[t.Norm] = struct{}{}
		}
	}
	n := 0
	hit := map[string]struct{}{}
	for _, t := range b {
		if t.Norm == "" {
			continue
		}
		if _, ok := set[t.Norm]; !ok {
			continue
		}
		if _, dup := hit[t.Norm]; dup {
			continue
		}
		hit[t.Norm] = struct{}{}
		n++
	}
	return n
}
