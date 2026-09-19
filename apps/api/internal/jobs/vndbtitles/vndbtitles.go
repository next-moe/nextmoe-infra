package vndbtitles

import (
	"context"
	"fmt"

	"api/internal/infrastructure/database"
	"api/internal/platform/provenance"

	"gorm.io/gorm"
)

const writeChunk = 500

type Opts struct {
	Apply bool
	DSN   string
	Limit int
}

type Stats struct {
	WorksPopulation  int
	WorksMultiAnchor int
	WorksVNMissing   int
	WorksChanged     int

	TitlesOLangInserted int
	TitlesENInserted    int
	TitlesPresent       int
	TitlesLost          int

	DisplayFilled       int
	DisplayHumanSkipped int
	DisplayLost         int
	Errors              int
	FirstError          string
}

// Cron scripts read a counter with a greedy
// sed -n "s/.*${name}=\([0-9]*\).*/\1/p", so a key that is a suffix of another
// reads the wrong number.
var summaryKeys = []string{
	"works_population",
	"works_multi_anchor",
	"works_vn_missing",
	"works_changed",
	"titles_olang_inserted",
	"titles_en_inserted",
	"titles_present",
	"titles_lost",
	"display_filled",
	"display_human_skipped",
	"display_lost",
	"errors",
}

func (st *Stats) logValues() []int {
	return []int{
		st.WorksPopulation,
		st.WorksMultiAnchor,
		st.WorksVNMissing,
		st.WorksChanged,
		st.TitlesOLangInserted,
		st.TitlesENInserted,
		st.TitlesPresent,
		st.TitlesLost,
		st.DisplayFilled,
		st.DisplayHumanSkipped,
		st.DisplayLost,
		st.Errors,
	}
}

func (st *Stats) LogArgs() []any {
	vals := st.logValues()
	out := make([]any, 0, len(summaryKeys)*2)
	for i, k := range summaryKeys {
		out = append(out, k, vals[i])
	}
	return out
}

func (st *Stats) noteError(err error) {
	st.Errors++
	if st.FirstError == "" {
		st.FirstError = err.Error()
	}
}

func Run(ctx context.Context, opts Opts) (*Stats, error) {
	if opts.DSN == "" {
		return nil, fmt.Errorf("catalog DSN is required (--dsn); refusing to guess")
	}
	db, err := database.OpenJob(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect catalog db: %w", err)
	}
	defer closeDB(db)
	return run(ctx, db, opts)
}

func run(ctx context.Context, db *gorm.DB, opts Opts) (*Stats, error) {
	reg, err := resolveRegistry(ctx, db)
	if err != nil {
		return nil, err
	}
	rows, err := loadAnchored(ctx, db, reg)
	if err != nil {
		return nil, fmt.Errorf("load vndb-anchored works: %w", err)
	}
	population, multi, missing := classify(rows, opts.Limit)
	st := &Stats{
		WorksPopulation:  len(population),
		WorksMultiAnchor: multi,
		WorksVNMissing:   missing,
	}
	ids := make([]int64, len(population))
	for i, r := range population {
		ids[i] = r.WorkID
	}
	titles, err := loadExistingTitles(ctx, db, ids)
	if err != nil {
		return nil, fmt.Errorf("load work titles: %w", err)
	}

	prepared := make([]preparedWork, 0, len(population))
	for _, r := range population {
		d := decide(decisionIn{
			Titles:       titles[r.WorkID],
			DisplayName:  r.DisplayName,
			DisplayHuman: provenance.IsHuman(provenance.FirstSource(r.FieldProvenance, "display_name")),
			OLang:        r.OLang,
			OLangTitle:   r.OLangTitle,
			ENTitle:      r.ENTitle,
		})
		if d.OLangPresent {
			st.TitlesPresent++
		}
		if d.ENPresent {
			st.TitlesPresent++
		}
		if d.SkipDisplayHuman {
			st.DisplayHumanSkipped++
		}
		prepared = append(prepared, preparedWork{row: r, plan: d})
	}

	if !opts.Apply {
		for _, p := range prepared {
			countPlan(st, p.plan)
		}
		return st, nil
	}
	if err := applyPrepared(ctx, db, prepared, st); err != nil {
		return st, err
	}
	return st, nil
}

func countPlan(st *Stats, d decision) {
	if d.InsertOLang {
		st.TitlesOLangInserted++
	}
	if d.InsertEN {
		st.TitlesENInserted++
	}
	if d.FillDisplay {
		st.DisplayFilled++
	}
	if d.writes() {
		st.WorksChanged++
	}
}

type registry struct {
	vndbSource int16
}

func resolveRegistry(ctx context.Context, db *gorm.DB) (registry, error) {
	var r registry
	if err := db.WithContext(ctx).Raw(`SELECT id FROM catalog_source WHERE key = 'vndb'`).Scan(&r.vndbSource).Error; err != nil {
		return r, fmt.Errorf("resolve vndb source: %w", err)
	}
	if r.vndbSource == 0 {
		return r, fmt.Errorf("registry not seeded (vndb source missing)")
	}
	return r, nil
}

func closeDB(db *gorm.DB) {
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.Close()
	}
}
