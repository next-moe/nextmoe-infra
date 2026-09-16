package getchuintros

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"api/internal/infrastructure/database"
	"api/internal/jobs/workpop"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const langJa = "ja"

const maxSamples = 8

const previewRunes = 40

type Opts struct {
	DSN        string
	GetchuDSN  string
	Apply      bool
	Limit      int
	Offset     int
	Population workpop.Population
}

type Sample struct {
	WorkID   int64
	GetchuID string
	Preview  string
}

type Stats struct {
	Works      int
	NoStory    int
	SkipBundle int
	SkipHasJa  int
	Planned    int
	Written    int
	Conflict   int
	Errors     int

	PlanSamples    []Sample
	NoStorySamples []Sample
}

func (s Stats) String() string {
	return fmt.Sprintf("works=%d no_story=%d skip_bundle=%d skip_has_ja=%d planned=%d written=%d conflict=%d errors=%d",
		s.Works, s.NoStory, s.SkipBundle, s.SkipHasJa, s.Planned, s.Written, s.Conflict, s.Errors)
}

func Run(ctx context.Context, opts Opts) (*Stats, error) {
	if opts.DSN == "" || opts.GetchuDSN == "" {
		return nil, fmt.Errorf("--dsn and --getchu-dsn are both REQUIRED; refusing to guess either")
	}
	db, err := open(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect catalog: %w", err)
	}
	defer closeDB(db)
	gdb, err := open(opts.GetchuDSN)
	if err != nil {
		return nil, fmt.Errorf("connect getchu staging: %w", err)
	}
	defer closeDB(gdb)

	var source int16
	if err := db.WithContext(ctx).
		Raw(`SELECT id FROM catalog_source WHERE key = 'getchu'`).Scan(&source).Error; err != nil {
		return nil, err
	}
	if source == 0 {
		return nil, fmt.Errorf("catalog_source has no getchu row — seed it first (refs/proj/167)")
	}

	anchors, err := loadAnchors(ctx, db, source, opts.Population, opts.Limit, opts.Offset)
	if err != nil {
		return nil, err
	}
	stories, err := loadStories(ctx, gdb)
	if err != nil {
		return nil, err
	}
	cands := pickStory(anchors, stories)

	workIDs := make([]int64, 0, len(cands))
	for _, c := range cands {
		workIDs = append(workIDs, c.WorkID)
	}
	exist, err := preloadExistingLangs(ctx, db, workIDs)
	if err != nil {
		return nil, fmt.Errorf("preload existing intro langs: %w", err)
	}

	r := &runner{db: db, source: source, exist: exist, stats: &Stats{Works: len(cands)}}
	slog.Info("getchu-intros candidates", "works", len(cands), "stories", len(stories),
		"apply", opts.Apply, "population", opts.Population, "offset", opts.Offset, "limit", opts.Limit)
	for _, c := range cands {
		if ctx.Err() != nil {
			break
		}
		r.enrich(ctx, c, opts.Apply)
	}
	if err := r.touch(ctx); err != nil {
		return nil, fmt.Errorf("touch works: %w", err)
	}

	slog.Info("getchu-intros done", "apply", opts.Apply, "result", r.stats.String())
	logSamples("planned", r.stats.PlanSamples)
	logSamples("no_story", r.stats.NoStorySamples)
	return r.stats, nil
}

type candidate struct {
	WorkID   int64
	GetchuID string
	Story    string
	Bundle   bool
}

func pickStory(anchors []anchorRow, stories map[string]string) []candidate {
	var out []candidate
	for i := 0; i < len(anchors); {
		work := anchors[i].WorkID
		c := candidate{WorkID: work, GetchuID: anchors[i].GetchuID}
		for ; i < len(anchors) && anchors[i].WorkID == work; i++ {
			if c.Story != "" {
				continue
			}
			s := strings.TrimSpace(stories[anchors[i].GetchuID])
			if s == "" {
				continue
			}
			if anchors[i].Bundle {
				c.Bundle = true
				continue
			}
			c.GetchuID, c.Story = anchors[i].GetchuID, s
		}
		out = append(out, c)
	}
	return out
}

type runner struct {
	db      *gorm.DB
	source  int16
	exist   map[int64]map[string]bool
	stats   *Stats
	touched []int64
}

func (r *runner) touch(ctx context.Context) error {
	return repository.TouchWorks(ctx, r.db, r.touched)
}

func (r *runner) enrich(ctx context.Context, c candidate, apply bool) {
	if c.Story == "" && c.Bundle {
		r.stats.SkipBundle++
		return
	}
	if c.Story == "" {
		r.stats.NoStory++
		r.collect(&r.stats.NoStorySamples, c)
		return
	}
	if r.exist[c.WorkID][langJa] {
		r.stats.SkipHasJa++
		return
	}

	r.stats.Planned++
	r.collect(&r.stats.PlanSamples, c)
	if !apply {
		return
	}

	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "work_id"}, {Name: "lang"}, {Name: "source_id"}},
		DoNothing: true,
	}).Create(&model.CatalogWorkIntro{
		WorkID: c.WorkID, Lang: langJa, Intro: c.Story, SourceID: r.source,
	})
	switch {
	case res.Error != nil:
		r.stats.Errors++
		slog.Warn("write work intro", "work", c.WorkID, "getchu", c.GetchuID, "err", res.Error)
	case res.RowsAffected == 0:
		r.stats.Conflict++
	default:
		r.stats.Written++
		r.touched = append(r.touched, c.WorkID)
		set := r.exist[c.WorkID]
		if set == nil {
			set = map[string]bool{}
			r.exist[c.WorkID] = set
		}
		set[langJa] = true
	}
}

func (r *runner) collect(dst *[]Sample, c candidate) {
	if len(*dst) >= maxSamples {
		return
	}
	*dst = append(*dst, Sample{WorkID: c.WorkID, GetchuID: c.GetchuID, Preview: preview(c.Story)})
}

func preview(s string) string {
	s = strings.NewReplacer("\n", " ", "\r", " ").Replace(s)
	runes := []rune(s)
	if len(runes) <= previewRunes {
		return s
	}
	return string(runes[:previewRunes]) + "…"
}

func logSamples(category string, samples []Sample) {
	for _, s := range samples {
		slog.Info("getchu-intros sample", "category", category,
			"work_id", s.WorkID, "getchu_id", s.GetchuID, "preview", s.Preview)
	}
}

func open(dsn string) (*gorm.DB, error) {
	return database.OpenJob(dsn)
}

func closeDB(db *gorm.DB) {
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
}
