// Undo one executed work merge.
//
// MergeService.Unmerge has existed since the merge machinery was written and
// has never had a route or a CLI, so on 2026-09-05 when the queue adjudicator
// folded two different games called ふたりぐらし into one work — nine days
// before the contradicting-exact-ref veto shipped — there was no way to take it
// back, and the survivor has carried vndb v7635 (2008) and v63090 (2025) since.
//
// What Unmerge restores is the work row and its titles, from the merged_source
// revision snapshot, under a NEW id. Refs, releases, credits, tags, covers and
// characters moved to the survivor during the merge and stay there: the
// snapshot does not carry them. So this prints what is still sitting on the
// survivor and names the upstream ids on each side, because redistributing
// those is a second, hand-adjudicated step and the operator needs to see the
// size of it before starting.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"
	"api/internal/platform/catalog/service"
	"api/pkg/logger"

	"gorm.io/gorm"
)

func main() {
	dsn := flag.String("dsn", "", "catalog DSN (REQUIRED)")
	actor := flag.Int64("actor", 0, "acting user id (REQUIRED)")
	proposalID := flag.Int64("proposal", 0, "executed merge proposal to undo (REQUIRED)")
	run := flag.Bool("run", false, "write (default: dry-run preview)")
	flag.Parse()

	logger.Init("development")
	if *dsn == "" || *actor == 0 || *proposalID == 0 {
		fmt.Fprintln(os.Stderr, "usage: unmerge-work --dsn <dsn> -actor <uid> -proposal <id> [-run]")
		os.Exit(2)
	}

	db, err := database.OpenJob(*dsn)
	if err != nil {
		slog.Error("connect", "err", err)
		os.Exit(1)
	}
	ctx := context.Background()

	p, err := loadExecuted(ctx, db, *proposalID)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := describe(ctx, db, p); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if !*run {
		fmt.Printf("DRY-RUN (pass -run) proposal %d: would rebuild work %d from its merged_source snapshot\n",
			p.ID, p.SourceEntityID)
		return
	}

	resolve := service.NewResolveService(repository.NewRedirectRepository(db))
	merge := service.NewMergeService(db, resolve,
		repository.NewProposalRepository(db), repository.NewRevisionRepository(db))
	newID, err := merge.Unmerge(ctx, p.ID, actor)
	if err != nil {
		slog.Error("unmerge", "proposal", p.ID, "err", err)
		os.Exit(1)
	}
	fmt.Printf("APPLIED proposal %d: work %d rebuilt as %d (titles only; refs, releases, credits, "+
		"tags, covers and characters are still on %d and need a separate pass)\n",
		p.ID, p.SourceEntityID, newID, p.TargetEntityID)
}

func loadExecuted(ctx context.Context, db *gorm.DB, id int64) (*model.CatalogMergeProposal, error) {
	var p model.CatalogMergeProposal
	if err := db.WithContext(ctx).First(&p, id).Error; err != nil {
		return nil, fmt.Errorf("proposal %d: %w", id, err)
	}
	if p.EntityType != model.EntityTypeWork {
		return nil, fmt.Errorf("proposal %d is entity type %d, this tool only undoes work merges", id, p.EntityType)
	}
	if p.Status != model.ProposalStatusExecuted {
		return nil, fmt.Errorf("proposal %d is in status %d, want executed", id, p.Status)
	}
	return &p, nil
}

// describe prints the survivor's exact upstream ids and its release years. A
// merge that should never have happened shows up here as two upstream
// identities under one work, and the operator redistributes by those ids —
// there is nothing else to key the split on once the children have moved.
func describe(ctx context.Context, db *gorm.DB, p *model.CatalogMergeProposal) error {
	var refs []struct {
		Key        string `gorm:"column:key"`
		ExternalID string `gorm:"column:external_id"`
		LinkKind   int16  `gorm:"column:link_kind"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT s.key, r.external_id, r.link_kind
		FROM catalog_external_ref r JOIN catalog_source s ON s.id = r.source_id
		WHERE r.entity_type = ? AND r.entity_id = ? AND r.dead_at IS NULL
		ORDER BY s.key, r.external_id`,
		model.EntityTypeWork, p.TargetEntityID).Scan(&refs).Error; err != nil {
		return err
	}
	var rels []struct {
		ID        int64  `gorm:"column:id"`
		ReleasedY *int   `gorm:"column:released_y"`
		Key       string `gorm:"column:key"`
		Ext       string `gorm:"column:external_id"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT rl.id, rl.released_y, s.key, r.external_id
		FROM catalog_release rl
		LEFT JOIN catalog_external_ref r ON r.entity_type = ? AND r.entity_id = rl.id
		LEFT JOIN catalog_source s ON s.id = r.source_id
		WHERE rl.work_id = ? ORDER BY rl.released_y NULLS LAST, rl.id`,
		model.EntityTypeRelease, p.TargetEntityID).Scan(&rels).Error; err != nil {
		return err
	}

	fmt.Printf("proposal %d: work %d was merged into %d\n", p.ID, p.SourceEntityID, p.TargetEntityID)
	fmt.Printf("survivor %d now holds %d live refs and %d releases:\n", p.TargetEntityID, len(refs), len(rels))
	for _, r := range refs {
		fmt.Printf("  ref      %-14s %-24s link_kind=%d\n", r.Key, r.ExternalID, r.LinkKind)
	}
	for _, r := range rels {
		year := "----"
		if r.ReleasedY != nil {
			year = fmt.Sprintf("%d", *r.ReleasedY)
		}
		fmt.Printf("  release  %-8d %s  %s:%s\n", r.ID, year, r.Key, r.Ext)
	}
	return nil
}
