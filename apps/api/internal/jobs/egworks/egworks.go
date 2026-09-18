package egworks

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"

	"gorm.io/gorm"
)

const writeChunk = 1000

type Opts struct {
	Apply         bool
	DSN           string
	EGDSN         string
	Limit         int
	Receipts      string
	HoldoutReport bool
	Now           time.Time
}

type Stats struct {
	Population         int
	PackGames          int
	PortGames          int
	Attached           int
	Quarantined        int
	MintedLive         int
	EditionFolded      int
	RejectedSkips      int
	Limited            int
	RefsPlanned        int
	CandidatesPlanned  int
	Written            int
	Errors             int
	HoldoutCorrect     int
	HoldoutWrong       int
	HoldoutQuarantined int
	HoldoutMinted      int
	HoldoutRules       []holdoutRule
}

func Run(ctx context.Context, opts Opts) (*Stats, error) {
	if opts.DSN == "" || opts.EGDSN == "" {
		return nil, fmt.Errorf("both --dsn and --eg-dsn are required")
	}
	db, err := database.OpenJob(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect catalog db: %w", err)
	}
	defer closeGorm(db)
	egDB, err := database.OpenJob(opts.EGDSN)
	if err != nil {
		return nil, fmt.Errorf("connect eg mirror: %w", err)
	}
	defer closeGorm(egDB)
	return RunWithDB(ctx, db, egDB, opts)
}

func RunWithDB(ctx context.Context, db, egDB *gorm.DB, opts Opts) (*Stats, error) {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	ids, err := resolveIDs(ctx, db)
	if err != nil {
		return nil, err
	}
	snap, err := loadSnapshot(ctx, db, egDB, ids, opts.Now)
	if err != nil {
		return nil, err
	}
	if opts.HoldoutReport {
		return runHoldout(ctx, snap, opts)
	}
	planned, st := decide(snap, unanchored(snap), opts.Limit)
	if err := writeReceipts(opts.Receipts, planned); err != nil {
		return nil, err
	}
	if opts.Apply {
		if err := applyPlanned(ctx, db, ids, snap, planned, &st); err != nil {
			return nil, err
		}
	}
	logSummary(st)
	return &st, nil
}

func unanchored(snap snapshot) []game {
	out := make([]game, 0, len(snap.games))
	for _, g := range snap.games {
		if _, retired := snap.exactAnywhere[g.ID]; retired {
			continue
		}
		if len(snap.holdings[g.ID]) == 0 {
			out = append(out, g)
		}
	}
	return out
}

func applyPlanned(ctx context.Context, db *gorm.DB, ids registryIDs, snap snapshot, planned []plannedAction, st *Stats) error {
	var touched []int64
	var attaches []plannedAction
	var mints []plannedAction
	for _, p := range planned {
		switch p.Action {
		case actionMint:
			mints = append(mints, p)
		default:
			if p.WorkID != 0 {
				attaches = append(attaches, p)
			}
		}
	}
	for start := 0; start < len(attaches); start += writeChunk {
		end := start + writeChunk
		if end > len(attaches) {
			end = len(attaches)
		}
		chunk := attaches[start:end]
		var chunkTouched []int64
		var written int
		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			for _, p := range chunk {
				wrote, err := repository.InsertRefIfAbsent(tx, model.CatalogExternalRef{
					EntityType: model.EntityTypeWork, EntityID: p.WorkID,
					SourceID: ids.eg, ExternalID: strconv.FormatInt(p.EgID, 10),
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
			slog.Warn("egworks attach chunk", "start", start, "err", err)
			continue
		}
		st.Written += written
		touched = append(touched, chunkTouched...)
	}
	mintTouched, mintWritten, mintErrs := applyMints(ctx, db, ids, snap, mints)
	st.Written += mintWritten
	st.Errors += mintErrs
	touched = append(touched, mintTouched...)
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
		Action: p.Action, EgID: p.EgID, WorkID: p.WorkID,
		LinkKind: p.LinkKind, MatchedBy: p.MatchedBy, Hits: hits,
	}
}

type receipt struct {
	Action    string  `json:"action"`
	EgID      int64   `json:"eg_id"`
	WorkID    int64   `json:"work_id,omitempty"`
	LinkKind  int16   `json:"link_kind"`
	MatchedBy string  `json:"matched_by"`
	Hits      []int64 `json:"hits,omitempty"`
}

func logSummary(st Stats) {
	slog.Info("egworks summary",
		"population", st.Population,
		"pack_games", st.PackGames,
		"port_games", st.PortGames,
		"attached", st.Attached,
		"quarantined", st.Quarantined,
		"minted_live", st.MintedLive,
		"edition_folded", st.EditionFolded,
		"rejected_skips", st.RejectedSkips,
		"limited", st.Limited,
		"refs_planned", st.RefsPlanned,
		"candidates_planned", st.CandidatesPlanned,
		"written", st.Written,
		"errors", st.Errors,
	)
}

func closeGorm(db *gorm.DB) {
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.Close()
	}
}
