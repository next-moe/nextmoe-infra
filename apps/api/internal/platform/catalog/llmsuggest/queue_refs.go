package llmsuggest

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"api/internal/platform/catalog/service"

	"gorm.io/gorm"
)

type refItem struct {
	EntityType int16
	EntityID   int64
	SourceID   int16
	ExternalID string
	MatchedBy  string
	Hash       string
}

func RunQueueRefs(ctx context.Context, db *gorm.DB, up StagingDBs, c *Client, opts Options) (judged, errs int, err error) {
	reg, err := loadSourceReg(db)
	if err != nil {
		return 0, 0, err
	}
	items, err := loadProbableRefs(db)
	if err != nil {
		return 0, 0, err
	}
	wantChain, wantLLM := familiesWant(opts.Families)
	st := &tally{}
	var chainWork, llmWork []refItem
	for _, it := range items {
		switch {
		case chainFamily(it.MatchedBy):
			if !wantChain {
				st.add("skipped_family_filter", 1)
				continue
			}
			if it.MatchedBy == matchedByHLTBSteam && up.HLTB == nil {
				st.add("skipped_hltb_unavailable", 1)
				continue
			}
			if (it.MatchedBy == matchedByEGDMM || it.MatchedBy == matchedByEGSteam) && up.EG == nil {
				st.add("skipped_eg_unavailable", 1)
				continue
			}
			chainWork = append(chainWork, it)
		case llmWorkFamily(it.MatchedBy) || llmEntityFamily(it.EntityType):
			if !wantLLM {
				st.add("skipped_family_filter", 1)
				continue
			}
			llmWork = append(llmWork, it)
		default:
			st.add("skipped_unknown_family", 1)
		}
	}

	if opts.DryRun {
		return dryRunRefs(ctx, db, up, c, reg, chainWork, llmWork, opts.Limit)
	}

	var nJudged, nErrs atomic.Int64
	if len(chainWork) > 0 {
		done, err := loadDoneHashes(db, "src_llm.queue_verdict", ChainModel, PromptChainV2, "queue", QueueRef)
		if err != nil {
			return 0, 0, err
		}
		var work []refItem
		for _, it := range chainWork {
			if !done[it.Hash] {
				work = append(work, it)
			}
		}
		if opts.Limit > 0 && len(work) > opts.Limit {
			work = work[:opts.Limit]
		}
		j, e := runChainLane(ctx, db, up, reg, work, QueueRef, opts.Concurrency, &nJudged, &nErrs)
		st.add("chain_written", j)
		st.add("chain_errors", e)
	}
	if len(llmWork) > 0 {
		if c == nil {
			return 0, 0, fmt.Errorf("queue-refs llm lane requires an LLM client")
		}
		done, err := loadDoneHashes(db, "src_llm.queue_verdict", opts.Model, PromptRefV1, "queue", QueueRef)
		if err != nil {
			return 0, 0, err
		}
		var work []refItem
		for _, it := range llmWork {
			if !done[it.Hash] {
				work = append(work, it)
			}
		}
		remain := opts.Limit
		if remain > 0 {
			remain -= int(nJudged.Load()) + int(nErrs.Load())
			if remain < 0 {
				remain = 0
			}
			if remain == 0 {
				work = nil
			} else if len(work) > remain {
				work = work[:remain]
			}
		}
		j, e, skip := runRefLLMLane(ctx, db, up.EG, c, reg, work, QueueRef, opts, &nJudged, &nErrs)
		st.add("llm_written", j)
		st.add("llm_errors", e)
		for k, v := range skip {
			st.add(k, v)
		}
	}
	judged, errs = int(nJudged.Load()), int(nErrs.Load())
	counts := st.snapshot()
	counts["judged"] = judged
	counts["errors"] = errs
	counts["total_probable"] = len(items)
	_ = recordRun(db, "queue-refs", opts.Model, PromptRefV1, counts, time.Now(), opts.Families)
	return judged, errs, nil
}

func loadProbableRefs(db *gorm.DB) ([]refItem, error) {
	var rows []struct {
		EntityType int16  `gorm:"column:entity_type"`
		EntityID   int64  `gorm:"column:entity_id"`
		SourceID   int16  `gorm:"column:source_id"`
		ExternalID string `gorm:"column:external_id"`
		MatchedBy  string `gorm:"column:matched_by"`
	}
	if err := db.Raw(`SELECT entity_type, entity_id, source_id, external_id, matched_by
		FROM catalog_external_ref
		WHERE link_kind = 1 AND verified_at IS NULL AND dead_at IS NULL
		  AND ` + service.ExactSlotFreeSQL + `
		ORDER BY entity_type, entity_id, source_id, external_id`).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]refItem, len(rows))
	for i, r := range rows {
		out[i] = refItem{
			EntityType: r.EntityType, EntityID: r.EntityID, SourceID: r.SourceID,
			ExternalID: r.ExternalID, MatchedBy: r.MatchedBy,
			Hash: refInputHash(r.EntityType, r.EntityID, r.SourceID, r.ExternalID),
		}
	}
	return out, nil
}

func dryRunRefs(ctx context.Context, db *gorm.DB, up StagingDBs, c *Client, reg sourceReg, chain, llm []refItem, limit int) (int, int, error) {
	limit = dryLimit(limit)
	shown := 0
	if len(chain) > 0 {
		n := min(limit, len(chain))
		results, err := verifyChainBatch(db, up, reg, chain[:n])
		if err != nil {
			return 0, 0, err
		}
		for _, it := range chain[:n] {
			v := results[it.Hash]
			fmt.Printf("[dry] ref chain et=%d id=%d src=%d ext=%s matched_by=%s → %s | %s\n",
				it.EntityType, it.EntityID, it.SourceID, it.ExternalID, it.MatchedBy, v.Verdict, v.Reason)
			shown++
		}
	}
	remain := limit - shown
	if remain > 0 && len(llm) > 0 {
		if c == nil {
			fmt.Printf("[dry] ref llm skipped: no client (%d pending)\n", len(llm))
			return 0, 0, nil
		}
		n := min(remain, len(llm))
		evs, skips, err := buildRefLLMEvidence(db, up.EG, reg, llm[:n])
		if err != nil {
			return 0, 0, err
		}
		for k, v := range skips {
			fmt.Printf("[dry] ref llm skip %s=%d\n", k, v)
		}
		for _, it := range llm[:n] {
			ev, ok := evs[it.Hash]
			if !ok {
				continue
			}
			v, jerr := judge(ctx, c, refSystem, refUser(ev), 512)
			if jerr != nil {
				fmt.Printf("[dry] ref llm et=%d id=%d src=%d ext=%s ERROR: %v\n",
					it.EntityType, it.EntityID, it.SourceID, it.ExternalID, jerr)
				continue
			}
			fmt.Printf("[dry] ref llm et=%d id=%d src=%d ext=%s matched_by=%s → %s conf=%.2f | %s\n",
				it.EntityType, it.EntityID, it.SourceID, it.ExternalID, it.MatchedBy, v.Verdict, v.Confidence, v.Reason)
		}
	}
	fmt.Printf("[dry] ref chain_candidates=%d llm_candidates=%d\n", len(chain), len(llm))
	return 0, 0, nil
}
