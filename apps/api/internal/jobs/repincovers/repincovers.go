package repincovers

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/editspec"
	"api/pkg/config"
	"api/pkg/imageclient"

	"gorm.io/gorm"
)

const defaultTimeout = 60 * time.Second

const metaBatchSize = 1000

type Opts struct {
	DSN     string
	Apply   bool
	IDs     []int64
	Limit   int
	PlanOut string

	ExportDir        string
	ReinjectDir      string
	PurgeBadUpscales bool

	ImageBaseURL string
	UploadGap    time.Duration
}

type Stats struct {
	Works      int
	Covers     int
	NoDims     int
	Agreed     int
	NoWinner   int
	DirectPin  int
	NeedUpsc   int
	DeferNSFW  int
	Exported   int
	Uploaded   int
	Repinned   int
	Purged     int
	Skipped    int
	Errors     int
	QuotaBreak bool
}

func (s Stats) String() string {
	return fmt.Sprintf("works=%d covers=%d no_dims=%d agreed=%d no_winner=%d direct_pin=%d need_upscale=%d nsfw_deferred=%d exported=%d uploaded=%d repinned=%d purged=%d skipped=%d errors=%d quota=%t",
		s.Works, s.Covers, s.NoDims, s.Agreed, s.NoWinner, s.DirectPin, s.NeedUpsc, s.DeferNSFW,
		s.Exported, s.Uploaded, s.Repinned, s.Purged, s.Skipped, s.Errors, s.QuotaBreak)
}

func Run(ctx context.Context, cfg *config.Config, opts Opts) (*Stats, error) {
	if opts.DSN == "" {
		return nil, fmt.Errorf("--dsn is REQUIRED; refusing to guess a catalog database")
	}
	if err := checkModes(opts); err != nil {
		return nil, err
	}
	db, err := open(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect catalog: %w", err)
	}
	defer closeDB(db)

	r := &runner{db: db, stats: &Stats{}, gap: opts.UploadGap}
	if needsImageClient(opts) {
		if r.cli, err = newImageClient(ctx, cfg, opts); err != nil {
			return nil, err
		}
	}

	if opts.PurgeBadUpscales {
		err = r.purge(ctx, opts)
		slog.Info("repin-covers done", "mode", "purge", "apply", opts.Apply, "result", r.stats.String())
		return r.stats, err
	}

	plans, err := r.plan(ctx, opts)
	if err != nil {
		return r.stats, err
	}
	if opts.PlanOut != "" {
		if err := writePlanCSV(opts.PlanOut, plans, r.url); err != nil {
			return r.stats, fmt.Errorf("write plan csv: %w", err)
		}
		slog.Info("plan written", "path", opts.PlanOut, "rows", len(plans))
	}

	switch {
	case opts.ExportDir != "":
		err = r.export(ctx, opts, plans)
	case opts.ReinjectDir != "":
		err = r.reinject(ctx, opts, plans)
	case opts.Apply:
		err = r.directPin(ctx, opts, plans)
	}
	slog.Info("repin-covers done", "apply", opts.Apply, "result", r.stats.String())
	return r.stats, err
}

func checkModes(o Opts) error {
	n := 0
	for _, on := range []bool{o.ExportDir != "", o.ReinjectDir != "", o.PurgeBadUpscales} {
		if on {
			n++
		}
	}
	if n > 1 {
		return fmt.Errorf("--export-dir, --reinject-dir and --purge-bad-upscales are mutually exclusive")
	}
	if o.PurgeBadUpscales && !o.Apply {
		slog.Warn("purge is a DRY listing without --apply")
	}
	return nil
}

func needsImageClient(o Opts) bool { return !o.PurgeBadUpscales }

func newImageClient(ctx context.Context, cfg *config.Config, opts Opts) (*imageclient.Client, error) {
	c := cfg.CatalogImageClient
	if c.ClientID == "" || c.ClientSecret == "" {
		return nil, fmt.Errorf("catalog image client credentials are not configured")
	}
	base := opts.ImageBaseURL
	if base == "" {
		base = c.BaseURL
	}
	if base == "" {
		base = fmt.Sprintf("http://%s:%d", cfg.ImageService.Host, cfg.ImageService.Port)
	}
	cli := imageclient.New(imageclient.Config{
		BaseURL: base, CDNBase: cfg.ImageService.CDNBase,
		ClientID: c.ClientID, ClientSecret: c.ClientSecret, Timeout: defaultTimeout,
	})
	hctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := cli.Health(hctx); err != nil {
		return nil, fmt.Errorf("image_service unreachable at %s: %w", base, err)
	}
	return cli, nil
}

type runner struct {
	db      *gorm.DB
	cli     *imageclient.Client
	gap     time.Duration
	stats   *Stats
	touched []int64
	fresh   []string
}

type coverRow struct {
	ID        int64  `gorm:"column:id"`
	WorkID    int64  `gorm:"column:work_id"`
	Hash      string `gorm:"column:image_hash"`
	Kind      string `gorm:"column:kind"`
	SourceKey string `gorm:"column:source_key"`
	Sexual    int16  `gorm:"column:sexual"`
	SortOrder int    `gorm:"column:sort_order"`
	Pinned    bool   `gorm:"column:portrait_pinned"`
}

func (r *runner) plan(ctx context.Context, opts Opts) ([]Plan, error) {
	q := r.db.WithContext(ctx).Raw(fmt.Sprintf(`
		SELECT c.id, c.work_id, c.image_hash, c.kind, s.key AS source_key,
		       c.sexual, c.sort_order, c.portrait_pinned
		FROM catalog_work_cover c
		JOIN catalog_source s ON s.id = c.source_id
		WHERE c.work_id IN (SELECT work_id FROM catalog_work_cover WHERE portrait_pinned)
		  AND c.work_id NOT IN (SELECT work_id FROM catalog_work_cover WHERE portrait_pinned AND %s)
		  AND (NOT c.merged_in OR NOT EXISTS (SELECT 1 FROM catalog_work_cover o
		                                       WHERE o.work_id = c.work_id AND NOT o.merged_in))
		ORDER BY c.work_id, c.sort_order, c.image_hash`, editspec.CuratedLaneSQL("source_id")))
	var rows []coverRow
	if err := q.Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load covers: %w", err)
	}
	want := idSet(opts.IDs)
	byWork := map[int64][]Cover{}
	hashes := make([]string, 0, len(rows))
	for _, row := range rows {
		if want != nil && !want[row.WorkID] {
			continue
		}
		byWork[row.WorkID] = append(byWork[row.WorkID], Cover{
			ID: row.ID, WorkID: row.WorkID, Hash: row.Hash, Kind: row.Kind,
			SourceKey: row.SourceKey, Sexual: row.Sexual, SortOrder: row.SortOrder, Pinned: row.Pinned,
		})
		hashes = append(hashes, row.Hash)
	}
	meta, err := r.loadMeta(ctx, hashes)
	if err != nil {
		return nil, err
	}
	r.stats.Works = len(byWork)
	r.stats.Covers = len(hashes)

	workIDs := make([]int64, 0, len(byWork))
	for id := range byWork {
		workIDs = append(workIDs, id)
	}
	slices.Sort(workIDs)

	plans := make([]Plan, 0, len(workIDs))
	for _, id := range workIDs {
		covers := byWork[id]
		for i := range covers {
			if m, ok := meta[covers[i].Hash]; ok && m.Width > 0 && m.Height > 0 {
				covers[i].Width, covers[i].Height, covers[i].DimsKnown = m.Width, m.Height, true
			} else {
				r.stats.NoDims++
			}
		}
		p := planWork(id, covers)
		switch {
		case p.New == nil:
			r.stats.NoWinner++
		case p.Action == ActionNone:
			r.stats.Agreed++
		case p.Action == ActionDirectPin:
			r.stats.DirectPin++
		case p.Action == ActionUpscale:
			r.stats.NeedUpsc++
		case p.Action == ActionDeferredNSFW:
			r.stats.DeferNSFW++
		}
		plans = append(plans, p)
	}
	return plans, nil
}

func (r *runner) loadMeta(ctx context.Context, hashes []string) (map[string]imageclient.ImageMeta, error) {
	out := make(map[string]imageclient.ImageMeta, len(hashes))
	for i := 0; i < len(hashes); i += metaBatchSize {
		batch := hashes[i:min(i+metaBatchSize, len(hashes))]
		m, err := r.cli.MetaBatch(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("meta-batch at %d: %w", i, err)
		}
		maps.Copy(out, m)
	}
	return out, nil
}

func actionable(plans []Plan, want Action, limit int) []Plan {
	out := make([]Plan, 0, len(plans))
	for _, p := range plans {
		if p.Action != want {
			continue
		}
		out = append(out, p)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func idSet(ids []int64) map[int64]bool {
	if len(ids) == 0 {
		return nil
	}
	m := make(map[int64]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}

func open(dsn string) (*gorm.DB, error) {
	return database.OpenJob(dsn)
}

func closeDB(db *gorm.DB) {
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
}
