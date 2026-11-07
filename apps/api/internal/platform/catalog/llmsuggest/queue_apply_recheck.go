package llmsuggest

import (
	"context"
	"fmt"
	"time"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/service"

	"gorm.io/gorm"
)

const (
	// A pair the rules once accepted and then took back goes to a person, and
	// its quarantined side stays quarantined: the kept-apart stamps would
	// publish it, and the verdict was already good enough to merge once.
	stampWithdrawnOnRecheck = "withdrawn_on_recheck"
	// Not in currentStamps: the row is planned again in the same run and files
	// its merge in the direction the current rules pick.
	stampRedirectedOnRecheck = "accept_redirected_on_recheck"

	recheckKept       = "recheck_kept"
	recheckWithdrawn  = "recheck_withdrawn"
	recheckRedirected = "recheck_redirected"
	recheckMoved      = "recheck_endpoint_moved"
)

type pendingAccept struct {
	Row        QueueVerdict
	ProposalID int64
	Source     int64
	Target     int64
}

// recheckPendingAccepts plans every accepted work pair whose merge has not
// executed yet against the rules this build ships. On 2026-11-07 the nightly
// lane held 1,282 such proposals inside their 48h cooling window, about 700 of
// them accepted by name corroborators #519 had just tightened; execute checks
// only the survivor claim, so every one of them would have merged anyway.
func recheckPendingAccepts(ctx context.Context, db *gorm.DB, queues *service.AdminQueueService, opts Options, st *tally) error {
	pending, err := loadPendingAccepts(db)
	if err != nil || len(pending) == 0 {
		return err
	}
	rows := make([]QueueVerdict, len(pending))
	for i, p := range pending {
		rows[i] = p.Row
	}
	sides, err := loadApplySides(db, rows)
	if err != nil {
		return err
	}
	names, err := loadPairEvidence(db, rows)
	if err != nil {
		return err
	}
	for _, p := range pending {
		row := p.Row
		if sides[row.AID].DeletedA || sides[row.BID].DeletedA {
			st.add(recheckMoved, 1)
			continue
		}
		plan := planFor(QueueWorkPair, row, sides, opts, false, applyEvidence{Pair: names[row.ID]})
		if plan.Action == applyAccept && plan.Source == p.Source && plan.Target == p.Target {
			st.add(recheckKept, 1)
			continue
		}
		key, stamp, to := recheckWithdrawn, stampWithdrawnOnRecheck, model.CandidateStatusNeedsManual
		reason := "recheck: current rules plan " + plan.stamp()
		if plan.Skip != "" {
			reason = "recheck: current rules skip " + plan.Skip
		}
		if plan.Action == applyAccept {
			key, stamp, to = recheckRedirected, stampRedirectedOnRecheck, model.CandidateStatusPending
			reason = fmt.Sprintf("recheck: current rules merge %d into %d", plan.Source, plan.Target)
		}
		if opts.DryRun {
			fmt.Printf("[dry] recheck %s queue=workpair id=%d proposal=%d %s a=%d b=%d conf=%.2f %s\n",
				key, row.ID, p.ProposalID, row.Verdict, row.AID, row.BID, row.Confidence, reason)
			st.add("would_"+key, 1)
			continue
		}
		err := queues.WithdrawWorkAccept(ctx, service.WorkAcceptWithdrawal{
			ProposalID: p.ProposalID, AID: row.AID, BID: row.BID,
			Actor: opts.Actor, ToStatus: to, Reason: reason,
		}, func(tx *gorm.DB) error {
			now := time.Now()
			return tx.Model(&QueueVerdict{}).Where("id = ? AND applied_action = ?", row.ID, applyAccept).
				Updates(map[string]any{"applied_action": stamp, "applied_at": now, "applied_by": opts.Actor}).Error
		})
		if err != nil {
			st.add(classifyApplyErr(err), 1)
			fmt.Printf("  ! recheck id=%d proposal=%d: %v\n", row.ID, p.ProposalID, err)
			continue
		}
		st.add(key, 1)
	}
	return nil
}

func loadPendingAccepts(db *gorm.DB) ([]pendingAccept, error) {
	var found []struct {
		QueueVerdict
		ProposalID int64 `gorm:"column:proposal_id"`
		Source     int64 `gorm:"column:source_entity_id"`
		Target     int64 `gorm:"column:target_entity_id"`
	}
	if err := db.Raw(`SELECT v.*, p.id AS proposal_id, p.source_entity_id, p.target_entity_id
		FROM catalog_merge_proposal p
		JOIN src_llm.queue_verdict v
		  ON v.id = substring(p.note FROM '^llm:queue-adjudicator ([0-9]+) ')::bigint
		 AND v.a_id = LEAST(p.source_entity_id, p.target_entity_id)
		 AND v.b_id = GREATEST(p.source_entity_id, p.target_entity_id)
		WHERE p.entity_type = ? AND p.status IN ? AND p.note LIKE 'llm:queue-adjudicator %'
		  AND v.queue = ? AND v.applied_action = ?
		ORDER BY v.id`,
		model.EntityTypeWork, []int16{model.ProposalStatusOpen, model.ProposalStatusApproved},
		QueueWorkPair, applyAccept).Scan(&found).Error; err != nil {
		return nil, err
	}
	out := make([]pendingAccept, len(found))
	for i, f := range found {
		out[i] = pendingAccept{Row: f.QueueVerdict, ProposalID: f.ProposalID, Source: f.Source, Target: f.Target}
	}
	return out, nil
}
