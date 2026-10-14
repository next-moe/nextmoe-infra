package main

import (
	"context"
	stderrors "errors"
	"fmt"
	"io"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"
	"api/internal/platform/catalog/service"

	"gorm.io/gorm"
)

const (
	ruleSetCoCredit = "cocredit"
	coCreditNote    = "rule:person-cocredit"
)

type coCreditStats struct {
	Attached, Ambiguous, Weak, AlreadyLinked int
	MergeApproved, MergeProposed             int
	RefConflict, ProposalExists              int
	Executed, Errors                         int
}

// runCoCredit settles the people two sources spell differently on one work: an
// orphan name joins the person holding its spelling there, and two people who
// both hold it become a merge. Display names that fold equal are approved and
// executed after the cooling-off like any approved merge; the rest wait in the
// console as open proposals.
func runCoCredit(ctx context.Context, db *gorm.DB, w io.Writer, actor int64, apply bool) (coCreditStats, error) {
	var st coCreditStats
	link := service.NewLinkService(db)
	merge := service.NewMergeService(db, service.NewResolveService(repository.NewRedirectRepository(db)))

	if apply {
		if err := executeCooledCoCredit(ctx, db, w, merge, actor, &st); err != nil {
			return st, err
		}
	}

	plan, err := link.CoCreditAttachPlan(ctx)
	if err != nil {
		return st, err
	}
	for _, a := range plan {
		if a.Hosts > 1 {
			st.Ambiguous++
			continue
		}
		if !a.Strong {
			st.Weak++
			continue
		}
		if !apply {
			st.Attached++
			fmt.Fprintf(w, "[attach] %q (cn %d) → P%d works=%d\n", a.Name, a.CreditNameID, a.PersonID, a.Works)
			continue
		}
		ok, err := link.AttachName(ctx, a.CreditNameID, a.PersonID, &actor)
		switch {
		case err != nil:
			st.Errors++
			fmt.Fprintf(w, "  ! attach cn %d → P%d: %v\n", a.CreditNameID, a.PersonID, err)
		case ok:
			st.Attached++
		default:
			st.AlreadyLinked++
		}
	}

	pairs, err := link.CoCreditPersonPairs(ctx)
	if err != nil {
		return st, err
	}
	for _, p := range pairs {
		switch {
		case p.ProposalExists:
			st.ProposalExists++
			continue
		case p.RefConflict:
			st.RefConflict++
			fmt.Fprintf(w, "[conflict] %s\n", p)
			continue
		}
		src, dst := p.SurvivorTarget()
		verb := "propose"
		if p.SameDisplay {
			verb = "approve"
		}
		fmt.Fprintf(w, "[%s] %s: P%d → P%d\n", verb, p, src, dst)
		if !apply {
			if p.SameDisplay {
				st.MergeApproved++
			} else {
				st.MergeProposed++
			}
			continue
		}
		prop, err := merge.ProposeMerge(ctx, model.EntityTypePerson, src, dst, actor,
			fmt.Sprintf("%s: same name credited on %d shared works", coCreditNote, p.Works))
		if err == nil && p.SameDisplay {
			err = merge.ApproveMerge(ctx, prop.ID, actor)
		}
		switch {
		case stderrors.Is(err, service.ErrDuplicateOpenProposal):
			st.ProposalExists++
		case err != nil:
			st.Errors++
			fmt.Fprintf(w, "  ! merge P%d → P%d: %v\n", src, dst, err)
		case p.SameDisplay:
			st.MergeApproved++
		default:
			st.MergeProposed++
		}
	}

	mode := "DRY-RUN (nothing written; pass --run to apply)"
	if apply {
		mode = "APPLIED"
	}
	fmt.Fprintf(w, "%s [cocredit] — attached=%d ambiguous=%d weak=%d already_linked=%d | merge_approved=%d merge_proposed=%d ref_conflict=%d proposal_exists=%d | executed=%d errors=%d\n",
		mode, st.Attached, st.Ambiguous, st.Weak, st.AlreadyLinked, st.MergeApproved, st.MergeProposed,
		st.RefConflict, st.ProposalExists, st.Executed, st.Errors)
	return st, nil
}

func executeCooledCoCredit(ctx context.Context, db *gorm.DB, w io.Writer, merge *service.MergeService, actor int64, st *coCreditStats) error {
	var ids []int64
	if err := db.WithContext(ctx).Model(&model.CatalogMergeProposal{}).
		Where("entity_type = ? AND status = ? AND execute_after <= now() AND note LIKE ?",
			model.EntityTypePerson, model.ProposalStatusApproved, coCreditNote+"%").
		Order("id").Pluck("id", &ids).Error; err != nil {
		return err
	}
	for _, id := range ids {
		if err := merge.ExecuteMerge(ctx, id, &actor); err != nil {
			st.Errors++
			fmt.Fprintf(w, "  ! execute proposal %d: %v\n", id, err)
			continue
		}
		st.Executed++
	}
	return nil
}
