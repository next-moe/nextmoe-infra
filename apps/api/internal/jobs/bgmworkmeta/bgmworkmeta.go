package bgmworkmeta

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/editspec"

	"gorm.io/gorm"
)

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

type TagSample struct {
	WorkID    int64
	SubjectID int64
	Name      string
}

type FavSample struct {
	WorkID    int64
	SubjectID int64
	Bucket    string
	Metric    int16
	Value     int64
}

type NameFreq struct {
	Name  string
	Works int
}

type Stats struct {
	Candidates int

	MetaNoTags     int
	MetaNotArray   int
	MetaNameBlank  int
	MetaDup        int
	MetaPlanned    int
	MetaSuppressed int
	MetaWritten    int
	MetaConflict   int
	MetaDistinct   int

	FavNoObject   int
	FavUnknownKey int
	FavBadValue   int
	FavPlanned    int
	FavWritten    int
	FavUnchanged  int

	Errors     int
	FirstError string

	MetaTopNames []NameFreq
	MetaSamples  []TagSample
	FavSamples   []FavSample
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

	suppressed, err := editspec.LoadSuppressedWorkTags(ctx, db, reg.bangumiSource)
	if err != nil {
		return nil, err
	}

	st := &Stats{Candidates: len(cands)}
	w := &writer{db: db, stats: st, suppressed: suppressed}
	nameFreq := map[string]int{}

	for _, c := range cands {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		for _, name := range parseMetaTags(c.MetaTags, st) {
			st.MetaPlanned++
			nameFreq[name]++
			collectTag(&st.MetaSamples, TagSample{WorkID: c.WorkID, SubjectID: c.SubjectID, Name: name})
			w.writeTag(ctx, tagRow{WorkID: c.WorkID, SourceID: reg.bangumiSource, Name: name}, opts.Apply)
		}
		for _, b := range parseFavorite(c.Favorite, st) {
			st.FavPlanned++
			collectFav(&st.FavSamples, FavSample{WorkID: c.WorkID, SubjectID: c.SubjectID, Bucket: b.Bucket, Metric: b.Metric, Value: b.Value})
			w.writeFavorite(ctx, favRow{WorkID: c.WorkID, SourceID: reg.bangumiSource, Metric: b.Metric, Value: b.Value}, opts.Apply)
		}
	}

	if err := w.touch(ctx); err != nil {
		return nil, fmt.Errorf("touch works: %w", err)
	}

	st.MetaDistinct = len(nameFreq)
	st.MetaTopNames = topNames(nameFreq)

	slog.Info("backfill-bgm-work-meta done", "apply", opts.Apply,
		"candidates", st.Candidates,
		"meta_no_tags", st.MetaNoTags, "meta_not_array", st.MetaNotArray,
		"meta_name_blank", st.MetaNameBlank, "meta_dup", st.MetaDup,
		"meta_planned", st.MetaPlanned, "meta_distinct_names", st.MetaDistinct,
		"meta_suppressed", st.MetaSuppressed,
		"meta_written", st.MetaWritten, "meta_conflict", st.MetaConflict,
		"fav_no_object", st.FavNoObject, "fav_unknown_key", st.FavUnknownKey,
		"fav_bad_value", st.FavBadValue, "fav_planned", st.FavPlanned,
		"fav_written", st.FavWritten, "fav_unchanged", st.FavUnchanged,
		"errors", st.Errors)
	for _, nf := range st.MetaTopNames {
		slog.Info("backfill-bgm-work-meta top meta tag", "name", nf.Name, "works", nf.Works)
	}
	for _, s := range st.MetaSamples {
		slog.Info("backfill-bgm-work-meta meta sample", "work_id", s.WorkID, "subject_id", s.SubjectID, "name", s.Name)
	}
	for _, s := range st.FavSamples {
		slog.Info("backfill-bgm-work-meta favorite sample", "work_id", s.WorkID, "subject_id", s.SubjectID,
			"bucket", s.Bucket, "metric", s.Metric, "value", s.Value)
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

func collectTag(dst *[]TagSample, s TagSample) {
	if len(*dst) < maxSamples {
		*dst = append(*dst, s)
	}
}

func collectFav(dst *[]FavSample, s FavSample) {
	if len(*dst) < maxSamples {
		*dst = append(*dst, s)
	}
}

func openGorm(dsn string) (*gorm.DB, error) {
	return database.OpenJob(dsn)
}
