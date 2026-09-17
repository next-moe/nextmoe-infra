package charportraits

import (
	"bytes"
	"context"
	stderrors "errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"api/internal/infrastructure/database"
	"api/pkg/config"
	"api/pkg/imageclient"

	"gorm.io/gorm"
)

const (
	preset         = "character"
	uploaderSub    = "system:catalog-portrait-backfill"
	site           = "catalog"
	defaultTimeout = 60 * time.Second
)

type Opts struct {
	Apply        bool
	Limit        int
	Offset       int
	DSN          string
	VNDBImageDir string
	FilesOut     string
	ImageBaseURL string
	UploadGap    time.Duration
}

type candidate struct {
	CatalogCharacterID int64   `gorm:"column:catalog_character_id"`
	ImageID            string  `gorm:"column:image_id"`
	ImageHash          *string `gorm:"column:image_hash"`
}

type runner struct {
	db  *gorm.DB
	cli *imageclient.Client
	gap time.Duration

	uploaded, skippedHasHash, missingFile, rejected, errors, dedup int
	pingHashes                                                     []string
}

func Run(ctx context.Context, cfg *config.Config, opts Opts) (map[string]any, error) {
	if opts.DSN == "" {
		return nil, fmt.Errorf("catalog DSN is required (--dsn); refusing to guess — pass the rehearsal copy locally, the live catalog only in the production run")
	}
	if opts.VNDBImageDir == "" {
		return nil, fmt.Errorf("--vndb-image-dir is required (local rsync mirror containing ch/)")
	}

	clientCfg := cfg.CatalogImageClient
	if opts.Apply && (clientCfg.ClientID == "" || clientCfg.ClientSecret == "") {
		return nil, fmt.Errorf("catalog image client not configured (set KUN_CATALOG_IMAGE_CLIENT_ID/SECRET); refusing to --apply")
	}

	db, err := database.OpenJob(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect catalog db: %w", err)
	}
	if sqlDB, e := db.DB(); e == nil {
		defer sqlDB.Close()
	}

	cands, err := loadCandidates(ctx, db, opts.Limit, opts.Offset)
	if err != nil {
		return nil, fmt.Errorf("load candidates: %w", err)
	}
	slog.Info("char-portraits candidates", "candidates", len(cands), "apply", opts.Apply, "offset", opts.Offset, "limit", opts.Limit)

	r := &runner{db: db, gap: opts.UploadGap}

	if !opts.Apply {
		f := forecastMirror(cands, opts.VNDBImageDir)
		if opts.FilesOut != "" {
			if err := os.WriteFile(opts.FilesOut, []byte(f.missingList()), 0o644); err != nil {
				return nil, fmt.Errorf("write --files-out: %w", err)
			}
		}
		slog.Info("char-portraits DRY forecast",
			"candidates", len(cands), "skipped_has_hash", f.hasHash,
			"local_present", f.present, "missing_file", len(f.missing), "bad_id", f.badID)
		return map[string]any{
			"apply":            false,
			"candidates":       len(cands),
			"skipped_has_hash": f.hasHash,
			"local_present":    f.present,
			"missing_file":     len(f.missing),
			"bad_id":           f.badID,
		}, nil
	}

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
		return nil, fmt.Errorf("image_service unreachable at %s: %w", resolveBaseURL(cfg, clientCfg, opts.ImageBaseURL), err)
	}

	quota := false
	for _, c := range cands {
		if err := ctx.Err(); err != nil {
			return r.summary(len(cands)), err
		}
		if c.ImageHash != nil && *c.ImageHash != "" {
			r.skippedHasHash++
			continue
		}
		if r.fillPortrait(ctx, opts.VNDBImageDir, c) {
			quota = true
			break
		}
	}

	for _, b := range chunk(r.pingHashes, 1000) {
		if _, err := r.cli.ReferencePing(ctx, b); err != nil {
			slog.Warn("char-portraits reference-ping failed", "err", err)
		}
	}

	slog.Info("char-portraits done",
		"uploaded", r.uploaded, "skipped_has_hash", r.skippedHasHash,
		"missing_file", r.missingFile, "rejected", r.rejected, "errors", r.errors, "dedup", r.dedup)
	if quota {
		return r.summary(len(cands)), fmt.Errorf("image quota exceeded after %d uploads", r.uploaded)
	}
	return r.summary(len(cands)), nil
}

func (r *runner) fillPortrait(ctx context.Context, dir string, c candidate) (quota bool) {
	rel, err := chRelPath(c.ImageID)
	if err != nil {
		r.errors++
		slog.Warn("bad image id", "char", c.CatalogCharacterID, "image_id", c.ImageID, "err", err)
		return false
	}
	body, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		if os.IsNotExist(err) {
			r.missingFile++
			return false
		}
		r.errors++
		slog.Warn("read portrait", "char", c.CatalogCharacterID, "rel", rel, "err", err)
		return false
	}
	if len(body) == 0 {
		r.missingFile++
		return false
	}
	if r.gap > 0 {
		time.Sleep(r.gap)
	}
	res, err := r.cli.UploadWithSub(ctx, bytes.NewReader(body), c.ImageID+".jpg", preset, uploaderSub)
	if err != nil {
		switch {
		case stderrors.Is(err, imageclient.ErrQuotaExceeded):
			return true
		case stderrors.Is(err, imageclient.ErrModerationRejected):
			r.rejected++
			slog.Warn("portrait rejected by moderation", "char", c.CatalogCharacterID, "image_id", c.ImageID, "err", err)
			return false
		default:
			r.errors++
			slog.Warn("upload portrait", "char", c.CatalogCharacterID, "image_id", c.ImageID, "err", err)
			return false
		}
	}
	if res.Deduplicated {
		r.dedup++
	}
	tx := r.db.WithContext(ctx).Exec(
		`UPDATE catalog_character SET image_hash = ?, updated_at = NOW() WHERE id = ? AND image_hash IS NULL`,
		res.Hash, c.CatalogCharacterID)
	if tx.Error != nil {
		r.errors++
		slog.Warn("write image_hash", "char", c.CatalogCharacterID, "err", tx.Error)
		return false
	}
	if tx.RowsAffected == 0 {
		r.skippedHasHash++
		return false
	}
	r.uploaded++
	r.pingHashes = append(r.pingHashes, res.Hash)
	return false
}

func (r *runner) summary(candidates int) map[string]any {
	return map[string]any{
		"apply":            true,
		"candidates":       candidates,
		"uploaded":         r.uploaded,
		"skipped_has_hash": r.skippedHasHash,
		"missing_file":     r.missingFile,
		"rejected":         r.rejected,
		"errors":           r.errors,
		"dedup":            r.dedup,
	}
}

func loadCandidates(ctx context.Context, db *gorm.DB, limit, offset int) ([]candidate, error) {
	q := db.WithContext(ctx).
		Table("src_vndb.portrait_backfill AS pb").
		Select("pb.catalog_character_id, pb.image_id, c.image_hash").
		Joins("JOIN catalog_character c ON c.id = pb.catalog_character_id AND c.deleted_at IS NULL").
		Order("pb.catalog_character_id")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if offset > 0 {
		q = q.Offset(offset)
	}
	var out []candidate
	if err := q.Scan(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func fileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir() && fi.Size() > 0
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

func chunk[T any](src []T, n int) [][]T {
	var out [][]T
	for i := 0; i < len(src); i += n {
		out = append(out, src[i:min(i+n, len(src))])
	}
	return out
}
