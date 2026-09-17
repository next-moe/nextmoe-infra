package vndbcovers

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/repository"
	"api/pkg/config"
	"api/pkg/imageclient"

	"gorm.io/gorm"
)

const defaultTimeout = 60 * time.Second

type Opts struct {
	DSN          string
	Apply        bool
	Limit        int
	Offset       int
	IDs          []int64
	ImageBaseURL string
	UploadGap    time.Duration
	APIBase      string
	Manifest     string
	FromDump     bool
	ImageDir     string
	MirrorOnly   bool
	FilesOut     string
	Workers      int
}

type Stats struct {
	Candidates int
	NoImage    int
	Portrait   int
	Landscape  int
	Planned    int
	Uploaded   int
	Dedup      int
	Rejected   int
	Errors     int
	Local      int
	Quota      bool
	Unrated    int
	Missing    int
	ToFetch    int
}

func (s Stats) String() string {
	return fmt.Sprintf("candidates=%d no_image=%d portrait=%d landscape=%d planned=%d uploaded=%d dedup=%d rejected=%d errors=%d local=%d quota=%t unrated=%d missing=%d to_fetch=%d",
		s.Candidates, s.NoImage, s.Portrait, s.Landscape, s.Planned, s.Uploaded, s.Dedup, s.Rejected, s.Errors, s.Local, s.Quota,
		s.Unrated, s.Missing, s.ToFetch)
}

type imageUploader interface {
	UploadWithSub(ctx context.Context, r io.Reader, filename, preset, uploaderSub string) (*imageclient.UploadResult, error)
	ReferencePing(ctx context.Context, hashes []string) (*imageclient.ReferencePingResult, error)
	Health(ctx context.Context) error
}

type runner struct {
	db         *gorm.DB
	cli        imageUploader
	sourceID   int16
	gap        time.Duration
	imageDir   string
	mirrorOnly bool
	stats      *Stats
	touched    []int64
	pingHashes []string

	mu       sync.Mutex
	paceLast time.Time
}

func (r *runner) bump(f func(*Stats)) {
	r.mu.Lock()
	f(r.stats)
	r.mu.Unlock()
}

func (r *runner) stopped() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stats.Quota
}

func (r *runner) drain(ctx context.Context, rows []planRow, workers int) {
	if workers < 1 {
		workers = 1
	}
	jobs := make(chan planRow)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for row := range jobs {
				if ctx.Err() != nil || r.stopped() {
					continue
				}
				r.fill(ctx, row)
			}
		}()
	}
	for i, row := range rows {
		if ctx.Err() != nil || r.stopped() {
			break
		}
		jobs <- row
		if (i+1)%1000 == 0 {
			r.mu.Lock()
			slog.Info("vndb-covers progress", "queued", i+1, "of", len(rows),
				"uploaded", r.stats.Uploaded, "dedup", r.stats.Dedup, "errors", r.stats.Errors, "local", r.stats.Local)
			r.mu.Unlock()
		}
	}
	close(jobs)
	wg.Wait()
}

func Run(ctx context.Context, cfg *config.Config, opts Opts) (*Stats, error) {
	if opts.DSN == "" {
		return nil, fmt.Errorf("catalog DSN is required (--dsn); refusing to guess — pass the rehearsal copy locally, the live catalog only in the production run")
	}
	if opts.FromDump && opts.Manifest != "" {
		return nil, fmt.Errorf("--from-dump and --manifest are two sources for the same metadata; pass one")
	}
	if (opts.MirrorOnly || opts.FilesOut != "") && opts.ImageDir == "" {
		return nil, fmt.Errorf("--mirror-only and --files-out need --image-dir")
	}
	clientCfg := cfg.CatalogImageClient
	if opts.Apply && (clientCfg.ClientID == "" || clientCfg.ClientSecret == "") {
		return nil, fmt.Errorf("catalog image client not configured (set KUN_CATALOG_IMAGE_CLIENT_ID/SECRET); refusing to --apply cover upload")
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
	cands, err := loadCandidates(ctx, db, reg, opts.IDs)
	if err != nil {
		return nil, fmt.Errorf("load candidates: %w", err)
	}
	cands = window(cands, opts.Offset)
	stats := &Stats{Candidates: len(cands)}
	slog.Info("vndb-covers candidates", "works", len(cands), "apply", opts.Apply,
		"offset", opts.Offset, "limit", opts.Limit, "explicit_ids", len(opts.IDs))

	var images map[string]*vnImage
	switch {
	case opts.FromDump:
		if images, stats.Unrated, err = loadDumpImages(ctx, db, anchorIDs(cands)); err != nil {
			return stats, err
		}
		slog.Info("vndb dump images", "entries", len(images), "unrated", stats.Unrated)
	case opts.Manifest != "":
		if images, err = loadManifest(opts.Manifest); err != nil {
			return stats, fmt.Errorf("load manifest: %w", err)
		}
		slog.Info("vndb image manifest", "entries", len(images), "path", opts.Manifest)
	default:
		api := newVNDBAPI(opts.APIBase)
		if images, err = api.fetchImages(ctx, anchorIDs(cands)); err != nil {
			return stats, fmt.Errorf("query vndb api: %w", err)
		}
	}
	plan := buildPlan(cands, images, stats)
	if opts.FilesOut != "" {
		if stats.ToFetch, err = writeFilesOut(opts.FilesOut, opts.ImageDir, plan); err != nil {
			return stats, fmt.Errorf("write --files-out: %w", err)
		}
	}
	printForecast(plan, opts)
	if !opts.Apply {
		printTotals(stats)
		slog.Info("vndb-covers done (dry run)", "result", stats.String())
		return stats, nil
	}

	r := &runner{db: db, sourceID: reg.vndbSource, gap: opts.UploadGap, imageDir: opts.ImageDir,
		mirrorOnly: opts.MirrorOnly, stats: stats}
	r.cli = imageclient.New(imageclient.Config{
		BaseURL:      resolveBaseURL(cfg, clientCfg, opts.ImageBaseURL),
		CDNBase:      cfg.ImageService.CDNBase,
		ClientID:     clientCfg.ClientID,
		ClientSecret: clientCfg.ClientSecret,
		Timeout:      defaultTimeout,
	})
	hctx, hcancel := context.WithTimeout(ctx, 5*time.Second)
	defer hcancel()
	if err := r.cli.Health(hctx); err != nil {
		return stats, fmt.Errorf("image_service unreachable at %s: %w", resolveBaseURL(cfg, clientCfg, opts.ImageBaseURL), err)
	}

	r.drain(ctx, actionable(plan, opts.Limit), opts.Workers)
	if err := repository.TouchWorks(ctx, db, r.touched); err != nil {
		return stats, fmt.Errorf("touch works: %w", err)
	}
	if err := r.ping(ctx); err != nil {
		slog.Warn("refping fresh hashes", "err", err)
	}
	printTotals(stats)
	slog.Info("vndb-covers done", "result", stats.String())
	if stats.Quota {
		return stats, fmt.Errorf("image quota exceeded — aborted (rerun to resume; idempotent)")
	}
	return stats, nil
}

func openGorm(dsn string) (*gorm.DB, error) {
	return database.OpenJob(dsn)
}

func resolveBaseURL(cfg *config.Config, clientCfg config.ImageClientConfig, override string) string {
	if override != "" {
		return override
	}
	if clientCfg.BaseURL != "" {
		return clientCfg.BaseURL
	}
	return fmt.Sprintf("http://%s:%d", cfg.ImageService.Host, cfg.ImageService.Port)
}

func window(c []candidate, offset int) []candidate {
	if offset > 0 {
		if offset >= len(c) {
			return nil
		}
		c = c[offset:]
	}
	return c
}

func (r *runner) ping(ctx context.Context) error {
	if r.cli == nil || len(r.pingHashes) == 0 {
		return nil
	}
	for i := 0; i < len(r.pingHashes); i += 1000 {
		batch := r.pingHashes[i:min(i+1000, len(r.pingHashes))]
		if _, err := r.cli.ReferencePing(ctx, batch); err != nil {
			return err
		}
	}
	return nil
}
