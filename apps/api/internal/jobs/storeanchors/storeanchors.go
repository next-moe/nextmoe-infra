package storeanchors

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"

	"gorm.io/gorm"
)

const maxSamples = 8

type Opts struct {
	Apply bool
	DSN   string
	Only  string
	Limit int
}

type LaneStats struct {
	Candidates        int
	Planned           int
	PlannedWorkGrain  int
	Written           int
	Conflict          int
	Errors            int
	SkippedMalformed  int
	SkippedRejection  int
	SkippedValueTaken int
	SkippedAmbiguous  int
	SkippedDedup      int
	SkippedSibling    int
	SkippedWorkHeld   int
	TakenSamples      []string
	AmbiguousSamples  []string
}

type Stats struct {
	Order []string
	Lanes map[string]*LaneStats
}

func Run(ctx context.Context, opts Opts) (*Stats, error) {
	if opts.DSN == "" {
		return nil, fmt.Errorf("catalog DSN is required (--dsn); refusing to guess the target database")
	}
	db, err := openGorm(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect catalog db: %w", err)
	}
	defer closeGorm(db)
	return RunWithDB(ctx, db, opts)
}

func RunWithDB(ctx context.Context, db *gorm.DB, opts Opts) (*Stats, error) {
	selected, err := selectedLanes(opts.Only)
	if err != nil {
		return nil, err
	}
	vndbSource, err := resolveSource(ctx, db, "vndb")
	if err != nil {
		return nil, err
	}
	st := &Stats{Lanes: map[string]*LaneStats{}}
	for _, l := range selected {
		ls := &LaneStats{}
		st.Order = append(st.Order, l.name)
		st.Lanes[l.name] = ls
		if err := runLane(ctx, db, opts, l, vndbSource, ls); err != nil {
			return nil, fmt.Errorf("lane %s: %w", l.name, err)
		}
	}
	return st, nil
}

type plannedRef struct {
	entityType int16
	entityID   int64
	workID     int64
	externalID string
}

func runLane(ctx context.Context, db *gorm.DB, opts Opts, l lane, vndbSource int16, ls *LaneStats) error {
	laneSource, err := resolveSource(ctx, db, l.sourceKey)
	if err != nil {
		return err
	}
	cands, err := loadCandidates(ctx, db, vndbSource, laneSource, l.site, opts.Limit)
	if err != nil {
		return fmt.Errorf("load candidates: %w", err)
	}
	ls.Candidates = len(cands)

	taken, err := loadTakenExact(ctx, db, laneSource)
	if err != nil {
		return err
	}
	rejected, err := loadRejections(ctx, db, laneSource)
	if err != nil {
		return err
	}

	plan := decide(cands, l, taken, rejected, ls)
	ls.Planned = len(plan)
	if opts.Apply {
		writePlan(ctx, db, plan, laneSource, l.matchedBy, ls)
	}
	logLane(l.name, opts.Apply, ls)
	return nil
}

// decide gives an id that several candidate releases carry to their work when
// they all belong to one: a Steam page VNDB lists on the Japanese, English and
// Chinese releases of one title sold none of them on its own, and skipping the
// group left 373 works with no Steam anchor at all on the 2026-09-26 copy. An
// id spread over releases of different works stays skipped.
func decide(cands []candidate, l lane, taken takenExact, rejected map[string]struct{}, ls *LaneStats) []plannedRef {
	type normalized struct {
		candidate
		ext string
	}
	norm := make([]normalized, 0, len(cands))
	holders := map[string]map[int64]int64{}
	for _, c := range cands {
		ext := l.normalize(c.RawValue)
		if ext == "" {
			ls.SkippedMalformed++
			continue
		}
		norm = append(norm, normalized{candidate: c, ext: ext})
		if holders[ext] == nil {
			holders[ext] = map[int64]int64{}
		}
		holders[ext][c.ReleaseID] = c.WorkID
	}

	seen := map[string]struct{}{}
	grouped := map[string]struct{}{}
	plan := make([]plannedRef, 0, len(norm))
	for _, n := range norm {
		key := rejKey(model.EntityTypeRelease, n.ReleaseID, n.ext)
		if _, dup := seen[key]; dup {
			ls.SkippedDedup++
			continue
		}
		seen[key] = struct{}{}
		if _, hit := rejected[key]; hit {
			ls.SkippedRejection++
			continue
		}
		if _, hit := taken.release[n.ext]; hit {
			ls.SkippedValueTaken++
			addSample(&ls.TakenSamples, n.ext)
			continue
		}
		if len(holders[n.ext]) == 1 {
			plan = append(plan, plannedRef{
				entityType: model.EntityTypeRelease, entityID: n.ReleaseID,
				workID: n.WorkID, externalID: n.ext,
			})
			continue
		}
		if !oneWork(holders[n.ext]) {
			ls.SkippedAmbiguous++
			addSample(&ls.AmbiguousSamples, n.ext)
			continue
		}
		if _, done := grouped[n.ext]; done {
			ls.SkippedSibling++
			continue
		}
		grouped[n.ext] = struct{}{}
		if _, hit := rejected[rejKey(model.EntityTypeWork, n.WorkID, n.ext)]; hit {
			ls.SkippedRejection++
			continue
		}
		if holder, held := taken.work[n.ext]; held {
			if holder == n.WorkID {
				ls.SkippedWorkHeld++
			} else {
				ls.SkippedValueTaken++
				addSample(&ls.TakenSamples, n.ext)
			}
			continue
		}
		ls.PlannedWorkGrain++
		plan = append(plan, plannedRef{
			entityType: model.EntityTypeWork, entityID: n.WorkID,
			workID: n.WorkID, externalID: n.ext,
		})
	}
	return plan
}

func oneWork(holders map[int64]int64) bool {
	var first int64
	for _, w := range holders {
		if first == 0 {
			first = w
		} else if w != first {
			return false
		}
	}
	return true
}

func writePlan(ctx context.Context, db *gorm.DB, plan []plannedRef, laneSource int16, matchedBy string, ls *LaneStats) {
	var touched []int64
	for _, p := range plan {
		wrote, err := repository.InsertRefIfAbsent(db.WithContext(ctx), model.CatalogExternalRef{
			EntityType: p.entityType, EntityID: p.entityID,
			SourceID: laneSource, ExternalID: p.externalID,
			LinkKind: model.LinkKindExact, MatchedBy: matchedBy,
		})
		switch {
		case err != nil:
			ls.Errors++
			slog.Warn("write store anchor", "entity_type", p.entityType, "entity", p.entityID,
				"source", laneSource, "ext", p.externalID, "err", err)
		case wrote:
			ls.Written++
			touched = append(touched, p.workID)
		default:
			ls.Conflict++
		}
	}
	if err := repository.TouchWorks(ctx, db, touched); err != nil {
		ls.Errors++
		slog.Warn("touch works", "err", err)
	}
}

func addSample(dst *[]string, v string) {
	if len(*dst) < maxSamples {
		*dst = append(*dst, v)
	}
}

func logLane(name string, apply bool, ls *LaneStats) {
	sort.Strings(ls.TakenSamples)
	sort.Strings(ls.AmbiguousSamples)
	slog.Info("store-anchors lane done", "lane", name, "apply", apply,
		"candidates", ls.Candidates, "planned", ls.Planned,
		"written", ls.Written, "conflict", ls.Conflict, "errors", ls.Errors,
		"skipped_malformed", ls.SkippedMalformed,
		"skipped_rejection", ls.SkippedRejection,
		"skipped_value_taken", ls.SkippedValueTaken,
		"skipped_ambiguous", ls.SkippedAmbiguous,
		"skipped_dedup", ls.SkippedDedup,
		"skipped_sibling", ls.SkippedSibling,
		"skipped_work_held", ls.SkippedWorkHeld,
		"planned_work_grain", ls.PlannedWorkGrain)
}

func unknownLaneError(only string) error {
	names := make([]string, 0, len(lanes))
	for _, l := range lanes {
		names = append(names, l.name)
	}
	return fmt.Errorf("unknown lane %q (want one of %v, or empty for all)", only, names)
}

func openGorm(dsn string) (*gorm.DB, error) {
	return database.OpenJob(dsn)
}

func closeGorm(db *gorm.DB) {
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.Close()
	}
}
