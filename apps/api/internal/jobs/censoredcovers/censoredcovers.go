package censoredcovers

import (
	"context"
	"fmt"
	"io"
	"log/slog"
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
}

type Stats struct {
	Candidates int
	Planned    int
	Uploaded   int
	Dedup      int
	Rejected   int
	Errors     int
	Quota      bool
}

func (s Stats) String() string {
	return fmt.Sprintf("candidates=%d planned=%d uploaded=%d dedup=%d rejected=%d errors=%d quota=%t",
		s.Candidates, s.Planned, s.Uploaded, s.Dedup, s.Rejected, s.Errors, s.Quota)
}

type imageUploader interface {
	UploadWithSub(ctx context.Context, r io.Reader, filename, preset, uploaderSub string) (*imageclient.UploadResult, error)
	ReferencePing(ctx context.Context, hashes []string) (*imageclient.ReferencePingResult, error)
	Health(ctx context.Context) error
}

type runner struct {
	db         *gorm.DB
	cli        imageUploader
	cdnBase    string
	sourceID   int16
	gap        time.Duration
	stats      *Stats
	touched    []int64
	pingHashes []string
}

func Run(ctx context.Context, cfg *config.Config, opts Opts) (*Stats, error) {
	if opts.DSN == "" {
		return nil, fmt.Errorf("catalog DSN is required (--dsn); refusing to guess — pass the rehearsal copy locally, the live catalog only in the production run")
	}
	clientCfg := cfg.CatalogImageClient
	if opts.Apply && (clientCfg.ClientID == "" || clientCfg.ClientSecret == "") {
		return nil, fmt.Errorf("catalog image client not configured (set KUN_CATALOG_IMAGE_CLIENT_ID/SECRET); refusing to --apply ghost upload")
	}
	if cfg.ImageService.CDNBase == "" {
		return nil, fmt.Errorf("KUN_IMAGE_PUBLIC_BASE_URL is empty; the job downloads source art from the CDN")
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
	cands = window(cands, opts.Offset, opts.Limit)
	stats := &Stats{Candidates: len(cands)}
	slog.Info("censored-covers candidates", "works", len(cands), "apply", opts.Apply,
		"offset", opts.Offset, "limit", opts.Limit, "explicit_ids", len(opts.IDs))
	if !opts.Apply {
		slog.Info("censored-covers done (dry run)", "result", stats.String())
		return stats, nil
	}

	r := &runner{db: db, cdnBase: cfg.ImageService.CDNBase, sourceID: reg.censoredSource,
		gap: opts.UploadGap, stats: stats}
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

	for _, cand := range cands {
		if ctx.Err() != nil || stats.Quota {
			break
		}
		r.fill(ctx, cand)
	}
	if err := repository.TouchWorks(ctx, db, r.touched); err != nil {
		return stats, fmt.Errorf("touch works: %w", err)
	}
	if err := r.ping(ctx); err != nil {
		slog.Warn("refping fresh hashes", "err", err)
	}
	slog.Info("censored-covers done", "result", stats.String())
	if stats.Quota {
		return stats, fmt.Errorf("image quota exceeded — aborted (rerun to resume; idempotent)")
	}
	return stats, nil
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

func window(c []candidate, offset, limit int) []candidate {
	if offset > 0 {
		if offset >= len(c) {
			return nil
		}
		c = c[offset:]
	}
	if limit > 0 && limit < len(c) {
		c = c[:limit]
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
