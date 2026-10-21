package jobs

import (
	"context"
	"fmt"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/repository"
	catsvc "api/internal/platform/catalog/service"
	"api/pkg/config"
)

const JobCatalogDeletionRequests = "catalog-deletion-requests"

func RunCatalogDeletionRequests(ctx context.Context, cfg *config.Config) (Summary, error) {
	pg, err := database.NewPostgresDB(cfg.CatalogDatabase)
	if err != nil {
		return nil, fmt.Errorf("db connect: %w", err)
	}
	defer pg.Close()

	db := pg.DB()
	svc := catsvc.NewDeletionService(db, catsvc.NewResolveService(repository.NewRedirectRepository(db)))
	rep, err := svc.ExecuteDue(ctx)
	return Summary{"due": rep.Due, "executed": rep.Executed, "closed": rep.Closed, "failed": rep.Failed}, err
}
