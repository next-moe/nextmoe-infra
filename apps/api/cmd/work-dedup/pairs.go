package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/service"

	"gorm.io/gorm"
)

type workPair struct {
	source   int64
	target   int64
	evidence string
}

func runPairs(ctx context.Context, db *gorm.DB, w io.Writer, merge *service.MergeService,
	resolve *service.ResolveService, actor int64, note, path string, run bool) error {
	pairs, err := loadPairs(path)
	if err != nil {
		return err
	}
	if err := guardPairsNote(note); err != nil {
		return err
	}

	var proposed, skippedExisting, moved, failed int
	summary := func() {
		mode := "DRY-RUN (pass -run to file)"
		if run {
			mode = "APPLIED"
		}
		fmt.Fprintf(w, "%s [pairs] note=%s rows=%d proposed=%d skipped_existing=%d moved=%d failed=%d\n",
			mode, note, len(pairs), proposed, skippedExisting, moved, failed)
	}

	// Every row is checked before any is filed: the first cut filed the rows
	// ahead of a moved one and then failed, leaving half a stale worklist open.
	var todo []workPair
	for _, pair := range pairs {
		var existing []model.CatalogMergeProposal
		if err := db.WithContext(ctx).
			Where("entity_type = ? AND source_entity_id = ? AND target_entity_id = ? AND status IN ?",
				model.EntityTypeWork, pair.source, pair.target,
				[]int16{model.ProposalStatusOpen, model.ProposalStatusApproved, model.ProposalStatusExecuted,
					model.ProposalStatusRejected}).
			Order("id").
			Limit(1).
			Find(&existing).Error; err != nil {
			return fmt.Errorf("%d->%d: lookup: %w", pair.source, pair.target, err)
		}
		if len(existing) > 0 {
			skippedExisting++
			fmt.Fprintf(w, "SKIP existing proposal #%d (%s) %d <- %d\n",
				existing[0].ID, pairsProposalStatus(existing[0].Status), pair.target, pair.source)
			continue
		}

		src, srcMoved, err := resolve.Resolve(ctx, model.EntityTypeWork, pair.source)
		if err != nil {
			return fmt.Errorf("%d->%d: resolve source: %w", pair.source, pair.target, err)
		}
		tgt, tgtMoved, err := resolve.Resolve(ctx, model.EntityTypeWork, pair.target)
		if err != nil {
			return fmt.Errorf("%d->%d: resolve target: %w", pair.source, pair.target, err)
		}
		if srcMoved || tgtMoved {
			moved++
			fmt.Fprintf(w, "SKIP moved %d→%d %d→%d\n", pair.source, src, pair.target, tgt)
			continue
		}
		todo = append(todo, pair)
	}
	if moved > 0 {
		summary()
		return fmt.Errorf("pairs: %d pair(s) have an endpoint merged away since the adjudication; nothing filed", moved)
	}

	for _, pair := range todo {
		if !run {
			fmt.Fprintf(w, "PLAN %d <- %d  %s\n", pair.target, pair.source, pair.evidence)
			proposed++
			continue
		}
		p, err := merge.ProposeMerge(ctx, model.EntityTypeWork, pair.source, pair.target, actor, note+" | "+pair.evidence)
		if err != nil {
			failed++
			fmt.Fprintf(w, "  %d->%d: propose ERROR %v\n", pair.source, pair.target, err)
			continue
		}
		fmt.Fprintf(w, "OK proposal #%d %d <- %d\n", p.ID, pair.target, pair.source)
		proposed++
	}

	summary()
	if failed > 0 {
		return fmt.Errorf("pairs: %d proposal(s) failed", failed)
	}
	return nil
}

func loadPairs(path string) ([]workPair, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()

	var pairs []workPair
	sourceLine := map[int64]int{}
	targetLine := map[int64]int{}
	sc := bufio.NewScanner(fh)
	for line := 1; sc.Scan(); line++ {
		raw := sc.Text()
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Split(raw, "\t")
		if len(fields) != 3 {
			return nil, fmt.Errorf("%s:%d: want 3 tab-separated fields, got %d", path, line, len(fields))
		}
		src, err := parsePositiveID(strings.TrimSpace(fields[0]))
		if err != nil {
			return nil, fmt.Errorf("%s:%d: source_work_id: %w", path, line, err)
		}
		tgt, err := parsePositiveID(strings.TrimSpace(fields[1]))
		if err != nil {
			return nil, fmt.Errorf("%s:%d: target_work_id: %w", path, line, err)
		}
		evidence := strings.TrimSpace(fields[2])
		if evidence == "" {
			return nil, fmt.Errorf("%s:%d: evidence is empty", path, line)
		}
		if src == tgt {
			return nil, fmt.Errorf("%s:%d: source and target are both %d", path, line, src)
		}
		if prev, ok := sourceLine[src]; ok {
			return nil, fmt.Errorf("%s:%d: work %d is already a source on line %d", path, line, src, prev)
		}
		if prev, ok := targetLine[src]; ok {
			return nil, fmt.Errorf("%s:%d: work %d is a target on line %d and a source here", path, line, src, prev)
		}
		if prev, ok := sourceLine[tgt]; ok {
			return nil, fmt.Errorf("%s:%d: work %d is a source on line %d and a target here", path, line, tgt, prev)
		}
		sourceLine[src] = line
		if _, ok := targetLine[tgt]; !ok {
			targetLine[tgt] = line
		}
		pairs = append(pairs, workPair{source: src, target: tgt, evidence: evidence})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(pairs) == 0 {
		return nil, fmt.Errorf("%s: no pairs", path)
	}
	return pairs, nil
}

func parsePositiveID(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, err
	}
	if id <= 0 {
		return 0, fmt.Errorf("must be a positive id")
	}
	return id, nil
}

func guardPairsNote(note string) error {
	if strings.TrimSpace(note) == "" {
		return fmt.Errorf("note must not be empty")
	}
	if note == waveTagW1 {
		return fmt.Errorf("note must not be %q", waveTagW1)
	}
	for _, banned := range []string{"rule:work-dedup", "llm:queue-adjudicator"} {
		if strings.Contains(note, banned) {
			return fmt.Errorf("note %q contains reserved tag %q", note, banned)
		}
	}
	return nil
}

func pairsProposalStatus(status int16) string {
	switch status {
	case model.ProposalStatusOpen:
		return "open"
	case model.ProposalStatusApproved:
		return "approved"
	case model.ProposalStatusExecuted:
		return "executed"
	case model.ProposalStatusRejected:
		return "rejected"
	default:
		return fmt.Sprintf("%d", status)
	}
}
