package llmsuggest

import (
	"context"
	"fmt"
	"strconv"
	"sync/atomic"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

// One upstream release can legitimately be many catalog releases: a VNDB
// collection release is imported as one catalog_release per work it contains,
// so r162406 "すぺじゃに共和国スペシャルパック2" is 16 rows with the same title
// and 16 different work_ids. Asking which one IS r162406 has no answer -- they
// are equally part of it -- and neither a judge nor a reviewer can produce one.
//
// Measured on prod 2026-09-15 over the actionable queue: 385 of the 386
// entity_type 6 rows are fan-out, across 111 upstream ids, and every group had
// zero exact holders. planRef confirms the first row of an unclaimed group and
// marks the rest related, so the group's outcome was decided by row order: one
// row got an exact link asserting something false, the other fifteen were left
// permanently unactionable behind uq_catalog_external_ref_exact. That is where
// the standing error_exact_taken residue comes from.
//
// Deliberately Release only. Two catalog WORKS sharing one upstream id is the
// duplicate signal the work-pair lane exists to act on, not a fan-out; in the
// same pool every entity_type 1/3/4 row was a singleton.
func fanoutMembers(items []refItem) map[string]int {
	n := map[string]int{}
	for _, it := range items {
		if it.EntityType != model.EntityTypeRelease {
			continue
		}
		n[exactSlotKey(it.EntityType, it.SourceID, it.ExternalID)]++
	}
	return n
}

func isFanout(members map[string]int, it refItem) bool {
	return members[exactSlotKey(it.EntityType, it.SourceID, it.ExternalID)] > 1
}

func fanoutRow(it refItem, queue string, members int) QueueVerdict {
	return QueueVerdict{
		Queue: queue, Lane: LaneChain,
		EntityType: it.EntityType, EntityID: it.EntityID, SourceID: it.SourceID, ExternalID: it.ExternalID,
		InputHash: it.Hash, Model: ChainModel, PromptVersion: PromptFanout,
		Verdict: VerdictRelated, Confidence: 1,
		Reason: "one upstream release fans out to " + strconv.Itoa(members) +
			" catalog releases; related, and no single one of them holds it",
	}
}

func runFanoutLane(ctx context.Context, db *gorm.DB, work []refItem, members map[string]int, queue string, conc int, nJudged *atomic.Int64) int {
	if len(work) == 0 {
		return 0
	}
	start := nJudged.Load()
	runPool(ctx, work, conc, func(_ context.Context, it refItem) {
		row := fanoutRow(it, queue, members[exactSlotKey(it.EntityType, it.SourceID, it.ExternalID)])
		nJudged.Add(1)
		persistQueueVerdict(db, &row)
	})
	return int(nJudged.Load() - start)
}

func dryRunFanout(work []refItem, members map[string]int, limit int) {
	for _, it := range work[:min(limit, len(work))] {
		fmt.Printf("[dry] ref fanout et=%d id=%d src=%d ext=%s members=%d → %s\n",
			it.EntityType, it.EntityID, it.SourceID, it.ExternalID,
			members[exactSlotKey(it.EntityType, it.SourceID, it.ExternalID)], VerdictRelated)
	}
}
