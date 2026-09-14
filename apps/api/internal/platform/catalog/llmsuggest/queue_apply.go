package llmsuggest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/service"

	"gorm.io/gorm"
)

type ApplyStats struct {
	Applied int
	Counts  map[string]int
}

func RunApply(ctx context.Context, db *gorm.DB, queues *service.AdminQueueService, opts Options) (ApplyStats, error) {
	if !isLiveQueue(opts.Queue) || isGoldQueue(opts.Queue) {
		return ApplyStats{}, fmt.Errorf("apply refuses queue %q", opts.Queue)
	}
	if opts.Actor <= 0 {
		return ApplyStats{}, fmt.Errorf("apply requires --actor > 0")
	}
	if opts.MinConfidence < 0 {
		opts.MinConfidence = 0
	}
	var rows []QueueVerdict
	q := db.Where("queue = ? AND applied_action = '' AND error = '' AND confidence >= ? AND verdict IN ?",
		opts.Queue, opts.MinConfidence, []string{VerdictSame, VerdictDifferent, VerdictChainVerified}).
		Order("id")
	if opts.Limit > 0 {
		q = q.Limit(opts.Limit)
	}
	if err := q.Find(&rows).Error; err != nil {
		return ApplyStats{}, err
	}

	sides := map[int64]workPairSides{}
	if opts.Queue == QueueWorkPair {
		var err error
		sides, err = loadApplySides(db, rows)
		if err != nil {
			return ApplyStats{}, err
		}
	}
	holders := map[string]int64{}
	if opts.Queue == QueueRef {
		var err error
		holders, err = exactSlotHolders(db, rows)
		if err != nil {
			return ApplyStats{}, err
		}
	}

	st := &tally{}
	for _, row := range rows {
		plan := planFor(opts.Queue, row, sides, opts.MinConfidence)
		if plan.Action == applyConfirm {
			if h, ok := holders[exactSlotKey(row.EntityType, row.SourceID, row.ExternalID)]; ok && h != row.EntityID {
				plan = applyPlan{Skip: skipRefExactTaken}
			}
		}
		if plan.Skip != "" {
			st.add(plan.Skip, 1)
			if opts.DryRun {
				fmt.Printf("[dry] apply skip %s queue=%s id=%d verdict=%s conf=%.2f\n",
					plan.Skip, row.Queue, row.ID, row.Verdict, row.Confidence)
			}
			continue
		}
		if opts.DryRun {
			fmt.Printf("[dry] apply %s queue=%s id=%d %s a=%d b=%d entity=%d src=%d ext=%s source=%d target=%d conf=%.2f\n",
				plan.Action, row.Queue, row.ID, row.Verdict, row.AID, row.BID, row.EntityID, row.SourceID, row.ExternalID, plan.Source, plan.Target, row.Confidence)
			st.add("would_"+plan.Action, 1)
			continue
		}
		if err := executeApply(ctx, queues, opts.Queue, row, plan, opts.Actor); err != nil {
			class := classifyApplyErr(err)
			st.add(class, 1)
			fmt.Printf("  ! apply %s id=%d: %v\n", class, row.ID, err)
			continue
		}
		now := time.Now()
		actor := opts.Actor
		res := db.Model(&QueueVerdict{}).Where("id = ? AND applied_action = ''", row.ID).
			Updates(map[string]any{"applied_action": plan.Action, "applied_at": now, "applied_by": actor})
		if res.Error != nil {
			st.add(errOther, 1)
			fmt.Printf("  ! stamp id=%d: %v\n", row.ID, res.Error)
			continue
		}
		st.add("applied_"+plan.Action, 1)
		st.add("applied", 1)
	}
	counts := st.snapshot()
	_ = recordRun(db, "queue-apply-"+opts.Queue, opts.Model, "apply", counts, time.Now(),
		fmt.Sprintf("actor=%d min_confidence=%.2f dry=%v", opts.Actor, opts.MinConfidence, opts.DryRun))
	return ApplyStats{Applied: counts["applied"], Counts: counts}, nil
}

func planFor(queue string, row QueueVerdict, sides map[int64]workPairSides, min float64) applyPlan {
	switch queue {
	case QueueCreditName:
		return planCreditName(row.Verdict, row.Confidence, min)
	case QueueWorkPair:
		a := sides[row.AID]
		b := sides[row.BID]
		s := workPairSides{
			AID: row.AID, BID: row.BID,
			ClaimedA: a.ClaimedA, ClaimedB: b.ClaimedA,
			ExactA: a.ExactA, ExactB: b.ExactA,
		}
		return planWorkPair(row.Verdict, row.Confidence, min, s)
	case QueueRef:
		return planRef(row.Verdict, row.Confidence, min)
	default:
		return applyPlan{Skip: skipGoldQueue}
	}
}

func executeApply(ctx context.Context, queues *service.AdminQueueService, queue string, row QueueVerdict, plan applyPlan, actor int64) error {
	note := applyNote(row.ID, row.Confidence)
	switch queue {
	case QueueCreditName:
		_, err := queues.DecideCandidate(ctx, service.CandidateDecision{
			EntityType: model.EntityTypeCreditName, AID: row.AID, BID: row.BID,
			Action: plan.Action, DecidedBy: actor, Note: note,
		})
		return err
	case QueueWorkPair:
		_, err := queues.DecideCandidate(ctx, service.CandidateDecision{
			EntityType: model.EntityTypeWork, AID: row.AID, BID: row.BID,
			Action: plan.Action, SourceID: plan.Source, TargetID: plan.Target,
			DecidedBy: actor, Note: note,
		})
		return err
	case QueueRef:
		return queues.ConfirmRef(ctx, service.RefKey{
			EntityType: row.EntityType, EntityID: row.EntityID,
			SourceID: row.SourceID, ExternalID: row.ExternalID,
		}, actor)
	default:
		return fmt.Errorf("unknown queue %q", queue)
	}
}

func classifyApplyErr(err error) string {
	switch {
	case errors.Is(err, service.ErrExactTaken):
		return errExactTaken
	case errors.Is(err, service.ErrProposalState):
		return errState
	case errors.Is(err, service.ErrNotFound):
		return errNotFound
	default:
		return errOther
	}
}

func loadApplySides(db *gorm.DB, rows []QueueVerdict) (map[int64]workPairSides, error) {
	out := map[int64]workPairSides{}
	ids := make([]int64, 0, len(rows)*2)
	seen := map[int64]struct{}{}
	for _, r := range rows {
		for _, id := range []int64{r.AID, r.BID} {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	for _, chunk := range chunkBy(ids, 500) {
		var works []struct {
			ID   int64   `gorm:"column:id"`
			Site *string `gorm:"column:site"`
		}
		if err := db.Raw(`SELECT id, site FROM catalog_work WHERE id IN ?`, chunk).Scan(&works).Error; err != nil {
			return nil, err
		}
		for _, w := range works {
			s := out[w.ID]
			s.AID = w.ID
			s.ClaimedA = siteClaimed(w.Site)
			out[w.ID] = s
		}
		var refs []struct {
			ID int64 `gorm:"column:entity_id"`
			N  int   `gorm:"column:n"`
		}
		if err := db.Raw(`SELECT entity_id, count(*) AS n FROM catalog_external_ref
			WHERE entity_type = ? AND link_kind = ? AND dead_at IS NULL AND entity_id IN ?
			GROUP BY entity_id`, model.EntityTypeWork, model.LinkKindExact, chunk).Scan(&refs).Error; err != nil {
			return nil, err
		}
		for _, r := range refs {
			s := out[r.ID]
			s.ExactA = r.N
			out[r.ID] = s
		}
	}
	return out, nil
}

func exactSlotKey(entityType, sourceID int16, externalID string) string {
	return fmt.Sprintf("%d|%d|%s", entityType, sourceID, externalID)
}

// exactSlotHolders maps each slot these rows want to the entity that already
// holds it exactly, if any.
//
// A confirm whose slot another entity holds is refused by ConfirmRef with
// ErrExactTaken, and the apply loop does not stamp a row that errored - so
// before this, every run retried the same doomed confirms. 2026-09-14: 10,684
// of the 11,829 rows waiting to apply were in that state, every one a
// chain-verified vndb release backfill, which is a bundle release fanned out
// across works where only one of them can hold exact.
//
// Those rows are skipped rather than stamped: the blocker is a fact about the
// catalog and not about the verdict, so if the holder is ever merged away the
// row becomes actionable again on its own.
func exactSlotHolders(db *gorm.DB, rows []QueueVerdict) (map[string]int64, error) {
	type group struct {
		et  int16
		src int16
	}
	wanted := map[group][]string{}
	seen := map[string]struct{}{}
	for _, r := range rows {
		k := exactSlotKey(r.EntityType, r.SourceID, r.ExternalID)
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		g := group{r.EntityType, r.SourceID}
		wanted[g] = append(wanted[g], r.ExternalID)
	}

	out := map[string]int64{}
	for g, exts := range wanted {
		for _, chunk := range chunkBy(exts, 500) {
			var found []struct {
				ExternalID string `gorm:"column:external_id"`
				EntityID   int64  `gorm:"column:entity_id"`
			}
			if err := db.Raw(`SELECT external_id, entity_id FROM catalog_external_ref
				WHERE entity_type = ? AND source_id = ? AND link_kind = 0 AND external_id IN ?`,
				g.et, g.src, chunk).Scan(&found).Error; err != nil {
				return nil, err
			}
			for _, f := range found {
				out[exactSlotKey(g.et, g.src, f.ExternalID)] = f.EntityID
			}
		}
	}
	return out, nil
}
