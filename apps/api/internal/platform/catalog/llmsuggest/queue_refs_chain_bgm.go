package llmsuggest

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"
	"api/internal/platform/catalog/srcbangumi"

	"gorm.io/gorm"
)

// verifyBgmSteamChain re-proves what jobs/storerefs asserted: a Bangumi
// subject this work is exactly anchored to names one Steam appid, this one.
// It also refuses an appid another work already holds exact, because
// confirming that ref would anchor one store page to two works.
func verifyBgmSteamChain(db *gorm.DB, reg sourceReg, items []refItem, out map[string]chainResult) error {
	if len(items) == 0 {
		return nil
	}
	bgmSrc, steamSrc := reg.id(sourceKeyBangumi), reg.id(sourceKeySteam)
	if bgmSrc == 0 || steamSrc == 0 {
		return fmt.Errorf("source registry has no bangumi or steam entry")
	}
	workIDs := make([]int64, 0, len(items))
	seen := map[int64]struct{}{}
	for _, it := range items {
		if _, ok := seen[it.EntityID]; !ok {
			seen[it.EntityID] = struct{}{}
			workIDs = append(workIDs, it.EntityID)
		}
	}
	stated := map[int64]map[string][]int64{}
	for _, chunk := range chunkBy(workIDs, 500) {
		var rows []struct {
			WorkID    int64  `gorm:"column:work_id"`
			SubjectID int64  `gorm:"column:subject_id"`
			Infobox   []byte `gorm:"column:infobox_parsed"`
		}
		if err := db.Raw(`SELECT r.entity_id AS work_id, s.id AS subject_id, s.infobox_parsed
			FROM catalog_external_ref r
			JOIN src_bangumi.subject s ON s.id = r.external_id::bigint
			WHERE r.entity_type = ? AND r.source_id = ? AND r.link_kind = ? AND r.dead_at IS NULL
			  AND r.entity_id IN ?`,
			model.EntityTypeWork, bgmSrc, model.LinkKindExact, chunk).Scan(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			if stated[r.WorkID] == nil {
				stated[r.WorkID] = map[string][]int64{}
			}
			for _, appid := range srcbangumi.SteamAppIDs(r.Infobox) {
				stated[r.WorkID][appid] = append(stated[r.WorkID][appid], r.SubjectID)
			}
		}
	}
	anchored, err := repository.SteamAnchoredWorks(context.Background(), db, steamSrc)
	if err != nil {
		return err
	}
	for _, it := range items {
		appids, anchoredBgm := stated[it.EntityID]
		if !anchoredBgm {
			out[it.Hash] = unproven("work_exact_bangumi", "work holds no exact bangumi anchor with a staged subject")
			continue
		}
		subjects, named := appids[it.ExternalID]
		if !named || len(appids) != 1 {
			out[it.Hash] = unproven("subject_names_appid", fmt.Sprintf(
				"bangumi subjects name steam appids %s, want exactly %s", joinKeys(appids), it.ExternalID))
			continue
		}
		var others []string
		for _, w := range anchored[it.ExternalID] {
			if w != it.EntityID {
				others = append(others, strconv.FormatInt(w, 10))
			}
		}
		if len(others) > 0 {
			out[it.Hash] = unproven("steam_anchor", fmt.Sprintf(
				"steam appid %s already anchors work %s", it.ExternalID, strings.Join(others, ",")))
			continue
		}
		out[it.Hash] = verified([]chainStep{
			{Name: "work_exact_bangumi", OK: true, Detail: fmt.Sprintf("subjects %v", subjects)},
			{Name: "subject_names_appid", OK: true, Detail: "steam appid " + it.ExternalID},
			{Name: "steam_anchor", OK: true, Detail: "no other work holds steam appid " + it.ExternalID},
		})
	}
	return nil
}

func joinKeys(m map[string][]int64) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return "none"
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}
