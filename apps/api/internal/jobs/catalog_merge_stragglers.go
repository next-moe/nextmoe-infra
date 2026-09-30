package jobs

import (
	"context"
	"fmt"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/repository"
	catsvc "api/internal/platform/catalog/service"
	"api/pkg/config"
)

const JobCatalogMergeStragglers = "catalog-merge-stragglers"

func RunCatalogMergeStragglers(ctx context.Context, cfg *config.Config) (Summary, error) {
	pg, err := database.NewPostgresDB(cfg.CatalogDatabase)
	if err != nil {
		return nil, fmt.Errorf("db connect: %w", err)
	}
	defer pg.Close()

	db := pg.DB()
	svc := catsvc.NewMergeService(db, catsvc.NewResolveService(repository.NewRedirectRepository(db)),
		repository.NewProposalRepository(db), repository.NewRevisionRepository(db))
	rep, err := svc.SweepStragglers(ctx)
	return Summary{
		"pairs":     rep.Pairs,
		"found":     rep.Found,
		"repaired":  rep.Repaired,
		"orphaned":  rep.Orphaned,
		"uncovered": rep.Uncovered,
	}, err
}
