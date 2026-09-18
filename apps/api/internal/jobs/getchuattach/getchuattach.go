package getchuattach

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const writeChunk = 1000

type Opts struct {
	Apply         bool
	DSN           string
	GetchuDSN     string
	Receipts      string
	HoldoutReport bool
	Now           time.Time
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
	HoldoutRules   []holdoutRule
}

func Run(ctx context.Context, opts Opts) (*Stats, error) {
	if opts.DSN == "" || opts.GetchuDSN == "" {
		return nil, fmt.Errorf("both --dsn and --getchu-dsn are required")
	}
	db, err := database.OpenJob(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect catalog db: %w", err)
	}
	defer closeGorm(db)
	gcDB, err := database.OpenJob(opts.GetchuDSN)
	if err != nil {
		return nil, fmt.Errorf("connect getchu staging: %w", err)
	}
	defer closeGorm(gcDB)
	return RunWithDB(ctx, db, gcDB, opts)
}

func RunWithDB(ctx context.Context, db, gcDB *gorm.DB, opts Opts) (*Stats, error) {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	ids, err := resolveIDs(ctx, db)
	if err != nil {
		return nil, err
	}
	snap, err := loadSnapshot(ctx, db, gcDB, ids, opts.Now)
	if err != nil {
		return nil, err
	}
	if opts.HoldoutReport {
		return runHoldout(ctx, snap, opts)
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

func unanchored(snap snapshot) []item {
	out := make([]item, 0, len(snap.items))
	for _, it := range snap.items {
		if _, held := snap.exactAnywhere[it.GetchuID]; held {
			continue
		}
		out = append(out, it)
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
			n, hosts, err := writeAttachChunk(tx, ids, chunk)
			if err != nil {
				return err
			}
			written = n
			chunkTouched = hosts
			return nil
		})
		if err != nil {
			st.Errors++
			slog.Warn("getchuattach attach chunk", "start", start, "err", err)
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

func writeAttachChunk(tx *gorm.DB, ids registryIDs, chunk []plannedAction) (int, []int64, error) {
	releases := make([]model.CatalogRelease, len(chunk))
	for i, p := range chunk {
		releases[i] = model.CatalogRelease{
			WorkID: p.WorkID, Kind: model.ReleaseKindPhysical,
			ReleasedY: p.ReleasedY, ReleasedM: p.ReleasedM, ReleasedD: p.ReleasedD,
			Extra: datatypes.JSON([]byte(`{}`)), FieldProvenance: datatypes.JSON([]byte(`{}`)),
		}
	}
	if err := tx.CreateInBatches(releases, 1000).Error; err != nil {
		return 0, nil, err
	}
	refs := make([]model.CatalogExternalRef, len(chunk))
	revs := make([]model.CatalogRevision, len(chunk))
	hosts := make([]int64, 0, len(chunk))
	for i, p := range chunk {
		hosts = append(hosts, p.WorkID)
		refs[i] = model.CatalogExternalRef{
			EntityType: model.EntityTypeRelease, EntityID: releases[i].ID,
			SourceID: ids.getchu, ExternalID: p.GetchuID,
			LinkKind: model.LinkKindExact, MatchedBy: p.MatchedBy,
		}
		revs[i] = importedRev(releases[i].ID, releaseSnapshotJSON(releases[i]))
	}
	if err := tx.CreateInBatches(refs, 1000).Error; err != nil {
		return 0, nil, err
	}
	if err := tx.CreateInBatches(revs, 1000).Error; err != nil {
		return 0, nil, err
	}
	return len(refs), hosts, nil
}

func importedRev(id int64, snap datatypes.JSON) model.CatalogRevision {
	return model.CatalogRevision{
		EntityType: model.EntityTypeRelease, EntityID: id, Revision: 1,
		Action: model.RevisionActionImported, Snapshot: snap, IsMinor: false,
	}
}

func releaseSnapshotJSON(r model.CatalogRelease) datatypes.JSON {
	b, _ := json.Marshal(map[string]any{"release": r})
	return b
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
		Action: p.Action, GetchuID: p.GetchuID, WorkID: p.WorkID,
		MatchedBy: p.MatchedBy, Hits: hits,
	}
}

type receipt struct {
	Action    string  `json:"action"`
	GetchuID  string  `json:"getchu_id"`
	WorkID    int64   `json:"work_id,omitempty"`
	MatchedBy string  `json:"matched_by"`
	Hits      []int64 `json:"hits,omitempty"`
}

func logSummary(st Stats) {
	slog.Info("getchuattach summary",
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
