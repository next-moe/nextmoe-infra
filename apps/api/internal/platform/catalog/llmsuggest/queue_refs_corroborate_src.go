package llmsuggest

import (
	"strconv"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

// srcWorkSpec resolves an upstream record to the works it is attached to, and
// carries their titles in the same pass so a brand is read once. numeric says
// the upstream key column is an integer: casting it to text to meet a string
// bind would drop the index on tables with millions of rows.
type srcWorkSpec struct {
	sql     string
	numeric bool
	eg      bool
}

func sourceNeighbourSpec(reg sourceReg, et, sourceID int16) (srcWorkSpec, bool) {
	switch reg.key(sourceID) {
	case sourceKeyVNDB:
		switch et {
		case model.EntityTypeLabel:
			return srcWorkSpec{sql: `SELECT rp.pid AS owner, rv.vid AS work, coalesce(t.title,'') AS title
				FROM src_vndb.releases_producers rp
				JOIN src_vndb.releases_vn rv ON rv.id = rp.id
				LEFT JOIN src_vndb.vn_titles t ON t.id = rv.vid
				WHERE rp.pid IN ?`}, true
		case model.EntityTypeCharacter:
			return srcWorkSpec{sql: `SELECT cv.id AS owner, cv.vid AS work, coalesce(t.title,'') AS title
				FROM src_vndb.chars_vns cv
				LEFT JOIN src_vndb.vn_titles t ON t.id = cv.vid
				WHERE cv.id IN ?`}, true
		case model.EntityTypeCreditName:
			return srcWorkSpec{sql: `SELECT vs.aid::text AS owner, vs.id AS work, coalesce(t.title,'') AS title
				FROM src_vndb.vn_staff vs
				LEFT JOIN src_vndb.vn_titles t ON t.id = vs.id
				WHERE vs.aid IN ?`, numeric: true}, true
		}
	case sourceKeyBangumi:
		switch et {
		case model.EntityTypeLabel, model.EntityTypeCreditName:
			return srcWorkSpec{sql: `SELECT sp.person_id::text AS owner, s.id::text AS work, x.title
				FROM src_bangumi.subject_person sp
				JOIN src_bangumi.subject s ON s.id = sp.subject_id
				CROSS JOIN LATERAL (VALUES (s.name), (coalesce(s.name_cn,''))) AS x(title)
				WHERE sp.person_id IN ?`, numeric: true}, true
		case model.EntityTypeCharacter:
			return srcWorkSpec{sql: `SELECT sc.character_id::text AS owner, s.id::text AS work, x.title
				FROM src_bangumi.subject_character sc
				JOIN src_bangumi.subject s ON s.id = sc.subject_id
				CROSS JOIN LATERAL (VALUES (s.name), (coalesce(s.name_cn,''))) AS x(title)
				WHERE sc.character_id IN ?`, numeric: true}, true
		}
	case sourceKeyEG:
		if et == model.EntityTypeLabel {
			return srcWorkSpec{sql: `SELECT g.brand_id::text AS owner, g.id::text AS work,
				coalesce(g.gamename,'') AS title
				FROM games g WHERE g.brand_id IN ?`, numeric: true, eg: true}, true
		}
	}
	return srcWorkSpec{}, false
}

type srcGroup struct{ et, src int16 }

func (g srcGroup) owner(external string) string {
	return strconv.FormatInt(int64(g.et), 10) + ":" +
		strconv.FormatInt(int64(g.src), 10) + ":" + external
}

func sourceNeighbourWorks(db, eg *gorm.DB, reg sourceReg, rows []QueueVerdict) (
	map[int64][]string, map[string][]string, map[int64]bool, error) {
	byGroup := map[srcGroup][]string{}
	seen := map[string]bool{}
	for _, r := range rows {
		g := srcGroup{r.EntityType, r.SourceID}
		if seen[g.owner(r.ExternalID)] {
			continue
		}
		seen[g.owner(r.ExternalID)] = true
		byGroup[g] = append(byGroup[g], r.ExternalID)
	}

	works := map[string][]string{}
	titles := map[string][]string{}
	unavailable := map[int64]bool{}
	listed := map[string]bool{}
	for g, exts := range byGroup {
		spec, ok := sourceNeighbourSpec(reg, g.et, g.src)
		if !ok {
			continue
		}
		conn := db
		if spec.eg {
			if eg == nil {
				for _, r := range rows {
					if r.EntityType == g.et && r.SourceID == g.src {
						unavailable[r.ID] = true
					}
				}
				continue
			}
			conn = eg
		}
		scan := func(bind any) error {
			var got []struct {
				Owner string `gorm:"column:owner"`
				Work  string `gorm:"column:work"`
				Title string `gorm:"column:title"`
			}
			if err := conn.Raw(spec.sql, bind).Scan(&got).Error; err != nil {
				return err
			}
			for _, x := range got {
				own := g.owner(x.Owner)
				if !listed[own+"|"+x.Work] {
					listed[own+"|"+x.Work] = true
					works[own] = append(works[own], x.Work)
				}
				if k := titleKey(x.Title); k != "" {
					sw := srcWorkKey(g.src, x.Work)
					titles[sw] = append(titles[sw], k)
				}
			}
			return nil
		}
		if spec.numeric {
			for _, chunk := range chunkBy(numericIDs(exts), 500) {
				if err := scan(chunk); err != nil {
					return nil, nil, nil, err
				}
			}
			continue
		}
		for _, chunk := range chunkBy(exts, 500) {
			if err := scan(chunk); err != nil {
				return nil, nil, nil, err
			}
		}
	}

	out := map[int64][]string{}
	for _, r := range rows {
		out[r.ID] = works[srcGroup{r.EntityType, r.SourceID}.owner(r.ExternalID)]
	}
	return out, titles, unavailable, nil
}

func numericIDs(in []string) []int64 {
	out := make([]int64, 0, len(in))
	for _, s := range in {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// A work ref gets no title fallback. rule:bgm-title-only and
// rule:title-year-strict queue these rows by comparing the very same titles, so
// a title agreeing here is the matching rule agreeing with itself. What is left
// is the cast and the staff: a character id or a person id that both sides
// already carry was written by neither rule.
func corroborateWorkRefs(db *gorm.DB, reg sourceReg, rows []QueueVerdict, out map[int64]refEvidence) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.EntityID)
	}
	mine, err := catalogWorkNeighbourIDs(db, ids)
	if err != nil {
		return err
	}
	theirs, err := sourceWorkNeighbourIDs(db, reg, rows)
	if err != nil {
		return err
	}
	for _, r := range rows {
		want := theirs[srcWorkKey(r.SourceID, r.ExternalID)]
		if len(want) == 0 {
			continue
		}
		for _, nid := range mine[r.EntityID][r.SourceID] {
			if want[nid] {
				out[r.ID] = refEvidence{Corroborator: reg.key(r.SourceID) + " " + nid}
				break
			}
		}
	}
	return nil
}

const catalogWorkNeighbourSQL = `SELECT wc.work_id AS work, x.source_id, 'c:' || x.external_id AS nid
	FROM catalog_work_character wc
	JOIN catalog_external_ref x ON x.entity_type = 4 AND x.link_kind = 0 AND x.dead_at IS NULL
		AND x.entity_id = wc.character_id
	WHERE wc.work_id IN ?
	UNION
	SELECT c.work_id, x.source_id, 'p:' || x.external_id
	FROM catalog_credit c
	JOIN catalog_external_ref x ON x.entity_type = 1 AND x.link_kind = 0 AND x.dead_at IS NULL
		AND x.entity_id = c.credit_name_id
	WHERE c.work_id IN ?`

func catalogWorkNeighbourIDs(db *gorm.DB, works []int64) (map[int64]map[int16][]string, error) {
	out := map[int64]map[int16][]string{}
	for _, chunk := range chunkBy(dedupeIDs(works), 500) {
		var got []struct {
			Work int64  `gorm:"column:work"`
			Src  int16  `gorm:"column:source_id"`
			NID  string `gorm:"column:nid"`
		}
		if err := db.Raw(catalogWorkNeighbourSQL, chunk, chunk).Scan(&got).Error; err != nil {
			return nil, err
		}
		for _, g := range got {
			if out[g.Work] == nil {
				out[g.Work] = map[int16][]string{}
			}
			out[g.Work][g.Src] = append(out[g.Work][g.Src], g.NID)
		}
	}
	return out, nil
}

var sourceWorkNeighbourSQL = map[string]string{
	sourceKeyBangumi: `SELECT sc.subject_id::text AS owner, 'c:' || sc.character_id::text AS nid
		FROM src_bangumi.subject_character sc WHERE sc.subject_id IN ?
		UNION
		SELECT sp.subject_id::text, 'p:' || sp.person_id::text
		FROM src_bangumi.subject_person sp WHERE sp.subject_id IN ?`,
	sourceKeyVNDB: `SELECT cv.vid AS owner, 'c:' || cv.id AS nid
		FROM src_vndb.chars_vns cv WHERE cv.vid IN ?
		UNION
		SELECT vs.id, 'p:' || vs.aid::text
		FROM src_vndb.vn_staff vs WHERE vs.id IN ?`,
}

func sourceWorkNeighbourIDs(db *gorm.DB, reg sourceReg, rows []QueueVerdict) (map[string]map[string]bool, error) {
	bySource := map[int16][]string{}
	seen := map[string]bool{}
	for _, r := range rows {
		k := srcWorkKey(r.SourceID, r.ExternalID)
		if seen[k] {
			continue
		}
		seen[k] = true
		bySource[r.SourceID] = append(bySource[r.SourceID], r.ExternalID)
	}
	out := map[string]map[string]bool{}
	for src, exts := range bySource {
		key := reg.key(src)
		sql, ok := sourceWorkNeighbourSQL[key]
		if !ok {
			continue
		}
		scan := func(bind any) error {
			var got []struct {
				Owner string `gorm:"column:owner"`
				NID   string `gorm:"column:nid"`
			}
			if err := db.Raw(sql, bind, bind).Scan(&got).Error; err != nil {
				return err
			}
			for _, g := range got {
				k := srcWorkKey(src, g.Owner)
				if out[k] == nil {
					out[k] = map[string]bool{}
				}
				out[k][g.NID] = true
			}
			return nil
		}
		if key == sourceKeyBangumi {
			for _, chunk := range chunkBy(numericIDs(exts), 500) {
				if err := scan(chunk); err != nil {
					return nil, err
				}
			}
			continue
		}
		for _, chunk := range chunkBy(exts, 500) {
			if err := scan(chunk); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}
