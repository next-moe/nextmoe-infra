package eganchors

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
	Apply    bool
	DSN      string
	EGDSN    string
	Receipts string
}

type Stats struct {
	Games           int
	AnchoredGames   int
	NoEvidenceGames int
	MultiGames      int
	TwinGames       int
	CandidateGames  int
	RejectedSkips   int
	ExactPlanned    int
	ProbablePlanned int
	RelatedPlanned  int
	Corroborated    int
	Written         int
	Exists          int
	Errors          int
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
	ids, err := resolveIDs(ctx, db)
	if err != nil {
		return nil, err
	}
	snap, err := loadSnapshot(ctx, db, egDB, ids)
	if err != nil {
		return nil, err
	}
	draft := classify(snap)
	titles, dates, err := loadWorkCorroboration(ctx, db, singleFamilyWorks(draft))
	if err != nil {
		return nil, err
	}
	snap.workTitles = titles
	snap.workDates = dates
	planned, st := finish(snap, draft)
	if err := writeReceipts(opts.Receipts, planned); err != nil {
		return nil, err
	}
	if opts.Apply {
		if err := applyPlanned(ctx, db, ids.eg, planned, &st); err != nil {
			return nil, err
		}
	}
	slog.Info("eganchors summary",
		"games", st.Games,
		"anchored_games", st.AnchoredGames,
		"no_evidence_games", st.NoEvidenceGames,
		"multi_games", st.MultiGames,
		"twin_games", st.TwinGames,
		"candidate_games", st.CandidateGames,
		"rejected_skips", st.RejectedSkips,
		"exact_planned", st.ExactPlanned,
		"probable_planned", st.ProbablePlanned,
		"related_planned", st.RelatedPlanned,
		"corroborated", st.Corroborated,
		"written", st.Written,
		"exists", st.Exists,
		"errors", st.Errors,
	)
	return &st, nil
}

func applyPlanned(ctx context.Context, db *gorm.DB, egSource int16, planned []plannedRef, st *Stats) error {
	var touched []int64
	for start := 0; start < len(planned); start += writeChunk {
		end := start + writeChunk
		if end > len(planned) {
			end = len(planned)
		}
		chunk := planned[start:end]
		var chunkTouched []int64
		var written, exists int
		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			for _, p := range chunk {
				wrote, err := repository.InsertRefIfAbsent(tx, model.CatalogExternalRef{
					EntityType: model.EntityTypeWork, EntityID: p.WorkID,
					SourceID: egSource, ExternalID: strconv.FormatInt(p.EgID, 10),
					LinkKind: p.LinkKind, MatchedBy: p.MatchedBy,
				})
				if err != nil {
					return err
				}
				if wrote {
					written++
					chunkTouched = append(chunkTouched, p.WorkID)
				} else {
					exists++
				}
			}
			return nil
		})
		if err != nil {
			st.Errors++
			slog.Warn("eganchors write chunk", "start", start, "err", err)
			continue
		}
		st.Written += written
		st.Exists += exists
		touched = append(touched, chunkTouched...)
	}
	if err := repository.TouchWorks(ctx, db, touched); err != nil {
		return fmt.Errorf("touch works: %w", err)
	}
	return nil
}

func writeReceipts(path string, planned []plannedRef) error {
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
		fams := p.Families
		if fams == nil {
			fams = []string{}
		}
		if err := enc.Encode(receipt{
			EgID: p.EgID, WorkID: p.WorkID, LinkKind: p.LinkKind,
			MatchedBy: p.MatchedBy, Class: p.Class, Families: fams,
			Corroboration: p.Corroboration,
		}); err != nil {
			return fmt.Errorf("receipts: %w", err)
		}
	}
	return nil
}

type receipt struct {
	EgID          int64    `json:"eg_id"`
	WorkID        int64    `json:"work_id"`
	LinkKind      int16    `json:"link_kind"`
	MatchedBy     string   `json:"matched_by"`
	Class         string   `json:"class"`
	Families      []string `json:"families"`
	Corroboration string   `json:"corroboration"`
}

func closeGorm(db *gorm.DB) {
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.Close()
	}
}
