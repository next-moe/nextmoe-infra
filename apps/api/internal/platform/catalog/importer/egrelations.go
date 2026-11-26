package importer

import (
	"api/internal/platform/catalog/model"

	"gorm.io/gorm/clause"
)

var egGameRelations = map[string]bgmRelSpec{
	"sequel":     {typeID: relSequelOf},
	"fandisk":    {typeID: relFandiscOf},
	"remake":     {typeID: relRemakeOf},
	"spinoff":    {typeID: relSideStoryOf},
	"transplant": {typeID: relAlternativeVersion, symmetric: true},
}

var egSkippedByDesign = map[string]bool{"bundling": true, "apend": true, "free": true}

type EGRelationStats struct {
	TotalRows         int
	Mapped            int
	Edges             int
	EdgesWritten      int
	AlreadyInDB       int
	Folded            int
	SkippedByDesign   int
	SkippedUnmapped   int
	SkippedUnanchored int
	SkippedSelf       int
}

func (im *Importer) RunEGRelations() (EGRelationStats, error) {
	var st EGRelationStats

	anchor, err := im.loadEGWorkAnchors()
	if err != nil {
		return st, err
	}
	existing, err := im.loadExistingWorkRelations()
	if err != nil {
		return st, err
	}

	var rows []struct {
		Subject string `gorm:"column:subject"`
		Object  string `gorm:"column:object"`
		Kind    string `gorm:"column:kind"`
	}
	if err := im.eg.Raw(`SELECT game_subject::text AS subject, game_object::text AS object,
		raw::json->>'kind' AS kind FROM game_relations ORDER BY pk`).Scan(&rows).Error; err != nil {
		return st, err
	}
	st.TotalRows = len(rows)

	src := egSource
	seen := make(map[[3]int64]struct{})
	var edges []model.CatalogWorkRelation
	for _, r := range rows {
		spec, ok := egGameRelations[r.Kind]
		if !ok {
			if egSkippedByDesign[r.Kind] {
				st.SkippedByDesign++
			} else {
				st.SkippedUnmapped++
			}
			continue
		}
		st.Mapped++
		wSub, okSub := anchor[r.Subject]
		wObj, okObj := anchor[r.Object]
		if !okSub || !okObj {
			st.SkippedUnanchored++
			continue
		}
		if wSub == wObj {
			st.SkippedSelf++
			continue
		}
		a, b := wSub, wObj
		if spec.symmetric && a > b {
			a, b = b, a
		}
		key := [3]int64{a, b, spec.typeID}
		if _, inDB := existing[key]; inDB {
			st.AlreadyInDB++
			continue
		}
		if _, dup := seen[key]; dup {
			st.Folded++
			continue
		}
		seen[key] = struct{}{}
		edges = append(edges, model.CatalogWorkRelation{
			AWorkID: a, BWorkID: b, RelationTypeID: spec.typeID, SourceID: &src,
		})
	}
	if im.limit > 0 && len(edges) > im.limit {
		edges = edges[:im.limit]
	}
	st.Edges = len(edges)

	if im.dryRun || len(edges) == 0 {
		return st, nil
	}
	res := im.catalog.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&edges, 1000)
	if res.Error != nil {
		return st, res.Error
	}
	st.EdgesWritten = int(res.RowsAffected)
	st.AlreadyInDB += st.Edges - st.EdgesWritten
	if err := touchWorks(im.catalog, relationEndpoints(edges)); err != nil {
		return st, err
	}
	return st, nil
}

// loadEGWorkAnchors falls back to the sole holder of an EG id carried as an
// eg-xlink-twin when no work holds it exact. The twin is another EG record of
// the same VNDB game, and EG often files the port relation against it: 212193
// (EG 19150) is a transplant of 14276, which 458 holds only as a twin, so the
// edge was never written and the pair went unseen.
func (im *Importer) loadEGWorkAnchors() (map[string]int64, error) {
	var rows []struct {
		ExternalID string `gorm:"column:external_id"`
		EntityID   int64  `gorm:"column:entity_id"`
	}
	if err := im.catalog.Raw(`
		SELECT external_id, entity_id FROM catalog_external_ref
		WHERE entity_type = ? AND source_id = ? AND link_kind = ?`,
		model.EntityTypeWork, egSource, model.LinkKindExact).Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[string]int64, len(rows))
	for _, r := range rows {
		m[r.ExternalID] = r.EntityID
	}

	var twins []struct {
		ExternalID string `gorm:"column:external_id"`
		EntityID   int64  `gorm:"column:entity_id"`
	}
	if err := im.catalog.Raw(`
		SELECT r.external_id, min(r.entity_id) AS entity_id
		FROM catalog_external_ref r
		JOIN catalog_work w ON w.id = r.entity_id AND w.deleted_at IS NULL
		WHERE r.entity_type = ? AND r.source_id = ? AND r.link_kind = ? AND r.matched_by = ? AND r.dead_at IS NULL
		GROUP BY r.external_id
		HAVING count(DISTINCT r.entity_id) = 1`,
		model.EntityTypeWork, egSource, model.LinkKindRelated, ruleEGXlinkTwin).Scan(&twins).Error; err != nil {
		return nil, err
	}
	for _, t := range twins {
		if _, held := m[t.ExternalID]; !held {
			m[t.ExternalID] = t.EntityID
		}
	}
	return m, nil
}
