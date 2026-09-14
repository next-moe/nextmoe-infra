package main

import (
	"context"
	"fmt"
	"io"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/service"

	"gorm.io/gorm"
)

// runApprove moves open proposals carrying the note tag to approved, so that
// -mode execute can pick them up. The judge that files them (llm-suggest
// -mode apply) only ever creates them open, which is why an unattended lane
// needs this step to exist at all.
func runApprove(ctx context.Context, db *gorm.DB, w io.Writer, merge *service.MergeService,
	resolve *service.ResolveService, actor int64, note string, limit int, run bool) error {
	var props []model.CatalogMergeProposal
	q := db.WithContext(ctx).
		Where("status = ? AND note LIKE ? AND entity_type = ?",
			model.ProposalStatusOpen, "%"+note+"%", model.EntityTypeWork).
		Order("id")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Find(&props).Error; err != nil {
		return err
	}

	var approved, conflicted, superseded, errs int
	for _, p := range props {
		src, _, err := resolve.Resolve(ctx, p.EntityType, p.SourceEntityID)
		if err != nil {
			fmt.Fprintf(w, "  proposal %d: resolve source ERROR %v\n", p.ID, err)
			errs++
			continue
		}
		tgt, _, err := resolve.Resolve(ctx, p.EntityType, p.TargetEntityID)
		if err != nil {
			fmt.Fprintf(w, "  proposal %d: resolve target ERROR %v\n", p.ID, err)
			errs++
			continue
		}
		if src == tgt {
			superseded++
			continue
		}

		// The screen runs on the RESOLVED endpoints, not the ids the proposal
		// was filed with: a proposal whose source has since been merged away
		// lands on the survivor, and the pair that actually gets touched is the
		// one that has to be clean.
		conflict, err := contradictingExactRef(ctx, db, p.EntityType, src, tgt)
		if err != nil {
			fmt.Fprintf(w, "  proposal %d: ref screen ERROR %v\n", p.ID, err)
			errs++
			continue
		}
		if conflict != "" {
			conflicted++
			if run {
				if err := merge.RejectMerge(ctx, p.ID, actor, "ref-conflict: "+conflict); err != nil {
					fmt.Fprintf(w, "  proposal %d: reject ERROR %v\n", p.ID, err)
					conflicted--
					errs++
				}
			}
			continue
		}

		if run {
			if err := merge.ApproveMerge(ctx, p.ID, actor); err != nil {
				fmt.Fprintf(w, "  proposal %d: approve ERROR %v\n", p.ID, err)
				errs++
				continue
			}
		}
		approved++
	}

	mode := "DRY-RUN (pass -run to approve)"
	if run {
		mode = "APPLIED"
	}
	fmt.Fprintf(w, "%s [approve] note=%s open=%d approved=%d ref_conflict=%d superseded=%d errors=%d\n",
		mode, note, len(props), approved, conflicted, superseded, errs)
	if errs > 0 {
		return fmt.Errorf("%d approvals failed", errs)
	}
	return nil
}

// contradictingExactRef returns a human-readable description of one exact-tier
// disagreement between the two entities, or "" when they do not contradict.
// Two entities can never corroborate through exact refs — the partial unique
// uq_catalog_external_ref_exact makes sharing one impossible — so disagreement
// is the only signal this tier can carry.
func contradictingExactRef(ctx context.Context, db *gorm.DB, entityType int16, a, b int64) (string, error) {
	var row struct {
		SourceKey string `gorm:"column:source_key"`
		AExt      string `gorm:"column:a_ext"`
		BExt      string `gorm:"column:b_ext"`
	}
	err := db.WithContext(ctx).Raw(`
		SELECT cs.key AS source_key, ea.external_id AS a_ext, eb.external_id AS b_ext
		FROM catalog_external_ref ea
		JOIN catalog_external_ref eb
		  ON eb.entity_type = ea.entity_type
		 AND eb.source_id   = ea.source_id
		 AND eb.link_kind   = 0
		 AND eb.dead_at IS NULL
		 AND eb.entity_id   = ?
		 AND eb.external_id <> ea.external_id
		JOIN catalog_source cs ON cs.id = ea.source_id
		WHERE ea.entity_type = ?
		  AND ea.entity_id   = ?
		  AND ea.link_kind   = 0
		  AND ea.dead_at IS NULL
		  AND ea.source_id NOT IN ?
		ORDER BY cs.trust_tier, cs.id
		LIMIT 1`, b, entityType, a, model.IdentityVetoExemptSourceIDs).Scan(&row).Error
	if err != nil || row.SourceKey == "" {
		return "", err
	}
	return fmt.Sprintf("%s %s vs %s", row.SourceKey, row.AExt, row.BExt), nil
}
