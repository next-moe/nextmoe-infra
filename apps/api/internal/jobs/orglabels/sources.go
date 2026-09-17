package orglabels

import (
	"fmt"
	"sort"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

func loadSource(catalog, eg *gorm.DB, src string, limit int) ([]orgRec, ruleSet, int16, error) {
	switch src {
	case "vndb":
		o, err := loadVNDBOrgs(catalog, limit)
		return o, ruleSetFor(sourceVNDB), sourceVNDB, err
	case "bangumi":
		o, err := loadBGMOrgs(catalog, limit)
		return o, ruleSetFor(sourceBangumi), sourceBangumi, err
	case "eg":
		if eg == nil {
			return nil, ruleSet{}, 0, fmt.Errorf("eg source requested but no erogamescape connection")
		}
		o, err := loadEGOrgs(catalog, eg, limit)
		return o, ruleSetFor(sourceEG), sourceEG, err
	default:
		return nil, ruleSet{}, 0, fmt.Errorf("unknown source %q (want vndb|bangumi|eg|all)", src)
	}
}

func finalize(recByExt map[string]*orgRec, limit int) []orgRec {
	out := make([]orgRec, 0, len(recByExt))
	for _, r := range recByExt {
		r.nameNorms = dedupStrings(r.nameNorms)
		r.works = dedupInts(r.works)
		r.attribWorks = dedupInts(r.attribWorks)
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].extID < out[j].extID })
	if limit > 0 && limit < len(out) {
		out = out[:limit]
	}
	return out
}

func loadVNDBOrgs(db *gorm.DB, limit int) ([]orgRec, error) {
	var meta []struct {
		ExtID     string `gorm:"column:ext_id"`
		Type      string `gorm:"column:type"`
		Lang      string `gorm:"column:lang"`
		Latin     string `gorm:"column:latin"`
		Name      string `gorm:"column:name"`
		NameNorm  string `gorm:"column:name_norm"`
		LatinNorm string `gorm:"column:latin_norm"`
	}
	if err := db.Raw(`
		SELECT id AS ext_id, type, coalesce(lang,'') AS lang, coalesce(latin,'') AS latin, name,
		       lower(normalize(name, NFKC)) AS name_norm,
		       CASE WHEN coalesce(latin,'') <> '' THEN lower(normalize(latin, NFKC)) ELSE '' END AS latin_norm
		FROM src_vndb.producers WHERE type IN ('co','ng','in')`).Scan(&meta).Error; err != nil {
		return nil, err
	}
	recByExt := make(map[string]*orgRec, len(meta))
	for i := range meta {
		m := &meta[i]
		kind, canCreate := model.LabelKindGameBrand, true
		switch m.Type {
		case "ng":
			kind = model.LabelKindDoujinCircle
		case "in":
			canCreate = false
		}
		var latin *string
		if m.Latin != "" {
			l := m.Latin
			latin = &l
		}
		norms := []string{m.NameNorm}
		if m.LatinNorm != "" {
			norms = append(norms, m.LatinNorm)
		}
		recByExt[m.ExtID] = &orgRec{
			extID: m.ExtID, nameNorms: norms, displayName: m.Name,
			latin: latin, lang: m.Lang, newKind: kind, canCreate: canCreate,
		}
	}

	var aliases []struct {
		ExtID string `gorm:"column:ext_id"`
		Norm  string `gorm:"column:norm"`
	}
	if err := db.Raw(`
		SELECT p.id AS ext_id, lower(normalize(trim(a), NFKC)) AS norm
		FROM src_vndb.producers p, unnest(string_to_array(p.alias, E'\n')) a
		WHERE p.type IN ('co','ng','in') AND coalesce(p.alias,'') <> '' AND trim(a) <> ''`).Scan(&aliases).Error; err != nil {
		return nil, err
	}
	for _, al := range aliases {
		if r := recByExt[al.ExtID]; r != nil && al.Norm != "" {
			r.nameNorms = append(r.nameNorms, al.Norm)
		}
	}

	if err := attachWorks(db, recByExt, `
		SELECT DISTINCT rp.pid AS ext_id, vwa.work_id AS work_id
		FROM src_vndb.releases_producers rp
		JOIN src_vndb.releases_vn rv ON rv.id = rp.id
		JOIN (SELECT external_id AS vid, entity_id AS work_id FROM catalog_external_ref
		      WHERE entity_type = 5 AND source_id = 2 AND link_kind = 0) vwa ON vwa.vid = rv.vid`,
		func(r *orgRec, workID int64) { r.works = append(r.works, workID) }); err != nil {
		return nil, err
	}
	if err := attachWorks(db, recByExt, `
		SELECT DISTINCT rp.pid AS ext_id, vwa.work_id AS work_id
		FROM src_vndb.releases_producers rp
		JOIN src_vndb.releases sr ON sr.id = rp.id
		JOIN src_vndb.releases_vn rv ON rv.id = rp.id
		JOIN (SELECT external_id AS vid, entity_id AS work_id FROM catalog_external_ref
		      WHERE entity_type = 5 AND source_id = 2 AND link_kind = 0) vwa ON vwa.vid = rv.vid
		JOIN catalog_work w ON w.id = vwa.work_id AND w.deleted_at IS NULL
		WHERE NOT sr.patch AND sr.olang = w.olang`,
		func(r *orgRec, workID int64) { r.attribWorks = append(r.attribWorks, workID) }); err != nil {
		return nil, err
	}
	for _, r := range recByExt {
		r.editionAware = true
	}
	return finalize(recByExt, limit), nil
}

func loadBGMOrgs(db *gorm.DB, limit int) ([]orgRec, error) {
	var meta []struct {
		ExtID    string `gorm:"column:ext_id"`
		Name     string `gorm:"column:name"`
		Type     int    `gorm:"column:type"`
		NameNorm string `gorm:"column:name_norm"`
	}
	if err := db.Raw(`
		SELECT id::text AS ext_id, name, type, lower(normalize(name, NFKC)) AS name_norm
		FROM src_bangumi.person WHERE type IN (2, 3)`).Scan(&meta).Error; err != nil {
		return nil, err
	}
	recByExt := make(map[string]*orgRec, len(meta))
	for i := range meta {
		m := &meta[i]
		kind := model.LabelKindGameBrand
		if m.Type == 3 {
			kind = model.LabelKindGroup
		}
		recByExt[m.ExtID] = &orgRec{
			extID: m.ExtID, nameNorms: []string{m.NameNorm}, displayName: m.Name,
			lang: "", newKind: kind,
		}
	}

	var aliases []struct {
		ExtID string `gorm:"column:ext_id"`
		Norm  string `gorm:"column:norm"`
	}
	if err := db.Raw(`
		SELECT p.id::text AS ext_id, lower(normalize(it->>'Value', NFKC)) AS norm
		FROM src_bangumi.person p,
		     jsonb_array_elements(p.infobox_parsed->'Fields') f,
		     jsonb_array_elements(f->'Items') it
		WHERE p.type IN (2, 3) AND coalesce(p.parse_error,'') = ''
		  AND jsonb_typeof(p.infobox_parsed->'Fields') = 'array'
		  AND jsonb_typeof(f->'Items') = 'array'
		  AND (f->>'Key') = '别名' AND coalesce(it->>'Value','') <> ''`).Scan(&aliases).Error; err != nil {
		return nil, err
	}
	for _, al := range aliases {
		if r := recByExt[al.ExtID]; r != nil && al.Norm != "" {
			r.nameNorms = append(r.nameNorms, al.Norm)
		}
	}

	// Until 2026-09-17 any position on any subject counted, and the dry run
	// that day planned 175 new labels out of bands that sang a theme song,
	// magazines that serialised a manga, a streaming service and a manga artist.
	// Developer and publisher are the two positions that make a company a label,
	// and the lane only anchors: a label is minted from vndb or erogamescape.
	if err := attachWorks(db, recByExt, fmt.Sprintf(`
		SELECT DISTINCT sp.person_id::text AS ext_id, bwa.work_id AS work_id
		FROM src_bangumi.subject_person sp
		JOIN (SELECT external_id::bigint AS sid, entity_id AS work_id FROM catalog_external_ref
		      WHERE entity_type = 5 AND source_id = 3 AND link_kind = 0) bwa ON bwa.sid = sp.subject_id
		JOIN src_bangumi.person p ON p.id = sp.person_id
		WHERE p.type IN (2, 3) AND sp.position IN (%d, %d)`, bgmPositionDeveloper, bgmPositionPublisher),
		func(r *orgRec, workID int64) { r.works = append(r.works, workID) }); err != nil {
		return nil, err
	}
	return finalize(recByExt, limit), nil
}

const (
	bgmPositionDeveloper = 1001
	bgmPositionPublisher = 1002
)

func loadEGOrgs(catalog, eg *gorm.DB, limit int) ([]orgRec, error) {
	var meta []struct {
		ExtID     string `gorm:"column:ext_id"`
		Kind      string `gorm:"column:kind"`
		Brandname string `gorm:"column:brandname"`
		BNNorm    string `gorm:"column:bn_norm"`
		MNNorm    string `gorm:"column:mn_norm"`
		BFNorm    string `gorm:"column:bf_norm"`
		MFNorm    string `gorm:"column:mf_norm"`
	}
	if err := eg.Raw(`
		SELECT id::text AS ext_id, coalesce(raw->>'kind','') AS kind,
		       coalesce(raw->>'brandname','') AS brandname,
		       lower(normalize(coalesce(raw->>'brandname',''), NFKC)) AS bn_norm,
		       lower(normalize(coalesce(raw->>'makername',''), NFKC)) AS mn_norm,
		       lower(normalize(coalesce(raw->>'brandfurigana',''), NFKC)) AS bf_norm,
		       lower(normalize(coalesce(raw->>'makerfurigana',''), NFKC)) AS mf_norm
		FROM brands`).Scan(&meta).Error; err != nil {
		return nil, err
	}
	recByExt := make(map[string]*orgRec, len(meta))
	for i := range meta {
		m := &meta[i]
		kind := model.LabelKindGameBrand
		if m.Kind == "CIRCLE" {
			kind = model.LabelKindDoujinCircle
		}
		norms := nonEmpty(m.BNNorm, m.MNNorm, m.BFNorm, m.MFNorm)
		recByExt[m.ExtID] = &orgRec{
			extID: m.ExtID, nameNorms: norms, displayName: m.Brandname,
			lang: "", newKind: kind, canCreate: true,
		}
	}

	var gb []struct {
		GameID  int64 `gorm:"column:game_id"`
		BrandID int64 `gorm:"column:brand_id"`
	}
	if err := eg.Raw(`SELECT id AS game_id, brand_id FROM games WHERE brand_id IS NOT NULL`).Scan(&gb).Error; err != nil {
		return nil, err
	}
	var gw []struct {
		GameID int64 `gorm:"column:game_id"`
		WorkID int64 `gorm:"column:work_id"`
	}
	if err := catalog.Raw(`
		SELECT external_id::bigint AS game_id, entity_id AS work_id FROM catalog_external_ref
		WHERE entity_type = 5 AND source_id = 5 AND link_kind = 0`).Scan(&gw).Error; err != nil {
		return nil, err
	}
	gameWork := make(map[int64]int64, len(gw))
	for _, x := range gw {
		gameWork[x.GameID] = x.WorkID
	}
	for _, x := range gb {
		work, ok := gameWork[x.GameID]
		if !ok {
			continue
		}
		if r := recByExt[fmt.Sprintf("%d", x.BrandID)]; r != nil {
			r.works = append(r.works, work)
		}
	}
	return finalize(recByExt, limit), nil
}

func attachWorks(db *gorm.DB, recByExt map[string]*orgRec, query string, add func(*orgRec, int64)) error {
	var works []struct {
		ExtID  string `gorm:"column:ext_id"`
		WorkID int64  `gorm:"column:work_id"`
	}
	if err := db.Raw(query).Scan(&works).Error; err != nil {
		return err
	}
	for _, w := range works {
		if r := recByExt[w.ExtID]; r != nil {
			add(r, w.WorkID)
		}
	}
	return nil
}

func dedupStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := in[:0]
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func dedupInts(in []int64) []int64 {
	seen := make(map[int64]bool, len(in))
	out := in[:0]
	for _, v := range in {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func nonEmpty(vs ...string) []string {
	var out []string
	for _, v := range vs {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}
