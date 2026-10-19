package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"api/internal/infrastructure/database"
	"api/pkg/config"
	"api/pkg/imageclient"

	"gorm.io/gorm"
)

type GalgameImageRefpingOpts struct {
	Batch   int
	Timeout time.Duration
	DryRun  bool
}

func DefaultGalgameImageRefpingOpts() GalgameImageRefpingOpts {
	return GalgameImageRefpingOpts{Batch: 1000, Timeout: 30 * time.Minute}
}

func RunGalgameImageRefping(ctx context.Context, cfg *config.Config, opts GalgameImageRefpingOpts) (Summary, error) {
	if opts.Batch < 1 || opts.Batch > 1000 {
		opts.Batch = 1000
	}
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	editDB, err := database.NewPostgresDB(cfg.CatalogDatabase)
	if err != nil {
		return nil, fmt.Errorf("catalog db connect: %w", err)
	}
	defer editDB.Close()

	hashes, err := collectEditRefpingHashes(ctx, editDB.DB())
	if err != nil {
		return nil, fmt.Errorf("collect edit_* refping hashes: %w", err)
	}

	slog.Info("refping: collected edit-history hashes", "count", len(hashes))
	if len(hashes) == 0 {
		return Summary{"distinct_hashes": 0, "note": "nothing to ping"}, nil
	}
	if opts.DryRun {
		return Summary{"dry_run": true, "would_ping": len(hashes)}, nil
	}

	clientCfg := cfg.ImageClient
	usingDedicated := false
	if cfg.GalgameImageClient.ClientID != "" && cfg.GalgameImageClient.ClientSecret != "" {
		clientCfg = cfg.GalgameImageClient
		usingDedicated = true
	}
	if clientCfg.ClientID == "" || clientCfg.ClientSecret == "" {
		return nil, fmt.Errorf("galgame image client not configured (set KUN_GALGAME_IMAGE_CLIENT_ID/SECRET on the oauth container, or KUN_IMAGE_CLIENT_ID/SECRET); refusing to run, banners would rot")
	}
	slog.Info("refping: image client selected",
		"dedicated_galgame_client", usingDedicated, "client_id", clientCfg.ClientID)
	cli := imageclient.New(imageclient.Config{
		BaseURL:      clientCfg.BaseURL,
		CDNBase:      cfg.ImageService.CDNBase,
		ClientID:     clientCfg.ClientID,
		ClientSecret: clientCfg.ClientSecret,
	})

	var (
		totalUpdated  int64
		totalNotFound int
		batchErrors   int
		sampleMissing []string
	)
	for _, b := range chunk(hashes, opts.Batch) {
		res, err := cli.ReferencePing(ctx, b)
		if err != nil {
			batchErrors++
			slog.Error("refping: batch failed", "size", len(b), "err", err)
			continue
		}
		totalUpdated += res.Updated
		totalNotFound += len(res.NotFound)
		if len(sampleMissing) < 10 && len(res.NotFound) > 0 {
			sampleMissing = append(sampleMissing, res.NotFound...)
			if len(sampleMissing) > 10 {
				sampleMissing = sampleMissing[:10]
			}
		}
	}

	summary := Summary{
		"distinct_hashes": len(hashes),
		"updated":         totalUpdated,
		"not_found":       totalNotFound,
		"batch_errors":    batchErrors,
	}
	if totalNotFound > 0 {
		summary["sample_missing"] = sampleMissing
		slog.Warn("refping: dangling local refs (hashes unknown to image_service)",
			"not_found", totalNotFound, "sample", sampleMissing)
	}
	if batchErrors > 0 {
		return summary, fmt.Errorf("reference-ping had %d failed batch(es)", batchErrors)
	}
	if totalUpdated == 0 && len(hashes) > 0 {
		return summary, fmt.Errorf("reference-ping kept 0/%d hashes alive (all not_found) — wrong image client/site (need site=galgame_wiki) or images already deleted", len(hashes))
	}
	return summary, nil
}

func collectEditRefpingHashes(ctx context.Context, db *gorm.DB) ([]string, error) {
	const q = `
WITH all_hashes AS (
    SELECT (jsonb_array_elements(r.snapshot->k))->>'image_hash' AS hash
        FROM edit_revision r, LATERAL unnest(ARRAY[
            'galgame.game.covers', 'galgame.game.screenshots',
            'catalog.work.covers', 'catalog.work.screenshots']) AS k
        WHERE r.entity_type IN ('galgame.game', 'catalog.work')
          AND jsonb_typeof(r.snapshot->k) = 'array'
    UNION
    SELECT (jsonb_array_elements(r.baseline_snapshot->k))->>'image_hash'
        FROM edit_revision r, LATERAL unnest(ARRAY[
            'catalog.work.covers', 'catalog.work.screenshots']) AS k
        WHERE r.entity_type = 'catalog.work'
          AND jsonb_typeof(r.baseline_snapshot->k) = 'array'
    UNION
    SELECT (jsonb_array_elements(p.patch->k))->>'image_hash'
        FROM edit_proposal p, LATERAL unnest(ARRAY[
            'galgame.game.covers', 'galgame.game.screenshots',
            'catalog.work.covers', 'catalog.work.screenshots']) AS k
        WHERE p.entity_type IN ('galgame.game', 'catalog.work')
          AND jsonb_typeof(p.patch->k) = 'array'
    UNION
    SELECT doc->>k
        FROM (SELECT r.snapshot AS doc FROM edit_revision r
              UNION ALL SELECT r.baseline_snapshot FROM edit_revision r WHERE r.baseline_snapshot IS NOT NULL
              UNION ALL SELECT p.patch FROM edit_proposal p) d,
             LATERAL unnest(ARRAY[
            'catalog.character.image_hash', 'catalog.character.figure_hash',
            'catalog.label.logo_hash', 'catalog.person.photo']) AS k
        WHERE jsonb_typeof(doc->k) = 'string'
    UNION
    -- legacy_meta is the archived OLD-wire snapshot, whose keys are bare
    -- ('covers' / 'screenshots') and are not rewritten by the rekey.
    SELECT (jsonb_array_elements(p.legacy_meta->'snapshot'->k))->>'image_hash'
        FROM edit_proposal p, LATERAL unnest(ARRAY['covers', 'screenshots']) AS k
        WHERE p.entity_type IN ('galgame.game', 'catalog.work')
          AND jsonb_typeof(p.legacy_meta->'snapshot'->k) = 'array'
)
SELECT DISTINCT hash FROM all_hashes
WHERE hash IS NOT NULL AND hash <> ''
`
	var hashes []string
	if err := db.WithContext(ctx).Raw(q).Scan(&hashes).Error; err != nil {
		return nil, err
	}
	return hashes, nil
}
