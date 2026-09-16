package worktags

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"api/internal/infrastructure/database"

	"gorm.io/gorm"
)

const ruleTitleYear = "rule:bgm-title-year"

const (
	maxSamples  = 8
	maxTopNames = 10
)

type Opts struct {
	Apply  bool
	DSN    string
	Limit  int
	Offset int
}

type Sample struct {
	WorkID    int64
	SubjectID int64
	Name      string
	Count     int
}

type NameFreq struct {
	Name  string
	Works int
}

type Stats struct {
	Candidates   int
	NoTags       int
	NotArray     int
	NameBlank    int
	DupCollapsed int
	Planned      int
	Written      int
	Conflict     int
	Errors       int
	FirstError   string

	SexualInherited int

	DistinctNames int
	TopNames      []NameFreq
	Samples       []Sample
}

func Run(ctx context.Context, opts Opts) (*Stats, error) {
	if opts.DSN == "" {
		return nil, fmt.Errorf("catalog DSN is required (--dsn); refusing to guess — pass the rehearsal copy locally, the live catalog only in the acceptance run")
	}
	db, err := openGorm(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect catalog db: %w", err)
	}
	if sqlDB, e := db.DB(); e == nil {
		defer sqlDB.Close()
	}

	reg, err := resolveRegistry(ctx, db)
	if err != nil {
		return nil, err
	}
	cands, err := loadCandidates(ctx, db, reg, opts.Limit, opts.Offset)
	if err != nil {
		return nil, fmt.Errorf("load candidates: %w", err)
	}

	st := &Stats{Candidates: len(cands)}
	w := &writer{db: db, source: reg.bangumiSource, stats: st}
	nameFreq := map[string]int{}

	for _, c := range cands {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		tags := parseSubjectTags(c.Tags, st)
		if tags == nil {
			continue
		}
		for _, tg := range tags {
			st.Planned++
			nameFreq[tg.Name]++
			collect(&st.Samples, Sample{WorkID: c.WorkID, SubjectID: c.SubjectID, Name: tg.Name, Count: tg.Count})
			w.write(ctx, plannedRow{
				WorkID: c.WorkID, SourceID: reg.bangumiSource,
				Name: tg.Name, Count: tg.Count,
			}, opts.Apply)
		}
	}

	if err := w.finish(ctx, opts.Apply); err != nil {
		return nil, fmt.Errorf("finish writes: %w", err)
	}

	st.DistinctNames = len(nameFreq)
	st.TopNames = topNames(nameFreq)

	slog.Info("backfill-work-tags done", "apply", opts.Apply,
		"candidates", st.Candidates, "no_tags", st.NoTags, "not_array", st.NotArray,
		"name_blank", st.NameBlank, "dup_collapsed", st.DupCollapsed,
		"planned", st.Planned, "distinct_names", st.DistinctNames,
		"written", st.Written, "conflict", st.Conflict, "sexual_inherited", st.SexualInherited,
		"errors", st.Errors)
	for _, nf := range st.TopNames {
		slog.Info("backfill-work-tags top tag", "name", nf.Name, "works", nf.Works)
	}
	for _, s := range st.Samples {
		slog.Info("backfill-work-tags sample", "work_id", s.WorkID, "subject_id", s.SubjectID,
			"name", s.Name, "count", s.Count)
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
