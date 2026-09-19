package releasemeta

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/sourcedate"

	"gorm.io/gorm"
)

const maxSamples = 8

const (
	laneVNDB = "vndb"
	laneDL   = "dl"
	laneGC   = "gc"
	laneEG   = "eg"
	laneBGM  = "bgm"

	kindFill  = "fill"
	kindMove  = "move"
	kindClear = "clear"
)

type Opts struct {
	Apply     bool
	DSN       string
	DlsiteDSN string
	EGDSN     string
	GetchuDSN string
	Limit     int
	Offset    int
	Receipts  string
}

type DateSample struct {
	Lane      string
	Kind      string
	WorkID    int64
	ReleaseID int64
	Ext       string
	Old, New  string
}

type RatingSample struct {
	WorkID int64
	Source string
	Ext    string
	Rating int16
}

type Stats struct {
	DatesCandidates int
	DatesSame       int
	DatesUnknown    int
	DatesHuman      int
	DatesWritten    int
	DatesLost       int

	AllFilled  int
	AllMoved   int
	AllCleared int

	VndbDateFilled  int
	VndbDateMoved   int
	VndbDateCleared int
	DlDateFilled    int
	DlDateMoved     int
	DlDateCleared   int
	GcDateFilled    int
	GcDateMoved     int
	GcDateCleared   int
	EgDateFilled    int
	EgDateMoved     int
	EgDateCleared   int
	BgmDateFilled   int
	BgmDateMoved    int
	BgmDateCleared  int

	VndbMissing int
	DlMissing   int
	GcMissing   int
	EgMissing   int
	BgmMissing  int

	RatingCandidates      int
	RatingVndbR18         int
	RatingDlR18           int
	RatingDlSensitive     int
	RatingDlAllAges       int
	RatingEgR18           int
	RatingBgmR18          int
	RatingNoVerdict       int
	RatingPlanned         int
	RatingFilled          int
	RatingSkippedNonEmpty int
	RatingCuratedOverride int

	Errors int

	DateSamples   []DateSample
	RatingSamples []RatingSample
}

func (st *Stats) DateLogArgs() []any {
	return []any{
		"dates_candidates", st.DatesCandidates,
		"dates_same", st.DatesSame,
		"dates_unknown", st.DatesUnknown,
		"dates_human", st.DatesHuman,
		"dates_written", st.DatesWritten,
		"dates_lost", st.DatesLost,
		"all_filled", st.AllFilled,
		"all_moved", st.AllMoved,
		"all_cleared", st.AllCleared,
		"vndb_date_filled", st.VndbDateFilled,
		"vndb_date_moved", st.VndbDateMoved,
		"vndb_date_cleared", st.VndbDateCleared,
		"dl_date_filled", st.DlDateFilled,
		"dl_date_moved", st.DlDateMoved,
		"dl_date_cleared", st.DlDateCleared,
		"gc_date_filled", st.GcDateFilled,
		"gc_date_moved", st.GcDateMoved,
		"gc_date_cleared", st.GcDateCleared,
		"eg_date_filled", st.EgDateFilled,
		"eg_date_moved", st.EgDateMoved,
		"eg_date_cleared", st.EgDateCleared,
		"bgm_date_filled", st.BgmDateFilled,
		"bgm_date_moved", st.BgmDateMoved,
		"bgm_date_cleared", st.BgmDateCleared,
		"vndb_missing", st.VndbMissing,
		"dl_missing", st.DlMissing,
		"gc_missing", st.GcMissing,
		"eg_missing", st.EgMissing,
		"bgm_missing", st.BgmMissing,
	}
}

func Run(ctx context.Context, opts Opts) (*Stats, error) {
	if opts.DSN == "" {
		return nil, fmt.Errorf("catalog DSN is required (--dsn); refusing to guess — pass the rehearsal copy locally, the live catalog only in the acceptance run")
	}
	if opts.DlsiteDSN == "" {
		return nil, fmt.Errorf("DLsite mirror DSN is required (--dlsite-dsn); refusing to guess")
	}
	if opts.EGDSN == "" {
		return nil, fmt.Errorf("EG mirror DSN is required (--eg-dsn); refusing to guess")
	}
	if opts.GetchuDSN == "" {
		return nil, fmt.Errorf("Getchu mirror DSN is required (--getchu-dsn); refusing to guess")
	}
	db, err := openGorm(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect catalog db: %w", err)
	}
	defer closeDB(db)
	dlDB, err := openGorm(opts.DlsiteDSN)
	if err != nil {
		return nil, fmt.Errorf("connect DLsite mirror db: %w", err)
	}
	defer closeDB(dlDB)
	egDB, err := openGorm(opts.EGDSN)
	if err != nil {
		return nil, fmt.Errorf("connect EG mirror db: %w", err)
	}
	defer closeDB(egDB)
	gcDB, err := openGorm(opts.GetchuDSN)
	if err != nil {
		return nil, fmt.Errorf("connect Getchu mirror db: %w", err)
	}
	defer closeDB(gcDB)

	reg, err := resolveRegistry(ctx, db)
	if err != nil {
		return nil, err
	}
	st := &Stats{}
	w := &writer{db: db, stats: st, apply: opts.Apply, receiptsPath: opts.Receipts}
	defer func() { _ = w.closeReceipts() }()

	now := time.Now()
	maxYear := sourcedate.MaxYear(now)

	if err := runDateSync(ctx, db, dlDB, egDB, gcDB, w, reg, opts, now, maxYear); err != nil {
		return nil, err
	}
	if err := runRatingLane(ctx, db, dlDB, egDB, w, reg, opts); err != nil {
		return nil, err
	}
	if err := w.touch(ctx); err != nil {
		return nil, fmt.Errorf("touch works: %w", err)
	}
	if err := w.closeReceipts(); err != nil {
		return nil, err
	}

	done := []any{"apply", opts.Apply}
	done = append(done, st.DateLogArgs()...)
	done = append(done,
		"rating_candidates", st.RatingCandidates,
		"rating_vndb_r18", st.RatingVndbR18,
		"rating_dl_r18", st.RatingDlR18, "rating_dl_sensitive", st.RatingDlSensitive,
		"rating_dl_all_ages", st.RatingDlAllAges,
		"rating_eg_r18", st.RatingEgR18,
		"rating_bgm_r18", st.RatingBgmR18,
		"rating_no_verdict", st.RatingNoVerdict, "rating_planned", st.RatingPlanned,
		"rating_filled", st.RatingFilled, "rating_skipped_non_empty", st.RatingSkippedNonEmpty,
		"rating_curated_override", st.RatingCuratedOverride,
		"errors", st.Errors)
	slog.Info("backfill-release-meta done", done...)
	for _, s := range st.DateSamples {
		slog.Info("backfill-release-meta date sample",
			"lane", s.Lane, "kind", s.Kind,
			"work_id", s.WorkID, "release_id", s.ReleaseID, "ext", s.Ext,
			"old", s.Old, "new", s.New)
	}
	for _, s := range st.RatingSamples {
		slog.Info("backfill-release-meta rating sample",
			"work_id", s.WorkID, "source", s.Source, "ext", s.Ext, "content_rating", s.Rating)
	}
	return st, nil
}

func collectDate(st *Stats, s DateSample) {
	n := 0
	for _, have := range st.DateSamples {
		if have.Kind == s.Kind {
			n++
		}
	}
	if n < maxSamples {
		st.DateSamples = append(st.DateSamples, s)
	}
}

func collectRating(dst *[]RatingSample, s RatingSample) {
	if len(*dst) < maxSamples {
		*dst = append(*dst, s)
	}
}

func window[T any](in []T, limit, offset int) []T {
	if offset > 0 {
		if offset >= len(in) {
			return nil
		}
		in = in[offset:]
	}
	if limit > 0 && limit < len(in) {
		in = in[:limit]
	}
	return in
}

func openGorm(dsn string) (*gorm.DB, error) {
	return database.OpenJob(dsn)
}

func closeDB(db *gorm.DB) {
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.Close()
	}
}
