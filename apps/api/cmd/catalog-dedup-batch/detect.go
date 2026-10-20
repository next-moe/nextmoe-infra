package main

import (
	"sort"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

const (
	classCharacter        = "character"
	classCreditName       = "credit_name"
	classOrphanCreditName = "orphan-creditname"
	classMixedCreditName  = "mixed-creditname"
	classPerson           = "person"
	classLabel            = "label"
)

type mergeGroup struct {
	class    string
	survivor int64
	sources  []int64
	sample   string
}

type detectStats struct {
	charGroups        int
	charPairs         int
	charDirtyBkt      int
	charBridged       int
	charInstanceDetox int
	creditGroups      int
	creditPairs       int
	orphanGroups      int
	orphanPairs       int
	orphanDirtyBkt    int
	orphanBridged     int
	mixedGroups       int
	mixedPairs        int
	mixedDirtyBkt     int
	mixedBridged      int
	mixedFrozen       int
}

type charRow struct {
	WorkID      int64  `gorm:"column:work_id"`
	Fold        string `gorm:"column:fold"`
	CharacterID int64  `gorm:"column:character_id"`
	HasImage    bool   `gorm:"column:has_image"`
	Naliases    int    `gorm:"column:naliases"`
	Sources     string `gorm:"column:sources"`
}

type charMeta struct {
	hasImage bool
	naliases int
	sources  map[int16]bool
	fold     string
}

func detectCharacters(db *gorm.DB) ([]mergeGroup, detectStats, error) {
	var rows []charRow
	if err := db.Raw(`
		WITH char_work AS (
		  SELECT character_id, work_id FROM catalog_work_character WHERE matched_by <> 'curated'
		  UNION
		  SELECT character_id, work_id FROM catalog_credit WHERE character_id IS NOT NULL),
		folded AS (
		  SELECT DISTINCT cw.character_id, cw.work_id,
		         regexp_replace(c.display_name_norm, '\s', '', 'g') AS fold
		  FROM char_work cw
		  JOIN catalog_character c ON c.id = cw.character_id AND c.deleted_at IS NULL),
		bn AS (
		  SELECT work_id, fold FROM folded
		  GROUP BY work_id, fold HAVING count(DISTINCT character_id) > 1)
		SELECT f.work_id, f.fold, f.character_id,
		       (c.image_hash IS NOT NULL) AS has_image,
		       (SELECT count(*) FROM catalog_character_alias a WHERE a.character_id = f.character_id) AS naliases,
		       COALESCE((SELECT string_agg(DISTINCT r.source_id::text, ',' ORDER BY r.source_id::text)
		                   FROM catalog_external_ref r
		                  WHERE r.entity_type = 4 AND r.entity_id = f.character_id), '') AS sources
		FROM folded f
		JOIN bn USING (work_id, fold)
		JOIN catalog_character c ON c.id = f.character_id
		ORDER BY f.work_id, f.fold, f.character_id`).Scan(&rows).Error; err != nil {
		return nil, detectStats{}, err
	}

	instOf, err := loadVndbInstanceMap(db)
	if err != nil {
		return nil, detectStats{}, err
	}

	meta := make(map[int64]charMeta, len(rows))
	for _, r := range rows {
		if _, ok := meta[r.CharacterID]; !ok {
			meta[r.CharacterID] = charMeta{hasImage: r.HasImage, naliases: r.Naliases, sources: parseSources(r.Sources), fold: r.Fold}
		}
	}
	srcOf := func(id int64) map[int16]bool { return meta[id].sources }

	uf := newUnionFind()
	var st detectStats
	inClean := map[int64]bool{}
	for i := 0; i < len(rows); {
		j := i
		for j < len(rows) && rows[j].WorkID == rows[i].WorkID && rows[j].Fold == rows[i].Fold {
			j++
		}
		bucket := rows[i:j]
		i = j
		if filtered := detoxVndbInstances(bucket, instOf); len(filtered) < len(bucket) {
			st.charInstanceDetox++
			bucket = filtered
		}
		if len(bucket) < 2 {
			continue
		}
		if hasSourceCollision(idsOf(bucket), srcOf) {
			st.charDirtyBkt++
			continue
		}
		for k := 1; k < len(bucket); k++ {
			uf.union(bucket[0].CharacterID, bucket[k].CharacterID)
		}
		for _, m := range bucket {
			inClean[m.CharacterID] = true
		}
	}

	comps := map[int64][]int64{}
	for id := range inClean {
		root := uf.find(id)
		comps[root] = append(comps[root], id)
	}
	var groups []mergeGroup
	for _, ids := range comps {
		if len(ids) < 2 {
			continue
		}
		if hasSourceCollision(ids, srcOf) {
			st.charBridged++
			continue
		}
		survivor, sources := pickCharSurvivor(ids, meta)
		st.charGroups++
		st.charPairs += len(sources)
		groups = append(groups, mergeGroup{
			class: classCharacter, survivor: survivor, sources: sources,
			sample: "'" + meta[survivor].fold + "' survivor=" + itoa(survivor) + " sources=" + joinIDs(sources),
		})
	}
	sort.Slice(groups, func(a, b int) bool { return groups[a].survivor < groups[b].survivor })
	return groups, st, nil
}

func hasSourceCollision(ids []int64, sourcesOf func(int64) map[int16]bool) bool {
	seen := map[int16]bool{}
	for _, id := range ids {
		for s := range sourcesOf(id) {
			if seen[s] {
				return true
			}
			seen[s] = true
		}
	}
	return false
}

func pickCharSurvivor(ids []int64, meta map[int64]charMeta) (survivor int64, sources []int64) {
	ordered := append([]int64(nil), ids...)
	sort.Slice(ordered, func(a, b int) bool {
		ma, mb := meta[ordered[a]], meta[ordered[b]]
		if ma.hasImage != mb.hasImage {
			return ma.hasImage
		}
		if ma.naliases != mb.naliases {
			return ma.naliases > mb.naliases
		}
		return ordered[a] < ordered[b]
	})
	return ordered[0], ordered[1:]
}

type creditRow struct {
	ID       int64  `gorm:"column:id"`
	PersonID int64  `gorm:"column:person_id"`
	Fold     string `gorm:"column:fold"`
	Naliases int    `gorm:"column:naliases"`
	Ncredits int    `gorm:"column:ncredits"`
}

func detectCreditNames(db *gorm.DB) ([]mergeGroup, detectStats, error) {
	var rows []creditRow
	if err := db.Raw(`
		WITH g AS (
		  SELECT person_id, regexp_replace(name_norm, '\s', '', 'g') AS fold
		  FROM catalog_credit_name WHERE person_id IS NOT NULL
		  GROUP BY person_id, regexp_replace(name_norm, '\s', '', 'g')
		  HAVING count(*) > 1)
		SELECT cn.id, cn.person_id, regexp_replace(cn.name_norm, '\s', '', 'g') AS fold,
		       (SELECT count(*) FROM catalog_name_alias a WHERE a.credit_name_id = cn.id) AS naliases,
		       (SELECT count(*) FROM catalog_credit c WHERE c.credit_name_id = cn.id) AS ncredits
		FROM catalog_credit_name cn
		JOIN g ON g.person_id = cn.person_id
		      AND g.fold = regexp_replace(cn.name_norm, '\s', '', 'g')
		ORDER BY cn.person_id, fold, cn.id`).Scan(&rows).Error; err != nil {
		return nil, detectStats{}, err
	}

	var groups []mergeGroup
	var st detectStats
	for i := 0; i < len(rows); {
		j := i
		for j < len(rows) && rows[j].PersonID == rows[i].PersonID && rows[j].Fold == rows[i].Fold {
			j++
		}
		bucket := rows[i:j]
		i = j
		ordered := append([]creditRow(nil), bucket...)
		sort.Slice(ordered, func(a, b int) bool {
			if ordered[a].Naliases != ordered[b].Naliases {
				return ordered[a].Naliases > ordered[b].Naliases
			}
			if ordered[a].Ncredits != ordered[b].Ncredits {
				return ordered[a].Ncredits > ordered[b].Ncredits
			}
			return ordered[a].ID < ordered[b].ID
		})
		survivor := ordered[0].ID
		sources := make([]int64, 0, len(ordered)-1)
		for _, r := range ordered[1:] {
			sources = append(sources, r.ID)
		}
		st.creditGroups++
		st.creditPairs += len(sources)
		groups = append(groups, mergeGroup{
			class: classCreditName, survivor: survivor, sources: sources,
			sample: "person=" + itoa(ordered[0].PersonID) + " '" + ordered[0].Fold + "' survivor=" + itoa(survivor) + " sources=" + joinIDs(sources),
		})
	}
	return groups, st, nil
}

type orphanRow struct {
	WorkID       int64  `gorm:"column:work_id"`
	RoleID       int64  `gorm:"column:role_id"`
	Fold         string `gorm:"column:fold"`
	CreditNameID int64  `gorm:"column:credit_name_id"`
	Ncredits     int    `gorm:"column:ncredits"`
	Naliases     int    `gorm:"column:naliases"`
	Sources      string `gorm:"column:sources"`
}

type orphanMeta struct {
	ncredits int
	naliases int
	sources  map[int16]bool
	fold     string
}

func detectOrphanCreditNames(db *gorm.DB) ([]mergeGroup, detectStats, error) {
	var rows []orphanRow
	if err := db.Raw(`
		WITH cn_work AS (
		  SELECT DISTINCT c.credit_name_id, c.work_id, c.role_id
		  FROM catalog_credit c
		  JOIN catalog_credit_name cn ON cn.id = c.credit_name_id AND cn.person_id IS NULL),
		folded AS (
		  SELECT cw.credit_name_id, cw.work_id, cw.role_id,
		         regexp_replace(cn.name_norm, '\s', '', 'g') AS fold
		  FROM cn_work cw
		  JOIN catalog_credit_name cn ON cn.id = cw.credit_name_id),
		bn AS (
		  SELECT work_id, role_id, fold FROM folded
		  GROUP BY work_id, role_id, fold HAVING count(DISTINCT credit_name_id) > 1)
		SELECT f.work_id, f.role_id, f.fold, f.credit_name_id,
		       (SELECT count(*) FROM catalog_credit c WHERE c.credit_name_id = f.credit_name_id) AS ncredits,
		       (SELECT count(*) FROM catalog_name_alias a WHERE a.credit_name_id = f.credit_name_id) AS naliases,
		       COALESCE((SELECT string_agg(DISTINCT r.source_id::text, ',' ORDER BY r.source_id::text)
		                   FROM catalog_external_ref r
		                  WHERE r.entity_type = 1 AND r.entity_id = f.credit_name_id), '') AS sources
		FROM folded f
		JOIN bn USING (work_id, role_id, fold)
		ORDER BY f.work_id, f.role_id, f.fold, f.credit_name_id`).Scan(&rows).Error; err != nil {
		return nil, detectStats{}, err
	}

	meta := make(map[int64]orphanMeta, len(rows))
	for _, r := range rows {
		if _, ok := meta[r.CreditNameID]; !ok {
			meta[r.CreditNameID] = orphanMeta{ncredits: r.Ncredits, naliases: r.Naliases, sources: parseSources(r.Sources), fold: r.Fold}
		}
	}
	srcOf := func(id int64) map[int16]bool { return meta[id].sources }

	uf := newUnionFind()
	var st detectStats
	inClean := map[int64]bool{}
	for i := 0; i < len(rows); {
		j := i
		for j < len(rows) && rows[j].WorkID == rows[i].WorkID && rows[j].RoleID == rows[i].RoleID && rows[j].Fold == rows[i].Fold {
			j++
		}
		bucket := rows[i:j]
		i = j
		ids := make([]int64, len(bucket))
		for k, r := range bucket {
			ids[k] = r.CreditNameID
		}
		if hasSourceCollision(ids, srcOf) {
			st.orphanDirtyBkt++
			continue
		}
		for k := 1; k < len(ids); k++ {
			uf.union(ids[0], ids[k])
		}
		for _, id := range ids {
			inClean[id] = true
		}
	}

	comps := map[int64][]int64{}
	for id := range inClean {
		root := uf.find(id)
		comps[root] = append(comps[root], id)
	}
	var groups []mergeGroup
	for _, ids := range comps {
		if len(ids) < 2 {
			continue
		}
		if hasSourceCollision(ids, srcOf) {
			st.orphanBridged++
			continue
		}
		survivor, sources := pickOrphanSurvivor(ids, meta)
		st.orphanGroups++
		st.orphanPairs += len(sources)
		groups = append(groups, mergeGroup{
			class: classOrphanCreditName, survivor: survivor, sources: sources,
			sample: "'" + meta[survivor].fold + "' survivor=" + itoa(survivor) + " sources=" + joinIDs(sources),
		})
	}
	sort.Slice(groups, func(a, b int) bool { return groups[a].survivor < groups[b].survivor })
	return groups, st, nil
}

func pickOrphanSurvivor(ids []int64, meta map[int64]orphanMeta) (survivor int64, sources []int64) {
	ordered := append([]int64(nil), ids...)
	sort.Slice(ordered, func(a, b int) bool {
		ma, mb := meta[ordered[a]], meta[ordered[b]]
		if ma.ncredits != mb.ncredits {
			return ma.ncredits > mb.ncredits
		}
		if ma.naliases != mb.naliases {
			return ma.naliases > mb.naliases
		}
		return ordered[a] < ordered[b]
	})
	return ordered[0], ordered[1:]
}

func parseSources(s string) map[int16]bool {
	out := map[int16]bool{}
	if s == "" {
		return out
	}
	for _, p := range strings.Split(s, ",") {
		if n, err := strconv.Atoi(p); err == nil {
			out[int16(n)] = true
		}
	}
	return out
}

func idsOf(bucket []charRow) []int64 {
	out := make([]int64, len(bucket))
	for i, r := range bucket {
		out[i] = r.CharacterID
	}
	return out
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func joinIDs(ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = itoa(id)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

type unionFind struct{ parent map[int64]int64 }

func newUnionFind() *unionFind { return &unionFind{parent: map[int64]int64{}} }

func (u *unionFind) find(x int64) int64 {
	p, ok := u.parent[x]
	if !ok {
		u.parent[x] = x
		return x
	}
	if p != x {
		u.parent[x] = u.find(p)
	}
	return u.parent[x]
}

func (u *unionFind) union(a, b int64) {
	ra, rb := u.find(a), u.find(b)
	if ra != rb {
		u.parent[ra] = rb
	}
}

type mixedRow struct {
	WorkID       int64  `gorm:"column:work_id"`
	RoleID       int64  `gorm:"column:role_id"`
	Fold         string `gorm:"column:fold"`
	CreditNameID int64  `gorm:"column:credit_name_id"`
	PersonID     *int64 `gorm:"column:person_id"`
	Ncredits     int    `gorm:"column:ncredits"`
	Naliases     int    `gorm:"column:naliases"`
	Sources      string `gorm:"column:sources"`
}

type mixedMeta struct {
	personID *int64
	ncredits int
	naliases int
	sources  map[int16]bool
	fold     string
}

func detectMixedCreditNames(db *gorm.DB) ([]mergeGroup, detectStats, error) {
	var rows []mixedRow
	if err := db.Raw(`
		WITH cn_work AS (
		  SELECT DISTINCT c.credit_name_id, c.work_id, c.role_id
		  FROM catalog_credit c),
		folded AS (
		  SELECT cw.credit_name_id, cw.work_id, cw.role_id, cn.person_id,
		         regexp_replace(cn.name_norm, '\s', '', 'g') AS fold
		  FROM cn_work cw
		  JOIN catalog_credit_name cn ON cn.id = cw.credit_name_id),
		bn AS (
		  SELECT work_id, role_id, fold FROM folded
		  GROUP BY work_id, role_id, fold
		  HAVING count(DISTINCT credit_name_id) > 1
		     AND count(*) FILTER (WHERE person_id IS NULL) > 0
		     AND count(*) FILTER (WHERE person_id IS NOT NULL) > 0)
		SELECT f.work_id, f.role_id, f.fold, f.credit_name_id, f.person_id,
		       (SELECT count(*) FROM catalog_credit c WHERE c.credit_name_id = f.credit_name_id) AS ncredits,
		       (SELECT count(*) FROM catalog_name_alias a WHERE a.credit_name_id = f.credit_name_id) AS naliases,
		       COALESCE((SELECT string_agg(DISTINCT r.source_id::text, ',' ORDER BY r.source_id::text)
		                   FROM catalog_external_ref r
		                  WHERE r.entity_type = 1 AND r.entity_id = f.credit_name_id), '') AS sources
		FROM folded f
		JOIN bn USING (work_id, role_id, fold)
		ORDER BY f.work_id, f.role_id, f.fold, f.credit_name_id`).Scan(&rows).Error; err != nil {
		return nil, detectStats{}, err
	}

	meta := make(map[int64]mixedMeta, len(rows))
	for _, r := range rows {
		if _, ok := meta[r.CreditNameID]; !ok {
			meta[r.CreditNameID] = mixedMeta{personID: r.PersonID, ncredits: r.Ncredits,
				naliases: r.Naliases, sources: parseSources(r.Sources), fold: r.Fold}
		}
	}
	srcOf := func(id int64) map[int16]bool { return meta[id].sources }

	distinctPersons := func(ids []int64) map[int64]bool {
		out := map[int64]bool{}
		for _, id := range ids {
			if p := meta[id].personID; p != nil {
				out[*p] = true
			}
		}
		return out
	}

	uf := newUnionFind()
	var st detectStats
	inClean := map[int64]bool{}
	for i := 0; i < len(rows); {
		j := i
		for j < len(rows) && rows[j].WorkID == rows[i].WorkID && rows[j].RoleID == rows[i].RoleID && rows[j].Fold == rows[i].Fold {
			j++
		}
		bucket := rows[i:j]
		i = j
		ids := make([]int64, len(bucket))
		for k, r := range bucket {
			ids[k] = r.CreditNameID
		}
		if len(distinctPersons(ids)) >= 2 {
			st.mixedFrozen++
			continue
		}
		if hasSourceCollision(ids, srcOf) {
			st.mixedDirtyBkt++
			continue
		}
		for k := 1; k < len(ids); k++ {
			uf.union(ids[0], ids[k])
		}
		for _, id := range ids {
			inClean[id] = true
		}
	}

	comps := map[int64][]int64{}
	for id := range inClean {
		root := uf.find(id)
		comps[root] = append(comps[root], id)
	}
	var groups []mergeGroup
	for _, ids := range comps {
		if len(ids) < 2 {
			continue
		}
		if len(distinctPersons(ids)) >= 2 {
			st.mixedFrozen++
			continue
		}
		if hasSourceCollision(ids, srcOf) {
			st.mixedBridged++
			continue
		}
		survivor, sources := pickMixedSurvivor(ids, meta)
		if survivor == 0 {
			continue
		}
		st.mixedGroups++
		st.mixedPairs += len(sources)
		groups = append(groups, mergeGroup{
			class: classMixedCreditName, survivor: survivor, sources: sources,
			sample: "'" + meta[survivor].fold + "' survivor=" + itoa(survivor) + " (anchored) sources=" + joinIDs(sources),
		})
	}
	sort.Slice(groups, func(a, b int) bool { return groups[a].survivor < groups[b].survivor })
	return groups, st, nil
}

func pickMixedSurvivor(ids []int64, meta map[int64]mixedMeta) (survivor int64, sources []int64) {
	var anchored []int64
	for _, id := range ids {
		if meta[id].personID != nil {
			anchored = append(anchored, id)
		}
	}
	if len(anchored) == 0 {
		return 0, nil
	}
	sort.Slice(anchored, func(a, b int) bool {
		ma, mb := meta[anchored[a]], meta[anchored[b]]
		if ma.ncredits != mb.ncredits {
			return ma.ncredits > mb.ncredits
		}
		if ma.naliases != mb.naliases {
			return ma.naliases > mb.naliases
		}
		return anchored[a] < anchored[b]
	})
	survivor = anchored[0]
	for _, id := range ids {
		if id != survivor {
			sources = append(sources, id)
		}
	}
	return survivor, sources
}

func loadVndbInstanceMap(db *gorm.DB) (map[int64]int64, error) {
	var rows []struct {
		Inst int64 `gorm:"column:inst"`
		Main int64 `gorm:"column:main_id"`
	}
	if err := db.Raw(`
		SELECT ri.entity_id AS inst, rm.entity_id AS main_id
		FROM catalog_external_ref ri
		JOIN catalog_source s ON s.id = ri.source_id AND s.key = 'vndb'
		JOIN src_vndb.chars ch ON ch.id = ri.external_id AND ch.main <> ''
		JOIN catalog_external_ref rm ON rm.source_id = ri.source_id AND rm.entity_type = 4
			AND rm.link_kind = 0 AND rm.external_id = ch.main AND rm.entity_id <> ri.entity_id
		WHERE ri.entity_type = 4 AND ri.link_kind = 0`).Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[int64]int64, len(rows))
	for _, r := range rows {
		m[r.Inst] = r.Main
	}
	return m, nil
}

func detoxVndbInstances(bucket []charRow, instOf map[int64]int64) []charRow {
	if len(instOf) == 0 {
		return bucket
	}
	present := make(map[int64]bool, len(bucket))
	for _, m := range bucket {
		present[m.CharacterID] = true
	}
	out := bucket[:0:0]
	for _, m := range bucket {
		if base, ok := instOf[m.CharacterID]; ok && present[base] {
			continue
		}
		out = append(out, m)
	}
	return out
}
