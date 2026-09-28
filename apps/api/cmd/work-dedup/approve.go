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

	var approved, conflicted, crossMedium, superseded, errs int
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

		// The census stopped pairing across mediums on 2026-09-02, but a
		// hand-filed pair still reached execute: on 2026-09-19 an OVA was
		// merged into the game it adapts. Twelve such merges were unmerged on
		// 2026-09-28 (novels, a manga and that OVA folded into galgames, and a
		// game into a novel). A same-product pair filed under two mediums gets
		// its medium fixed first; then it passes here.
		mediums, err := differentMediums(ctx, db, src, tgt)
		if err != nil {
			fmt.Fprintf(w, "  proposal %d: medium screen ERROR %v\n", p.ID, err)
			errs++
			continue
		}
		if mediums != "" {
			crossMedium++
			if run {
				if err := merge.RejectMerge(ctx, p.ID, actor, "cross-medium: "+mediums); err != nil {
					fmt.Fprintf(w, "  proposal %d: reject ERROR %v\n", p.ID, err)
					crossMedium--
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
	fmt.Fprintf(w, "%s [approve] note=%s open=%d approved=%d ref_conflict=%d cross_medium=%d superseded=%d errors=%d\n",
		mode, note, len(props), approved, conflicted, crossMedium, superseded, errs)
	if errs > 0 {
		return fmt.Errorf("%d approvals failed", errs)
	}
	return nil
}

func differentMediums(ctx context.Context, db *gorm.DB, a, b int64) (string, error) {
	var rows []struct {
		ID  int64  `gorm:"column:id"`
		Key string `gorm:"column:key"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT w.id, m.key FROM catalog_work w JOIN catalog_medium m ON m.id = w.medium_id
		WHERE w.id IN ?`, []int64{a, b}).Scan(&rows).Error; err != nil {
		return "", err
	}
	if len(rows) != 2 || rows[0].Key == rows[1].Key {
		return "", nil
	}
	by := map[int64]string{rows[0].ID: rows[0].Key, rows[1].ID: rows[1].Key}
	return fmt.Sprintf("%s %d vs %s %d", by[a], a, by[b], b), nil
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
		LIMIT 1`, b, entityType, a, model.IdentityVetoExemptSourceIDsFor(entityType)).Scan(&row).Error
	if err != nil || row.SourceKey == "" {
		return "", err
	}
	return fmt.Sprintf("%s %s vs %s", row.SourceKey, row.AExt, row.BExt), nil
}
