package jobs

import (
	"context"
	"fmt"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/repository"
	catsvc "api/internal/platform/catalog/service"
	"api/pkg/config"
)

const JobCatalogMergeRequests = "catalog-merge-requests"

func RunCatalogMergeRequests(ctx context.Context, cfg *config.Config) (Summary, error) {
	pg, err := database.NewPostgresDB(cfg.CatalogDatabase)
	if err != nil {
		return nil, fmt.Errorf("db connect: %w", err)
	}
	defer pg.Close()

	db := pg.DB()
	svc := catsvc.NewMergeService(db, catsvc.NewResolveService(repository.NewRedirectRepository(db)))
	rep, err := svc.ExecuteDueRequests(ctx, 0)
	return Summary{"due": rep.Due, "executed": rep.Executed, "failed": rep.Failed}, err
}
