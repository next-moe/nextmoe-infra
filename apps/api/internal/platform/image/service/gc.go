package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"api/internal/platform/image/model"
	"api/internal/platform/image/repository"
	"api/internal/platform/image/storage"

	"gorm.io/gorm"
)

type GCService struct {
	db      *gorm.DB
	storage *storage.Client
	imgRepo *repository.ImageRepository
}

func NewGCService(db *gorm.DB, s *storage.Client, imgRepo *repository.ImageRepository) *GCService {
	return &GCService{db: db, storage: s, imgRepo: imgRepo}
}

type GCConfig struct {
	ColdAfter    time.Duration
	SoftDelAfter time.Duration
	HardDelAfter time.Duration
	DryRun       bool
	MaxPerRun    int
}

func (c *GCConfig) defaults() {
	if c.ColdAfter == 0 {
		c.ColdAfter = 60 * 24 * time.Hour
	}
	if c.SoftDelAfter == 0 {
		c.SoftDelAfter = 365 * 24 * time.Hour
	}
	if c.HardDelAfter == 0 {
		c.HardDelAfter = 30 * 24 * time.Hour
	}
	if c.MaxPerRun == 0 {
		c.MaxPerRun = 10000
	}
}

type GCRunSummary struct {
	ColdCandidates int64 `json:"cold_candidates"`
	SoftDeleted    int64 `json:"soft_deleted"`
	HardDeleted    int64 `json:"hard_deleted"`
	Errors         int   `json:"errors"`
}

func (g *GCService) Run(ctx context.Context, cfg GCConfig) (*GCRunSummary, error) {
	cfg.defaults()
	summary := &GCRunSummary{}

	coldThreshold := time.Now().Add(-cfg.ColdAfter)
	softThreshold := time.Now().Add(-cfg.SoftDelAfter)
	hardThreshold := time.Now().Add(-cfg.HardDelAfter)

	if err := g.db.WithContext(ctx).Model(&model.Image{}).
		Where("deleted_at IS NULL AND last_referenced_at < ?", coldThreshold).
		Count(&summary.ColdCandidates).Error; err != nil {
		summary.Errors++
		slog.Error("gc count cold candidates", "err", err)
	}
	slog.Info("gc cold candidates",
		"threshold", coldThreshold.Format(time.RFC3339),
		"count", summary.ColdCandidates,
		"dry_run", cfg.DryRun,
	)

	if n, err := g.softDelete(ctx, softThreshold, cfg); err != nil {
		summary.Errors++
	} else {
		summary.SoftDeleted = n
	}

	if n, err := g.hardDelete(ctx, hardThreshold, cfg); err != nil {
		summary.Errors++
	} else {
		summary.HardDeleted = n
	}

	if summary.Errors > 0 {
		return summary, fmt.Errorf("gc: %d phase(s) failed (see logs)", summary.Errors)
	}
	return summary, nil
}

func (g *GCService) softDelete(ctx context.Context, threshold time.Time, cfg GCConfig) (int64, error) {
	var rows []model.Image
	if err := g.db.WithContext(ctx).
		Where("deleted_at IS NULL AND last_referenced_at < ?", threshold).
		Limit(cfg.MaxPerRun).
		Find(&rows).Error; err != nil {
		return 0, fmt.Errorf("gc: fetch soft-delete candidates: %w", err)
	}
	if len(rows) == 0 {
		slog.Info("gc soft-delete: nothing to do", "threshold", threshold.Format(time.RFC3339))
		return 0, nil
	}
	slog.Info("gc soft-delete candidates",
		"count", len(rows), "threshold", threshold.Format(time.RFC3339), "dry_run", cfg.DryRun)

	if cfg.DryRun {
		return int64(len(rows)), nil
	}

	now := time.Now()
	ids := make([]int64, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	res := g.db.WithContext(ctx).
		Model(&model.Image{}).
		Where("id IN ? AND deleted_at IS NULL AND last_referenced_at < ?", ids, threshold).
		Update("deleted_at", now)
	if res.Error != nil {
		return 0, fmt.Errorf("gc: apply soft-delete: %w", res.Error)
	}
	return res.RowsAffected, nil
}

func (g *GCService) hardDelete(ctx context.Context, threshold time.Time, cfg GCConfig) (int64, error) {
	var rows []model.Image
	if err := g.db.WithContext(ctx).Unscoped().
		Where("deleted_at IS NOT NULL AND deleted_at < ?", threshold).
		Limit(cfg.MaxPerRun).
		Find(&rows).Error; err != nil {
		return 0, fmt.Errorf("gc: fetch hard-delete candidates: %w", err)
	}
	if len(rows) == 0 {
		slog.Info("gc hard-delete: nothing to do", "threshold", threshold.Format(time.RFC3339))
		return 0, nil
	}
	slog.Info("gc hard-delete candidates",
		"count", len(rows), "threshold", threshold.Format(time.RFC3339), "dry_run", cfg.DryRun)

	if cfg.DryRun {
		return int64(len(rows)), nil
	}

	deleted := int64(0)
	for _, r := range rows {
		if err := g.storage.Delete(ctx, r.StorageKey); err != nil {
			slog.Warn("gc: delete main s3 object", "hash", r.Hash, "err", err)
		}
		for _, v := range r.VariantList() {
			key := fmt.Sprintf("%s/%s/%s_%s.%s", r.Hash[:2], r.Hash[2:4], r.Hash, v, r.Ext)
			if err := g.storage.Delete(ctx, key); err != nil {
				slog.Warn("gc: delete variant", "hash", r.Hash, "variant", v, "err", err)
			}
		}

		if err := g.db.WithContext(ctx).Unscoped().
			Where("id = ?", r.ID).
			Delete(&model.Image{}).Error; err != nil {
			slog.Error("gc: row hard-delete", "id", r.ID, "err", err)
			continue
		}

		if err := g.db.WithContext(ctx).
			Where("hash = ?", r.Hash).
			Delete(&model.ImageSiteUsage{}).Error; err != nil {
			slog.Warn("gc: delete site_usage", "hash", r.Hash, "err", err)
		}
		deleted++
	}

	return deleted, nil
}
