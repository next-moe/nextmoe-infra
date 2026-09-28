package storerefs

import (
	"context"
	"fmt"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/srcbangumi"

	"gorm.io/gorm"
)

const RuleBgmSteam = "rule:bgm-steam"

func runBgmLane(ctx context.Context, db *gorm.DB, opts Opts, ids registryIDs, rejected map[string]struct{}, st *Stats) ([]int64, error) {
	var rows []struct {
		WorkID  int64  `gorm:"column:work_id"`
		Infobox []byte `gorm:"column:infobox_parsed"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT w.id AS work_id, s.infobox_parsed
		FROM catalog_work w
		JOIN catalog_external_ref r ON r.entity_type = ? AND r.entity_id = w.id
			AND r.source_id = ? AND r.link_kind = ? AND r.dead_at IS NULL
		JOIN src_bangumi.subject s ON s.id = r.external_id::bigint
		WHERE w.medium_id = ? AND w.deleted_at IS NULL AND s.infobox_parsed IS NOT NULL
		ORDER BY w.id, s.id`,
		model.EntityTypeWork, ids.bgmSource, model.LinkKindExact, ids.galgameMedium).
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load bangumi anchors: %w", err)
	}
	stated := map[int64]map[string]struct{}{}
	var order []int64
	for _, r := range rows {
		if _, ok := stated[r.WorkID]; !ok {
			stated[r.WorkID] = map[string]struct{}{}
			order = append(order, r.WorkID)
		}
		for _, appid := range srcbangumi.SteamAppIDs(r.Infobox) {
			stated[r.WorkID][appid] = struct{}{}
		}
	}
	st.BgmAnchored = len(order)

	holding, err := worksHoldingSteam(ctx, db, ids.steamSource)
	if err != nil {
		return nil, err
	}

	var touched []int64
	for _, workID := range order {
		appids := stated[workID]
		if len(appids) == 0 {
			continue
		}
		st.BgmStated++
		if len(appids) > 1 {
			st.BgmAmbiguous++
			continue
		}
		if _, has := holding[workID]; has {
			st.BgmHasSteam++
			continue
		}
		for appid := range appids {
			if writeRef(ctx, db, opts.Apply, workID, ids.steamSource, appid, RuleBgmSteam, rejected,
				&st.BgmPlanned, &st.BgmWritten, &st.BgmExists, &st.Rejected, &st.Errors) {
				touched = append(touched, workID)
			}
		}
	}
	return touched, nil
}

func worksHoldingSteam(ctx context.Context, db *gorm.DB, steamSource int16) (map[int64]struct{}, error) {
	var ids []int64
	if err := db.WithContext(ctx).Raw(`
		SELECT entity_id FROM catalog_external_ref WHERE entity_type = ? AND source_id = ?
		UNION
		SELECT rel.work_id FROM catalog_external_ref r
		JOIN catalog_release rel ON rel.id = r.entity_id
		WHERE r.entity_type = ? AND r.source_id = ? AND r.link_kind = ?`,
		model.EntityTypeWork, steamSource,
		model.EntityTypeRelease, steamSource, model.LinkKindExact).Scan(&ids).Error; err != nil {
		return nil, fmt.Errorf("load works holding steam: %w", err)
	}
	out := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		out[id] = struct{}{}
	}
	return out, nil
}
