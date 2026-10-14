package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/service"

	"gorm.io/gorm"
)

func runExecute(ctx context.Context, db *gorm.DB, w io.Writer, merge *service.MergeService,
	resolve *service.ResolveService, actor int64, note string, limit int, run bool) error {
	var props []model.CatalogMergeProposal
	q := db.WithContext(ctx).
		Where("status = ? AND execute_after <= now() AND note LIKE ? AND entity_type = ?",
			model.ProposalStatusApproved, "%"+note+"%", model.EntityTypeWork).
		Order("id")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Find(&props).Error; err != nil {
		return err
	}
	var executed, superseded, residual, held, errs int
	for _, p := range props {
		rsrc, srcMoved, err := resolve.Resolve(ctx, p.EntityType, p.SourceEntityID)
		if err != nil {
			fmt.Fprintf(w, "  proposal %d: resolve source ERROR %v\n", p.ID, err)
			errs++
			continue
		}
		rtgt, tgtMoved, err := resolve.Resolve(ctx, p.EntityType, p.TargetEntityID)
		if err != nil {
			fmt.Fprintf(w, "  proposal %d: resolve target ERROR %v\n", p.ID, err)
			errs++
			continue
		}
		switch {
		case rsrc == rtgt:
			superseded++
			if run {
				if err := merge.RejectMerge(ctx, p.ID, actor, fmt.Sprintf(
					"chain-superseded: both endpoints resolve to %d — merged by proxy earlier this wave", rsrc)); err != nil {
					fmt.Fprintf(w, "  proposal %d: reject ERROR %v\n", p.ID, err)
					superseded--
					errs++
				}
			}
		case srcMoved || tgtMoved:
			residual++
			if run {
				if err := merge.RejectMerge(ctx, p.ID, actor, fmt.Sprintf(
					"chain-residual: endpoints moved (%d→%d, %d→%d) — re-covered by the next detect pass on live rows",
					p.SourceEntityID, rsrc, p.TargetEntityID, rtgt)); err != nil {
					fmt.Fprintf(w, "  proposal %d: reject ERROR %v\n", p.ID, err)
					residual--
					errs++
				}
			}
		default:
			check := func() error { return merge.CheckSurvivor(ctx, p.EntityType, rsrc, rtgt) }
			if run {
				check = func() error { return merge.ExecuteMerge(ctx, p.ID, &actor) }
			}
			if err := check(); err != nil {
				if errors.Is(err, service.ErrHiddenSurvivor) {
					held++
					fmt.Fprintf(w, "  proposal %d: HELD approved, %v\n", p.ID, err)
					continue
				}
				fmt.Fprintf(w, "  proposal %d: execute ERROR %v\n", p.ID, err)
				errs++
				continue
			}
			executed++
		}
	}
	mode := "DRY-RUN (pass -run to execute)"
	if run {
		mode = "APPLIED"
	}
	fmt.Fprintf(w, "%s [execute] note=%s cooled=%d executed=%d chain_superseded=%d chain_residual=%d hidden_survivor_held=%d errors=%d\n",
		mode, note, len(props), executed, superseded, residual, held, errs)
	if errs > 0 {
		return fmt.Errorf("%d executions failed", errs)
	}
	return nil
}
