package llmsuggest

import (
	"fmt"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

// verifyHLTBSteamChain re-proves at confirm time exactly what the importer
// asserted at write time: the work owns a release anchored to a Steam appid,
// the HowLongToBeat record names that same appid, and the appid maps to one
// work and one HLTB record and nothing else. jobs/hltbrefs skips every
// ambiguous appid rather than guessing, so a row that survives this check
// carries the same 1:1 evidence it was created from — no model call, only a
// join. Until this existed the whole family was dropped from the lane
// (skipped_hltb_unreachable), leaving 2,168 rows that no machine would look at
// and no reviewer could evaluate by hand.
func verifyHLTBSteamChain(db *gorm.DB, up StagingDBs, reg sourceReg, items []refItem, out map[string]chainResult) error {
	if len(items) == 0 {
		return nil
	}
	if up.HLTB == nil {
		for _, it := range items {
			out[it.Hash] = unproven("hltb_mirror", "the howlongtobeat mirror is not reachable")
		}
		return nil
	}
	steamID, ok := reg.idByKey["steam"]
	if !ok {
		return fmt.Errorf("source registry has no steam entry")
	}

	worksByAppid, err := loadSteamAnchoredWorks(db, steamID)
	if err != nil {
		return err
	}
	hltbByAppid, err := loadHLTBSteamAppids(up.HLTB)
	if err != nil {
		return err
	}
	appidByHLTB := make(map[string]string, len(hltbByAppid))
	for appid, ids := range hltbByAppid {
		for _, id := range ids {
			appidByHLTB[id] = appid
		}
	}

	for _, it := range items {
		appid, ok := appidByHLTB[it.ExternalID]
		if !ok {
			out[it.Hash] = unproven("hltb_appid",
				"hltb record "+it.ExternalID+" names no steam appid in the mirror")
			continue
		}
		works, mirror := worksByAppid[appid], hltbByAppid[appid]
		if len(works) != 1 || len(mirror) != 1 {
			out[it.Hash] = unproven("steam_appid_ambiguous", fmt.Sprintf(
				"steam appid %s maps to %d works and %d hltb records", appid, len(works), len(mirror)))
			continue
		}
		if works[0] != it.EntityID {
			out[it.Hash] = unproven("steam_anchor", fmt.Sprintf(
				"steam appid %s anchors work %d, not %d", appid, works[0], it.EntityID))
			continue
		}
		out[it.Hash] = verified([]chainStep{
			{Name: "hltb_appid", OK: true,
				Detail: fmt.Sprintf("hltb %s names steam appid %s", it.ExternalID, appid)},
			{Name: "steam_anchor", OK: true,
				Detail: fmt.Sprintf("steam appid %s anchors a release of work %d and of nothing else", appid, it.EntityID)},
		})
	}
	return nil
}

// Scoped to the galgame medium, matching jobs/hltbrefs: the ambiguity test has
// to see the same population the importer saw, or it would call a pair
// ambiguous that the importer had already cleared.
func loadSteamAnchoredWorks(db *gorm.DB, steamSource int16) (map[string][]int64, error) {
	var rows []struct {
		WorkID int64  `gorm:"column:work_id"`
		Appid  string `gorm:"column:appid"`
	}
	if err := db.Raw(`
		SELECT DISTINCT w.id AS work_id, r.external_id AS appid
		FROM catalog_work w
		JOIN catalog_release rel ON rel.work_id = w.id AND rel.deleted_at IS NULL
		JOIN catalog_external_ref r ON r.entity_type = ? AND r.entity_id = rel.id
			AND r.source_id = ? AND r.link_kind = ? AND r.dead_at IS NULL
		WHERE w.medium_id = (SELECT id FROM catalog_medium WHERE key = 'galgame')
		  AND w.deleted_at IS NULL`,
		model.EntityTypeRelease, steamSource, model.LinkKindExact).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string][]int64, len(rows))
	for _, r := range rows {
		out[r.Appid] = append(out[r.Appid], r.WorkID)
	}
	return out, nil
}

func loadHLTBSteamAppids(hltb *gorm.DB) (map[string][]string, error) {
	var rows []struct {
		HltbID string `gorm:"column:hltb_id"`
		Appid  string `gorm:"column:appid"`
	}
	if err := hltb.Raw(`
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
