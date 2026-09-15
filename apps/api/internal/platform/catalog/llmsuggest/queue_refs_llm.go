package llmsuggest

import (
	"context"
	"encoding/json"
	"strconv"
	"sync/atomic"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

type refLLMEvidence struct {
	EntityType string         `json:"entity_type"`
	EntityID   int64          `json:"entity_id"`
	SourceKey  string         `json:"source_key"`
	ExternalID string         `json:"external_id"`
	MatchedBy  string         `json:"matched_by"`
	Catalog    map[string]any `json:"catalog"`
	Source     map[string]any `json:"source"`
}

func refUser(ev refLLMEvidence) string {
	b, _ := json.Marshal(ev)
	return "Decide whether the external record and the catalog entity denote the same identity.\n" + string(b)
}

func runRefLLMLane(ctx context.Context, db, eg *gorm.DB, c *Client, reg sourceReg, work []refItem, queue string, opts Options, nJudged, nErrs *atomic.Int64) (judged, errs int, skips map[string]int) {
	evs, skips, err := buildRefLLMEvidence(db, eg, reg, work)
	if err != nil {
		runPool(ctx, work, opts.Concurrency, func(_ context.Context, it refItem) {
			row := llmRefRow(it, queue, opts.Model)
			row.Error = truncate(err.Error(), 500)
			nErrs.Add(1)
			persistQueueVerdict(db, &row)
		})
		return 0, len(work), skips
	}
	var runnable []refItem
	for _, it := range work {
		if _, ok := evs[it.Hash]; ok {
			runnable = append(runnable, it)
		}
	}
	startJ, startE := nJudged.Load(), nErrs.Load()
	runPool(ctx, runnable, opts.Concurrency, func(ctx context.Context, it refItem) {
		ev := evs[it.Hash]
		row := llmRefRow(it, queue, opts.Model)
		row.Evidence = evidenceJSON(ev)
		v, jerr := judge(ctx, c, refSystem, refUser(ev), 512)
		if jerr != nil {
			row.Error = truncate(jerr.Error(), 500)
			nErrs.Add(1)
		} else {
			row.Verdict, row.Reason, row.Confidence = v.Verdict, v.Reason, v.Confidence
			nJudged.Add(1)
		}
		persistQueueVerdict(db, &row)
	})
	return int(nJudged.Load() - startJ), int(nErrs.Load() - startE), skips
}

func llmRefRow(it refItem, queue, model string) QueueVerdict {
	return QueueVerdict{
		Queue: queue, Lane: LaneLLM,
		EntityType: it.EntityType, EntityID: it.EntityID, SourceID: it.SourceID, ExternalID: it.ExternalID,
		InputHash: it.Hash, Model: model, PromptVersion: PromptRef,
	}
}

func buildRefLLMEvidence(db, eg *gorm.DB, reg sourceReg, items []refItem) (map[string]refLLMEvidence, map[string]int, error) {
	out := map[string]refLLMEvidence{}
	skips := map[string]int{}
	if len(items) == 0 {
		return out, skips, nil
	}
	byET := map[int16][]refItem{}
	for _, it := range items {
		byET[it.EntityType] = append(byET[it.EntityType], it)
	}
	catalog := map[string]map[string]any{}
	if err := loadCatalogEntities(db, byET, catalog); err != nil {
		return nil, skips, err
	}
	sourceRecs, srcSkip, err := loadSourceRecords(db, eg, reg, items)
	if err != nil {
		return nil, skips, err
	}
	for k, v := range srcSkip {
		skips[k] += v
	}
	if err := attachRefContext(db, eg, reg, items, catalog, sourceRecs); err != nil {
		return nil, skips, err
	}
	for _, it := range items {
		cat := catalog[entityKey(it.EntityType, it.EntityID)]
		src := sourceRecs[it.Hash]
		if cat == nil {
			skips["skipped_catalog_missing"]++
			continue
		}
		if src == nil {
			continue
		}
		out[it.Hash] = refLLMEvidence{
			EntityType: entityTypeName(it.EntityType), EntityID: it.EntityID,
			SourceKey: reg.key(it.SourceID), ExternalID: it.ExternalID, MatchedBy: it.MatchedBy,
			Catalog: cat, Source: src,
		}
	}
	return out, skips, nil
}

func entityKey(et int16, id int64) string {
	return strconv.FormatInt(int64(et), 10) + ":" + strconv.FormatInt(id, 10)
}

func loadCatalogEntities(db *gorm.DB, byET map[int16][]refItem, catalog map[string]map[string]any) error {
	ids := func(et int16) []int64 {
		seen := map[int64]struct{}{}
		var out []int64
		for _, it := range byET[et] {
			if _, ok := seen[it.EntityID]; ok {
				continue
			}
			seen[it.EntityID] = struct{}{}
			out = append(out, it.EntityID)
		}
		return out
	}
	if ws := ids(model.EntityTypeWork); len(ws) > 0 {
		sides, err := loadWorkSides(db, ws)
		if err != nil {
			return err
		}
		for id, s := range sides {
			catalog[entityKey(model.EntityTypeWork, id)] = map[string]any{
				"id": id, "display_name": s.DisplayName, "titles": s.Titles, "olang": s.OLang,
				"min_release_year": s.MinReleaseYear, "labels": s.Labels,
			}
		}
	}
	type named struct {
		ID    int64  `gorm:"column:id"`
		Name  string `gorm:"column:name"`
		Latin string `gorm:"column:latin"`
	}
	loadNamed := func(et int16, sql string) error {
		list := ids(et)
		for _, chunk := range chunkBy(list, 500) {
			if len(chunk) == 0 {
				continue
			}
			var rows []named
			if err := db.Raw(sql, chunk).Scan(&rows).Error; err != nil {
				return err
			}
			for _, r := range rows {
				catalog[entityKey(et, r.ID)] = map[string]any{"id": r.ID, "name": r.Name, "latin": r.Latin}
			}
		}
		return nil
	}
	if err := loadNamed(model.EntityTypeLabel,
		`SELECT id, display_name AS name, coalesce(latin,'') AS latin FROM catalog_label WHERE id IN ?`); err != nil {
		return err
	}
	if err := loadNamed(model.EntityTypeCharacter,
		`SELECT id, display_name AS name, coalesce(latin,'') AS latin FROM catalog_character WHERE id IN ?`); err != nil {
		return err
	}
	if err := loadNamed(model.EntityTypeCreditName,
		`SELECT id, name, coalesce(latin,'') AS latin FROM catalog_credit_name WHERE id IN ?`); err != nil {
		return err
	}
	return nil
}

func loadSourceRecords(db, eg *gorm.DB, reg sourceReg, items []refItem) (map[string]map[string]any, map[string]int, error) {
	out := map[string]map[string]any{}
	skips := map[string]int{}
	vndb, bgm, egSrc, dlsite := reg.id(sourceKeyVNDB), reg.id(sourceKeyBangumi), reg.id(sourceKeyEG), reg.id(sourceKeyDLsite)

	var bgmWorks, bgmPeople, bgmChars []refItem
	var vndbVN, vndbProd, vndbChars, vndbStaff []refItem
	var egGames, egBrands, egChars, egCreaters []refItem
	for _, it := range items {
		switch {
		case it.SourceID == dlsite:
			skips["skipped_dlsite_unreachable"]++
		case it.EntityType == model.EntityTypeWork && it.SourceID == bgm:
			bgmWorks = append(bgmWorks, it)
		case it.EntityType == model.EntityTypeLabel && it.SourceID == bgm:
			bgmPeople = append(bgmPeople, it)
		case it.EntityType == model.EntityTypeCharacter && it.SourceID == bgm:
			bgmChars = append(bgmChars, it)
		case it.EntityType == model.EntityTypeCreditName && it.SourceID == bgm:
			bgmPeople = append(bgmPeople, it)
		case it.EntityType == model.EntityTypeWork && it.SourceID == vndb:
			vndbVN = append(vndbVN, it)
		case it.EntityType == model.EntityTypeLabel && it.SourceID == vndb:
			vndbProd = append(vndbProd, it)
		case it.EntityType == model.EntityTypeCharacter && it.SourceID == vndb:
			vndbChars = append(vndbChars, it)
		case it.EntityType == model.EntityTypeCreditName && it.SourceID == vndb:
			vndbStaff = append(vndbStaff, it)
		case it.SourceID == egSrc:
			if eg == nil {
				skips["skipped_eg_unavailable"]++
				continue
			}
			switch it.EntityType {
			case model.EntityTypeWork:
				egGames = append(egGames, it)
			case model.EntityTypeLabel:
				egBrands = append(egBrands, it)
			case model.EntityTypeCharacter:
				egChars = append(egChars, it)
			case model.EntityTypeCreditName:
				egCreaters = append(egCreaters, it)
			default:
				skips["skipped_staging_unknown"]++
			}
		default:
			skips["skipped_staging_unknown"]++
		}
	}

	if err := loadBgmSubjects(db, bgmWorks, out); err != nil {
		return nil, skips, err
	}
	if err := loadBgmPeople(db, bgmPeople, out); err != nil {
		return nil, skips, err
	}
	if err := loadBgmCharacters(db, bgmChars, out); err != nil {
		return nil, skips, err
	}
	if err := loadVNDBVNs(db, vndbVN, out); err != nil {
		return nil, skips, err
	}
	if err := loadVNDBProducers(db, vndbProd, out); err != nil {
		return nil, skips, err
	}
	if err := loadVNDBChars(db, vndbChars, out); err != nil {
		return nil, skips, err
	}
	if err := loadVNDBStaffAliases(db, vndbStaff, out); err != nil {
		return nil, skips, err
	}
	if err := loadEGGames(eg, egGames, out); err != nil {
		return nil, skips, err
	}
	if err := loadEGBrands(eg, egBrands, out); err != nil {
		return nil, skips, err
	}
	if err := loadEGNamed(eg, `SELECT id, raw->>'name' AS name FROM characters WHERE id IN ?`, egChars, out); err != nil {
		return nil, skips, err
	}
	if err := loadEGNamed(eg, `SELECT id, raw->>'name' AS name FROM creaters WHERE id IN ?`, egCreaters, out); err != nil {
		return nil, skips, err
	}
	for _, it := range items {
		if _, ok := out[it.Hash]; !ok {
			if it.SourceID == dlsite {
				continue
			}
			if alreadySkipped(it, egSrc, dlsite, eg) {
				continue
			}
			skips["skipped_source_missing"]++
		}
	}
	return out, skips, nil
}

func alreadySkipped(it refItem, egSrc, dlsite int16, eg *gorm.DB) bool {
	if it.SourceID == dlsite {
		return true
	}
	if it.SourceID == egSrc && eg == nil {
		return true
	}
	return false
}

func extIDs(items []refItem) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, it := range items {
		if _, ok := seen[it.ExternalID]; ok {
			continue
		}
		seen[it.ExternalID] = struct{}{}
		out = append(out, it.ExternalID)
	}
	return out
}

func indexByExt(items []refItem) map[string][]refItem {
	m := map[string][]refItem{}
	for _, it := range items {
		m[it.ExternalID] = append(m[it.ExternalID], it)
	}
	return m
}

func loadBgmSubjects(db *gorm.DB, items []refItem, out map[string]map[string]any) error {
	for _, chunk := range chunkBy(extIDs(items), 500) {
		if len(chunk) == 0 {
			continue
		}
		var rows []struct {
			ID     int64  `gorm:"column:id"`
			Name   string `gorm:"column:name"`
			NameCN string `gorm:"column:name_cn"`
			Date   string `gorm:"column:date"`
		}
		if err := db.Raw(`SELECT id, name, name_cn, date FROM src_bangumi.subject WHERE id::text IN ?`, chunk).Scan(&rows).Error; err != nil {
			return err
		}
		idx := indexByExt(items)
		for _, r := range rows {
			ext := strconv.FormatInt(r.ID, 10)
			for _, it := range idx[ext] {
				out[it.Hash] = map[string]any{"id": r.ID, "name": r.Name, "name_cn": r.NameCN, "date": r.Date}
			}
		}
	}
	return nil
}

func loadBgmPeople(db *gorm.DB, items []refItem, out map[string]map[string]any) error {
	for _, chunk := range chunkBy(extIDs(items), 500) {
		if len(chunk) == 0 {
			continue
		}
		var rows []struct {
			ID   int64  `gorm:"column:id"`
			Name string `gorm:"column:name"`
			Type int    `gorm:"column:type"`
		}
		if err := db.Raw(`SELECT id, name, type FROM src_bangumi.person WHERE id::text IN ?`, chunk).Scan(&rows).Error; err != nil {
			return err
		}
		idx := indexByExt(items)
		for _, r := range rows {
			for _, it := range idx[strconv.FormatInt(r.ID, 10)] {
				out[it.Hash] = map[string]any{"id": r.ID, "name": r.Name, "type": r.Type}
			}
		}
	}
	return nil
}

func loadBgmCharacters(db *gorm.DB, items []refItem, out map[string]map[string]any) error {
	for _, chunk := range chunkBy(extIDs(items), 500) {
		if len(chunk) == 0 {
			continue
		}
		var rows []struct {
			ID   int64  `gorm:"column:id"`
			Name string `gorm:"column:name"`
		}
		if err := db.Raw(`SELECT id, name FROM src_bangumi.character WHERE id::text IN ?`, chunk).Scan(&rows).Error; err != nil {
			return err
		}
		idx := indexByExt(items)
		for _, r := range rows {
			for _, it := range idx[strconv.FormatInt(r.ID, 10)] {
				out[it.Hash] = map[string]any{"id": r.ID, "name": r.Name}
			}
		}
	}
	return nil
}

func loadVNDBVNs(db *gorm.DB, items []refItem, out map[string]map[string]any) error {
	for _, chunk := range chunkBy(extIDs(items), 500) {
		if len(chunk) == 0 {
			continue
		}
		var rows []struct {
			ID    string `gorm:"column:id"`
			Title string `gorm:"column:title"`
			Lang  string `gorm:"column:lang"`
			Latin string `gorm:"column:latin"`
		}
		if err := db.Raw(`SELECT t.id, t.title, t.lang, t.latin FROM src_vndb.vn_titles t WHERE t.id IN ?`, chunk).Scan(&rows).Error; err != nil {
			return err
		}
		idx := indexByExt(items)
		type acc struct {
			ID     string
			Titles []map[string]string
		}
		by := map[string]*acc{}
		for _, r := range rows {
			a := by[r.ID]
			if a == nil {
				a = &acc{ID: r.ID}
				by[r.ID] = a
			}
			a.Titles = append(a.Titles, map[string]string{"lang": r.Lang, "title": r.Title, "latin": r.Latin})
		}
		for id, a := range by {
			for _, it := range idx[id] {
				out[it.Hash] = map[string]any{"id": a.ID, "titles": a.Titles}
			}
		}
	}
	return nil
}

func loadVNDBProducers(db *gorm.DB, items []refItem, out map[string]map[string]any) error {
	for _, chunk := range chunkBy(extIDs(items), 500) {
		if len(chunk) == 0 {
			continue
		}
		var rows []struct {
			ID    string `gorm:"column:id"`
			Name  string `gorm:"column:name"`
			Latin string `gorm:"column:latin"`
			Alias string `gorm:"column:alias"`
			Type  string `gorm:"column:type"`
		}
		if err := db.Raw(`SELECT id, name, latin, alias, type FROM src_vndb.producers WHERE id IN ?`, chunk).Scan(&rows).Error; err != nil {
			return err
		}
		idx := indexByExt(items)
		for _, r := range rows {
			for _, it := range idx[r.ID] {
				out[it.Hash] = map[string]any{"id": r.ID, "name": r.Name, "latin": r.Latin, "alias": r.Alias, "type": r.Type}
			}
		}
	}
	return nil
}

func loadVNDBChars(db *gorm.DB, items []refItem, out map[string]map[string]any) error {
	for _, chunk := range chunkBy(extIDs(items), 500) {
		if len(chunk) == 0 {
			continue
		}
		var rows []struct {
			ID    string `gorm:"column:id"`
			Lang  string `gorm:"column:lang"`
			Name  string `gorm:"column:name"`
			Latin string `gorm:"column:latin"`
		}
		if err := db.Raw(`SELECT id, lang, name, latin FROM src_vndb.chars_names WHERE id IN ?`, chunk).Scan(&rows).Error; err != nil {
			return err
		}
		idx := indexByExt(items)
		type acc struct {
			Names []map[string]string
		}
		by := map[string]*acc{}
		for _, r := range rows {
			a := by[r.ID]
			if a == nil {
				a = &acc{}
				by[r.ID] = a
			}
			a.Names = append(a.Names, map[string]string{"lang": r.Lang, "name": r.Name, "latin": r.Latin})
		}
		for id, a := range by {
			for _, it := range idx[id] {
				out[it.Hash] = map[string]any{"id": id, "names": a.Names}
			}
		}
	}
	return nil
}

func loadVNDBStaffAliases(db *gorm.DB, items []refItem, out map[string]map[string]any) error {
	var aids []int
	idx := map[int][]refItem{}
	for _, it := range items {
		n, err := strconv.Atoi(it.ExternalID)
		if err != nil {
			continue
		}
		if _, ok := idx[n]; !ok {
			aids = append(aids, n)
		}
		idx[n] = append(idx[n], it)
	}
	for _, chunk := range chunkBy(aids, 500) {
		if len(chunk) == 0 {
			continue
		}
		var rows []struct {
			AID   int    `gorm:"column:aid"`
			ID    string `gorm:"column:id"`
			Name  string `gorm:"column:name"`
			Latin string `gorm:"column:latin"`
		}
		if err := db.Raw(`SELECT aid, id, name, latin FROM src_vndb.staff_alias WHERE aid IN ?`, chunk).Scan(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			for _, it := range idx[r.AID] {
				out[it.Hash] = map[string]any{"aid": r.AID, "staff_id": r.ID, "name": r.Name, "latin": r.Latin}
			}
		}
	}
	return nil
}

func loadEGGames(eg *gorm.DB, items []refItem, out map[string]map[string]any) error {
	if eg == nil || len(items) == 0 {
		return nil
	}
	var ids []int64
	idx := map[int64][]refItem{}
	for _, it := range items {
		n, err := strconv.ParseInt(it.ExternalID, 10, 64)
		if err != nil {
			continue
		}
		if _, ok := idx[n]; !ok {
			ids = append(ids, n)
		}
		idx[n] = append(idx[n], it)
	}
	for _, chunk := range chunkBy(ids, 500) {
		if len(chunk) == 0 {
			continue
		}
		var rows []struct {
			ID       int64  `gorm:"column:id"`
			Gamename string `gorm:"column:gamename"`
			VNDB     string `gorm:"column:vndb"`
		}
		if err := eg.Raw(`SELECT id, coalesce(gamename,'') AS gamename, coalesce(vndb::text,'') AS vndb FROM games WHERE id IN ?`, chunk).
			Scan(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			for _, it := range idx[r.ID] {
				out[it.Hash] = map[string]any{"id": r.ID, "gamename": r.Gamename, "vndb": r.VNDB}
			}
		}
	}
	return nil
}

func loadEGBrands(eg *gorm.DB, items []refItem, out map[string]map[string]any) error {
	if eg == nil || len(items) == 0 {
		return nil
	}
	for _, chunk := range chunkBy(extIDs(items), 500) {
		if len(chunk) == 0 {
			continue
		}
		var rows []struct {
			ID        string `gorm:"column:id"`
			Brandname string `gorm:"column:brandname"`
			Makername string `gorm:"column:makername"`
		}
		if err := eg.Raw(`SELECT id::text AS id, coalesce(raw->>'brandname','') AS brandname,
			coalesce(raw->>'makername','') AS makername FROM brands WHERE id::text IN ?`, chunk).Scan(&rows).Error; err != nil {
			return err
		}
		idx := indexByExt(items)
		for _, r := range rows {
			for _, it := range idx[r.ID] {
				out[it.Hash] = map[string]any{"id": r.ID, "brandname": r.Brandname, "makername": r.Makername}
			}
		}
	}
	return nil
}

func loadEGNamed(eg *gorm.DB, q string, items []refItem, out map[string]map[string]any) error {
	if eg == nil || len(items) == 0 {
		return nil
	}
	var ids []int64
	idx := map[int64][]refItem{}
	for _, it := range items {
		n, err := strconv.ParseInt(it.ExternalID, 10, 64)
		if err != nil {
			continue
		}
		if _, ok := idx[n]; !ok {
			ids = append(ids, n)
		}
		idx[n] = append(idx[n], it)
	}
	for _, chunk := range chunkBy(ids, 500) {
		if len(chunk) == 0 {
			continue
		}
		var rows []struct {
			ID   int64  `gorm:"column:id"`
			Name string `gorm:"column:name"`
		}
		if err := eg.Raw(q, chunk).Scan(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			for _, it := range idx[r.ID] {
				out[it.Hash] = map[string]any{"id": r.ID, "name": r.Name}
			}
		}
	}
	return nil
}
