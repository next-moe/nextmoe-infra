package main

import (
	"context"
	stderrors "errors"
	"fmt"
	"io"
	"sort"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"
	"api/internal/platform/catalog/service"

	"gorm.io/gorm"
)

const matchedByCurated = "curated"

type ruleStat struct {
	ToPromote int
	Promoted  int
	Already   int
}

type promoteStats struct {
	Rules    map[string]*ruleStat
	Promoted int
	Already  int
	Conflict int
	NotFound int
	Errors   int
}

type refRow struct {
	EntityID   int64  `gorm:"column:entity_id"`
	SourceID   int16  `gorm:"column:source_id"`
	ExternalID string `gorm:"column:external_id"`
	MatchedBy  string `gorm:"column:matched_by"`
	LinkKind   int16  `gorm:"column:link_kind"`
}

func runPromote(ctx context.Context, db *gorm.DB, w io.Writer, rules []string, actor int64, apply bool, limit int) (promoteStats, error) {
	for _, r := range rules {
		if r == matchedByCurated {
			return promoteStats{}, fmt.Errorf("curated is the human lane, not a promotion rule")
		}
	}
	st := promoteStats{Rules: map[string]*ruleStat{}}
	for _, r := range rules {
		st.Rules[r] = &ruleStat{}
	}

	q := db.WithContext(ctx).Raw(`SELECT entity_id, source_id, external_id, matched_by, link_kind
		FROM catalog_external_ref
		WHERE entity_type = ? AND matched_by IN ? AND link_kind IN (?, ?)
		ORDER BY matched_by, entity_id, source_id, external_id`,
		model.EntityTypeWork, rules, model.LinkKindProbable, model.LinkKindExact)
	if limit > 0 {
		q = db.WithContext(ctx).Raw(`SELECT entity_id, source_id, external_id, matched_by, link_kind
			FROM catalog_external_ref
			WHERE entity_type = ? AND matched_by IN ? AND link_kind IN (?, ?)
			ORDER BY matched_by, entity_id, source_id, external_id LIMIT ?`,
			model.EntityTypeWork, rules, model.LinkKindProbable, model.LinkKindExact, limit)
	}
	var rows []refRow
	if err := q.Scan(&rows).Error; err != nil {
		return st, err
	}

	queues := adminQueue(db)
	shown := 0
	for _, r := range rows {
		rs := st.Rules[r.MatchedBy]
		if r.LinkKind == model.LinkKindExact {
			rs.Already++
			st.Already++
			continue
		}
		rs.ToPromote++

		if !apply {
			if shown < 10 {
				fmt.Fprintf(w, "  [would promote] %-24s work=%d %s:%s\n", ruleShort(r.MatchedBy), r.EntityID, srcName(r.SourceID), r.ExternalID)
				shown++
			}
			continue
		}

		err := queues.ConfirmRef(ctx, service.RefKey{
			EntityType: model.EntityTypeWork, EntityID: r.EntityID,
			SourceID: r.SourceID, ExternalID: r.ExternalID,
		}, actor)
		switch {
		case err == nil:
			rs.Promoted++
			st.Promoted++
		case stderrors.Is(err, service.ErrProposalState):
			rs.Already++
			st.Already++
		case stderrors.Is(err, service.ErrExactTaken):
			st.Conflict++
			fmt.Fprintf(w, "  ! conflict: %s work=%d %s:%s — %v\n", ruleShort(r.MatchedBy), r.EntityID, srcName(r.SourceID), r.ExternalID, err)
		case stderrors.Is(err, service.ErrNotFound):
			st.NotFound++
		default:
			st.Errors++
			fmt.Fprintf(w, "  ! error: work=%d %s:%s — %v\n", r.EntityID, srcName(r.SourceID), r.ExternalID, err)
		}
		if apply && st.Promoted%5000 == 0 && st.Promoted > 0 {
			fmt.Fprintf(w, "  … promoted %d\n", st.Promoted)
		}
	}

	mode := "DRY-RUN (nothing written; pass --run to promote)"
	if apply {
		mode = "APPLIED"
	}
	fmt.Fprintf(w, "%s — total to_promote=%d promoted=%d already_exact=%d conflict=%d not_found=%d errors=%d\n",
		mode, sumToPromote(st), st.Promoted, st.Already, st.Conflict, st.NotFound, st.Errors)
	for _, name := range sortedRules(st.Rules) {
		rs := st.Rules[name]
		fmt.Fprintf(w, "  · %-24s to_promote=%d promoted=%d already_exact=%d\n", ruleShort(name), rs.ToPromote, rs.Promoted, rs.Already)
	}
	return st, nil
}

func sumToPromote(st promoteStats) int {
	n := 0
	for _, rs := range st.Rules {
		n += rs.ToPromote
	}
	return n
}

func sortedRules(m map[string]*ruleStat) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func ruleShort(matchedBy string) string {
	switch matchedBy {
	case "rule:eg-vndb-rosetta":
		return "rosetta"
	case "rule:title-year-strict":
		return "title-year-strict"
	default:
		return matchedBy
	}
}

func srcName(id int16) string {
	switch id {
	case 2:
		return "vndb"
	case 3:
		return "bangumi"
	case 5:
		return "eg"
	default:
		return fmt.Sprintf("src%d", id)
	}
}

func adminQueue(db *gorm.DB) *service.AdminQueueService {
	resolve := service.NewResolveService(repository.NewRedirectRepository(db))
	merge := service.NewMergeService(db, resolve)
	return service.NewAdminQueueService(db, merge)
}
