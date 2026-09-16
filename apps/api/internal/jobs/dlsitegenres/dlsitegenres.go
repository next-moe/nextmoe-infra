package dlsitegenres

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"api/internal/infrastructure/database"

	"gorm.io/gorm"
)

const taxonomyLocale = "zh_CN"

const (
	maxSamples  = 8
	maxTopNames = 10
)

type Opts struct {
	Apply     bool
	DSN       string
	DlsiteDSN string
	Limit     int
	Offset    int
}

type Sample struct {
	WorkID       int64
	Workno       string
	GenreID      int
	Name         string
	FromTaxonomy bool
}

type NameFreq struct {
	Name  string
	Works int
}

type Stats struct {
	TaxonomyRows  int
	Candidates    int
	SkippedBundle int
	MissingMirror int
	NoGenres      int
	NotArray      int
	ZhHit         int
	JaFallback    int
	NameBlank     int
	DupCollapsed  int
	Planned       int
	Written       int
	Conflict      int
	Errors        int

	DistinctNames   int
	TopNames        []NameFreq
	Samples         []Sample
	FallbackSamples []Sample
}

func Run(ctx context.Context, opts Opts) (*Stats, error) {
	if opts.DSN == "" {
		return nil, fmt.Errorf("catalog DSN is required (--dsn); refusing to guess — pass the rehearsal copy locally, the live catalog only in the acceptance run")
	}
	if opts.DlsiteDSN == "" {
		return nil, fmt.Errorf("DLsite mirror DSN is required (--dlsite-dsn); refusing to guess")
	}
	db, err := openGorm(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect catalog db: %w", err)
	}
	if sqlDB, e := db.DB(); e == nil {
		defer sqlDB.Close()
	}
	dlsiteDB, err := openGorm(opts.DlsiteDSN)
	if err != nil {
		return nil, fmt.Errorf("connect DLsite mirror db: %w", err)
	}
	if sqlDB, e := dlsiteDB.DB(); e == nil {
		defer sqlDB.Close()
	}

	reg, err := resolveRegistry(ctx, db)
	if err != nil {
		return nil, err
	}
	taxonomy, err := loadTaxonomy(ctx, dlsiteDB)
	if err != nil {
		return nil, fmt.Errorf("load genre taxonomy: %w", err)
	}
	if len(taxonomy) == 0 {
		// An unfilled vocabulary must fail loudly, never silently degrade every
		// genre to its embedded ja name (the prod dlsite DB ships WITHOUT wave
		// 67's rows — they must be copied / re-fetched before the first apply).
		return nil, fmt.Errorf("genre_taxonomy has no %s rows — load wave 67's vocabulary into this mirror first", taxonomyLocale)
	}
	cands, err := loadCandidates(ctx, db, reg, opts.Limit, opts.Offset)
	if err != nil {
		return nil, fmt.Errorf("load candidates: %w", err)
	}

	st := &Stats{TaxonomyRows: len(taxonomy), Candidates: len(cands)}
	w := &writer{db: db, stats: st}
	nameFreq := map[string]int{}

	worknos := make([]string, 0, len(cands))
	for _, c := range cands {
		if !c.Bundle {
			worknos = append(worknos, c.Workno)
		}
	}
	mirror, err := loadMirrorGenres(ctx, dlsiteDB, worknos)
	if err != nil {
		return nil, fmt.Errorf("load DLsite mirror genres: %w", err)
	}

	for _, c := range cands {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if c.Bundle {
			st.SkippedBundle++
			continue
		}
		raw, ok := mirror[c.Workno]
		if !ok {
			st.MissingMirror++
			continue
		}
		for _, g := range resolveGenres(raw, taxonomy, st) {
			st.Planned++
			nameFreq[g.Name]++
			s := Sample{WorkID: c.WorkID, Workno: c.Workno, GenreID: g.ID, Name: g.Name, FromTaxonomy: g.FromTaxonomy}
			collect(&st.Samples, s)
			if !g.FromTaxonomy {
				collect(&st.FallbackSamples, s)
			}
			w.write(ctx, plannedRow{
				WorkID: c.WorkID, SourceID: reg.dlsiteSource, Name: g.Name,
			}, opts.Apply)
		}
	}

	if err := w.touch(ctx); err != nil {
		return nil, fmt.Errorf("touch works: %w", err)
	}

	st.DistinctNames = len(nameFreq)
	st.TopNames = topNames(nameFreq)

	slog.Info("backfill-dlsite-genres done", "apply", opts.Apply,
		"taxonomy_rows", st.TaxonomyRows, "candidates", st.Candidates,
		"skipped_bundle", st.SkippedBundle, "missing_mirror", st.MissingMirror, "no_genres", st.NoGenres, "not_array", st.NotArray,
		"zh_hit", st.ZhHit, "ja_fallback", st.JaFallback, "name_blank", st.NameBlank,
		"dup_collapsed", st.DupCollapsed, "planned", st.Planned,
		"distinct_names", st.DistinctNames, "written", st.Written, "conflict", st.Conflict,
		"errors", st.Errors)
	for _, nf := range st.TopNames {
		slog.Info("backfill-dlsite-genres top genre", "name", nf.Name, "works", nf.Works)
	}
	for _, s := range st.Samples {
		slog.Info("backfill-dlsite-genres sample", "work_id", s.WorkID, "workno", s.Workno,
			"genre_id", s.GenreID, "name", s.Name, "from_taxonomy", s.FromTaxonomy)
	}
	for _, s := range st.FallbackSamples {
		slog.Info("backfill-dlsite-genres retired-id fallback", "work_id", s.WorkID,
			"workno", s.Workno, "genre_id", s.GenreID, "name", s.Name)
	}
	return st, nil
}

func topNames(freq map[string]int) []NameFreq {
	out := make([]NameFreq, 0, len(freq))
	for name, works := range freq {
		out = append(out, NameFreq{Name: name, Works: works})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Works != out[j].Works {
			return out[i].Works > out[j].Works
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > maxTopNames {
		out = out[:maxTopNames]
	}
	return out
}

func collect(dst *[]Sample, s Sample) {
	if len(*dst) >= maxSamples {
		return
	}
	*dst = append(*dst, s)
}

func openGorm(dsn string) (*gorm.DB, error) {
	return database.OpenJob(dsn)
}
