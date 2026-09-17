package llmsuggest

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

type creditNameItem struct {
	AID, BID int64
	Hash     string
	Guard    string
	Dossier  creditPairDossier
}

const creditNameSystem = "You are a meticulous creator-identity expert for Japanese ACG and visual-novel metadata. " +
	"Given two credit-name records with an evidence dossier each, decide whether they denote the SAME real person (or the same circle). " +
	"why_paired says how the pair was found, and it is where you start, not the evidence: alias_declared means one source's profile lists the other record's name among its aliases (alias_declared_by says which side's aliases carry it), and shared_handle means both link the same social account. A common name can be listed as an alias of a different person. " +
	"Each side lists its roles, sample works with medium and first release year, first_year and last_year of its credits, and the labels (brands and publishers) of its works. " +
	"Answer SAME only on a positive agreement you can point at: the same kind of work (one career may span voice acting and singing, or illustration and character design), overlapping years, shared_labels or shared_works, or a distinctive full name that both sources spell the same way together with compatible careers. " +
	"Answer DIFFERENT when the dossiers describe careers that cannot be one person's: unrelated trades with no overlap (a voice actor against a programmer or a producer), or credit years that no single career spans. " +
	"A side with no credits has no career to compare; an absent field is missing, not conflicting. " +
	"Answer \"unsure\" when you have neither an agreement nor a discriminator. Keep the reason to one short clause."

func RunQueueCreditName(ctx context.Context, db *gorm.DB, c *Client, opts Options) (judged, errs int, err error) {
	items, err := loadCreditNameQueue(db)
	if err != nil {
		return 0, 0, err
	}
	if opts.DryRun {
		return dryRunCreditNames(ctx, c, items, opts.Limit)
	}
	done, err := loadDoneHashes(db, "src_llm.queue_verdict", opts.Model, PromptCreditName, "queue", QueueCreditName)
	if err != nil {
		return 0, 0, err
	}
	var work []creditNameItem
	for _, it := range items {
		if !done[it.Hash] {
			work = append(work, it)
		}
	}
	if opts.Limit > 0 && len(work) > opts.Limit {
		work = work[:opts.Limit]
	}

	var nJudged, nErrs, nGuarded atomic.Int64
	runPool(ctx, work, opts.Concurrency, func(ctx context.Context, it creditNameItem) {
		raw, _ := json.Marshal(it.Dossier)
		row := QueueVerdict{
			Queue: QueueCreditName, Lane: LaneLLM,
			EntityType: model.EntityTypeCreditName, AID: it.AID, BID: it.BID,
			InputHash: it.Hash, Model: opts.Model, PromptVersion: PromptCreditName,
			Evidence: raw,
		}
		if it.Guard != "" {
			row.Lane, row.Verdict, row.Reason = LaneGuard, VerdictUnsure, "guard: "+it.Guard
			nGuarded.Add(1)
			persistQueueVerdict(db, &row)
			return
		}
		v, jerr := judge(ctx, c, creditNameSystem, creditNameUser(raw), 512)
		if jerr != nil {
			row.Error = truncate(jerr.Error(), 500)
			nErrs.Add(1)
		} else {
			row.Verdict, row.Reason, row.Confidence = v.Verdict, v.Reason, v.Confidence
			nJudged.Add(1)
		}
		persistQueueVerdict(db, &row)
	})
	judged, errs = int(nJudged.Load()+nGuarded.Load()), int(nErrs.Load())
	_ = recordRun(db, "queue-creditname", opts.Model, PromptCreditName,
		map[string]int{"judged": int(nJudged.Load()), "guarded": int(nGuarded.Load()), "errors": errs,
			"total": len(items), "todo": len(work)}, time.Now(), "")
	return judged, errs, nil
}

func creditNameUser(raw []byte) string {
	return "Judge whether these two credit names denote the same person.\n" + string(raw)
}

func dryRunCreditNames(ctx context.Context, c *Client, items []creditNameItem, limit int) (int, int, error) {
	limit = dryLimit(limit)
	for i, it := range items {
		if i >= limit {
			break
		}
		raw, _ := json.Marshal(it.Dossier)
		head := fmt.Sprintf("[dry] creditname %d⇔%d %s ⇔ %s", it.AID, it.BID, it.Dossier.A.Name, it.Dossier.B.Name)
		if it.Guard != "" {
			fmt.Printf("%s → guard=%s\n\t%s\n", head, it.Guard, raw)
			continue
		}
		v, err := judge(ctx, c, creditNameSystem, creditNameUser(raw), 512)
		if err != nil {
			fmt.Printf("%s ERROR: %v\n", head, err)
			continue
		}
		fmt.Printf("%s → %s conf=%.2f | %s\n\t%s\n", head, v.Verdict, v.Confidence, v.Reason, raw)
	}
	return 0, 0, nil
}

func loadCreditNameQueue(db *gorm.DB) ([]creditNameItem, error) {
	var cands []struct {
		AID    int64 `gorm:"column:a_id"`
		BID    int64 `gorm:"column:b_id"`
		Reason int16 `gorm:"column:reason"`
	}
	if err := db.Raw(`SELECT a_id, b_id, reason FROM catalog_match_candidate
		WHERE entity_type = ? AND status = ? ORDER BY a_id, b_id`,
		model.EntityTypeCreditName, model.CandidateStatusPending).Scan(&cands).Error; err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(cands)*2)
	seen := map[int64]bool{}
	for _, c := range cands {
		for _, id := range []int64{c.AID, c.BID} {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	sides, err := loadCreditSides(db, ids)
	if err != nil {
		return nil, err
	}
	out := make([]creditNameItem, 0, len(cands))
	for _, c := range cands {
		a, b := sides[c.AID], sides[c.BID]
		if a == nil || b == nil || a.Name == "" || b.Name == "" {
			slog.Warn("queue-creditname missing name", "a", c.AID, "b", c.BID)
			continue
		}
		d := buildCreditPair(c.Reason, a, b)
		out = append(out, creditNameItem{
			AID: c.AID, BID: c.BID, Dossier: d, Guard: creditNameGuard(d),
			Hash: creditNameHash(c.AID, c.BID, a.Name, b.Name),
		})
	}
	return out, nil
}
