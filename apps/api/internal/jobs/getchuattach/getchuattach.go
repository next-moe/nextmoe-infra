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
	EGDSN         string
	Receipts      string
	HoldoutReport bool
	Now           time.Time
}

type Stats struct {
	Population        int
	Attached          int
	JanVNDB           int
	JanEG             int
	TitleDate         int
	TitleCut          int
	EGBrand           int
	EGBrandNear       int
	JanConflict       int
	Bundles           int
	Goods             int
	AllAges           int
	Extras            int
	General           int
	Addons            int
	Reissues          int
	Cancelled         int
	Undated           int
	BrandUnknown      int
	UnmappedRelations int
	EGEditions        int
	RejectedSkips     int
	MintGroups        int
	MintedLive        int
	MintedQuarantined int
	Candidates        int
	Written           int
	Errors            int
	HoldoutCorrect    int
	HoldoutWrong      int
	HoldoutSkipped    int
	HoldoutRules      []holdoutRule
}

func Run(ctx context.Context, opts Opts) (*Stats, error) {
	if opts.DSN == "" || opts.GetchuDSN == "" || opts.EGDSN == "" {
		return nil, fmt.Errorf("all of --dsn, --getchu-dsn and --eg-dsn are required")
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
	egDB, err := database.OpenJob(opts.EGDSN)
	if err != nil {
		return nil, fmt.Errorf("connect eg staging: %w", err)
	}
	defer closeGorm(egDB)
	return RunWithDB(ctx, db, gcDB, egDB, opts)
}

func RunWithDB(ctx context.Context, db, gcDB, egDB *gorm.DB, opts Opts) (*Stats, error) {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	ids, err := resolveIDs(ctx, db)
	if err != nil {
		return nil, err
	}
	snap, err := loadSnapshot(ctx, db, gcDB, egDB, ids, opts.Now)
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
		if err := applyPlanned(ctx, db, ids, snap, planned, &st); err != nil {
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

func applyPlanned(ctx context.Context, db *gorm.DB, ids registryIDs, snap snapshot, planned []plannedAction, st *Stats) error {
	var attaches []plannedAction
	var anchors []plannedAction
	var mints []plannedAction
	for _, p := range planned {
		switch p.Action {
		case actionAnchor:
			anchors = append(anchors, p)
		case actionMint:
			mints = append(mints, p)
		default:
			attaches = append(attaches, p)
		}
	}
	var touched []int64
	if err := applyChunks(ctx, db, anchors, st, &touched, func(tx *gorm.DB, chunk []plannedAction) (int, []int64, error) {
		return writeAnchorChunk(tx, ids, chunk)
	}); err != nil {
		return err
	}
	if err := applyChunks(ctx, db, attaches, st, &touched, func(tx *gorm.DB, chunk []plannedAction) (int, []int64, error) {
		return writeAttachChunk(tx, ids, chunk)
	}); err != nil {
		return err
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

func applyChunks(
	ctx context.Context, db *gorm.DB, actions []plannedAction, st *Stats, touched *[]int64,
	write func(*gorm.DB, []plannedAction) (int, []int64, error),
) error {
	for start := 0; start < len(actions); start += writeChunk {
		end := start + writeChunk
		if end > len(actions) {
			end = len(actions)
		}
		chunk := actions[start:end]
		var chunkTouched []int64
		var written int
		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			n, hosts, err := write(tx, chunk)
			if err != nil {
				return err
			}
			written = n
			chunkTouched = hosts
			return nil
		})
		if err != nil {
			st.Errors++
			slog.Warn("getchuattach write chunk", "start", start, "err", err)
			continue
		}
		st.Written += written
		*touched = append(*touched, chunkTouched...)
	}
	return nil
}

func writeAnchorChunk(tx *gorm.DB, ids registryIDs, chunk []plannedAction) (int, []int64, error) {
	hosts := make([]int64, 0, len(chunk))
	written := 0
	for _, p := range chunk {
		res := tx.Create(&model.CatalogExternalRef{
			EntityType: model.EntityTypeRelease, EntityID: p.ReleaseID,
			SourceID: ids.getchu, ExternalID: p.GetchuID,
			LinkKind: model.LinkKindExact, MatchedBy: p.MatchedBy,
		})
		if res.Error != nil {
			return 0, nil, res.Error
		}
		if res.RowsAffected == 1 {
			written++
			hosts = append(hosts, p.WorkID)
		}
	}
	return written, hosts, nil
}

func writeAttachChunk(tx *gorm.DB, ids registryIDs, chunk []plannedAction) (int, []int64, error) {
	releases := make([]model.CatalogRelease, len(chunk))
	for i, p := range chunk {
		releases[i] = model.CatalogRelease{
			WorkID: p.WorkID, Kind: model.ReleaseKindPhysical,
			ReleasedY: p.ReleasedY, ReleasedM: p.ReleasedM, ReleasedD: p.ReleasedD,
			Extra: emptyJSON(), FieldProvenance: emptyJSON(),
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
		revs[i] = importedRev(model.EntityTypeRelease, releases[i].ID, releaseSnapshotJSON(releases[i]))
	}
	if err := tx.CreateInBatches(refs, 1000).Error; err != nil {
		return 0, nil, err
	}
	if err := tx.CreateInBatches(revs, 1000).Error; err != nil {
		return 0, nil, err
	}
	return len(refs), hosts, nil
}

func importedRev(etype int16, id int64, snap datatypes.JSON) model.CatalogRevision {
	return model.CatalogRevision{
		EntityType: etype, EntityID: id, Revision: 1,
		Action: model.RevisionActionImported, Snapshot: snap, IsMinor: false,
	}
}

func emptyJSON() datatypes.JSON { return datatypes.JSON([]byte(`{}`)) }

func workSnapshotJSON(w model.CatalogWork, titles []model.CatalogWorkTitle) datatypes.JSON {
	b, _ := json.Marshal(map[string]any{"work": w, "titles": titles})
	return b
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
	rec := receipt{
		Action: p.Action, GetchuID: p.GetchuID, GetchuIDs: p.GetchuIDs,
		WorkID: p.WorkID, MatchedBy: p.MatchedBy, Hits: hits,
	}
	if p.Action == actionMint {
		if p.Quarantine {
			rec.Status = "quarantine"
		} else {
			rec.Status = "live"
		}
		rec.RelatedWorks = p.Hits
		if rec.RelatedWorks == nil {
			rec.RelatedWorks = []int64{}
		}
	}
	return rec
}

type receipt struct {
	Action       string   `json:"action"`
	GetchuID     string   `json:"getchu_id,omitempty"`
	GetchuIDs    []string `json:"getchu_ids,omitempty"`
	WorkID       int64    `json:"work_id,omitempty"`
	MatchedBy    string   `json:"matched_by,omitempty"`
	Hits         []int64  `json:"hits,omitempty"`
	Status       string   `json:"status,omitempty"`
	RelatedWorks []int64  `json:"related_works,omitempty"`
}

func logSummary(st Stats) {
	slog.Info("getchuattach summary",
		"population", st.Population,
		"attached", st.Attached,
		"jan_vndb", st.JanVNDB,
		"jan_eg", st.JanEG,
		"title_date", st.TitleDate,
		"title_cut", st.TitleCut,
		"eg_brand", st.EGBrand,
		"eg_near", st.EGBrandNear,
		"jan_conflict", st.JanConflict,
		"bundles", st.Bundles,
		"goods", st.Goods,
		"all_ages", st.AllAges,
		"extras", st.Extras,
		"general", st.General,
		"addons", st.Addons,
		"reissues", st.Reissues,
		"cancelled", st.Cancelled,
		"undated", st.Undated,
		"brand_unknown", st.BrandUnknown,
		"unmapped_relations", st.UnmappedRelations,
		"eg_editions", st.EGEditions,
		"rejected_skips", st.RejectedSkips,
		"mint_groups", st.MintGroups,
		"minted_live", st.MintedLive,
		"minted_quarantined", st.MintedQuarantined,
		"candidates", st.Candidates,
		"written", st.Written,
		"errors", st.Errors,
	)
}

func closeGorm(db *gorm.DB) {
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.Close()
	}
}
