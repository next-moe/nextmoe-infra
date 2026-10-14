package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"
	"api/internal/platform/catalog/service"
	"api/pkg/logger"

	"gorm.io/gorm"
)

const notePrefix = "dup-submission(170): "

type pair struct{ src, dst int64 }

type pairFlag []pair

func (p *pairFlag) String() string { return fmt.Sprint(*p) }

func (p *pairFlag) Set(v string) error {
	a, b, ok := strings.Cut(v, ":")
	if !ok {
		return fmt.Errorf("pair %q: want source:target", v)
	}
	src, err := strconv.ParseInt(a, 10, 64)
	if err != nil {
		return fmt.Errorf("pair %q: bad source: %w", v, err)
	}
	dst, err := strconv.ParseInt(b, 10, 64)
	if err != nil {
		return fmt.Errorf("pair %q: bad target: %w", v, err)
	}
	if src == dst {
		return fmt.Errorf("pair %q: source == target", v)
	}
	*p = append(*p, pair{src, dst})
	return nil
}

func main() {
	mode := flag.String("mode", "", "propose | execute")
	dsn := flag.String("dsn", "", "catalog DSN (REQUIRED)")
	actor := flag.Int64("actor", 0, "acting user id (REQUIRED)")
	run := flag.Bool("run", false, "write (default dry)")
	var pairs pairFlag
	flag.Var(&pairs, "pair", "source:target work ids (repeatable, REQUIRED)")
	flag.Parse()

	logger.Init("development")
	if *dsn == "" || *actor == 0 || len(pairs) == 0 || (*mode != "propose" && *mode != "execute") {
		fmt.Fprintln(os.Stderr, "--dsn, -actor, -pair and -mode propose|execute are required")
		os.Exit(2)
	}
	db, err := database.OpenJob(*dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open db:", err)
		os.Exit(1)
	}
	resolve := service.NewResolveService(repository.NewRedirectRepository(db))
	merge := service.NewMergeService(db, resolve)
	ctx := context.Background()

	failed := 0
	for _, pr := range pairs {
		var err error
		switch *mode {
		case "propose":
			err = propose(ctx, merge, pr, *actor, *run)
		case "execute":
			err = execute(ctx, db, merge, pr, *actor, *run)
		}
		if err != nil {
			failed++
			fmt.Printf("FAIL  %d -> %d: %v\n", pr.src, pr.dst, err)
		}
	}
	fmt.Printf("done: mode=%s pairs=%d failed=%d run=%v\n", *mode, len(pairs), failed, *run)
	if failed > 0 {
		os.Exit(1)
	}
}

func propose(ctx context.Context, merge *service.MergeService, pr pair, actor int64, run bool) error {
	note := fmt.Sprintf("%swork %d duplicates work %d", notePrefix, pr.src, pr.dst)
	if !run {
		fmt.Printf("DRY   would propose+approve %d -> %d (%s)\n", pr.src, pr.dst, note)
		return nil
	}
	p, err := merge.ProposeMerge(ctx, model.EntityTypeWork, pr.src, pr.dst, actor, note)
	if err != nil {
		return fmt.Errorf("propose: %w", err)
	}
	if err := merge.ApproveMerge(ctx, p.ID, actor); err != nil {
		return fmt.Errorf("approve %d: %w", p.ID, err)
	}
	fmt.Printf("OK    proposed+approved %d -> %d (proposal %d)\n", pr.src, pr.dst, p.ID)
	return nil
}

func execute(ctx context.Context, db *gorm.DB, merge *service.MergeService, pr pair, actor int64, run bool) error {
	var ids []int64
	err := db.WithContext(ctx).Raw(`
		SELECT id FROM catalog_merge_proposal
		WHERE entity_type = ? AND source_entity_id = ? AND target_entity_id = ?
			AND status = ? AND note LIKE ?`,
		model.EntityTypeWork, pr.src, pr.dst, model.ProposalStatusApproved, notePrefix+"%").Scan(&ids).Error
	if err != nil {
		return err
	}
	if len(ids) != 1 {
		return fmt.Errorf("want exactly 1 approved proposal, found %d", len(ids))
	}
	if !run {
		fmt.Printf("DRY   would execute proposal %d (%d -> %d)\n", ids[0], pr.src, pr.dst)
		return nil
	}
	if err := merge.ExecuteMerge(ctx, ids[0], &actor); err != nil {
		return err
	}
	fmt.Printf("OK    executed proposal %d (%d -> %d)\n", ids[0], pr.src, pr.dst)
	return nil
}
