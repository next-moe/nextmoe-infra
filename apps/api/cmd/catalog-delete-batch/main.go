package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strconv"
	"strings"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"
	"api/internal/platform/catalog/service"
	"api/pkg/logger"

	"gorm.io/gorm"
)

type stats struct {
	Listed, Alive, Gone, Pending, Executed, Closed, Failed int
}

func main() {
	dsn := flag.String("dsn", "", "catalog DSN (required)")
	idsPath := flag.String("ids", "", "file of work ids, one per line; the first tab/space-separated field is read, # starts a comment (required)")
	reason := flag.String("reason", model.DeletionReasonOutOfScope, "deletion reason: "+strings.Join(model.DeletionReasons, ", "))
	note := flag.String("note", "", "note recorded on every request (required)")
	actor := flag.Int64("actor", 0, "user id recorded as proposer, approver and executor (required)")
	waive := flag.Bool("waive-cooling", false, "execute right after approval instead of leaving the cooling-off to the deletion job")
	run := flag.Bool("run", false, "write (default: dry run, counts only)")
	flag.Parse()

	logger.Init("development")
	if *dsn == "" || *idsPath == "" || strings.TrimSpace(*note) == "" || *actor <= 0 {
		slog.Error("--dsn, --ids, --note and --actor are required")
		os.Exit(2)
	}
	if !slices.Contains(model.DeletionReasons, *reason) {
		slog.Error("unknown --reason", "reason", *reason)
		os.Exit(2)
	}
	ids, err := readIDs(*idsPath)
	if err != nil {
		slog.Error("read ids", "error", err)
		os.Exit(1)
	}
	db, err := database.OpenJob(*dsn)
	if err != nil {
		slog.Error("catalog db connect", "error", err)
		os.Exit(1)
	}

	st, err := deleteBatch(context.Background(), db, ids, *reason, *note, *actor, *waive, *run)
	slog.Info("catalog delete batch", "dry", !*run, "listed", st.Listed, "alive", st.Alive, "gone", st.Gone,
		"pending", st.Pending, "executed", st.Executed, "closed", st.Closed, "failed", st.Failed)
	if err != nil {
		slog.Error("catalog delete batch failed", "error", err)
		os.Exit(1)
	}
	if !*run {
		slog.Info("DRY RUN — nothing written; re-run with --run")
	}
}

func deleteBatch(ctx context.Context, db *gorm.DB, ids []int64, reason, note string, actor int64, waive, run bool) (stats, error) {
	if !run {
		return survey(ctx, db, ids)
	}
	st := stats{Listed: len(ids)}
	svc := service.NewDeletionService(db, service.NewResolveService(repository.NewRedirectRepository(db)))
	var firstErr error
	for _, id := range ids {
		r, err := svc.Request(ctx, model.EntityTypeWork, id, actor, reason, note)
		switch {
		case errors.Is(err, service.ErrEntityNotLive):
			st.Gone++
			continue
		case errors.Is(err, service.ErrDeletionPending):
			st.Pending++
			continue
		case err != nil:
			st.Failed++
			firstErr = firstOf(firstErr, fmt.Errorf("request work %d: %w", id, err))
			continue
		}
		st.Alive++
		closed, err := approveAndExecute(ctx, db, svc, r.ID, actor, waive)
		switch {
		case err != nil:
			st.Failed++
			firstErr = firstOf(firstErr, fmt.Errorf("work %d (request %d): %w", id, r.ID, err))
		case closed:
			st.Closed++
		case waive:
			st.Executed++
		}
	}
	return st, firstErr
}

func survey(ctx context.Context, db *gorm.DB, ids []int64) (stats, error) {
	st := stats{Listed: len(ids)}
	var alive, pending []int64
	if err := db.WithContext(ctx).Raw(`SELECT id FROM catalog_work WHERE id IN ? AND deleted_at IS NULL`, ids).
		Scan(&alive).Error; err != nil {
		return st, err
	}
	if err := db.WithContext(ctx).Raw(`SELECT DISTINCT entity_id FROM catalog_deletion_request
		WHERE entity_type = ? AND entity_id IN ? AND status IN (?, ?)`,
		model.EntityTypeWork, ids, model.ProposalStatusOpen, model.ProposalStatusApproved).Scan(&pending).Error; err != nil {
		return st, err
	}
	st.Pending = len(pending)
	st.Alive = len(alive) - st.Pending
	st.Gone = len(ids) - len(alive)
	return st, nil
}

func approveAndExecute(ctx context.Context, db *gorm.DB, svc *service.DeletionService, id, actor int64, waive bool) (bool, error) {
	if err := svc.Approve(ctx, id, actor); err != nil {
		return false, err
	}
	if !waive {
		return false, nil
	}
	if err := db.WithContext(ctx).Exec(`UPDATE catalog_deletion_request
		SET execute_after = now(), decision_note = 'cooling-off waived by the operator'
		WHERE id = ? AND status = ?`, id, model.ProposalStatusApproved).Error; err != nil {
		return false, err
	}
	return svc.Execute(ctx, id, &actor)
}

func firstOf(first, err error) error {
	if first != nil {
		return first
	}
	return err
}

func readIDs(path string) ([]int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var ids []int64
	seen := map[int64]struct{}{}
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		id, err := strconv.ParseInt(strings.Fields(line)[0], 10, 64)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("%s:%d: not a work id: %q", path, n, line)
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, sc.Err()
}
