package llmsuggest

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync/atomic"
	"time"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

type creditNameItem struct {
	AID, BID     int64
	AName, BName string
	ACredits     int64
	BCredits     int64
	ASource      string
	BSource      string
	Hash         string
}

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

	var nJudged, nErrs atomic.Int64
	runPool(ctx, work, opts.Concurrency, func(ctx context.Context, it creditNameItem) {
		row := QueueVerdict{
			Queue: QueueCreditName, Lane: LaneLLM,
			EntityType: model.EntityTypeCreditName, AID: it.AID, BID: it.BID,
			InputHash: it.Hash, Model: opts.Model, PromptVersion: PromptCreditName,
			Evidence: evidenceJSON(map[string]any{
				"a": map[string]any{"id": it.AID, "name": it.AName, "credits": it.ACredits, "min_exact_source": it.ASource},
				"b": map[string]any{"id": it.BID, "name": it.BName, "credits": it.BCredits, "min_exact_source": it.BSource},
			}),
		}
		v, jerr := judge(ctx, c, namePairSystem, creditNameUser(it), 256)
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
	_ = recordRun(db, "queue-creditname", opts.Model, PromptCreditName,
		map[string]int{"judged": judged, "errors": errs, "total": len(items), "todo": len(work)}, time.Now(), "")
	return judged, errs, nil
}

func creditNameUser(it creditNameItem) string {
	return fmt.Sprintf("Name A: %s\nContext A: credits=%d min_exact_source=%s\nName B: %s\nContext B: credits=%d min_exact_source=%s",
		it.AName, it.ACredits, it.ASource, it.BName, it.BCredits, it.BSource)
}

func dryRunCreditNames(ctx context.Context, c *Client, items []creditNameItem, limit int) (int, int, error) {
	limit = dryLimit(limit)
	for i, it := range items {
		if i >= limit {
			break
		}
		v, err := judge(ctx, c, namePairSystem, creditNameUser(it), 256)
		if err != nil {
			fmt.Printf("[dry] creditname %d⇔%d %s ⇔ %s ERROR: %v\n", it.AID, it.BID, it.AName, it.BName, err)
			continue
		}
		fmt.Printf("[dry] creditname %d⇔%d %s ⇔ %s → %s conf=%.2f | %s\n",
			it.AID, it.BID, it.AName, it.BName, v.Verdict, v.Confidence, v.Reason)
	}
	return 0, 0, nil
}

func loadCreditNameQueue(db *gorm.DB) ([]creditNameItem, error) {
	var cands []struct {
		AID int64 `gorm:"column:a_id"`
		BID int64 `gorm:"column:b_id"`
	}
	if err := db.Raw(`SELECT a_id, b_id FROM catalog_match_candidate
		WHERE entity_type = ? AND status = ? ORDER BY a_id, b_id`,
		model.EntityTypeCreditName, model.CandidateStatusPending).Scan(&cands).Error; err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(cands)*2)
	for _, c := range cands {
		ids = append(ids, c.AID, c.BID)
	}
	names := map[int64]string{}
	credits := map[int64]int64{}
	sources := map[int64]string{}
	for _, chunk := range chunkBy(ids, 500) {
		if len(chunk) == 0 {
			continue
		}
		var nameRows []struct {
			ID   int64  `gorm:"column:id"`
			Name string `gorm:"column:name"`
		}
		if err := db.Raw(`SELECT id, name FROM catalog_credit_name WHERE id IN ?`, chunk).Scan(&nameRows).Error; err != nil {
			return nil, err
		}
		for _, r := range nameRows {
			names[r.ID] = r.Name
		}
		var creditRows []struct {
			ID    int64 `gorm:"column:credit_name_id"`
			Count int64 `gorm:"column:count"`
		}
		if err := db.Raw(`SELECT credit_name_id, count(*) AS count FROM catalog_credit
			WHERE credit_name_id IN ? GROUP BY credit_name_id`, chunk).Scan(&creditRows).Error; err != nil {
			return nil, err
		}
		for _, r := range creditRows {
			credits[r.ID] = r.Count
		}
		var sourceRows []struct {
			ID     int64 `gorm:"column:entity_id"`
			Source int16 `gorm:"column:source_id"`
		}
		if err := db.Raw(`SELECT entity_id, min(source_id) AS source_id FROM catalog_external_ref
			WHERE entity_type = ? AND link_kind = ? AND entity_id IN ? GROUP BY entity_id`,
			model.EntityTypeCreditName, model.LinkKindExact, chunk).Scan(&sourceRows).Error; err != nil {
			return nil, err
		}
		for _, r := range sourceRows {
			sources[r.ID] = strconv.FormatInt(int64(r.Source), 10)
		}
	}
	srcOrNone := func(id int64) string {
		if s := sources[id]; s != "" {
			return s
		}
		return "none"
	}
	out := make([]creditNameItem, 0, len(cands))
	for _, c := range cands {
		aName, bName := names[c.AID], names[c.BID]
		if aName == "" || bName == "" {
			slog.Warn("queue-creditname missing name", "a", c.AID, "b", c.BID)
			continue
		}
		out = append(out, creditNameItem{
			AID: c.AID, BID: c.BID, AName: aName, BName: bName,
			ACredits: credits[c.AID], BCredits: credits[c.BID],
			ASource: srcOrNone(c.AID), BSource: srcOrNone(c.BID),
			Hash: creditNameHash(c.AID, c.BID, aName, bName),
		})
	}
	return out, nil
}
