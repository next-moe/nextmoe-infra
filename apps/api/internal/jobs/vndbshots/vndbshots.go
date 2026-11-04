package vndbshots

import (
	"bytes"
	"context"
	stderrors "errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"
	"api/pkg/config"
	"api/pkg/imageclient"
	"api/pkg/imageshrink"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	shotPreset     = "catalog_screenshot"
	uploaderSub    = "system:vndb-screenshot-backfill"
	uploadRetries  = 6
	defaultTimeout = 60 * time.Second
	DefaultPerWork = 10
)

type Opts struct {
	DSN          string
	Apply        bool
	IDs          []int64
	Limit        int
	PerWork      int
	ImageDir     string
	FilesOut     string
	ImageBaseURL string
	UploadGap    time.Duration
	Workers      int
}

type Stats struct {
	Candidates int
	NoShots    int
	Unrated    int
	Works      int
	Planned    int
	ToFetch    int
	Incomplete int
	Uploaded   int
	Dedup      int
	Rejected   int
	Errors     int
	Quota      bool
}

func (s Stats) String() string {
	return fmt.Sprintf("candidates=%d no_shots=%d unrated=%d works=%d planned=%d to_fetch=%d incomplete=%d uploaded=%d dedup=%d rejected=%d errors=%d quota=%t",
		s.Candidates, s.NoShots, s.Unrated, s.Works, s.Planned, s.ToFetch, s.Incomplete,
		s.Uploaded, s.Dedup, s.Rejected, s.Errors, s.Quota)
}

type imageUploader interface {
	UploadWithSub(ctx context.Context, r io.Reader, filename, preset, uploaderSub string) (*imageclient.UploadResult, error)
	ReferencePing(ctx context.Context, hashes []string) (*imageclient.ReferencePingResult, error)
	Health(ctx context.Context) error
}

type runner struct {
	db       *gorm.DB
	cli      imageUploader
	sourceID int16
	imageDir string
	gap      time.Duration

	mu         sync.Mutex
	stats      *Stats
	touched    []int64
	pingHashes []string

	paceMu   sync.Mutex
	paceLast time.Time
}

func Run(ctx context.Context, cfg *config.Config, opts Opts) (*Stats, error) {
	if opts.DSN == "" {
		return nil, fmt.Errorf("catalog DSN is required (--dsn); refusing to guess — pass the rehearsal copy locally, the live catalog only in the production run")
	}
	if opts.ImageDir == "" {
		return nil, fmt.Errorf("--image-dir is required: screenshots are read from a local rsync mirror of vndb-img, never fetched over HTTP")
	}
	if opts.FilesOut != "" && opts.Apply {
		return nil, fmt.Errorf("--files-out lists what a dry run would need; drop --apply")
	}
	if opts.PerWork <= 0 {
		opts.PerWork = DefaultPerWork
	}
	clientCfg := cfg.CatalogImageClient
	if opts.Apply && (clientCfg.ClientID == "" || clientCfg.ClientSecret == "") {
		return nil, fmt.Errorf("catalog image client not configured (set KUN_CATALOG_IMAGE_CLIENT_ID/SECRET); refusing to --apply screenshot upload")
	}

	db, err := database.OpenJob(opts.DSN)
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
	stats := &Stats{Candidates: len(cands)}
	plan, err := buildPlan(ctx, db, cands, opts.PerWork, opts.Limit, stats)
	if err != nil {
		return stats, err
	}
	if opts.FilesOut != "" {
		if stats.ToFetch, err = writeFilesOut(opts.FilesOut, opts.ImageDir, plan); err != nil {
			return stats, fmt.Errorf("write --files-out: %w", err)
		}
	}
	if !opts.Apply {
		slog.Info("vndb-screenshots done (dry run)", "result", stats.String())
		return stats, nil
	}

	r := &runner{db: db, sourceID: reg.vndbSource, imageDir: opts.ImageDir, gap: opts.UploadGap, stats: stats}
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
		return stats, fmt.Errorf("image_service unreachable: %w", err)
	}
	if err := r.apply(ctx, plan, opts.Workers); err != nil {
		return stats, err
	}
	slog.Info("vndb-screenshots done", "result", stats.String())
	if stats.Quota {
		return stats, fmt.Errorf("image quota exceeded — aborted (rerun to resume; idempotent)")
	}
	return stats, nil
}

func (r *runner) apply(ctx context.Context, plan []workPlan, workers int) error {
	jobs := make(chan workPlan)
	var wg sync.WaitGroup
	for range max(workers, 1) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for w := range jobs {
				if ctx.Err() == nil && !r.stopped() {
					r.fill(ctx, w)
				}
			}
		}()
	}
	for i, w := range plan {
		if ctx.Err() != nil || r.stopped() {
			break
		}
		jobs <- w
		if (i+1)%500 == 0 {
			r.mu.Lock()
			slog.Info("vndb-screenshots progress", "works", i+1, "of", len(plan), "uploaded", r.stats.Uploaded, "errors", r.stats.Errors)
			r.mu.Unlock()
		}
	}
	close(jobs)
	wg.Wait()

	if err := repository.TouchWorks(ctx, r.db, r.touched); err != nil {
		return fmt.Errorf("touch works: %w", err)
	}
	for i := 0; i < len(r.pingHashes); i += 1000 {
		if _, err := r.cli.ReferencePing(ctx, r.pingHashes[i:min(i+1000, len(r.pingHashes))]); err != nil {
			slog.Warn("refping fresh hashes", "err", err)
			break
		}
	}
	return nil
}

func (r *runner) stopped() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stats.Quota
}

func (r *runner) bump(f func(*Stats)) {
	r.mu.Lock()
	f(r.stats)
	r.mu.Unlock()
}

// A work is written whole or not at all: once it holds any screenshot it stops
// being a candidate, so a half-written work would keep its gap forever.
func (r *runner) fill(ctx context.Context, w workPlan) {
	for _, s := range w.Shots {
		if !mirrored(r.imageDir, s.Rel) {
			r.bump(func(st *Stats) { st.Incomplete++ })
			return
		}
	}
	type uploaded struct {
		shot
		sort int
		hash string
	}
	var done []uploaded
	for i, s := range w.Shots {
		hash, err := r.uploadFile(ctx, s.Rel)
		switch {
		case err == nil:
			done = append(done, uploaded{shot: s, sort: i, hash: hash})
		case stderrors.Is(err, imageclient.ErrQuotaExceeded):
			r.bump(func(st *Stats) { st.Quota = true })
			return
		case imageclient.IsPermanent(err):
			r.bump(func(st *Stats) { st.Rejected++ })
			slog.Warn("vndb screenshot rejected by the image service", "work", w.WorkID, "scr", s.Scr, "err", err)
		default:
			r.bump(func(st *Stats) { st.Errors++ })
			slog.Warn("upload vndb screenshot", "work", w.WorkID, "scr", s.Scr, "err", err)
			return
		}
	}
	if len(done) == 0 {
		return
	}
	rows := make([]model.CatalogWorkScreenshot, len(done))
	for i, d := range done {
		rows[i] = model.CatalogWorkScreenshot{
			WorkID: w.WorkID, ImageHash: d.hash, SortOrder: d.sort,
			Sexual: d.Sexual, Violence: d.Violence, SourceID: r.sourceID,
		}
	}
	tx := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "work_id"}, {Name: "image_hash"}},
		DoNothing: true,
	}).Create(&rows)
	r.mu.Lock()
	defer r.mu.Unlock()
	if tx.Error != nil {
		r.stats.Errors++
		slog.Warn("write vndb screenshot rows", "work", w.WorkID, "err", tx.Error)
		return
	}
	for _, d := range done {
		r.pingHashes = append(r.pingHashes, d.hash)
	}
	r.stats.Uploaded += int(tx.RowsAffected)
	r.stats.Dedup += len(done) - int(tx.RowsAffected)
	if tx.RowsAffected > 0 {
		r.touched = append(r.touched, w.WorkID)
	}
}

func (r *runner) uploadFile(ctx context.Context, rel string) (string, error) {
	body, err := os.ReadFile(filepath.Join(r.imageDir, filepath.FromSlash(rel)))
	if err != nil {
		return "", err
	}
	body, filename, err := imageshrink.Shrink(body, path.Base(rel))
	if err != nil {
		return "", err
	}
	var lastErr error
	for attempt := range uploadRetries {
		if !r.pace(ctx) {
			return "", ctx.Err()
		}
		res, err := r.cli.UploadWithSub(ctx, bytes.NewReader(body), filename, shotPreset, uploaderSub)
		if err == nil {
			return res.Hash, nil
		}
		if stderrors.Is(err, imageclient.ErrQuotaExceeded) || imageclient.IsPermanent(err) {
			return "", err
		}
		lastErr = err
		if attempt < uploadRetries-1 && !sleepCtx(ctx, retryWait(attempt)) {
			return "", ctx.Err()
		}
	}
	return "", lastErr
}

var retryWait = func(attempt int) time.Duration { return time.Duration(min(5<<attempt, 30)) * time.Second }

func (r *runner) pace(ctx context.Context) bool {
	if r.gap <= 0 {
		return ctx.Err() == nil
	}
	r.paceMu.Lock()
	next := r.paceLast.Add(r.gap)
	if now := time.Now(); next.Before(now) {
		next = now
	}
	r.paceLast = next
	r.paceMu.Unlock()
	return sleepCtx(ctx, time.Until(next))
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
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
