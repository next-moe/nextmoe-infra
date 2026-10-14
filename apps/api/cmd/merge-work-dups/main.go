package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"
	"api/internal/platform/catalog/service"
	"api/pkg/logger"

	"gorm.io/gorm"
)

const notePrefix = "qa-mergedup(97): "

type tsvRow struct {
	gid        string
	targetWork int64
	sourceWork int64
	correctBid string
	evidence   string
}

func main() {
	mode := flag.String("mode", "", "propose | execute | reject")
	dsn := flag.String("dsn", "", "catalog DSN (REQUIRED)")
	actor := flag.Int64("actor", 0, "acting user id (REQUIRED)")
	run := flag.Bool("run", false, "write (default dry)")
	proposalID := flag.Int64("proposal", 0, "proposal id (reject mode)")
	reason := flag.String("reason", "", "rejection reason (reject mode)")
	var tsvs sliceFlag
	flag.Var(&tsvs, "tsv", "worklist TSV (repeatable; propose mode)")
	flag.Parse()

	logger.Init("development")
	if *dsn == "" || *actor == 0 {
		fmt.Fprintln(os.Stderr, "--dsn and -actor are required")
		os.Exit(2)
	}
	db, err := database.OpenJob(*dsn)
	if err != nil {
		slog.Error("connect", "err", err)
		os.Exit(1)
	}
	resolve := service.NewResolveService(repository.NewRedirectRepository(db))
	merge := service.NewMergeService(db, resolve)
	ctx := context.Background()

	switch *mode {
	case "propose":
		if len(tsvs) == 0 {
			fmt.Fprintln(os.Stderr, "propose mode needs at least one --tsv")
			os.Exit(2)
		}
		propose(ctx, db, merge, tsvs, *actor, *run)
	case "execute":
		execute(ctx, db, merge, *actor, *run)
	case "reject":
		if *proposalID == 0 || *reason == "" {
			fmt.Fprintln(os.Stderr, "reject mode needs -proposal and -reason")
			os.Exit(2)
		}
		reject(ctx, db, merge, *proposalID, *actor, *reason, *run)
	default:
		fmt.Fprintln(os.Stderr, "usage: -mode propose|execute|reject")
		os.Exit(2)
	}
}

func reject(ctx context.Context, db *gorm.DB, merge *service.MergeService, proposalID, actor int64, reason string, run bool) {
	var p model.CatalogMergeProposal
	if err := db.WithContext(ctx).First(&p, proposalID).Error; err != nil {
		slog.Error("load proposal", "id", proposalID, "err", err)
		os.Exit(1)
	}
	fmt.Printf("proposal #%d: entity_type=%d source=%d target=%d status=%d\n  note: %s\n",
		p.ID, p.EntityType, p.SourceEntityID, p.TargetEntityID, p.Status, p.Note)
	if !run {
		fmt.Println("DRY: would reject (pass -run to write)")
		return
	}
	if err := merge.RejectMerge(ctx, proposalID, actor, reason); err != nil {
		slog.Error("reject", "id", proposalID, "err", err)
		os.Exit(1)
	}
	fmt.Printf("REJECTED #%d\n", proposalID)
}

func propose(ctx context.Context, db *gorm.DB, merge *service.MergeService, tsvs []string, actor int64, run bool) {
	var rows []tsvRow
	for _, path := range tsvs {
		rows = append(rows, readTSV(path)...)
	}
	fmt.Printf("worklist rows: %d\n", len(rows))
	proposed, skipped, failed := 0, 0, 0
	for _, r := range rows {
		if err := sanity(ctx, db, r); err != nil {
			failed++
			fmt.Printf("FAIL gid=%s src=%d dst=%d: %v\n", r.gid, r.sourceWork, r.targetWork, err)
			continue
		}
		var n int64
		db.WithContext(ctx).Model(&model.CatalogMergeProposal{}).
			Where("entity_type = ? AND source_entity_id = ? AND target_entity_id = ? AND status IN ?",
				model.EntityTypeWork, r.sourceWork, r.targetWork,
				[]int16{model.ProposalStatusOpen, model.ProposalStatusApproved, model.ProposalStatusExecuted}).
			Count(&n)
		if n > 0 {
			skipped++
			fmt.Printf("SKIP gid=%s src=%d dst=%d: proposal already exists\n", r.gid, r.sourceWork, r.targetWork)
			continue
		}
		if !run {
			proposed++
			fmt.Printf("PLAN gid=%s: merge %d (holder, bgm %s) → %d (claimed)\n", r.gid, r.sourceWork, r.correctBid, r.targetWork)
			continue
		}
		note := notePrefix + fmt.Sprintf("gid=%s correct_bid=%s | %s", r.gid, r.correctBid, r.evidence)
		p, err := merge.ProposeMerge(ctx, model.EntityTypeWork, r.sourceWork, r.targetWork, actor, note)
		if err != nil {
			failed++
			fmt.Printf("FAIL gid=%s propose: %v\n", r.gid, err)
			continue
		}
		if err := merge.ApproveMerge(ctx, p.ID, actor); err != nil {
			failed++
			fmt.Printf("FAIL gid=%s approve #%d: %v\n", r.gid, p.ID, err)
			continue
		}
		proposed++
		fmt.Printf("OK gid=%s: proposal #%d approved (cooling 48h)\n", r.gid, p.ID)
	}
	fmt.Printf("\n=== propose %s ===\nproposed=%d skipped=%d failed=%d\n", modeLabel(run), proposed, skipped, failed)
	if failed > 0 {
		os.Exit(1)
	}
}

func sanity(ctx context.Context, db *gorm.DB, r tsvRow) error {
	var tgt struct {
		ID   int64
		Site *string
	}
	if err := db.WithContext(ctx).Raw(`SELECT id, site FROM catalog_work
		WHERE id = ? AND deleted_at IS NULL`, r.targetWork).Scan(&tgt).Error; err != nil {
		return err
	}
	if tgt.ID == 0 {
		return fmt.Errorf("target work missing/deleted")
	}
	if tgt.Site == nil || *tgt.Site != "kungal" {
		return fmt.Errorf("target not a claimed kungal work")
	}
	var src struct {
		ID   int64
		Site *string
	}
	if err := db.WithContext(ctx).Raw(`SELECT id, site FROM catalog_work
		WHERE id = ? AND deleted_at IS NULL`, r.sourceWork).Scan(&src).Error; err != nil {
		return err
	}
	if src.ID == 0 {
		return fmt.Errorf("source work missing/deleted")
	}
	if src.Site != nil && *src.Site != "" {
		return fmt.Errorf("source is claimed (%s) — refusing", *src.Site)
	}
	var n int64
	if err := db.WithContext(ctx).Raw(`SELECT count(*) FROM catalog_external_ref
		WHERE entity_type = 5 AND entity_id = ? AND source_id = 3 AND link_kind = 0
		  AND external_id = ?`, r.sourceWork, r.correctBid).Scan(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("source does not carry exact bgm anchor %s", r.correctBid)
	}
	return nil
}

func execute(ctx context.Context, db *gorm.DB, merge *service.MergeService, actor int64, run bool) {
	var props []model.CatalogMergeProposal
	if err := db.WithContext(ctx).
		Where("entity_type = ? AND status = ? AND note LIKE ?",
			model.EntityTypeWork, model.ProposalStatusApproved, notePrefix+"%").
		Order("id").Find(&props).Error; err != nil {
		slog.Error("load proposals", "err", err)
		os.Exit(1)
	}
	now := time.Now()
	due, cooling := 0, 0
	executed, failed := 0, 0
	for _, p := range props {
		if p.ExecuteAfter == nil || p.ExecuteAfter.After(now) {
			cooling++
			fmt.Printf("COOLING #%d src=%d dst=%d until %v\n", p.ID, p.SourceEntityID, p.TargetEntityID, p.ExecuteAfter)
			continue
		}
		due++
		if !run {
			fmt.Printf("DUE #%d src=%d dst=%d\n", p.ID, p.SourceEntityID, p.TargetEntityID)
			continue
		}
		if err := merge.ExecuteMerge(ctx, p.ID, &actor); err != nil {
			failed++
			fmt.Printf("FAIL #%d: %v\n", p.ID, err)
			continue
		}
		executed++
		fmt.Printf("EXECUTED #%d src=%d dst=%d\n", p.ID, p.SourceEntityID, p.TargetEntityID)
	}
	fmt.Printf("\n=== execute %s ===\napproved=%d due=%d cooling=%d executed=%d failed=%d\n",
		modeLabel(run), len(props), due, cooling, executed, failed)
	if failed > 0 {
		os.Exit(1)
	}
}

func readTSV(path string) []tsvRow {
	f, err := os.Open(path)
	if err != nil {
		slog.Error("open tsv", "path", path, "err", err)
		os.Exit(1)
	}
	defer f.Close()
	var out []tsvRow
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	first := true
	for sc.Scan() {
		line := sc.Text()
		if first {
			first = false
			continue
		}
		p := strings.Split(line, "\t")
		if len(p) < 6 {
			continue
		}
		tgt, err1 := strconv.ParseInt(p[1], 10, 64)
		src, err2 := strconv.ParseInt(p[4], 10, 64)
		if err1 != nil || err2 != nil {
			slog.Warn("bad tsv row", "line", line)
			continue
		}
		out = append(out, tsvRow{gid: p[0], targetWork: tgt, sourceWork: src, correctBid: p[3], evidence: p[5]})
	}
	return out
}

type sliceFlag []string

func (s *sliceFlag) String() string     { return strings.Join(*s, ",") }
func (s *sliceFlag) Set(v string) error { *s = append(*s, v); return nil }

func modeLabel(run bool) string {
	if run {
		return "RUN"
	}
	return "DRY"
}
