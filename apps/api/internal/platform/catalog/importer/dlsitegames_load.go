package importer

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"api/internal/platform/catalog/dlsitecode"
	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/titlekey"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func (im *Importer) loadDLsiteGamesSnap(dlsiteDB *gorm.DB) (*dlGamesSnap, error) {
	roleMap, err := im.roleMap(dlsiteSource)
	if err != nil {
		return nil, err
	}
	prods, creaters, err := loadDLGameRows(dlsiteDB, roleMap)
	if err != nil {
		return nil, err
	}
	heldAny, heldLiveStub, err := im.loadDLHeldWorks()
	if err != nil {
		return nil, err
	}
	retired, err := im.loadDLExactWorknos()
	if err != nil {
		return nil, err
	}
	s := &dlGamesSnap{
		byWorkno:     prods,
		population:   map[string]struct{}{},
		heldAny:      heldAny,
		heldLiveStub: heldLiveStub,
		roleMap:      roleMap,
		creaters:     creaters,
	}
	for workno, p := range prods {
		if _, ok := dlGameTypes[p.workType]; !ok {
			continue
		}
		if _, held := heldAny[workno]; held {
			continue
		}
		if _, retired := retired[workno]; retired {
			continue
		}
		if p.pack {
			s.packCount++
			continue
		}
		s.population[workno] = struct{}{}
	}
	if s.declared, err = im.loadDLDeclaredWorks(); err != nil {
		return nil, err
	}
	if s.titleIndex, err = im.loadDLTitleIndex(); err != nil {
		return nil, err
	}
	if s.workLabels, s.makerLabel, err = im.loadDLCircleIndex(); err != nil {
		return nil, err
	}
	if s.workDates, err = im.loadDLWorkDates(); err != nil {
		return nil, err
	}
	if s.bgmDates, err = im.loadDLBgmDates(); err != nil {
		return nil, err
	}
	if s.rejected, err = im.loadDLRejections(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *dlGamesSnap) creatersOf(m dlGameProd) map[string]dlNamed {
	out := map[string]dlNamed{}
	for _, c := range m.credits {
		if n, ok := s.creaters[c.createrExt]; ok {
			out[c.createrExt] = n
			continue
		}
		out[c.createrExt] = dlNamed{ext: c.createrExt, name: c.createrExt}
	}
	return out
}

func loadDLGameRows(dlsiteDB *gorm.DB, roleMap map[string]int64) (map[string]dlGameProd, map[string]dlNamed, error) {
	var rows []struct {
		Workno    string         `gorm:"column:workno"`
		WorkName  string         `gorm:"column:work_name"`
		Kana      string         `gorm:"column:kana"`
		MakerID   string         `gorm:"column:maker_id"`
		MakerName string         `gorm:"column:maker_name"`
		Age       string         `gorm:"column:age_category"`
		WorkType  string         `gorm:"column:work_type"`
		YMD       string         `gorm:"column:ymd"`
		JSON      datatypes.JSON `gorm:"column:product_json"`
	}
	types := make([]string, 0, len(dlGameTypes))
	for t := range dlGameTypes {
		types = append(types, t)
	}
	if err := dlsiteDB.Raw(`
		SELECT workno, coalesce(work_name,'') AS work_name, coalesce(work_name_kana,'') AS kana,
		       coalesce(maker_id,'') AS maker_id, coalesce(maker_name,'') AS maker_name,
		       coalesce(age_category,'') AS age_category, coalesce(work_type,'') AS work_type,
		       `+dlRegistDaySQL+` AS ymd,
		       jsonb_strip_nulls(jsonb_build_object(
		         'editions', product_json->'editions',
		         'language_editions', product_json->'language_editions',
		         'alt_name', product_json->'alt_name',
		         'is_pack_parent', product_json->'is_pack_parent',
		         'creaters', product_json->'creaters')) AS product_json
		FROM works WHERE status = 'fetched' AND work_type IN ?`, types).Scan(&rows).Error; err != nil {
		return nil, nil, fmt.Errorf("load dlsite games: %w", err)
	}
	out := make(map[string]dlGameProd, len(rows))
	creaters := map[string]dlNamed{}
	for _, r := range rows {
		editions, langs, alts, pack := parseDLProductJSON(r.JSON)
		links := make([]string, 0, len(editions)+len(langs))
		lang := ""
		seenLink := map[string]struct{}{}
		for _, e := range editions {
			if e.Workno == "" {
				continue
			}
			if _, ok := seenLink[e.Workno]; !ok {
				seenLink[e.Workno] = struct{}{}
				links = append(links, e.Workno)
			}
		}
		for _, e := range langs {
			if e.Workno == "" {
				continue
			}
			if e.Workno == r.Workno && e.Lang != "" && lang == "" {
				lang = e.Lang
			}
			if _, ok := seenLink[e.Workno]; !ok {
				seenLink[e.Workno] = struct{}{}
				links = append(links, e.Workno)
			}
		}
		p := dlGameProd{
			workno: r.Workno, name: r.WorkName, kana: r.Kana,
			makerExt: r.MakerID, makerName: r.MakerName, age: r.Age,
			workType: r.WorkType, ymd: r.YMD, lang: lang, altNames: alts,
			pack: pack, links: links,
		}
		p.y, p.m, p.d = ymdParts(r.YMD)
		var creatersJSON datatypes.JSON
		var obj map[string]json.RawMessage
		if json.Unmarshal(r.JSON, &obj) == nil {
			if raw, ok := obj["creaters"]; ok {
				creatersJSON = datatypes.JSON(raw)
			}
		}
		if len(creatersJSON) == 0 {
			creatersJSON = datatypes.JSON([]byte(`{}`))
		}
		for _, c := range parseCreaters(creatersJSON) {
			if _, ok := creaters[c.id]; !ok {
				creaters[c.id] = dlNamed{ext: c.id, name: firstNonEmptyStr(c.name, c.id)}
			}
			roleID, ok := roleMap[c.classification]
			if !ok {
				continue
			}
			p.credits = append(p.credits, dlCredit{createrExt: c.id, roleID: roleID})
		}
		out[r.Workno] = p
	}
	return out, creaters, nil
}

type dlJSONEdition struct {
	Workno string `json:"workno"`
	Lang   string `json:"lang"`
}

func parseDLProductJSON(raw datatypes.JSON) (editions, langs []dlJSONEdition, alts []string, pack bool) {
	var obj struct {
		Editions         []dlJSONEdition `json:"editions"`
		LanguageEditions []dlJSONEdition `json:"language_editions"`
		AltName          json.RawMessage `json:"alt_name"`
		IsPackParent     bool            `json:"is_pack_parent"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, nil, nil, false
	}
	return obj.Editions, obj.LanguageEditions, parseAltNames(obj.AltName), obj.IsPackParent
}

func parseAltNames(raw json.RawMessage) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if strings.TrimSpace(s) == "" {
			return nil
		}
		return []string{s}
	}
	var arr []string
	if json.Unmarshal(raw, &arr) == nil {
		out := arr[:0]
		for _, n := range arr {
			if strings.TrimSpace(n) != "" {
				out = append(out, n)
			}
		}
		return out
	}
	return nil
}

func (im *Importer) loadDLHeldWorks() (anyStatus, liveStub map[string][]int64, err error) {
	var rows []struct {
		Workno string `gorm:"column:external_id"`
		WorkID int64  `gorm:"column:work_id"`
		Status int16  `gorm:"column:status"`
	}
	if err = im.catalog.Raw(`
		SELECT r.external_id, rel.work_id, w.status
		FROM catalog_external_ref r
		JOIN catalog_release rel ON rel.id = r.entity_id AND rel.deleted_at IS NULL
		JOIN catalog_work w ON w.id = rel.work_id AND w.deleted_at IS NULL
		WHERE r.entity_type = ? AND r.source_id = ? AND r.link_kind = ?`,
		model.EntityTypeRelease, dlsiteSource, model.LinkKindExact).Scan(&rows).Error; err != nil {
		return nil, nil, fmt.Errorf("load dlsite exact refs: %w", err)
	}
	anyStatus = map[string][]int64{}
	liveStub = map[string][]int64{}
	for _, r := range rows {
		appendUniqueID(anyStatus, r.Workno, r.WorkID)
		if r.Status == model.WorkStatusLive || r.Status == model.WorkStatusStub {
			appendUniqueID(liveStub, r.Workno, r.WorkID)
		}
	}
	return anyStatus, liveStub, nil
}

// uq_catalog_external_ref_exact holds one exact row per workno whatever became
// of its release, so a workno anchored on a deleted release can never take a
// second exact ref: minting it would abort the whole write transaction.
func (im *Importer) loadDLExactWorknos() (map[string]struct{}, error) {
	var worknos []string
	if err := im.catalog.Raw(`SELECT external_id FROM catalog_external_ref
		WHERE entity_type = ? AND source_id = ? AND link_kind = ?`,
		model.EntityTypeRelease, dlsiteSource, model.LinkKindExact).Scan(&worknos).Error; err != nil {
		return nil, fmt.Errorf("load dlsite exact worknos: %w", err)
	}
	out := make(map[string]struct{}, len(worknos))
	for _, wn := range worknos {
		out[wn] = struct{}{}
	}
	return out, nil
}

func appendUniqueID(m map[string][]int64, workno string, id int64) {
	for _, existing := range m[workno] {
		if existing == id {
			return
		}
	}
	m[workno] = append(m[workno], id)
}

func (im *Importer) loadDLDeclaredWorks() (map[string][]int64, error) {
	var rows []struct {
		WorkID     int64  `gorm:"column:work_id"`
		InfoboxRaw string `gorm:"column:infobox_raw"`
	}
	if err := im.catalog.Raw(`
		SELECT r.entity_id AS work_id, s.infobox_raw
		FROM catalog_external_ref r
		JOIN catalog_work w ON w.id = r.entity_id AND w.deleted_at IS NULL
			AND w.status IN (?, ?)
		JOIN catalog_medium m ON m.id = w.medium_id AND m.key = 'galgame'
		JOIN src_bangumi.subject s ON s.id::text = r.external_id
		WHERE r.entity_type = ? AND r.source_id = ? AND r.link_kind = ? AND r.dead_at IS NULL`,
		model.WorkStatusLive, model.WorkStatusStub,
		model.EntityTypeWork, bangumiSource, model.LinkKindExact).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load bangumi declared worknos: %w", err)
	}
	out := map[string][]int64{}
	seen := map[string]map[int64]struct{}{}
	for _, r := range rows {
		for _, wn := range dlsitecode.Worknos(r.InfoboxRaw) {
			if seen[wn] == nil {
				seen[wn] = map[int64]struct{}{}
			}
			if _, ok := seen[wn][r.WorkID]; ok {
				continue
			}
			seen[wn][r.WorkID] = struct{}{}
			out[wn] = append(out[wn], r.WorkID)
		}
	}
	return out, nil
}

func (im *Importer) loadDLTitleIndex() (map[string][]int64, error) {
	var names []struct {
		WorkID int64  `gorm:"column:work_id"`
		Name   string `gorm:"column:name"`
	}
	if err := im.catalog.Raw(`
		SELECT w.id AS work_id, w.display_name AS name
		FROM catalog_work w
		JOIN catalog_medium m ON m.id = w.medium_id AND m.key = 'galgame'
		WHERE w.deleted_at IS NULL AND w.status IN (?, ?)
		UNION ALL
		SELECT t.work_id, t.title
		FROM catalog_work_title t
		JOIN catalog_work w ON w.id = t.work_id AND w.deleted_at IS NULL AND w.status IN (?, ?)
		JOIN catalog_medium m ON m.id = w.medium_id AND m.key = 'galgame'
		WHERE t.kind IN (?, ?, ?) AND `+editspec.NotSuppressedWorkTitleSQL("t"),
		model.WorkStatusLive, model.WorkStatusStub,
		model.WorkStatusLive, model.WorkStatusStub,
		model.WorkTitleKindOfficial, model.WorkTitleKindAlias, model.WorkTitleKindAbbreviation,
	).Scan(&names).Error; err != nil {
		return nil, fmt.Errorf("load title corpus: %w", err)
	}
	idx := map[string]map[int64]struct{}{}
	for _, n := range names {
		for _, k := range titlekey.Keys(n.Name) {
			if idx[k] == nil {
				idx[k] = map[int64]struct{}{}
			}
			idx[k][n.WorkID] = struct{}{}
		}
	}
	out := make(map[string][]int64, len(idx))
	for k, works := range idx {
		ids := make([]int64, 0, len(works))
		for id := range works {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		out[k] = ids
	}
	return out, nil
}

func (im *Importer) loadDLCircleIndex() (workLabels map[int64]map[int64]struct{}, makerLabel map[string][]int64, err error) {
	var edges []struct {
		WorkID  int64 `gorm:"column:work_id"`
		LabelID int64 `gorm:"column:label_id"`
	}
	if err = im.catalog.Raw(`SELECT work_id, label_id FROM catalog_work_label`).Scan(&edges).Error; err != nil {
		return nil, nil, fmt.Errorf("load work labels: %w", err)
	}
	workLabels = map[int64]map[int64]struct{}{}
	for _, e := range edges {
		if workLabels[e.WorkID] == nil {
			workLabels[e.WorkID] = map[int64]struct{}{}
		}
		workLabels[e.WorkID][e.LabelID] = struct{}{}
	}
	var anchors []struct {
		MakerID string `gorm:"column:external_id"`
		LabelID int64  `gorm:"column:entity_id"`
	}
	if err = im.catalog.Raw(`
		SELECT external_id, entity_id FROM catalog_external_ref
		WHERE entity_type = ? AND source_id = ? AND link_kind = ?`,
		model.EntityTypeLabel, dlsiteSource, model.LinkKindExact).Scan(&anchors).Error; err != nil {
		return nil, nil, fmt.Errorf("load dlsite label anchors: %w", err)
	}
	makerLabel = map[string][]int64{}
	for _, a := range anchors {
		makerLabel[a.MakerID] = append(makerLabel[a.MakerID], a.LabelID)
	}
	return workLabels, makerLabel, nil
}

func (im *Importer) loadDLWorkDates() (map[int64][]string, error) {
	var rows []struct {
		WorkID int64  `gorm:"column:work_id"`
		YMD    string `gorm:"column:ymd"`
	}
	if err := im.catalog.Raw(`
		SELECT work_id,
		       lpad(released_y::text, 4, '0') || '-' ||
		       lpad(released_m::text, 2, '0') || '-' ||
		       lpad(released_d::text, 2, '0') AS ymd
		FROM catalog_release
		WHERE deleted_at IS NULL
		  AND released_y IS NOT NULL AND released_m IS NOT NULL AND released_d IS NOT NULL`).
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load work dates: %w", err)
	}
	out := map[int64][]string{}
	seen := map[int64]map[string]struct{}{}
	for _, r := range rows {
		if seen[r.WorkID] == nil {
			seen[r.WorkID] = map[string]struct{}{}
		}
		if _, ok := seen[r.WorkID][r.YMD]; ok {
			continue
		}
		seen[r.WorkID][r.YMD] = struct{}{}
		out[r.WorkID] = append(out[r.WorkID], r.YMD)
	}
	return out, nil
}

func (im *Importer) loadDLBgmDates() (map[int64][]string, error) {
	var rows []struct {
		WorkID int64  `gorm:"column:work_id"`
		Date   string `gorm:"column:date"`
	}
	if err := im.catalog.Raw(`
		SELECT r.entity_id AS work_id, s.date
		FROM catalog_external_ref r
		JOIN catalog_work w ON w.id = r.entity_id AND w.deleted_at IS NULL AND w.status IN (?, ?)
		JOIN src_bangumi.subject s ON s.id::text = r.external_id
		WHERE r.entity_type = ? AND r.source_id = ? AND r.link_kind = ? AND r.dead_at IS NULL
		  AND s.date <> ''`,
		model.WorkStatusLive, model.WorkStatusStub,
		model.EntityTypeWork, bangumiSource, model.LinkKindExact).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load bangumi dates: %w", err)
	}
	out := map[int64][]string{}
	seen := map[int64]map[string]struct{}{}
	for _, r := range rows {
		if seen[r.WorkID] == nil {
			seen[r.WorkID] = map[string]struct{}{}
		}
		if _, ok := seen[r.WorkID][r.Date]; ok {
			continue
		}
		seen[r.WorkID][r.Date] = struct{}{}
		out[r.WorkID] = append(out[r.WorkID], r.Date)
	}
	return out, nil
}

func (im *Importer) loadDLRejections() (map[string]struct{}, error) {
	var rows []struct {
		EntityID   int64  `gorm:"column:entity_id"`
		ExternalID string `gorm:"column:external_id"`
	}
	if err := im.catalog.Raw(`
		SELECT entity_id, external_id FROM catalog_match_rejection
		WHERE entity_type = ? AND source_id = ?`,
		model.EntityTypeWork, dlsiteSource).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load rejections: %w", err)
	}
	out := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		out[dlRejKey(r.EntityID, r.ExternalID)] = struct{}{}
	}
	return out, nil
}

func dlRejKey(workID int64, workno string) string {
	return fmt.Sprintf("%d\x00%s", workID, workno)
}
