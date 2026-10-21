package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/service"

	"gorm.io/gorm"
)

const (
	maxRedirectHops = 16
	noteNormRunes   = 40
)

type proposeStats struct {
	proposed, approved, superseded, needsManual, leftPending, skipped, errs int
}

func runPropose(ctx context.Context, db *gorm.DB, w io.Writer, merge *service.MergeService,
	actor int64, note string, limit int, run bool) error {
	c, err := buildCensus(ctx, db)
	if err != nil {
		return err
	}
	return proposeFromCensus(ctx, db, w, c, merge, actor, note, limit, run)
}

func proposeFromCensus(ctx context.Context, db *gorm.DB, w io.Writer, c *census,
	merge *service.MergeService, actor int64, note string, limit int, run bool) error {
	verdicts := c.verdictByPair()
	rows := c.rowByPair()
	groups := map[int64]*mergeGroup{}
	for i := range c.groups {
		g := &c.groups[i]
		groups[g.survivor] = g
		for _, src := range g.sources {
			groups[src] = g
		}
	}
	redirects, err := loadWorkRedirects(ctx, db)
	if err != nil {
		return err
	}
	var cands []model.CatalogMatchCandidate
	if err := db.WithContext(ctx).
		Where("entity_type = ? AND status = ?", model.EntityTypeWork, model.CandidateStatusPending).
		Order("a_id, b_id").Find(&cands).Error; err != nil {
		return err
	}

	var st proposeStats
	flip := func(a, b int64, status int16) {
		if !run {
			return
		}
		if err := flipWorkCandidate(ctx, db, a, b, status, actor); err != nil {
			fmt.Fprintf(w, "  candidate (%d,%d): flip ERROR %v\n", a, b, err)
			st.errs++
		}
	}
	for _, cand := range cands {
		key := [2]int64{cand.AID, cand.BID}
		if resolveWork(redirects, cand.AID) == resolveWork(redirects, cand.BID) {
			st.superseded++
			flip(cand.AID, cand.BID, model.CandidateStatusAccepted)
			continue
		}
		// A pair that has dropped out of the census is NOT auto-rejected: the
		// title edit that dissolved the collision may itself be the mistake,
		// and a rejected candidate never comes back on its own.
		if b, ok := verdicts[key]; !ok || b != bucketAuto {
			st.needsManual++
			flip(cand.AID, cand.BID, model.CandidateStatusNeedsManual)
			continue
		}
		g := groups[cand.AID]
		if g == nil || groups[cand.BID] != g || (cand.AID != g.survivor && cand.BID != g.survivor) {
			st.leftPending++
			continue
		}
		if limit > 0 && st.proposed >= limit {
			st.leftPending++
			continue
		}
		member := cand.AID
		if member == g.survivor {
			member = cand.BID
		}
		full := fmt.Sprintf("%s: norm=%s ev=%s", note,
			truncateRunes(rows[key].SharedNorm, noteNormRunes), pairEvidence(rows[key]))
		if !run {
			fmt.Fprintf(w, "PLAN %d <- %d  %s\n", g.survivor, member, full)
			st.proposed++
			st.approved++
			continue
		}
		p, err := merge.ProposeMerge(ctx, model.EntityTypeWork, member, g.survivor, actor, full)
		if err != nil {
			if errors.Is(err, service.ErrSameEntity) || errors.Is(err, service.ErrDuplicateOpenProposal) {
				st.skipped++
				flip(cand.AID, cand.BID, model.CandidateStatusAccepted)
				continue
			}
			fmt.Fprintf(w, "  %d->%d: propose ERROR %v\n", member, g.survivor, err)
			st.errs++
			continue
		}
		st.proposed++
		if err := merge.ApproveMerge(ctx, p.ID, actor); err != nil {
			fmt.Fprintf(w, "  proposal %d: approve ERROR %v\n", p.ID, err)
			st.errs++
			continue
		}
		st.approved++
		flip(cand.AID, cand.BID, model.CandidateStatusAccepted)
	}

	mode := "DRY-RUN (pass -run to propose+approve)"
	if run {
		mode = "APPLIED"
	}
	fmt.Fprintf(w, "%s [propose] note=%s pending=%d proposed=%d approved=%d superseded=%d needs_manual=%d left_pending=%d skipped=%d errors=%d\n",
		mode, note, len(cands), st.proposed, st.approved, st.superseded, st.needsManual, st.leftPending, st.skipped, st.errs)
	if st.errs > 0 {
		return fmt.Errorf("%d candidates failed to propose/approve", st.errs)
	}
	return nil
}

func flipWorkCandidate(ctx context.Context, db *gorm.DB, a, b int64, status int16, actor int64) error {
	return db.WithContext(ctx).Exec(`UPDATE catalog_match_candidate
		SET status = ?, decided_by = ?, decided_at = now()
		WHERE entity_type = ? AND a_id = ? AND b_id = ? AND status = ?`,
		status, actor, model.EntityTypeWork, a, b, model.CandidateStatusPending).Error
}

func loadWorkRedirects(ctx context.Context, db *gorm.DB) (map[int64]int64, error) {
	var rows []struct {
		OldID     int64 `gorm:"column:old_id"`
		CurrentID int64 `gorm:"column:current_id"`
	}
	if err := db.WithContext(ctx).Raw(
		`SELECT old_id, current_id FROM catalog_redirect WHERE entity_type = ? AND revoked_at IS NULL`,
		model.EntityTypeWork).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]int64, len(rows))
	for _, r := range rows {
		out[r.OldID] = r.CurrentID
	}
	return out, nil
}

func resolveWork(redirects map[int64]int64, id int64) int64 {
	for range maxRedirectHops {
		next, ok := redirects[id]
		if !ok || next == id {
			return id
		}
		id = next
	}
	return id
}

func pairEvidence(r pairRow) string {
	var parts []string
	if r.RefOverlap {
		parts = append(parts, "ref")
	}
	if r.RefOverlapCI && !r.RefOverlap {
		parts = append(parts, "ref_ci")
	}
	if datesNear(r.DateA, r.DateB) {
		parts = append(parts, fmt.Sprintf("date(%s~%s)", dateStr(r.DateA), dateStr(r.DateB)))
	}
	if r.LabelOverlap {
		parts = append(parts, "label")
	}
	if r.SharedNorms >= 2 {
		parts = append(parts, fmt.Sprintf("norms=%d", r.SharedNorms))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, "+")
}

func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}
