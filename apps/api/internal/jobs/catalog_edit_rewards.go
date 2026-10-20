package jobs

import (
	"context"
	"fmt"
	"time"

	"api/internal/infrastructure/database"
	"api/internal/jobs/editrewards"
	"api/internal/platform/settings/keys"
	"api/pkg/config"
)

const JobCatalogEditRewards = "catalog-edit-rewards"

func RunCatalogEditRewards(ctx context.Context, cfg *config.Config) (Summary, error) {
	amount := int64(keys.CatalogEditMergeReward.Get())
	if amount <= 0 {
		return Summary{"disabled": true}, nil
	}
	window := time.Duration(keys.CatalogEditMergeRewardWindowDays.Get()) * 24 * time.Hour

	catalog, err := database.NewPostgresDB(cfg.CatalogDatabase)
	if err != nil {
		return nil, fmt.Errorf("catalog db connect: %w", err)
	}
	defer catalog.Close()
	core, err := database.NewPostgresDB(cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("core db connect: %w", err)
	}
	defer core.Close()

	rep, err := editrewards.Run(ctx, catalog.DB(), core.DB(), editrewards.Opts{
		Amount: amount,
		Since:  time.Now().Add(-window),
	})
	return Summary{
		"amount":          amount,
		"candidates":      rep.Candidates,
		"awarded":         rep.Awarded,
		"already_awarded": rep.AlreadyAwarded,
		"forum_awarded":   rep.ForumAwarded,
		"proposer_gone":   rep.ProposerGone,
		"failed":          rep.Failed,
	}, err
}
