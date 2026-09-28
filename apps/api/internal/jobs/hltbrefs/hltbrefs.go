package hltbrefs

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"

	"gorm.io/gorm"
)

const rule = "rule:hltb-steam"

type Opts struct {
	Apply   bool
	DSN     string
	HltbDSN string
}

type Stats struct {
	SteamAnchored  int
	MirrorMatched  int
	AmbiguousSteam int
	Planned        int
	Written        int
	Exists         int
	Rejected       int
	Errors         int
}

func Run(ctx context.Context, opts Opts) (*Stats, error) {
	if opts.DSN == "" || opts.HltbDSN == "" {
		return nil, fmt.Errorf("both --dsn and --hltb-dsn are required")
	}
	db, err := database.OpenJob(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect catalog db: %w", err)
	}
	defer closeGorm(db)
	hltbDB, err := database.OpenJob(opts.HltbDSN)
	if err != nil {
		return nil, fmt.Errorf("connect hltb mirror: %w", err)
	}
	defer closeGorm(hltbDB)

	ids, err := resolveIDs(ctx, db)
	if err != nil {
		return nil, err
	}

	worksByAppid, err := repository.SteamAnchoredWorks(ctx, db, ids.steamSource)
	if err != nil {
		return nil, fmt.Errorf("load steam-anchored works: %w", err)
	}
	st := &Stats{SteamAnchored: len(worksByAppid)}

	hltbByAppid, err := loadMirrorSteam(ctx, hltbDB)
	if err != nil {
		return nil, fmt.Errorf("load hltb mirror steam ids: %w", err)
	}

	rejected, err := loadRejections(ctx, db, ids.hltbSource)
	if err != nil {
		return nil, err
	}

	appids := make([]string, 0, len(worksByAppid))
	for appid := range worksByAppid {
		appids = append(appids, appid)
	}
	sort.Strings(appids)

	var touched []int64
	for _, appid := range appids {
		hltbIDs, ok := hltbByAppid[appid]
		if !ok {
			continue
		}
		works := worksByAppid[appid]
		if len(works) > 1 || len(hltbIDs) > 1 {
			st.AmbiguousSteam++
			continue
		}
		st.MirrorMatched++
		workID, hltbID := works[0], hltbIDs[0]
		if _, hit := rejected[rejKey(workID, hltbID)]; hit {
			st.Rejected++
			continue
		}
		st.Planned++
		if !opts.Apply {
			continue
		}
		wrote, err := repository.InsertRefIfAbsent(db.WithContext(ctx), model.CatalogExternalRef{
			EntityType: model.EntityTypeWork, EntityID: workID,
			SourceID: ids.hltbSource, ExternalID: hltbID,
			LinkKind: model.LinkKindProbable, MatchedBy: rule,
		})
		if err != nil {
			st.Errors++
			slog.Warn("insert hltb ref", "work", workID, "hltb", hltbID, "err", err)
			continue
		}
		if wrote {
			st.Written++
			touched = append(touched, workID)
		} else {
			st.Exists++
		}
	}
	if err := repository.TouchWorks(ctx, db, touched); err != nil {
		return nil, fmt.Errorf("touch works: %w", err)
	}
	slog.Info("hltbrefs done", "apply", opts.Apply,
		"steam_anchored", st.SteamAnchored, "mirror_matched", st.MirrorMatched,
		"ambiguous_steam", st.AmbiguousSteam, "planned", st.Planned,
		"written", st.Written, "exists", st.Exists,
		"rejected", st.Rejected, "errors", st.Errors)
	return st, nil
}

type registryIDs struct {
	galgameMedium int16
	steamSource   int16
	hltbSource    int16
}

func resolveIDs(ctx context.Context, db *gorm.DB) (registryIDs, error) {
	var r registryIDs
	for key, dst := range map[string]*int16{
		"steam": &r.steamSource, "howlongtobeat": &r.hltbSource,
	} {
		if err := db.WithContext(ctx).Raw(`SELECT id FROM catalog_source WHERE key = ?`, key).Scan(dst).Error; err != nil {
			return r, fmt.Errorf("resolve source %q: %w", key, err)
		}
	}
	if err := db.WithContext(ctx).Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&r.galgameMedium).Error; err != nil {
		return r, fmt.Errorf("resolve galgame medium: %w", err)
	}
	if r.galgameMedium == 0 || r.steamSource == 0 || r.hltbSource == 0 {
		return r, fmt.Errorf("registry not seeded (medium=%d steam=%d howlongtobeat=%d)",
			r.galgameMedium, r.steamSource, r.hltbSource)
	}
	return r, nil
}

func loadMirrorSteam(ctx context.Context, hltbDB *gorm.DB) (map[string][]string, error) {
	var rows []struct {
		HltbID string `gorm:"column:hltb_id"`
		Appid  string `gorm:"column:appid"`
	}
	if err := hltbDB.WithContext(ctx).Raw(`
		SELECT hltb_id::text AS hltb_id, raw->'data'->'game'->0->>'profile_steam' AS appid
		FROM games
		WHERE status = 'fetched'
		  AND coalesce(raw->'data'->'game'->0->>'profile_steam', '0') NOT IN ('', '0')`).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string][]string, len(rows))
	for _, r := range rows {
		out[r.Appid] = append(out[r.Appid], r.HltbID)
	}
	return out, nil
}

func loadRejections(ctx context.Context, db *gorm.DB, source int16) (map[string]struct{}, error) {
	var rows []struct {
		EntityID   int64  `gorm:"column:entity_id"`
		ExternalID string `gorm:"column:external_id"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT entity_id, external_id FROM catalog_match_rejection
		WHERE entity_type = 5 AND source_id = ?`, source).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load rejections: %w", err)
	}
	out := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		out[rejKey(r.EntityID, r.ExternalID)] = struct{}{}
	}
	return out, nil
}

func rejKey(workID int64, externalID string) string {
	return fmt.Sprintf("%d\x00%s", workID, externalID)
}

func closeGorm(db *gorm.DB) {
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.Close()
	}
}
