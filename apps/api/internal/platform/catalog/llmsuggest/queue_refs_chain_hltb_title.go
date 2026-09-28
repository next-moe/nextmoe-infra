package llmsuggest

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/titlekey"

	"gorm.io/gorm"
)

// Measured 2026-09-28 over the 2,169 probable rule:hltb-title+date work refs:
// 767 share an exclusive Steam appid with the HLTB record, 1,048 agree only on
// a developer or publisher name, 41 name a Steam appid the work's anchors
// contradict, and ~313 have neither. Those 41 are mostly the same title on a
// different edition -- HLTB "Togainu no Chi - Lost Blood" (the 2020 remaster)
// attached to the 2005 original -- so a Steam conflict vetoes and the developer
// check must not override it.
func verifyHLTBTitleDateChain(db *gorm.DB, up StagingDBs, reg sourceReg, items []refItem, out map[string]chainResult) error {
	if len(items) == 0 {
		return nil
	}
	pending := make([]refItem, 0, len(items))
	for _, it := range items {
		if it.EntityType != model.EntityTypeWork {
			out[it.Hash] = unproven("entity_type", fmt.Sprintf("entity_type %d is not a work", it.EntityType))
			continue
		}
		pending = append(pending, it)
	}
	if len(pending) == 0 {
		return nil
	}
	if up.HLTB == nil {
		for _, it := range pending {
			out[it.Hash] = unproven("hltb_mirror", "the howlongtobeat mirror is not reachable")
		}
		return nil
	}
	steamID, ok := reg.idByKey["steam"]
	if !ok {
		return fmt.Errorf("source registry has no steam entry")
	}

	parsed := make([]int64, len(pending))
	valid := make([]bool, len(pending))
	var hltbIDs []int64
	for i, it := range pending {
		n, good := positiveHLTBID(it.ExternalID)
		parsed[i], valid[i] = n, good
		if good {
			hltbIDs = append(hltbIDs, n)
		}
	}
	records, err := loadHLTBTitleDateRows(up.HLTB, dedupeIDs(hltbIDs))
	if err != nil {
		return err
	}

	type ready struct {
		it  refItem
		rec hltbTitleDateRow
	}
	var got []ready
	var workIDs []int64
	var appids []string
	for i, it := range pending {
		if !valid[i] {
			out[it.Hash] = unproven("hltb_record", fmt.Sprintf("external id %q is not a positive integer", it.ExternalID))
			continue
		}
		rec, found := records[parsed[i]]
		if !found {
			out[it.Hash] = unproven("hltb_record", fmt.Sprintf("hltb %s has no fetched mirror row", it.ExternalID))
			continue
		}
		got = append(got, ready{it: it, rec: rec})
		workIDs = append(workIDs, it.EntityID)
		if steamAppidPresent(rec.Appid) {
			appids = append(appids, rec.Appid)
		}
	}
	if len(got) == 0 {
		return nil
	}
	workIDs = dedupeIDs(workIDs)
	anchors, err := loadWorkSteamAnchors(db, steamID, workIDs)
	if err != nil {
		return err
	}
	holders, err := loadLiveSteamHolders(db, steamID, dedupeStrings(appids))
	if err != nil {
		return err
	}
	labels, err := loadWorkLabelSides(db, workIDs)
	if err != nil {
		return err
	}
	for _, g := range got {
		out[g.it.Hash] = decideHLTBTitleDate(g.it, g.rec, anchors[g.it.EntityID], holders[g.rec.Appid], labels[g.it.EntityID])
	}
	return nil
}

func decideHLTBTitleDate(it refItem, rec hltbTitleDateRow, anchors map[string]struct{}, holders map[int64]struct{}, labels nameSide) chainResult {
	if steamAppidPresent(rec.Appid) {
		_, held := anchors[rec.Appid]
		if len(anchors) > 0 && !held {
			list := sortedKeys(anchors)
			return unproven("steam_conflict", fmt.Sprintf(
				"hltb %s names steam appid %s; work %d holds steam %s",
				it.ExternalID, rec.Appid, it.EntityID, strings.Join(list, ", ")))
		}
		if held && !otherLiveWork(holders, it.EntityID) {
			return verified([]chainStep{
				{Name: "title_date", OK: true, Detail: "the importer matched title and full release date"},
				{Name: "hltb_appid", OK: true, Detail: fmt.Sprintf("hltb %s names steam appid %s", it.ExternalID, rec.Appid)},
				{Name: "work_steam_anchor", OK: true, Detail: fmt.Sprintf(
					"steam appid %s is an exact anchor of work %d and of no other live work", rec.Appid, it.EntityID)},
			})
		}
	}
	return decideHLTBDeveloper(it, rec, labels)
}

func decideHLTBDeveloper(it refItem, rec hltbTitleDateRow, labels nameSide) chainResult {
	hltb := hltbNameSide(rec.Dev, rec.Pub)
	switch {
	case len(hltb.byKey) == 0:
		return unproven("hltb_developer", "hltb record names no usable developer or publisher")
	case len(labels.byKey) == 0:
		return unproven("work_labels", fmt.Sprintf("work %d has no usable label name", it.EntityID))
	}
	match := ""
	for k := range hltb.byKey {
		if _, ok := labels.byKey[k]; ok && (match == "" || k < match) {
			match = k
		}
	}
	if match == "" {
		return unproven("developer_mismatch", fmt.Sprintf(
			"hltb names %s; label names %s", truncatedNames(hltb.names, 5), truncatedNames(labels.names, 5)))
	}
	h, l := hltb.byKey[match], labels.byKey[match]
	return verified([]chainStep{
		{Name: "title_date", OK: true, Detail: "the importer matched title and full release date"},
		{Name: "developer", OK: true, Detail: fmt.Sprintf("hltb %q matches label %d %q", h.name, l.labelID, l.name)},
	})
}

type hltbTitleDateRow struct {
	HltbID int64  `gorm:"column:hltb_id"`
	Appid  string `gorm:"column:appid"`
	Dev    string `gorm:"column:dev"`
	Pub    string `gorm:"column:pub"`
}

type labelHit struct {
	labelID int64
	name    string
}

type nameSide struct {
	byKey map[string]labelHit
	names []string
	seen  map[string]struct{}
}

func positiveHLTBID(s string) (int64, bool) {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

func steamAppidPresent(appid string) bool {
	return appid != "" && appid != "0"
}

func otherLiveWork(holders map[int64]struct{}, self int64) bool {
	for id := range holders {
		if id != self {
			return true
		}
	}
	return false
}

func hltbNameSide(dev, pub string) nameSide {
	s := blankSide()
	for _, field := range []string{dev, pub} {
		for _, part := range strings.Split(field, ",") {
			addUsable(s, strings.TrimSpace(part), 0)
		}
	}
	return *s
}

func blankSide() *nameSide {
	return &nameSide{byKey: map[string]labelHit{}, seen: map[string]struct{}{}}
}

func addUsable(s *nameSide, name string, labelID int64) {
	if strings.TrimSpace(name) == "" {
		return
	}
	k := titlekey.Loose(name)
	if utf8.RuneCountInString(k) < 3 {
		return
	}
	if _, ok := s.byKey[k]; !ok {
		s.byKey[k] = labelHit{labelID: labelID, name: name}
	}
	if _, ok := s.seen[name]; ok {
		return
	}
	s.seen[name] = struct{}{}
	s.names = append(s.names, name)
}

func truncatedNames(names []string, n int) string {
	cp := append([]string(nil), names...)
	sort.Strings(cp)
	if len(cp) > n {
		cp = cp[:n]
	}
	return strings.Join(cp, ", ")
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func dedupeStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func loadHLTBTitleDateRows(hltb *gorm.DB, ids []int64) (map[int64]hltbTitleDateRow, error) {
	out := map[int64]hltbTitleDateRow{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []hltbTitleDateRow
	err := hltb.Raw(`
		SELECT hltb_id,
			coalesce(raw->'data'->'game'->0->>'profile_steam', '') AS appid,
			coalesce(raw->'data'->'game'->0->>'profile_dev', '') AS dev,
			coalesce(raw->'data'->'game'->0->>'profile_pub', '') AS pub
		FROM games
		WHERE status = 'fetched' AND hltb_id IN ?`, ids).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.HltbID] = r
	}
	return out, nil
}

func loadWorkSteamAnchors(db *gorm.DB, steamSource int16, workIDs []int64) (map[int64]map[string]struct{}, error) {
	out := map[int64]map[string]struct{}{}
	if len(workIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		WorkID int64  `gorm:"column:work_id"`
		Appid  string `gorm:"column:appid"`
	}
	err := db.Raw(`
		SELECT rel.work_id AS work_id, r.external_id AS appid
		FROM catalog_release rel
		JOIN catalog_external_ref r
			ON r.entity_type = ? AND r.entity_id = rel.id
			AND r.source_id = ? AND r.link_kind = ? AND r.dead_at IS NULL
		WHERE rel.deleted_at IS NULL AND rel.work_id IN ?
		UNION
		SELECT r.entity_id AS work_id, r.external_id AS appid
		FROM catalog_external_ref r
		WHERE r.entity_type = ? AND r.source_id = ? AND r.link_kind = ? AND r.dead_at IS NULL
			AND r.entity_id IN ?`,
		model.EntityTypeRelease, steamSource, model.LinkKindExact, workIDs,
		model.EntityTypeWork, steamSource, model.LinkKindExact, workIDs,
	).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if out[r.WorkID] == nil {
			out[r.WorkID] = map[string]struct{}{}
		}
		out[r.WorkID][r.Appid] = struct{}{}
	}
	return out, nil
}

func loadLiveSteamHolders(db *gorm.DB, steamSource int16, appids []string) (map[string]map[int64]struct{}, error) {
	out := map[string]map[int64]struct{}{}
	if len(appids) == 0 {
		return out, nil
	}
	var rows []struct {
		WorkID int64  `gorm:"column:work_id"`
		Appid  string `gorm:"column:appid"`
	}
	err := db.Raw(`
		SELECT w.id AS work_id, r.external_id AS appid
		FROM catalog_work w
		JOIN catalog_release rel ON rel.work_id = w.id AND rel.deleted_at IS NULL
		JOIN catalog_external_ref r
			ON r.entity_type = ? AND r.entity_id = rel.id
			AND r.source_id = ? AND r.link_kind = ? AND r.dead_at IS NULL
		WHERE w.deleted_at IS NULL AND r.external_id IN ?
		UNION
		SELECT w.id AS work_id, r.external_id AS appid
		FROM catalog_work w
		JOIN catalog_external_ref r
			ON r.entity_type = ? AND r.entity_id = w.id
			AND r.source_id = ? AND r.link_kind = ? AND r.dead_at IS NULL
		WHERE w.deleted_at IS NULL AND r.external_id IN ?`,
		model.EntityTypeRelease, steamSource, model.LinkKindExact, appids,
		model.EntityTypeWork, steamSource, model.LinkKindExact, appids,
	).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if out[r.Appid] == nil {
			out[r.Appid] = map[int64]struct{}{}
		}
		out[r.Appid][r.WorkID] = struct{}{}
	}
	return out, nil
}

func loadWorkLabelSides(db *gorm.DB, workIDs []int64) (map[int64]nameSide, error) {
	out := map[int64]nameSide{}
	if len(workIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		WorkID      int64  `gorm:"column:work_id"`
		LabelID     int64  `gorm:"column:label_id"`
		DisplayName string `gorm:"column:display_name"`
		Latin       string `gorm:"column:latin"`
		AliasName   string `gorm:"column:alias_name"`
		AliasLatin  string `gorm:"column:alias_latin"`
	}
	err := db.Raw(`
		SELECT wl.work_id, l.id AS label_id, l.display_name,
			coalesce(l.latin, '') AS latin,
			coalesce(a.name, '') AS alias_name,
			coalesce(a.latin, '') AS alias_latin
		FROM catalog_work_label wl
		JOIN catalog_label l ON l.id = wl.label_id AND l.deleted_at IS NULL
		LEFT JOIN catalog_label_alias a ON a.label_id = l.id
		WHERE wl.work_id IN ?
		UNION ALL
		SELECT rel.work_id, l.id, l.display_name,
			coalesce(l.latin, ''), coalesce(a.name, ''), coalesce(a.latin, '')
		FROM catalog_release rel
		JOIN catalog_release_label rl ON rl.release_id = rel.id
		JOIN catalog_label l ON l.id = rl.label_id AND l.deleted_at IS NULL
		LEFT JOIN catalog_label_alias a ON a.label_id = l.id
		WHERE rel.deleted_at IS NULL AND rel.work_id IN ?`,
		workIDs, workIDs).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	built := map[int64]*nameSide{}
	for _, r := range rows {
		s := built[r.WorkID]
		if s == nil {
			s = blankSide()
			built[r.WorkID] = s
		}
		addUsable(s, r.DisplayName, r.LabelID)
		addUsable(s, r.Latin, r.LabelID)
		addUsable(s, r.AliasName, r.LabelID)
		addUsable(s, r.AliasLatin, r.LabelID)
	}
	for id, s := range built {
		out[id] = *s
	}
	return out, nil
}
