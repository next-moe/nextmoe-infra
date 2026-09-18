package hltbattach

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"

	"gorm.io/gorm"
)

const writeChunk = 1000

type Opts struct {
	Apply         bool
	DSN           string
	HltbDSN       string
	Receipts      string
	HoldoutReport bool
}

type Stats struct {
	Population     int
	Attached       int
	Uncorroborated int
	MultiHit       int
	NoHit          int
	RejectedSkips  int
	Written        int
	Errors         int
	HoldoutCorrect int
	HoldoutWrong   int
	HoldoutSkipped int
}

func Run(ctx context.Context, opts Opts) (*Stats, error) {
	if opts.DSN == "" || opts.HltbDSN == "" {
		return nil, fmt.Errorf("both --dsn and --hltb-dsn are required")
	}
	db, err := database.OpenJob(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect catalog db: %w", err)
	}
	defer closeGorm(db)
	hltbDB, err := database.OpenJob(opts.HltbDSN)
	if err != nil {
		return nil, fmt.Errorf("connect hltb mirror: %w", err)
	}
	defer closeGorm(hltbDB)
	return RunWithDB(ctx, db, hltbDB, opts)
}

func RunWithDB(ctx context.Context, db, hltbDB *gorm.DB, opts Opts) (*Stats, error) {
	ids, err := resolveIDs(ctx, db)
	if err != nil {
		return nil, err
	}
	snap, err := loadSnapshot(ctx, db, hltbDB, ids)
	if err != nil {
		return nil, err
	}
	if opts.HoldoutReport {
		return runHoldout(snap, opts)
	}
	planned, st := decide(snap, unanchored(snap))
	if err := writeReceipts(opts.Receipts, planned); err != nil {
		return nil, err
	}
	if opts.Apply {
		if err := applyPlanned(ctx, db, ids, planned, &st); err != nil {
			return nil, err
		}
	}
	logSummary(st)
	return &st, nil
}

func unanchored(snap snapshot) []game {
	out := make([]game, 0, len(snap.games))
	for _, g := range snap.games {
		if _, claimed := snap.claimed[g.ID]; claimed {
			continue
		}
		out = append(out, g)
	}
	return out
}

func applyPlanned(ctx context.Context, db *gorm.DB, ids registryIDs, planned []plannedAction, st *Stats) error {
	var touched []int64
	for start := 0; start < len(planned); start += writeChunk {
		end := start + writeChunk
		if end > len(planned) {
			end = len(planned)
		}
		chunk := planned[start:end]
		var chunkTouched []int64
		var written int
		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			for _, p := range chunk {
				wrote, err := repository.InsertRefIfAbsent(tx, model.CatalogExternalRef{
					EntityType: model.EntityTypeWork, EntityID: p.WorkID,
					SourceID: ids.hltb, ExternalID: strconv.FormatInt(p.HltbID, 10),
					LinkKind: p.LinkKind, MatchedBy: p.MatchedBy,
				})
				if err != nil {
					return err
				}
				if wrote {
					written++
					chunkTouched = append(chunkTouched, p.WorkID)
				}
			}
			return nil
		})
		if err != nil {
			st.Errors++
			slog.Warn("hltbattach attach chunk", "start", start, "err", err)
			continue
		}
		st.Written += written
		touched = append(touched, chunkTouched...)
	}
	if err := repository.TouchWorks(ctx, db, touched); err != nil {
		return fmt.Errorf("touch works: %w", err)
	}
	return nil
}

func writeReceipts(path string, planned []plannedAction) error {
	if path == "" {
		return nil
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("receipts: %w", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, p := range planned {
		if err := enc.Encode(receiptFrom(p)); err != nil {
			return fmt.Errorf("receipts: %w", err)
		}
	}
	return nil
}

func receiptFrom(p plannedAction) receipt {
	hits := p.Hits
	if hits == nil {
		hits = []int64{}
	}
	return receipt{
		Action: p.Action, HltbID: p.HltbID, WorkID: p.WorkID,
		LinkKind: p.LinkKind, MatchedBy: p.MatchedBy, Hits: hits,
	}
}

type receipt struct {
	Action    string  `json:"action"`
	HltbID    int64   `json:"hltb_id"`
	WorkID    int64   `json:"work_id,omitempty"`
	LinkKind  int16   `json:"link_kind"`
	MatchedBy string  `json:"matched_by"`
	Hits      []int64 `json:"hits,omitempty"`
}

func logSummary(st Stats) {
	slog.Info("hltbattach summary",
		"population", st.Population,
		"attached", st.Attached,
		"uncorroborated", st.Uncorroborated,
		"multi_hit", st.MultiHit,
		"no_hit", st.NoHit,
		"rejected_skips", st.RejectedSkips,
		"written", st.Written,
		"errors", st.Errors,
	)
}

func closeGorm(db *gorm.DB) {
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.Close()
	}
}
