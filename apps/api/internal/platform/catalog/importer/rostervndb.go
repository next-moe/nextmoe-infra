package importer

import (
	"sort"
	"strings"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const vndbSource int16 = 2

const (
	ruleVNDBChar         = "rule:vndb-character-import"
	ruleSameWorkCharName = "rule:same-work-character-name"
)

type vndbName struct {
	native string
	romaji string
	lang   string
}

type vndbAliasPlan struct {
	charExtID string
	name      string
	lang      string
}

type anchorItem struct {
	extID    string
	entityID int64
}

type vndbGateRow struct {
	CharID  string `gorm:"column:char_id"`
	WorkID  int64  `gorm:"column:work_id"`
	Kind    int16  `gorm:"column:kind"`
	Spoiler int16  `gorm:"column:spoiler"`
}

func (im *Importer) runRosterVNDB() (RosterStats, error) {
	var st RosterStats

	var gates []vndbGateRow
	if err := im.catalog.Raw(`
		SELECT cv.id AS char_id, r.entity_id AS work_id,
		       min(CASE cv.role WHEN 'main' THEN 1 WHEN 'primary' THEN 1
		                        WHEN 'side' THEN 2 WHEN 'appears' THEN 3 ELSE 9 END) AS kind,
		       min(cv.spoil) AS spoiler
		FROM src_vndb.chars_vns cv
		JOIN catalog_external_ref r ON r.entity_type = ? AND r.source_id = ? AND r.link_kind = ? AND r.external_id = cv.vid
		GROUP BY cv.id, r.entity_id`,
		model.EntityTypeWork, vndbSource, model.LinkKindExact).Scan(&gates).Error; err != nil {
		return st, err
	}
	if im.limit > 0 {
		gates = capGatesByWork(gates, im.limit)
	}

	names, err := im.loadVNDBNames()
	if err != nil {
		return st, err
	}
	anchorV, err := im.loadCharAnchors(vndbSource)
	if err != nil {
		return st, err
	}
	attach, err := im.loadVNDBAttachTargets()
	if err != nil {
		return st, err
	}
	claimed, err := im.loadClaimedCharExtIDs(vndbSource)
	if err != nil {
		return st, err
	}
	for ext := range claimed {
		if anchorV[ext] != 0 {
			delete(claimed, ext)
		}
	}
	retiredExact, err := im.loadRetiredExactCharExtIDs(vndbSource)
	if err != nil {
		return st, err
	}

	var newChars []charItem
	var attachItems []anchorItem
	var aliasPlans []vndbAliasPlan
	knownIDs := map[string]int64{}
	seen := map[string]bool{}
	var plans []rosterPlan

	for _, g := range gates {
		if claimed[g.CharID] != 0 || retiredExact[g.CharID] != 0 {
			if !seen[g.CharID] {
				seen[g.CharID] = true
				if claimed[g.CharID] != 0 {
					st.SkippedClaimedProbable++
				} else {
					st.SkippedRetiredExactSquat++
				}
			}
			continue
		}

		kind := g.Kind
		if kind == 9 {
			kind = model.WorkCharacterKindUnknown
		}
		plans = append(plans, rosterPlan{workID: g.WorkID, charExtID: g.CharID, kind: kind, spoiler: g.Spoiler})

		if seen[g.CharID] {
			continue
		}
		seen[g.CharID] = true
		nm, ok := names[g.CharID]
		if !ok || strings.TrimSpace(nm.native) == "" {
			st.SkippedNoName++
			continue
		}
		if nm.romaji != "" && nm.romaji != nm.native {
			aliasPlans = append(aliasPlans, vndbAliasPlan{charExtID: g.CharID, name: nm.romaji, lang: nm.lang})
		}
		switch {
		case anchorV[g.CharID] != 0:
			knownIDs[g.CharID] = anchorV[g.CharID]
		case attach[g.CharID] != 0:
			knownIDs[g.CharID] = attach[g.CharID]
			attachItems = append(attachItems, anchorItem{extID: g.CharID, entityID: attach[g.CharID]})
			st.AttachedExisting++
		default:
			newChars = append(newChars, charItem{extID: g.CharID, name: nm.native, lang: nm.lang})
		}
	}
	st.CharactersCreated = len(newChars)

	st.PortraitCandidates, err = im.countVNDBPortraitCandidates()
	if err != nil {
		return st, err
	}

	if im.dryRun {
		st.EdgesWritten = len(plans)
		st.AliasesCreated = len(aliasPlans)
		return st, nil
	}

	err = im.catalog.Transaction(func(tx *gorm.DB) error {
		freshIDs, err := im.createCharacters(tx, vndbSource, ruleVNDBChar, newChars)
		if err != nil {
			return err
		}
		if err := im.attachCharAnchors(tx, vndbSource, ruleSameWorkCharName, attachItems); err != nil {
			return err
		}
		resolve := func(ext string) (int64, bool) {
			if id, ok := freshIDs[ext]; ok {
				return id, true
			}
			if id, ok := knownIDs[ext]; ok && id != 0 {
				return id, true
			}
			return 0, false
		}
		edges, dropped := materializeRoster(plans, resolve, ruleRosterVNDB)
		st.Errors += dropped
		written, err := insertRosterEdges(tx, edges)
		if err != nil {
			return err
		}
		st.EdgesWritten = written
		st.Already = len(edges) - written

		aliasesWritten, err := im.insertCharAliases(tx, aliasPlans, resolve)
		if err != nil {
			return err
		}
		st.AliasesCreated = aliasesWritten

		return rebuildVNDBPortraitBackfill(tx)
	})
	return st, err
}

func (im *Importer) loadVNDBNames() (map[string]vndbName, error) {
	var rows []struct {
		ID    string `gorm:"column:id"`
		Lang  string `gorm:"column:lang"`
		Name  string `gorm:"column:name"`
		Latin string `gorm:"column:latin"`
	}
	if err := im.catalog.Raw(`
		SELECT DISTINCT ON (id) id, lang, name, latin
		FROM src_vndb.chars_names
		WHERE btrim(name) <> ''
		ORDER BY id, (lang <> 'ja'), lang`).Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[string]vndbName, len(rows))
	for _, r := range rows {
		m[r.ID] = vndbName{native: r.Name, romaji: strings.TrimSpace(r.Latin), lang: r.Lang}
	}
	return m, nil
}

func (im *Importer) loadCharAnchors(source int16) (map[string]int64, error) {
	return im.loadCharRefsByKind(source, model.LinkKindExact)
}

func (im *Importer) loadClaimedCharExtIDs(source int16) (map[string]int64, error) {
	return im.loadCharRefsByKind(source, model.LinkKindProbable)
}

func (im *Importer) loadRetiredExactCharExtIDs(source int16) (map[string]int64, error) {
	var rows []struct {
		ExternalID string `gorm:"column:external_id"`
		EntityID   int64  `gorm:"column:entity_id"`
	}
	if err := im.catalog.Raw(`
		SELECT r.external_id, min(r.entity_id) AS entity_id
		FROM catalog_external_ref r
		JOIN catalog_character c ON c.id = r.entity_id AND c.deleted_at IS NOT NULL
		WHERE r.entity_type = ? AND r.source_id = ? AND r.link_kind = ?
		GROUP BY r.external_id`,
		model.EntityTypeCharacter, source, model.LinkKindExact).Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[string]int64, len(rows))
	for _, r := range rows {
		m[r.ExternalID] = r.EntityID
	}
	return m, nil
}

func (im *Importer) loadCharRefsByKind(source, linkKind int16) (map[string]int64, error) {
	var rows []struct {
		ExternalID string `gorm:"column:external_id"`
		EntityID   int64  `gorm:"column:entity_id"`
	}
	if err := im.catalog.Raw(`
		SELECT r.external_id, min(r.entity_id) AS entity_id
		FROM catalog_external_ref r
		JOIN catalog_character c ON c.id = r.entity_id AND c.deleted_at IS NULL
		WHERE r.entity_type = ? AND r.source_id = ? AND r.link_kind = ?
		GROUP BY r.external_id`,
		model.EntityTypeCharacter, source, linkKind).Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[string]int64, len(rows))
	for _, r := range rows {
		m[r.ExternalID] = r.EntityID
	}
	return m, nil
}

// loadVNDBAttachTargets deliberately reads catalog_character_alias WITHOUT the
// catalog.character.aliases suppression predicate. Suppressing a row means "do
// not render it", not "this name does not exist": the attach test asks whether
// this vndb character is already in the roster under any spelling, and hiding a
// name from it would mint a second character row for the same person.
func (im *Importer) loadVNDBAttachTargets() (map[string]int64, error) {
	var rows []struct {
		CharID     string `gorm:"column:char_id"`
		ExistingID int64  `gorm:"column:existing_id"`
	}
	if err := im.catalog.Raw(`
		WITH vndb_norms AS (
			SELECT id, lower(normalize(name, NFKC)) AS norm FROM src_vndb.chars_names WHERE btrim(name) <> ''
			UNION
			SELECT id, lower(normalize(latin, NFKC)) AS norm FROM src_vndb.chars_names WHERE btrim(latin) <> ''
		),
		existing AS (
			SELECT wc.work_id, wc.character_id, ch.display_name_norm AS norm
			FROM catalog_work_character wc JOIN catalog_character ch ON ch.id = wc.character_id AND ch.deleted_at IS NULL
			WHERE wc.matched_by <> 'curated'
			UNION
			SELECT wc.work_id, wc.character_id, al.name_norm AS norm
			FROM catalog_work_character wc JOIN catalog_character_alias al ON al.character_id = wc.character_id
			WHERE wc.matched_by <> 'curated'
		)
		SELECT cv.id AS char_id, min(e.character_id) AS existing_id
		FROM src_vndb.chars_vns cv
		JOIN catalog_external_ref r ON r.entity_type = ? AND r.source_id = ? AND r.link_kind = ? AND r.external_id = cv.vid
		JOIN vndb_norms vn ON vn.id = cv.id
		JOIN existing e ON e.work_id = r.entity_id AND e.norm = vn.norm
		GROUP BY cv.id`,
		model.EntityTypeWork, vndbSource, model.LinkKindExact).Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[string]int64, len(rows))
	for _, r := range rows {
		m[r.CharID] = r.ExistingID
	}
	return m, nil
}

func (im *Importer) countVNDBPortraitCandidates() (int, error) {
	var n int64
	if err := im.catalog.Raw(`
		SELECT count(DISTINCT cv.id)
		FROM src_vndb.chars_vns cv
		JOIN catalog_external_ref r ON r.entity_type = ? AND r.source_id = ? AND r.link_kind = ? AND r.external_id = cv.vid
		JOIN src_vndb.chars c ON c.id = cv.id
		JOIN src_vndb.images i ON i.id = c.image
		WHERE btrim(c.image) <> '' AND i.c_sexual_avg <= 100 AND i.c_violence_avg <= 100`,
		model.EntityTypeWork, vndbSource, model.LinkKindExact).Scan(&n).Error; err != nil {
		return 0, err
	}
	return int(n), nil
}

func rebuildVNDBPortraitBackfill(tx *gorm.DB) error {
	if err := tx.Exec(`TRUNCATE src_vndb.portrait_backfill`).Error; err != nil {
		return err
	}
	return tx.Exec(`
		INSERT INTO src_vndb.portrait_backfill (catalog_character_id, vndb_char_id, image_id, sexual_avg, violence_avg)
		SELECT DISTINCT ON (r.entity_id) r.entity_id, c.id, c.image, i.c_sexual_avg, i.c_violence_avg
		FROM src_vndb.chars c
		JOIN catalog_external_ref r ON r.entity_type = ? AND r.source_id = ? AND r.link_kind = ? AND r.external_id = c.id
		JOIN src_vndb.images i ON i.id = c.image
		WHERE btrim(c.image) <> '' AND i.c_sexual_avg <= 100 AND i.c_violence_avg <= 100
		ORDER BY r.entity_id, c.id`,
		model.EntityTypeCharacter, vndbSource, model.LinkKindExact).Error
}

func (im *Importer) attachCharAnchors(tx *gorm.DB, source int16, rule string, items []anchorItem) error {
	if len(items) == 0 {
		return nil
	}
	refs := make([]model.CatalogExternalRef, len(items))
	for i, it := range items {
		refs[i] = selfRef(model.EntityTypeCharacter, it.entityID, source, it.extID, rule)
	}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(refs, 1000).Error
}

func (im *Importer) insertCharAliases(tx *gorm.DB, plans []vndbAliasPlan, resolve func(string) (int64, bool)) (int, error) {
	type row struct {
		charID int64
		name   string
		lang   string
	}
	rows := make([]row, 0, len(plans))
	for _, p := range plans {
		id, ok := resolve(p.charExtID)
		if !ok {
			continue
		}
		rows = append(rows, row{charID: id, name: p.name, lang: p.lang})
	}
	written := 0
	const batch = 1000
	for start := 0; start < len(rows); start += batch {
		end := min(start+batch, len(rows))
		var sb strings.Builder
		sb.WriteString(`INSERT INTO catalog_character_alias (character_id, name, lang, kind, is_primary_for_locale, source_id, provenance) VALUES `)
		args := make([]any, 0, (end-start)*5)
		for i := start; i < end; i++ {
			if i > start {
				sb.WriteString(",")
			}
			sb.WriteString("(?,?,?,?,false,?,0)")
			args = append(args, rows[i].charID, rows[i].name, rows[i].lang,
				model.AliasKindSpellingVariant, vndbSource)
		}
		sb.WriteString(` ON CONFLICT (character_id, name, lang) DO NOTHING`)
		res := tx.Exec(sb.String(), args...)
		if res.Error != nil {
			return written, res.Error
		}
		written += int(res.RowsAffected)
	}
	return written, nil
}

func capGatesByWork(gates []vndbGateRow, n int) []vndbGateRow {
	works := map[int64]bool{}
	for _, g := range gates {
		works[g.WorkID] = true
	}
	keys := make([]int64, 0, len(works))
	for k := range works {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	if n < len(keys) {
		keys = keys[:n]
	}
	keep := make(map[int64]bool, len(keys))
	for _, k := range keys {
		keep[k] = true
	}
	out := gates[:0:0]
	for _, g := range gates {
		if keep[g.WorkID] {
			out = append(out, g)
		}
	}
	return out
}
