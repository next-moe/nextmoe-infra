package jobs

import (
	"context"
	"fmt"

	"api/internal/infrastructure/database"
	"api/internal/jobs/tagparents"
	"api/pkg/config"
)

const JobCatalogTagParents = "catalog-tag-parents"

func RunCatalogTagParents(ctx context.Context, cfg *config.Config) (Summary, error) {
	pg, err := database.NewPostgresDB(cfg.CatalogDatabase)
	if err != nil {
		return nil, fmt.Errorf("db connect: %w", err)
	}
	defer pg.Close()

	st, err := tagparents.Sync(ctx, pg.DB(), true)
	if err != nil {
		return nil, err
	}
	return Summary{
		"vndb_tags": st.VNDBTags,
		"mapped":    st.Mapped,
		"edges":     st.Edges,
		"added":     st.Added,
		"deleted":   st.Deleted,
		"cycles":    st.Cycles,
	}, nil
}
