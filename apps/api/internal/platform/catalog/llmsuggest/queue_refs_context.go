package llmsuggest

import (
	"strconv"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

const contextSample = 8

// Every enum that reaches a dossier is spelled out, because the judge invents a
// meaning for one that does not. Measured 2026-09-15 over the 461 queued entity
// refs whose two names are byte-identical: it answered same on 358, unsure on
// 101 and different on 8, and the reasons behind the last two groups are almost
// all an invented type clash -- "catalog entity is a work (type 3)" (3 is
// Label), "external record is a character (type 2)" for a bangumi person record
// (2 is company). Never hand this judge a bare code.
func entityTypeName(t int16) string {
	switch t {
	case model.EntityTypePerson:
		return "Person"
	case model.EntityTypeCreditName:
		return "CreditName (one name a person is credited under)"
	case model.EntityTypeLabel:
		return "Label (a brand, circle or company that publishes works)"
	case model.EntityTypeCharacter:
		return "Character"
	case model.EntityTypeWork:
		return "Work"
	case model.EntityTypeRelease:
		return "Release"
	case model.EntityTypeTag:
		return "Tag"
	case model.EntityTypeEngine:
		return "Engine"
	}
	return "unknown(" + strconv.Itoa(int(t)) + ")"
}

func bgmPersonTypeName(t int) string {
	switch t {
	case 1:
		return "individual"
	case 2:
		return "company"
	case 3:
		return "group or circle"
	}
	return "unspecified"
}

func vndbProducerTypeName(t string) string {
	switch t {
	case "co":
		return "company"
	case "in":
		return "individual"
	case "ng":
		return "amateur group"
	}
	return t
}

type ownerNameRow struct {
	Owner int64  `gorm:"column:owner"`
	Name  string `gorm:"column:name"`
}

type sourceContextRow struct {
	Owner int64  `gorm:"column:owner"`
	Works int64  `gorm:"column:works"`
	Name  string `gorm:"column:name"`
}

type catalogContextSpec struct {
	entityType int16
	counts     string
	samples    string
}

var catalogContextSpecs = []catalogContextSpec{
	{model.EntityTypeLabel,
		`SELECT l.id AS owner,
			(SELECT count(*) FROM catalog_work_label wl WHERE wl.label_id = l.id) AS works,
			(SELECT count(*) FROM catalog_release_label rl WHERE rl.label_id = l.id) AS releases
		 FROM catalog_label l WHERE l.id IN ?`,
		`SELECT l.id AS owner, s.name FROM catalog_label l
		 JOIN LATERAL (SELECT DISTINCT w.display_name AS name FROM catalog_work_label wl
			JOIN catalog_work w ON w.id = wl.work_id AND w.deleted_at IS NULL
			WHERE wl.label_id = l.id ORDER BY 1 LIMIT ?) s ON TRUE
		 WHERE l.id IN ?`},
	{model.EntityTypeCreditName,
		`SELECT n.id AS owner,
			(SELECT count(DISTINCT c.work_id) FROM catalog_credit c WHERE c.credit_name_id = n.id) AS works,
			0 AS releases
		 FROM catalog_credit_name n WHERE n.id IN ?`,
		`SELECT n.id AS owner, s.name FROM catalog_credit_name n
		 JOIN LATERAL (SELECT DISTINCT w.display_name AS name FROM catalog_credit c
			JOIN catalog_work w ON w.id = c.work_id AND w.deleted_at IS NULL
			WHERE c.credit_name_id = n.id ORDER BY 1 LIMIT ?) s ON TRUE
		 WHERE n.id IN ?`},
	{model.EntityTypeCharacter,
		`SELECT c.id AS owner,
			(SELECT count(*) FROM catalog_work_character wc WHERE wc.character_id = c.id) AS works,
			0 AS releases
		 FROM catalog_character c WHERE c.id IN ?`,
		`SELECT c.id AS owner, s.name FROM catalog_character c
		 JOIN LATERAL (SELECT DISTINCT w.display_name AS name FROM catalog_work_character wc
			JOIN catalog_work w ON w.id = wc.work_id AND w.deleted_at IS NULL
			WHERE wc.character_id = c.id ORDER BY 1 LIMIT ?) s ON TRUE
		 WHERE c.id IN ?`},
}

const labelAliasSamples = `SELECT l.id AS owner, s.name FROM catalog_label l
	JOIN LATERAL (SELECT a.name FROM catalog_label_alias a
		WHERE a.label_id = l.id ORDER BY a.id LIMIT ?) s ON TRUE
	WHERE l.id IN ?`

// attachRefContext gives the name-shaped entities the only evidence that can
// corroborate a name: what each side publishes. Without it the dossier for a
// Label ref was the catalog name and the source name and nothing else -- the
// same two strings the matching rule had already compared -- so the judge was
// re-deciding the rule's own input and its confidence was string similarity.
// That is why all 530 same verdicts topped out at 0.85 against a 0.9 bar.
func attachRefContext(db, eg *gorm.DB, reg sourceReg, items []refItem,
	catalog map[string]map[string]any, source map[string]map[string]any) error {
	byET := map[int16][]int64{}
	seen := map[string]bool{}
	for _, it := range items {
		k := entityKey(it.EntityType, it.EntityID)
		if seen[k] {
			continue
		}
		seen[k] = true
		byET[it.EntityType] = append(byET[it.EntityType], it.EntityID)
	}
	for _, sp := range catalogContextSpecs {
		for _, chunk := range chunkBy(byET[sp.entityType], 500) {
			if len(chunk) == 0 {
				continue
			}
			var counts []struct {
				Owner    int64 `gorm:"column:owner"`
				Works    int64 `gorm:"column:works"`
				Releases int64 `gorm:"column:releases"`
			}
			if err := db.Raw(sp.counts, chunk).Scan(&counts).Error; err != nil {
				return err
			}
			for _, r := range counts {
				if m := catalog[entityKey(sp.entityType, r.Owner)]; m != nil {
					m["works"] = r.Works
					if sp.entityType == model.EntityTypeLabel {
						m["releases"] = r.Releases
					}
				}
			}
			if err := putOwnerNames(db, sp.samples, chunk, "sample_works", sp.entityType, catalog); err != nil {
				return err
			}
			links, err := exactLinks(db, reg, sp.entityType, chunk)
			if err != nil {
				return err
			}
			for owner, list := range links {
				if m := catalog[entityKey(sp.entityType, owner)]; m != nil {
					m["already_linked"] = list
				}
			}
		}
	}
	for _, chunk := range chunkBy(byET[model.EntityTypeLabel], 500) {
		if len(chunk) == 0 {
			continue
		}
		if err := putOwnerNames(db, labelAliasSamples, chunk, "aliases", model.EntityTypeLabel, catalog); err != nil {
			return err
		}
	}
	return attachSourceContext(db, eg, reg, items, source)
}

func putOwnerNames(db *gorm.DB, sql string, ids []int64, field string, et int16, catalog map[string]map[string]any) error {
	var rows []ownerNameRow
	if err := db.Raw(sql, contextSample, ids).Scan(&rows).Error; err != nil {
		return err
	}
	for _, r := range rows {
		m := catalog[entityKey(et, r.Owner)]
		if m == nil || r.Name == "" {
			continue
		}
		list, _ := m[field].([]string)
		if len(list) >= contextSample {
			continue
		}
		m[field] = append(list, r.Name)
	}
	return nil
}

// The registries a catalog entity is already identified in. A queued label
// almost always holds one already -- 2 of ~600 did not -- so this is the field
// that turns "the names match" into "the same company, and here is who agrees".
func exactLinks(db *gorm.DB, reg sourceReg, et int16, ids []int64) (map[int64][]string, error) {
	var rows []struct {
		EntityID   int64  `gorm:"column:entity_id"`
		SourceID   int16  `gorm:"column:source_id"`
		ExternalID string `gorm:"column:external_id"`
	}
	if err := db.Raw(`SELECT entity_id, source_id, external_id FROM catalog_external_ref
		WHERE link_kind = 0 AND dead_at IS NULL AND entity_type = ? AND entity_id IN ?
		ORDER BY entity_id, source_id`, et, ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := map[int64][]string{}
	for _, r := range rows {
		if len(out[r.EntityID]) >= contextSample {
			continue
		}
		out[r.EntityID] = append(out[r.EntityID], reg.key(r.SourceID)+":"+r.ExternalID)
	}
	return out, nil
}

func attachSourceContext(db, eg *gorm.DB, reg sourceReg, items []refItem, source map[string]map[string]any) error {
	vndb, bgm, egSrc := reg.id(sourceKeyVNDB), reg.id(sourceKeyBangumi), reg.id(sourceKeyEG)
	var bgmPeople, vndbProd, egBrands []refItem
	for _, it := range items {
		named := it.EntityType == model.EntityTypeLabel || it.EntityType == model.EntityTypeCreditName
		switch {
		case named && it.SourceID == bgm:
			bgmPeople = append(bgmPeople, it)
		case it.EntityType == model.EntityTypeLabel && it.SourceID == vndb:
			vndbProd = append(vndbProd, it)
		case it.EntityType == model.EntityTypeLabel && it.SourceID == egSrc:
			egBrands = append(egBrands, it)
		}
	}
	if err := attachBgmPersonContext(db, bgmPeople, source); err != nil {
		return err
	}
	if err := attachVNDBProducerContext(db, vndbProd, source); err != nil {
		return err
	}
	return attachEGBrandContext(eg, egBrands, source)
}

func attachBgmPersonContext(db *gorm.DB, items []refItem, source map[string]map[string]any) error {
	idx := indexByExt(items)
	for _, chunk := range chunkBy(extIDs(items), 500) {
		if len(chunk) == 0 {
			continue
		}
		var rows []sourceContextRow
		if err := db.Raw(`SELECT p.id AS owner,
			(SELECT count(*) FROM src_bangumi.subject_person sp WHERE sp.person_id = p.id) AS works,
			coalesce(s.name,'') AS name
			FROM src_bangumi.person p
			LEFT JOIN LATERAL (SELECT DISTINCT sub.name FROM src_bangumi.subject_person sp
				JOIN src_bangumi.subject sub ON sub.id = sp.subject_id
				WHERE sp.person_id = p.id ORDER BY 1 LIMIT ?) s ON TRUE
			WHERE p.id::text IN ?`, contextSample, chunk).Scan(&rows).Error; err != nil {
			return err
		}
		spreadSourceContext(rows, idx, source)
	}
	for _, it := range items {
		if m := source[it.Hash]; m != nil {
			if t, ok := m["type"].(int); ok {
				m["type"] = bgmPersonTypeName(t)
			}
		}
	}
	return nil
}

func attachVNDBProducerContext(db *gorm.DB, items []refItem, source map[string]map[string]any) error {
	idx := indexByExt(items)
	for _, chunk := range chunkBy(extIDs(items), 500) {
		if len(chunk) == 0 {
			continue
		}
		var rows []struct {
			OwnerID string `gorm:"column:owner_id"`
			Works   int64  `gorm:"column:works"`
			Name    string `gorm:"column:name"`
		}
		if err := db.Raw(`SELECT p.id AS owner_id,
			(SELECT count(DISTINCT rv.vid) FROM src_vndb.releases_producers rp
				JOIN src_vndb.releases_vn rv ON rv.id = rp.id WHERE rp.pid = p.id) AS works,
			coalesce(s.name,'') AS name
			FROM src_vndb.producers p
			LEFT JOIN LATERAL (
				SELECT (SELECT t.title FROM src_vndb.vn_titles t WHERE t.id = v.vid ORDER BY t.lang LIMIT 1) AS name
				FROM (SELECT DISTINCT rv.vid FROM src_vndb.releases_producers rp
					JOIN src_vndb.releases_vn rv ON rv.id = rp.id
					WHERE rp.pid = p.id ORDER BY 1 LIMIT ?) v) s ON TRUE
			WHERE p.id IN ?`, contextSample, chunk).Scan(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			for _, it := range idx[r.OwnerID] {
				addSourceContext(source[it.Hash], r.Works, r.Name)
			}
		}
	}
	for _, it := range items {
		if m := source[it.Hash]; m != nil {
			if t, ok := m["type"].(string); ok {
				m["type"] = vndbProducerTypeName(t)
			}
		}
	}
	return nil
}

func attachEGBrandContext(eg *gorm.DB, items []refItem, source map[string]map[string]any) error {
	if eg == nil || len(items) == 0 {
		return nil
	}
	idx := indexByExt(items)
	for _, chunk := range chunkBy(extIDs(items), 500) {
		if len(chunk) == 0 {
			continue
		}
		var rows []sourceContextRow
		if err := eg.Raw(`SELECT b.id AS owner,
			(SELECT count(*) FROM games g WHERE g.brand_id = b.id) AS works,
			coalesce(s.gamename,'') AS name
			FROM brands b
			LEFT JOIN LATERAL (SELECT DISTINCT g.gamename FROM games g
				WHERE g.brand_id = b.id AND g.gamename IS NOT NULL ORDER BY 1 LIMIT ?) s ON TRUE
			WHERE b.id::text IN ?`, contextSample, chunk).Scan(&rows).Error; err != nil {
			return err
		}
		spreadSourceContext(rows, idx, source)
	}
	return nil
}

func spreadSourceContext(rows []sourceContextRow, idx map[string][]refItem, source map[string]map[string]any) {
	for _, r := range rows {
		for _, it := range idx[strconv.FormatInt(r.Owner, 10)] {
			addSourceContext(source[it.Hash], r.Works, r.Name)
		}
	}
}

func addSourceContext(m map[string]any, works int64, name string) {
	if m == nil {
		return
	}
	m["works"] = works
	if name == "" {
		return
	}
	list, _ := m["sample_works"].([]string)
	if len(list) >= contextSample {
		return
	}
	m["sample_works"] = append(list, name)
}
