package hihyou

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"api/internal/infrastructure/database"
	"api/pkg/config"

	"gorm.io/gorm"
)

type Opts struct {
	Dir          string
	Apply        bool
	NoImages     bool
	SegmentOnly  bool // report the segmentation without touching the database
	Only         []int
	DSN          string
	ImageBase    string
	Concurrency  int   // parallel picture fetch/upload; the database writes stay serial
	PublishActor int64 // uid on the release decisions; 0 leaves rows pending
}

type stats struct {
	issues       int
	quarantined  int
	items        int
	pictures     int
	created      int
	updated      int
	unchanged    int
	truncated    int
	droppedEmpty int
	imagesUp     int
	imagesFail   int
	wouldCreate  int
	wouldUpdate  int
	wouldImages  int
}

type IssueReport struct {
	Issue    int      `json:"issue"`
	CV       int64    `json:"cv"`
	Mode     string   `json:"mode"`
	Items    int      `json:"items"`
	Pictures int      `json:"pictures"`
	Orphans  int      `json:"orphans"`
	Failures []string `json:"failures,omitempty"`
}

type Summary struct {
	Corpus        int            `json:"corpus_issues"`
	Incomplete    []string       `json:"incomplete_files,omitempty"`
	Passing       int            `json:"passing_gate"`
	Quarantined   []IssueReport  `json:"quarantined,omitempty"`
	Items         int            `json:"items"`
	Pictures      int            `json:"pictures"`
	Modes         map[string]int `json:"modes"`
	UnknownHeads  []string       `json:"unknown_headings,omitempty"`
	Created       int            `json:"created,omitempty"`
	Updated       int            `json:"updated,omitempty"`
	Unchanged     int            `json:"unchanged,omitempty"`
	WouldCreate   int            `json:"would_create,omitempty"`
	WouldUpdate   int            `json:"would_update,omitempty"`
	WouldImages   int            `json:"would_images,omitempty"`
	Truncated     int            `json:"truncated,omitempty"`
	DroppedNoBody int            `json:"dropped_no_body,omitempty"`
	ImagesUp      int            `json:"images_uploaded,omitempty"`
	ImagesFailed  int            `json:"images_failed,omitempty"`
	Released      int            `json:"released,omitempty"`
	Apply         bool           `json:"apply"`
}

// Import segments the corpus and, with Apply, writes the items into kun_news as
// status=pending. With PublishActor set, the run ends by releasing every
// pending row of this source: on 2026-09-05 the user ruled the column is
// wholesale-released because bilibili already moderates it, accepting the
// remaining risk (our own extraction errors) on the 08-12 precedent. Without
// the actor, rows wait for the console as before.
func Import(ctx context.Context, cfg *config.Config, opts Opts) (*Summary, error) {
	if opts.Dir == "" {
		return nil, fmt.Errorf("import: --dir is required")
	}
	entries, bad, err := Corpus{Dir: opts.Dir}.Load()
	if err != nil {
		return nil, err
	}
	only := map[int]bool{}
	for _, n := range opts.Only {
		only[n] = true
	}

	sum := &Summary{Incomplete: bad, Modes: map[string]int{}, Apply: opts.Apply}
	st := &stats{}
	unknown := map[string]int{}

	var w *writer
	if !opts.SegmentOnly {
		db, closeDB, err := openNewsDB(cfg, opts.DSN)
		if err != nil {
			return nil, err
		}
		defer closeDB()
		w = newWriter(cfg, db, opts)
		if opts.Apply {
			if err := w.seedSource(ctx); err != nil {
				return nil, err
			}
		}
	}

	for _, e := range entries {
		seg := Segment(e.Article)
		if len(only) > 0 && !only[seg.IssueNo] {
			continue
		}
		sum.Corpus++
		st.issues++
		sum.Modes[seg.Mode]++
		for _, u := range seg.Unknown {
			unknown[u]++
		}
		pics := 0
		for _, it := range seg.Items {
			pics += len(it.Pictures)
		}
		rep := IssueReport{Issue: seg.IssueNo, CV: seg.CV, Mode: seg.Mode,
			Items: len(seg.Items), Pictures: pics, Orphans: seg.Orphans}

		if fail := Gate(seg); len(fail) > 0 {
			rep.Failures = fail
			sum.Quarantined = append(sum.Quarantined, rep)
			st.quarantined++
			continue
		}
		sum.Passing++
		st.items += len(seg.Items)
		st.pictures += pics

		if w == nil {
			continue
		}
		if err := w.applyIssue(ctx, seg, time.Unix(e.Article.Data.PublishTime, 0).UTC(), st); err != nil {
			return nil, err
		}
	}

	if w != nil && opts.Apply && opts.PublishActor != 0 {
		n, err := w.release(ctx, opts.PublishActor)
		if err != nil {
			return nil, fmt.Errorf("standing release: %w", err)
		}
		sum.Released = n
	}

	sum.Items, sum.Pictures = st.items, st.pictures
	sum.Created, sum.Updated, sum.Unchanged = st.created, st.updated, st.unchanged
	sum.WouldCreate, sum.WouldUpdate, sum.WouldImages = st.wouldCreate, st.wouldUpdate, st.wouldImages
	sum.Truncated, sum.DroppedNoBody = st.truncated, st.droppedEmpty
	sum.ImagesUp, sum.ImagesFailed = st.imagesUp, st.imagesFail
	for t := range unknown {
		sum.UnknownHeads = append(sum.UnknownHeads, t)
	}
	sort.Slice(sum.UnknownHeads, func(i, j int) bool {
		a, b := sum.UnknownHeads[i], sum.UnknownHeads[j]
		if unknown[a] != unknown[b] {
			return unknown[a] > unknown[b]
		}
		return a < b
	})
	if len(sum.Quarantined) > 0 {
		slog.Warn("hihyou: issues held out of the queue whole rather than best-efforted",
			"count", len(sum.Quarantined))
	}
	return sum, nil
}

func openNewsDB(cfg *config.Config, dsn string) (*gorm.DB, func(), error) {
	if dsn != "" {
		db, err := database.OpenJobWith(dsn, &gorm.Config{})
		if err != nil {
			return nil, nil, fmt.Errorf("news db (--dsn): %w", err)
		}
		return db, func() {
			if sqlDB, err := db.DB(); err == nil {
				_ = sqlDB.Close()
			}
		}, nil
	}
	conn, err := database.NewPostgresDB(cfg.NewsDatabase)
	if err != nil {
		return nil, nil, fmt.Errorf("news db: %w", err)
	}
	return conn.DB(), func() { _ = conn.Close() }, nil
}
