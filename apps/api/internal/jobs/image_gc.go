package jobs

import (
	"context"
	"fmt"
	"time"

	"api/internal/infrastructure/database"
	"api/internal/platform/image/cdn"
	"api/internal/platform/image/repository"
	"api/internal/platform/image/service"
	"api/internal/platform/image/storage"
	"api/internal/platform/settings/keys"
	"api/pkg/config"
)

type ImageGCOpts struct {
	ColdDays  int
	SoftDays  int
	HardDays  int
	DryRun    bool
	MaxPerRun int
}

func DefaultImageGCOpts() ImageGCOpts {
	return ImageGCOpts{
		ColdDays:  int(keys.ImageGCColdAfterDays.Get()),
		SoftDays:  int(keys.ImageGCSoftDeleteAfterDays.Get()),
		HardDays:  int(keys.ImageGCHardDeleteAfterDays.Get()),
		MaxPerRun: int(keys.ImageGCMaxPerRun.Get()),
	}
}

func RunImageGC(ctx context.Context, cfg *config.Config, opts ImageGCOpts) (Summary, error) {
	o := DefaultImageGCOpts()
	if opts.ColdDays > 0 {
		o.ColdDays = opts.ColdDays
	}
	if opts.SoftDays > 0 {
		o.SoftDays = opts.SoftDays
	}
	if opts.HardDays > 0 {
		o.HardDays = opts.HardDays
	}
	if opts.MaxPerRun > 0 {
		o.MaxPerRun = opts.MaxPerRun
	}
	o.DryRun = opts.DryRun

	db, err := database.NewPostgresDB(cfg.ImagesDatabase)
	if err != nil {
		return nil, fmt.Errorf("db connect: %w", err)
	}
	defer db.Close()

	s3, err := storage.NewClient(cfg.ImageS3)
	if err != nil {
		return nil, fmt.Errorf("s3 init: %w", err)
	}

	imgRepo := repository.NewImageRepository(db.DB())
	gc := service.NewGCService(db.DB(), s3, imgRepo).WithPurger(cdn.FromConfig(cfg.ImageService.Purge), cdn.Bases(cfg.ImageService))

	summary, err := gc.Run(ctx, service.GCConfig{
		ColdAfter:    time.Duration(o.ColdDays) * 24 * time.Hour,
		SoftDelAfter: time.Duration(o.SoftDays) * 24 * time.Hour,
		HardDelAfter: time.Duration(o.HardDays) * 24 * time.Hour,
		DryRun:       o.DryRun,
		MaxPerRun:    o.MaxPerRun,
	})
	if err != nil {
		return nil, fmt.Errorf("gc run: %w", err)
	}
	return Summary{
		"cold_candidates": summary.ColdCandidates,
		"soft_deleted":    summary.SoftDeleted,
		"hard_deleted":    summary.HardDeleted,
		"errors":          summary.Errors,
		"dry_run":         o.DryRun,
	}, nil
}
