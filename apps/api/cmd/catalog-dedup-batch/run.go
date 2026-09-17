package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"
	"api/internal/platform/catalog/service"

	"gorm.io/gorm"
)

func entityTypeOf(class string) int16 {
	switch class {
	case classCreditName, classOrphanCreditName, classMixedCreditName:
		return model.EntityTypeCreditName
	case classPerson:
		return model.EntityTypePerson
	case classLabel:
		return model.EntityTypeLabel
	}
	return model.EntityTypeCharacter
}

// executableEntityTypes bounds what -mode execute will ever touch: a second
// safety net under the note tag, so a proposal of a type this binary has no
// class for is never executed by it even if it somehow carries a matching note.
// EVERY class added above must be added here too — a class that proposes but is
// missing from this list opens proposals that silently never execute (the label
// class did exactly that until step 175 extended the list).
var executableEntityTypes = []int16{
	model.EntityTypeCharacter, model.EntityTypeCreditName,
	model.EntityTypePerson, model.EntityTypeLabel,
}

func runDetect(db *gorm.DB, w io.Writer) error {
	charGroups, cs, err := detectCharacters(db)
	if err != nil {
		return err
	}
	creditGroups, ns, err := detectCreditNames(db)
	if err != nil {
		return err
	}
	orphanGroups, os, err := detectOrphanCreditNames(db)
	if err != nil {
		return err
	}
	mixedGroups, ms, err := detectMixedCreditNames(db)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "[detect] character: groups=%d pairs=%d  (skipped: dirty_buckets=%d bridged_components=%d; vndb_instance_detox=%d)\n",
		cs.charGroups, cs.charPairs, cs.charDirtyBkt, cs.charBridged, cs.charInstanceDetox)
	fmt.Fprintf(w, "[detect] credit_name: groups=%d pairs=%d\n", ns.creditGroups, ns.creditPairs)
	fmt.Fprintf(w, "[detect] orphan-creditname: groups=%d pairs=%d  (skipped: dirty_buckets=%d bridged_components=%d)\n",
		os.orphanGroups, os.orphanPairs, os.orphanDirtyBkt, os.orphanBridged)
	fmt.Fprintf(w, "[detect] mixed-creditname: groups=%d pairs=%d  (skipped: dirty_buckets=%d bridged_components=%d frozen=%d)\n",
		ms.mixedGroups, ms.mixedPairs, ms.mixedDirtyBkt, ms.mixedBridged, ms.mixedFrozen)

	fmt.Fprintln(w, "  character samples:")
	printSamples(w, charGroups, 5)
	fmt.Fprintln(w, "  credit_name samples:")
	printSamples(w, creditGroups, 5)
	fmt.Fprintln(w, "  orphan-creditname samples:")
	printSamples(w, orphanGroups, 5)
	fmt.Fprintln(w, "  mixed-creditname samples:")
	printSamples(w, mixedGroups, 5)
	printOrphanGroupContaining(w, orphanGroups, 14695, 47429)
	return nil
}

func printOrphanGroupContaining(w io.Writer, groups []mergeGroup, a, b int64) {
	for _, g := range groups {
		ids := append([]int64{g.survivor}, g.sources...)
		var seenA, seenB bool
		for _, id := range ids {
			seenA = seenA || id == a
			seenB = seenB || id == b
		}
		if seenA && seenB {
			fmt.Fprintf(w, "  trigger group (%d↔%d): %s\n", a, b, g.sample)
			return
		}
	}
	fmt.Fprintf(w, "  trigger group (%d↔%d): NOT FOUND\n", a, b)
}

func printSamples(w io.Writer, groups []mergeGroup, n int) {
	for i, g := range groups {
		if i >= n {
			break
		}
		fmt.Fprintf(w, "    %s %s\n", g.class, g.sample)
	}
}

type proposeStats struct {
	groups, pairs, proposals, approved, skipped, decided, errs int
}

// decidedStatuses are the proposal states that already carry a decision on a
// pair. uq_catalog_merge_proposal_open only covers status 0 and ApproveMerge
// moves a proposal off it at once, so without this check a worklist proposed
// on a schedule files a second proposal for every pair still cooling, and
// re-files every pair a reviewer rejected.
var decidedStatuses = []int16{
	model.ProposalStatusApproved, model.ProposalStatusRejected, model.ProposalStatusWithdrawn,
}

// pairState resolves both ends and reports whether they are already one entity
// or the pair, in either direction, already has a decided proposal.
func pairState(ctx context.Context, db *gorm.DB, resolve *service.ResolveService,
	entityType int16, src, dst int64) (same, decided bool, err error) {
	rs, _, err := resolve.Resolve(ctx, entityType, src)
	if err != nil {
		return false, false, err
	}
	rd, _, err := resolve.Resolve(ctx, entityType, dst)
	if err != nil {
		return false, false, err
	}
	if rs == rd {
		return true, false, nil
	}
	var n int64
	err = db.WithContext(ctx).Model(&model.CatalogMergeProposal{}).
		Where(`entity_type = ? AND status IN ? AND
			((source_entity_id = ? AND target_entity_id = ?) OR (source_entity_id = ? AND target_entity_id = ?))`,
			entityType, decidedStatuses, rs, rd, rd, rs).
		Count(&n).Error
	return false, n > 0, err
}

func runPropose(ctx context.Context, db *gorm.DB, w io.Writer, merge *service.MergeService,
	actor int64, class, worklist, noteOverride string, limit int, run bool) error {
	groups, err := collectGroups(db, class, worklist)
	if err != nil {
		return err
	}
	if limit > 0 && limit < len(groups) {
		groups = groups[:limit]
	}
	note := noteTagFor(worklist, noteOverride)
	resolve := service.NewResolveService(repository.NewRedirectRepository(db))

	var st proposeStats
	for _, g := range groups {
		st.groups++
		et := entityTypeOf(g.class)
		for _, src := range g.sources {
			st.pairs++
			same, decided, err := pairState(ctx, db, resolve, et, src, g.survivor)
			switch {
			case err != nil:
				fmt.Fprintf(w, "  %s %d→%d: resolve ERROR %v\n", g.class, src, g.survivor, err)
				st.errs++
				continue
			case same:
				st.skipped++
				continue
			case decided:
				st.decided++
				continue
			}
			if !run {
				st.proposals++
				continue
			}
			p, err := merge.ProposeMerge(ctx, et, src, g.survivor, actor, note)
			if err != nil {
				if errors.Is(err, service.ErrSameEntity) || errors.Is(err, service.ErrDuplicateOpenProposal) {
					st.skipped++
					continue
				}
				fmt.Fprintf(w, "  %s %d→%d: propose ERROR %v\n", g.class, src, g.survivor, err)
				st.errs++
				continue
			}
			if err := merge.ApproveMerge(ctx, p.ID, actor); err != nil {
				fmt.Fprintf(w, "  %s proposal %d: approve ERROR %v\n", g.class, p.ID, err)
				st.errs++
				continue
			}
			st.proposals++
			st.approved++
		}
	}
	mode := "DRY-RUN (pass -run to propose+approve)"
	if run {
		mode = "APPLIED"
	}
	scope := "class=" + class
	if worklist != "" {
		scope = "worklist=" + worklist
	}
	fmt.Fprintf(w, "%s [propose] %s note=%s groups=%d pairs=%d proposals=%d approved=%d skipped=%d decided=%d errors=%d\n",
		mode, scope, note, st.groups, st.pairs, st.proposals, st.approved, st.skipped, st.decided, st.errs)
	if st.errs > 0 {
		return fmt.Errorf("%d pairs failed to propose/approve", st.errs)
	}
	return nil
}

func collectGroups(db *gorm.DB, class, worklist string) ([]mergeGroup, error) {
	if worklist != "" {
		return loadWorklist(worklist)
	}
	var out []mergeGroup
	if class == classCharacter || class == "both" {
		g, _, err := detectCharacters(db)
		if err != nil {
			return nil, err
		}
		out = append(out, g...)
	}
	if class == classCreditName || class == "both" {
		g, _, err := detectCreditNames(db)
		if err != nil {
			return nil, err
		}
		out = append(out, g...)
	}
	if class == classOrphanCreditName {
		g, _, err := detectOrphanCreditNames(db)
		if err != nil {
			return nil, err
		}
		out = append(out, g...)
	}
	if class == classMixedCreditName {
		g, _, err := detectMixedCreditNames(db)
		if err != nil {
			return nil, err
		}
		out = append(out, g...)
	}
	return out, nil
}

func runExecute(ctx context.Context, db *gorm.DB, w io.Writer, merge *service.MergeService,
	resolve *service.ResolveService, actor int64, note string, limit int, run bool) error {
	var props []model.CatalogMergeProposal
	q := db.WithContext(ctx).
		Where("status = ? AND execute_after <= now() AND note LIKE ? AND entity_type IN ?",
			model.ProposalStatusApproved, "%"+note+"%", executableEntityTypes).
		Order("id")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Find(&props).Error; err != nil {
		return err
	}
	var executed, superseded, residual, errs int
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
			if run {
				if err := merge.ExecuteMerge(ctx, p.ID, &actor); err != nil {
					fmt.Fprintf(w, "  proposal %d: execute ERROR %v\n", p.ID, err)
					errs++
					continue
				}
			}
			executed++
		}
	}
	mode := "DRY-RUN (pass -run to execute)"
	if run {
		mode = "APPLIED"
	}
	fmt.Fprintf(w, "%s [execute] cooled=%d executed=%d chain_superseded=%d chain_residual=%d errors=%d\n",
		mode, len(props), executed, superseded, residual, errs)
	if errs > 0 {
		return fmt.Errorf("%d executions failed", errs)
	}
	return nil
}

const cleanupMatch = `e.character_id IS NULL
	AND e.role_id = (SELECT id FROM catalog_role WHERE key = 'voice-actor')
	AND EXISTS (SELECT 1 FROM catalog_credit d
	             WHERE d.work_id = e.work_id AND d.credit_name_id = e.credit_name_id
	               AND d.role_id = e.role_id AND d.character_id IS NOT NULL)`

// A machine deduplicator may not delete a hand-written row. There is no receipt
// for it anywhere — no revision, no rejection row — so the person would re-add
// it and watch it disappear again. The curated rows it steps around are counted
// and printed instead of being passed over in silence.
var (
	cleanupScope   = cleanupMatch + ` AND ` + editspec.NotCuratedLaneSQL("e.source_id")
	cleanupCurated = cleanupMatch + ` AND ` + editspec.CuratedLaneSQL("e.source_id")
)

func runCleanup(db *gorm.DB, w io.Writer, run bool) error {
	var redundant, keptCurated int64
	if err := db.Raw(`SELECT count(*) FROM catalog_credit e WHERE ` + cleanupScope).Row().Scan(&redundant); err != nil {
		return err
	}
	if err := db.Raw(`SELECT count(*) FROM catalog_credit e WHERE ` + cleanupCurated).Row().Scan(&keptCurated); err != nil {
		return err
	}
	var samples []struct {
		ID           int64  `gorm:"column:id"`
		WorkID       int64  `gorm:"column:work_id"`
		CreditNameID int64  `gorm:"column:credit_name_id"`
		Name         string `gorm:"column:name"`
	}
	if err := db.Raw(`SELECT e.id, e.work_id, e.credit_name_id, cn.name
		FROM catalog_credit e JOIN catalog_credit_name cn ON cn.id = e.credit_name_id
		WHERE ` + cleanupScope + `
		ORDER BY e.work_id, e.id LIMIT 5`).Scan(&samples).Error; err != nil {
		return err
	}
	fmt.Fprintf(w, "[cleanup] redundant empty-role VA credits: %d (kept_curated=%d)\n", redundant, keptCurated)
	for _, s := range samples {
		fmt.Fprintf(w, "    credit id=%d work=%d cn=%d name=%q\n", s.ID, s.WorkID, s.CreditNameID, s.Name)
	}
	if !run {
		fmt.Fprintf(w, "DRY-RUN (pass -run to delete) [cleanup] would_delete=%d kept_curated=%d\n", redundant, keptCurated)
		return nil
	}
	res := db.Exec(`DELETE FROM catalog_credit e WHERE ` + cleanupScope)
	if res.Error != nil {
		return res.Error
	}
	fmt.Fprintf(w, "APPLIED [cleanup] deleted=%d kept_curated=%d\n", res.RowsAffected, keptCurated)
	return nil
}
