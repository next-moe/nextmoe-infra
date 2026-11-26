package llmsuggest

import (
	"fmt"
	"sort"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

const relationTypeAlternativeVersion int64 = 12

// UpstreamIndex holds what an upstream registry says about a pair that neither
// work's own identity keys show. 458 and 212193 were one game for months:
// VNDB r104272 on 458 lists DLsite VJ009575, the id 212193 was minted from,
// and EG records 212193's game as a transplant of 458's. The store-anchor lane
// skipped the declaration as value-taken, and no detector read either fact.
type UpstreamIndex struct {
	declared map[[2]int64]string
	ports    map[[2]int64]struct{}
}

type upstreamDeclRow struct {
	Declarer int64  `gorm:"column:declarer"`
	Holder   int64  `gorm:"column:holder"`
	Release  string `gorm:"column:rid"`
	Source   string `gorm:"column:source_key"`
	Value    string `gorm:"column:v"`
}

// upstreamDeclaredSQL keeps a declaration only when it is exclusive on both
// ends: one work declares the store id and one other work holds it. A VNDB
// release that belongs to several VNs is a pack and names a bundle SKU, so it
// declares nothing about either game.
const upstreamDeclaredSQL = `
	WITH decl AS (
		SELECT DISTINCT rel.work_id AS declarer, vr.external_id AS rid,
		       CASE e.site WHEN 'egs' THEN 'erogamescape' ELSE e.site END AS source_key,
		       lower(CASE e.site
		         WHEN 'dmm' THEN coalesce(substring(e.value FROM 'cid=([A-Za-z0-9_]+)'), substring(e.value FROM '/detail/([A-Za-z0-9_]+)/'))
		         ELSE e.value END) AS v
		FROM catalog_release rel
		JOIN catalog_work w ON w.id = rel.work_id AND w.deleted_at IS NULL
		JOIN catalog_external_ref vr ON vr.entity_type = @release AND vr.entity_id = rel.id
			AND vr.source_id = @vndb AND vr.link_kind = @exact AND vr.dead_at IS NULL
		JOIN src_vndb.releases_extlinks rx ON rx.id = vr.external_id
		JOIN src_vndb.extlinks e ON e.id = rx.link AND e.site IN ('dlsite', 'dmm', 'getchu', 'steam', 'egs')
		WHERE rel.deleted_at IS NULL
		  AND (SELECT count(DISTINCT rv.vid) FROM src_vndb.releases_vn rv WHERE rv.id = vr.external_id) = 1
	),
	held AS (
		SELECT DISTINCT coalesce(rel.work_id, r.entity_id) AS holder, s.key AS source_key, lower(r.external_id) AS v
		FROM catalog_external_ref r
		JOIN catalog_source s ON s.id = r.source_id
		LEFT JOIN catalog_release rel ON r.entity_type = @release AND rel.id = r.entity_id AND rel.deleted_at IS NULL
		JOIN catalog_work w ON w.id = coalesce(rel.work_id, r.entity_id) AND w.deleted_at IS NULL
		WHERE r.link_kind = @exact AND r.dead_at IS NULL
		  AND s.key IN ('dlsite', 'dmm', 'getchu', 'steam', 'erogamescape')
		  AND (r.entity_type = @work OR (r.entity_type = @release AND rel.id IS NOT NULL))
	),
	keys AS (
		SELECT d.source_key, d.v
		FROM decl d JOIN held h ON h.source_key = d.source_key AND h.v = d.v
		GROUP BY d.source_key, d.v
		HAVING count(DISTINCT d.declarer) = 1
		   AND count(DISTINCT h.holder) FILTER (WHERE h.holder <> d.declarer) = 1
	)
	SELECT DISTINCT ON (d.declarer, h.holder) d.declarer, h.holder, d.rid, d.source_key, d.v
	FROM keys k
	JOIN decl d ON d.source_key = k.source_key AND d.v = k.v
	JOIN held h ON h.source_key = k.source_key AND h.v = k.v AND h.holder <> d.declarer
	ORDER BY d.declarer, h.holder, d.source_key, d.v, d.rid`

func LoadUpstreamIndex(db *gorm.DB) (*UpstreamIndex, error) {
	idx := &UpstreamIndex{declared: map[[2]int64]string{}, ports: map[[2]int64]struct{}{}}
	var decls []upstreamDeclRow
	if err := db.Raw(upstreamDeclaredSQL, map[string]any{
		"release": model.EntityTypeRelease, "work": model.EntityTypeWork,
		"vndb": model.SourceVNDB, "exact": model.LinkKindExact,
	}).Scan(&decls).Error; err != nil {
		return nil, fmt.Errorf("upstream declared store ids: %w", err)
	}
	for _, d := range decls {
		k := pairKeyOf(d.Declarer, d.Holder)
		if _, seen := idx.declared[k]; seen {
			continue
		}
		idx.declared[k] = fmt.Sprintf("vndb %s on %d lists %s:%s held by %d", d.Release, d.Declarer, d.Source, d.Value, d.Holder)
	}

	var ports []struct {
		A int64 `gorm:"column:a_work_id"`
		B int64 `gorm:"column:b_work_id"`
	}
	if err := db.Raw(`
		SELECT r.a_work_id, r.b_work_id
		FROM catalog_work_relation r
		JOIN catalog_work wa ON wa.id = r.a_work_id AND wa.deleted_at IS NULL
		JOIN catalog_work wb ON wb.id = r.b_work_id AND wb.deleted_at IS NULL
		WHERE r.source_id = ? AND r.relation_type_id = ? AND wa.medium_id = wb.medium_id`,
		model.SourceErogameScape, relationTypeAlternativeVersion).Scan(&ports).Error; err != nil {
		return nil, fmt.Errorf("upstream port relations: %w", err)
	}
	for _, p := range ports {
		idx.ports[pairKeyOf(p.A, p.B)] = struct{}{}
	}
	return idx, nil
}

func (u *UpstreamIndex) Declared(a, b int64) string { return u.declared[pairKeyOf(a, b)] }

func (u *UpstreamIndex) Port(a, b int64) bool {
	_, ok := u.ports[pairKeyOf(a, b)]
	return ok
}

func (u *UpstreamIndex) DeclaredPairs() [][2]int64 { return sortedPairKeys(u.declared) }

func (u *UpstreamIndex) PortPairs() [][2]int64 { return sortedPairKeys(u.ports) }

func sortedPairKeys[V any](m map[[2]int64]V) [][2]int64 {
	out := make([][2]int64, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i][0] != out[j][0] {
			return out[i][0] < out[j][0]
		}
		return out[i][1] < out[j][1]
	})
	return out
}

func pairKeyOf(a, b int64) [2]int64 {
	if a > b {
		a, b = b, a
	}
	return [2]int64{a, b}
}

func attachUpstreamEvidence(db *gorm.DB, rows []QueueVerdict, out map[int64]pairEvidence) error {
	if len(rows) == 0 {
		return nil
	}
	idx, err := LoadUpstreamIndex(db)
	if err != nil {
		return err
	}
	for _, r := range rows {
		ev := out[r.ID]
		ev.UpstreamDeclared = idx.Declared(r.AID, r.BID)
		ev.UpstreamPort = idx.Port(r.AID, r.BID)
		out[r.ID] = ev
	}
	return nil
}
