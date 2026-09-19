package releasemeta

import (
	"context"
	"strconv"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

func loadDateCandidates(ctx context.Context, db *gorm.DB, reg registry, limit, offset int) ([]dateCandidate, error) {
	relSources := []int16{reg.vndbSource, reg.dlsiteSource, reg.getchuSource}
	var out []dateCandidate
	if err := db.WithContext(ctx).Raw(`
		SELECT rel.id AS release_id, rel.work_id AS work_id,
			rel.released_y, rel.released_m, rel.released_d,
			w.site,
			(SELECT count(*) FROM catalog_release r2 WHERE r2.work_id = w.id AND r2.deleted_at IS NULL) AS live_releases,
			COALESCE(rel.field_provenance -> 'released_y' -> 0 ->> 'source', '') AS date_source
		FROM catalog_release rel
		JOIN catalog_work w ON w.id = rel.work_id AND w.deleted_at IS NULL
		WHERE rel.deleted_at IS NULL
		  AND (
			EXISTS (
				SELECT 1 FROM catalog_external_ref r
				WHERE r.entity_type = ? AND r.entity_id = rel.id
				  AND r.source_id IN ? AND r.link_kind = ? AND r.dead_at IS NULL
			)
			OR (
				(w.site IS NULL OR w.site = '')
				AND (SELECT count(*) FROM catalog_release r2 WHERE r2.work_id = w.id AND r2.deleted_at IS NULL) = 1
				AND EXISTS (
					SELECT 1 FROM catalog_external_ref r
					WHERE r.entity_type = ? AND r.entity_id = w.id
					  AND r.source_id = ? AND r.link_kind = ? AND r.dead_at IS NULL
				)
			)
			OR (
				(SELECT count(*) FROM catalog_release r2 WHERE r2.work_id = w.id AND r2.deleted_at IS NULL) = 1
				AND EXISTS (
					SELECT 1 FROM catalog_external_ref r
					WHERE r.entity_type = ? AND r.entity_id = w.id
					  AND r.source_id = ? AND r.link_kind = ? AND r.dead_at IS NULL
				)
			)
		  )
		ORDER BY rel.id`,
		model.EntityTypeRelease, relSources, model.LinkKindExact,
		model.EntityTypeWork, reg.egSource, model.LinkKindExact,
		model.EntityTypeWork, reg.bangumiSource, model.LinkKindExact,
	).Scan(&out).Error; err != nil {
		return nil, err
	}
	return window(out, limit, offset), nil
}

func attachDateRefs(ctx context.Context, db *gorm.DB, reg registry, cands []dateCandidate) error {
	ids := make([]int64, len(cands))
	byID := make(map[int64]*dateCandidate, len(cands))
	for i := range cands {
		cands[i].refs = map[string][]string{}
		ids[i] = cands[i].ReleaseID
		byID[cands[i].ReleaseID] = &cands[i]
	}
	relSources := []int16{reg.vndbSource, reg.dlsiteSource, reg.getchuSource}
	workSources := []int16{reg.egSource, reg.bangumiSource}
	type row struct {
		ReleaseID  int64  `gorm:"column:release_id"`
		SourceID   int16  `gorm:"column:source_id"`
		ExternalID string `gorm:"column:external_id"`
		EntityType int16  `gorm:"column:entity_type"`
	}
	for start := 0; start < len(ids); start += mirrorBatch {
		end := min(start+mirrorBatch, len(ids))
		var rows []row
		if err := db.WithContext(ctx).Raw(`
			SELECT rel.id AS release_id, r.source_id, r.external_id, r.entity_type
			FROM catalog_release rel
			JOIN catalog_external_ref r ON r.dead_at IS NULL AND r.link_kind = ?
			  AND (
				(r.entity_type = ? AND r.entity_id = rel.id AND r.source_id IN ?)
				OR (r.entity_type = ? AND r.entity_id = rel.work_id AND r.source_id IN ?)
			  )
			WHERE rel.id IN ?`,
			model.LinkKindExact,
			model.EntityTypeRelease, relSources,
			model.EntityTypeWork, workSources,
			ids[start:end],
		).Scan(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			c := byID[r.ReleaseID]
			if c == nil {
				continue
			}
			lane := ""
			switch r.SourceID {
			case reg.vndbSource:
				if r.EntityType == model.EntityTypeRelease {
					lane = laneVNDB
				}
			case reg.dlsiteSource:
				if r.EntityType == model.EntityTypeRelease {
					lane = laneDL
				}
			case reg.getchuSource:
				if r.EntityType == model.EntityTypeRelease {
					lane = laneGC
				}
			case reg.egSource:
				if r.EntityType == model.EntityTypeWork && c.egOK() {
					lane = laneEG
				}
			case reg.bangumiSource:
				if r.EntityType == model.EntityTypeWork && c.bgmOK() {
					lane = laneBGM
				}
			}
			if lane == "" {
				continue
			}
			c.refs[lane] = append(c.refs[lane], r.ExternalID)
		}
	}
	return nil
}

func collectMirrorKeys(cands []dateCandidate) (vndb, dl, gc []string, eg, bgm []int64) {
	vndbSet, dlSet, gcSet := map[string]bool{}, map[string]bool{}, map[string]bool{}
	egSet, bgmSet := map[int64]bool{}, map[int64]bool{}
	for i := range cands {
		c := &cands[i]
		for _, ext := range c.refs[laneVNDB] {
			vndbSet[ext] = true
		}
		for _, ext := range c.refs[laneDL] {
			dlSet[ext] = true
		}
		for _, ext := range c.refs[laneGC] {
			gcSet[ext] = true
		}
		for _, ext := range c.refs[laneEG] {
			if id, err := strconv.ParseInt(ext, 10, 64); err == nil {
				egSet[id] = true
			}
		}
		for _, ext := range c.refs[laneBGM] {
			if id, err := strconv.ParseInt(ext, 10, 64); err == nil {
				bgmSet[id] = true
			}
		}
	}
	for k := range vndbSet {
		vndb = append(vndb, k)
	}
	for k := range dlSet {
		dl = append(dl, k)
	}
	for k := range gcSet {
		gc = append(gc, k)
	}
	for k := range egSet {
		eg = append(eg, k)
	}
	for k := range bgmSet {
		bgm = append(bgm, k)
	}
	return
}
