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

func RunCalibrateQueue(ctx context.Context, db *gorm.DB, up StagingDBs, c *Client, opts Options) ([]LayerMetrics, error) {
	switch opts.Queue {
	case QueueWorkPair:
		return calibrateWorkPair(ctx, db, c, opts)
	case QueueRef:
		return calibrateRefs(ctx, db, up, c, opts)
	default:
		return nil, fmt.Errorf("calibrate --queue must be workpair or ref, got %q", opts.Queue)
	}
}

func calibrateWorkPair(ctx context.Context, db *gorm.DB, c *Client, opts Options) ([]LayerMetrics, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 200
	}
	half := limit / 2
	pos, err := loadWorkPairGold(db, model.CandidateStatusAccepted, half)
	if err != nil {
		return nil, err
	}
	neg, err := loadWorkPairGold(db, model.CandidateStatusRejected, half)
	if err != nil {
		return nil, err
	}
	type labeled struct {
		it   workPairItem
		gold string
	}
	var labeledItems []labeled
	for _, it := range pos {
		labeledItems = append(labeledItems, labeled{it: it, gold: VerdictSame})
	}
	for _, it := range neg {
		labeledItems = append(labeledItems, labeled{it: it, gold: VerdictDifferent})
	}

	qname := goldQueue(QueueWorkPair)
	if opts.DryRun {
		limitN := dryLimit(opts.Limit)
		if limitN > len(labeledItems) {
			limitN = len(labeledItems)
		}
		var rows []NamePairJudgment
		for i := 0; i < limitN; i++ {
			it := labeledItems[i]
			raw, _ := json.Marshal(it.it.Dossier)
			v, jerr := judge(ctx, c, workPairSystem, workPairUser(raw), 512)
			row := NamePairJudgment{GoldLabel: it.gold}
			if jerr != nil {
				row.Error = jerr.Error()
				fmt.Printf("[dry] workpair-gold %d⇔%d gold=%s ERROR: %v\n", it.it.AID, it.it.BID, it.gold, jerr)
			} else {
				row.Verdict = v.Verdict
				fmt.Printf("[dry] workpair-gold %d⇔%d gold=%s → %s conf=%.2f | %s\n",
					it.it.AID, it.it.BID, it.gold, v.Verdict, v.Confidence, v.Reason)
			}
			rows = append(rows, row)
		}
		return []LayerMetrics{computeMetrics("overall", rows)}, nil
	}

	done, err := loadDoneHashes(db, "src_llm.queue_verdict", opts.Model, PromptWorkPairV1, "queue", qname)
	if err != nil {
		return nil, err
	}
	var work []labeled
	for _, it := range labeledItems {
		if !done[it.it.Hash] {
			work = append(work, it)
		}
	}

	var nJudged, nErrs atomic.Int64
	runPool(ctx, work, opts.Concurrency, func(ctx context.Context, it labeled) {
		raw, _ := json.Marshal(it.it.Dossier)
		ev := map[string]any{"gold": it.gold, "dossier": json.RawMessage(raw)}
		row := QueueVerdict{
			Queue: qname, Lane: LaneLLM,
			EntityType: model.EntityTypeWork, AID: it.it.AID, BID: it.it.BID,
			InputHash: it.it.Hash, Model: opts.Model, PromptVersion: PromptWorkPairV1,
			Evidence: evidenceJSON(ev),
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
	_ = recordRun(db, "queue-calibrate-workpair", opts.Model, PromptWorkPairV1,
		map[string]int{"judged": int(nJudged.Load()), "errors": int(nErrs.Load()), "gold_n": len(labeledItems)}, time.Now(), "")
	return metricsFromQueueGold(db, qname, opts.Model, PromptWorkPairV1)
}

func loadWorkPairGold(db *gorm.DB, status int16, limit int) ([]workPairItem, error) {
	var rows []struct {
		AID int64 `gorm:"column:a_id"`
		BID int64 `gorm:"column:b_id"`
	}
	if err := db.Raw(`SELECT c.a_id, c.b_id
		FROM catalog_match_candidate c
		JOIN catalog_work wa ON wa.id = c.a_id
		JOIN catalog_work wb ON wb.id = c.b_id AND wa.medium_id = wb.medium_id
		WHERE c.entity_type = ? AND c.status = ?
		ORDER BY md5(c.a_id::text || ',' || c.b_id::text)
		LIMIT ?`, model.EntityTypeWork, status, limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(rows)*2)
	for _, r := range rows {
		ids = append(ids, r.AID, r.BID)
	}
	sides, err := loadWorkSides(db, ids)
	if err != nil {
		return nil, err
	}
	out := make([]workPairItem, 0, len(rows))
	for _, r := range rows {
		a, aok := sides[r.AID]
		b, bok := sides[r.BID]
		if !aok || !bok {
			continue
		}
		d := workPairDossier{A: a, B: b, SharedRefs: intersectStrings(a.Refs, b.Refs), SharedTitleNorms: sharedNorms(a.Titles, b.Titles)}
		out = append(out, workPairItem{AID: r.AID, BID: r.BID, Hash: workPairHash(r.AID, r.BID), Dossier: d})
	}
	return out, nil
}

func calibrateRefs(ctx context.Context, db *gorm.DB, up StagingDBs, c *Client, opts Options) ([]LayerMetrics, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 200
	}
	half := limit / 2
	reg, err := loadSourceReg(db)
	if err != nil {
		return nil, err
	}
	pos, err := loadRefGoldPositives(db, half)
	if err != nil {
		return nil, err
	}
	neg, err := loadRefGoldNegatives(db, reg, half)
	if err != nil {
		return nil, err
	}
	type labeled struct {
		it   refItem
		gold string
	}
	var all []labeled
	for _, it := range pos {
		all = append(all, labeled{it: it, gold: VerdictSame})
	}
	for _, it := range neg {
		all = append(all, labeled{it: it, gold: VerdictDifferent})
	}
	qname := goldQueue(QueueRef)
	if opts.DryRun {
		n := dryLimit(opts.Limit)
		if n > len(all) {
			n = len(all)
		}
		sample := make([]refItem, 0, n)
		golds := map[string]string{}
		for i := 0; i < n; i++ {
			sample = append(sample, all[i].it)
			golds[all[i].it.Hash] = all[i].gold
		}
		rows, err := judgeRefGoldSample(ctx, db, up, c, reg, sample, golds, qname, opts, true)
		if err != nil {
			return nil, err
		}
		return []LayerMetrics{computeMetrics("overall", rows)}, nil
	}
	done, err := loadDoneHashes(db, "src_llm.queue_verdict", opts.Model, PromptRefV1, "queue", qname)
	if err != nil {
		return nil, err
	}
	chainDone, err := loadDoneHashes(db, "src_llm.queue_verdict", ChainModel, PromptChainV1, "queue", qname)
	if err != nil {
		return nil, err
	}
	var todo []labeled
	for _, it := range all {
		if chainFamily(it.it.MatchedBy) {
			if chainDone[it.it.Hash] {
				continue
			}
		} else if done[it.it.Hash] {
			continue
		}
		todo = append(todo, it)
	}
	items := make([]refItem, len(todo))
	golds := map[string]string{}
	for i, it := range todo {
		items[i] = it.it
		golds[it.it.Hash] = it.gold
	}
	if _, err := judgeRefGoldSample(ctx, db, up, c, reg, items, golds, qname, opts, false); err != nil {
		return nil, err
	}
	return metricsFromQueueGold(db, qname, opts.Model, PromptRefV1)
}

func loadRefGoldPositives(db *gorm.DB, limit int) ([]refItem, error) {
	var rows []struct {
		EntityType int16  `gorm:"column:entity_type"`
		EntityID   int64  `gorm:"column:entity_id"`
		SourceID   int16  `gorm:"column:source_id"`
		ExternalID string `gorm:"column:external_id"`
		MatchedBy  string `gorm:"column:matched_by"`
	}
	if err := db.Raw(`SELECT entity_type, entity_id, source_id, external_id, matched_by
		FROM catalog_external_ref
		WHERE link_kind = 0 AND verified_at IS NOT NULL AND dead_at IS NULL
		ORDER BY md5(entity_id::text || ':' || external_id)
		LIMIT ?`, limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rowsToRefItems(rows), nil
}

func loadRefGoldNegatives(db *gorm.DB, reg sourceReg, limit int) ([]refItem, error) {
	var rows []struct {
		EntityType int16  `gorm:"column:entity_type"`
		EntityID   int64  `gorm:"column:entity_id"`
		SourceID   int16  `gorm:"column:source_id"`
		ExternalID string `gorm:"column:external_id"`
	}
	if err := db.Raw(`SELECT entity_type, entity_id, source_id, external_id
		FROM catalog_match_rejection
		ORDER BY md5(entity_id::text || ':' || external_id)
		LIMIT ?`, limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]refItem, len(rows))
	for i, r := range rows {
		out[i] = refItem{
			EntityType: r.EntityType, EntityID: r.EntityID, SourceID: r.SourceID, ExternalID: r.ExternalID,
			MatchedBy: inferMatchedBy(r.EntityType, r.SourceID, reg),
			Hash:      refInputHash(r.EntityType, r.EntityID, r.SourceID, r.ExternalID),
		}
	}
	return out, nil
}

func inferMatchedBy(et int16, sourceID int16, reg sourceReg) string {
	switch {
	case et == model.EntityTypeRelease && sourceID == reg.id(sourceKeyVNDB):
		return matchedByVNDBReleaseBackfill
	case et == model.EntityTypeWork && sourceID == reg.id(sourceKeyDMM):
		return matchedByEGDMM
	case et == model.EntityTypeWork && sourceID == reg.id(sourceKeySteam):
		return matchedByEGSteam
	case et == model.EntityTypeWork && sourceID == reg.id(sourceKeyHLTB):
		return matchedByHLTBSteam
	case et == model.EntityTypeWork && sourceID == reg.id(sourceKeyBangumi):
		return matchedByBgmTitleOnly
	default:
		return ""
	}
}

func rowsToRefItems(rows []struct {
	EntityType int16  `gorm:"column:entity_type"`
	EntityID   int64  `gorm:"column:entity_id"`
	SourceID   int16  `gorm:"column:source_id"`
	ExternalID string `gorm:"column:external_id"`
	MatchedBy  string `gorm:"column:matched_by"`
}) []refItem {
	out := make([]refItem, len(rows))
	for i, r := range rows {
		out[i] = refItem{
			EntityType: r.EntityType, EntityID: r.EntityID, SourceID: r.SourceID,
			ExternalID: r.ExternalID, MatchedBy: r.MatchedBy,
			Hash: refInputHash(r.EntityType, r.EntityID, r.SourceID, r.ExternalID),
		}
	}
	return out
}

func judgeRefGoldSample(ctx context.Context, db *gorm.DB, up StagingDBs, c *Client, reg sourceReg, items []refItem, golds map[string]string, qname string, opts Options, dry bool) ([]NamePairJudgment, error) {
	var chain, llm []refItem
	for _, it := range items {
		if chainFamily(it.MatchedBy) {
			chain = append(chain, it)
		} else {
			llm = append(llm, it)
		}
	}
	var metrics []NamePairJudgment
	if len(chain) > 0 {
		results, err := verifyChainBatch(db, up, reg, chain)
		if err != nil {
			return nil, err
		}
		for _, it := range chain {
			v := results[it.Hash]
			mapped := mapChainToLabel(v.Verdict)
			metrics = append(metrics, NamePairJudgment{GoldLabel: golds[it.Hash], Verdict: mapped, Error: ""})
			if dry {
				fmt.Printf("[dry] ref-gold chain et=%d id=%d ext=%s gold=%s → %s | %s\n",
					it.EntityType, it.EntityID, it.ExternalID, golds[it.Hash], v.Verdict, v.Reason)
				continue
			}
			row := chainRow(it, qname)
			row.Verdict, row.Reason, row.Confidence = v.Verdict, v.Reason, v.Confidence
			row.Evidence = evidenceJSON(map[string]any{"gold": golds[it.Hash], "chain": v.Evidence})
			persistQueueVerdict(db, &row)
		}
	}
	if len(llm) > 0 && c != nil {
		evs, _, err := buildRefLLMEvidence(db, up.EG, reg, llm)
		if err != nil {
			return nil, err
		}
		if dry {
			for _, it := range llm {
				ev, ok := evs[it.Hash]
				if !ok {
					continue
				}
				v, jerr := judge(ctx, c, refSystem, refUser(ev), 512)
				row := NamePairJudgment{GoldLabel: golds[it.Hash]}
				if jerr != nil {
					row.Error = jerr.Error()
				} else {
					row.Verdict = v.Verdict
					fmt.Printf("[dry] ref-gold llm et=%d id=%d ext=%s gold=%s → %s conf=%.2f | %s\n",
						it.EntityType, it.EntityID, it.ExternalID, golds[it.Hash], v.Verdict, v.Confidence, v.Reason)
				}
				metrics = append(metrics, row)
			}
			return metrics, nil
		}
		var nJ, nE atomic.Int64
		runPool(ctx, llm, opts.Concurrency, func(ctx context.Context, it refItem) {
			ev, ok := evs[it.Hash]
			row := llmRefRow(it, qname, opts.Model)
			if !ok {
				row.Error = "no evidence"
				nE.Add(1)
				persistQueueVerdict(db, &row)
				return
			}
			row.Evidence = evidenceJSON(map[string]any{"gold": golds[it.Hash], "dossier": ev})
			v, jerr := judge(ctx, c, refSystem, refUser(ev), 512)
			if jerr != nil {
				row.Error = truncate(jerr.Error(), 500)
				nE.Add(1)
			} else {
				row.Verdict, row.Reason, row.Confidence = v.Verdict, v.Reason, v.Confidence
				nJ.Add(1)
			}
			persistQueueVerdict(db, &row)
		})
	}
	return metrics, nil
}

func mapChainToLabel(v string) string {
	switch v {
	case VerdictChainVerified:
		return VerdictSame
	case VerdictChainUnproven:
		return VerdictUnsure
	default:
		return v
	}
}

func metricsFromQueueGold(db *gorm.DB, queue, model, pv string) ([]LayerMetrics, error) {
	var rows []QueueVerdict
	if err := db.Where("queue = ?", queue).Find(&rows).Error; err != nil {
		return nil, err
	}
	var judged []NamePairJudgment
	byGold := map[string][]NamePairJudgment{}
	for _, r := range rows {
		gold := goldFromEvidence(r.Evidence)
		verdict := r.Verdict
		if r.Lane == LaneChain {
			verdict = mapChainToLabel(r.Verdict)
		}
		np := NamePairJudgment{GoldLabel: gold, Verdict: verdict, Error: r.Error, SourceRule: gold}
		judged = append(judged, np)
		byGold[gold] = append(byGold[gold], np)
	}
	out := []LayerMetrics{computeMetrics("overall", judged)}
	for _, g := range []string{VerdictSame, VerdictDifferent} {
		if rows := byGold[g]; len(rows) > 0 {
			out = append(out, computeMetrics("gold="+g, rows))
		}
	}
	_ = model
	_ = pv
	return out, nil
}

func goldFromEvidence(ev []byte) string {
	var wrap struct {
		Gold string `json:"gold"`
	}
	_ = json.Unmarshal(ev, &wrap)
	return wrap.Gold
}
