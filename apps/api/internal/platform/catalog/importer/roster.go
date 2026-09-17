package importer

import (
	"fmt"
	"strconv"
	"strings"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

const (
	ruleRosterBangumi = "import:character-roster-bangumi"
	ruleRosterEG      = "import:character-roster-eg"
	ruleRosterVNDB    = "import:character-roster-vndb"
)

type RosterStats struct {
	CharactersCreated        int
	EdgesWritten             int
	Already                  int
	SkippedNoWorkAnchor      int
	SkippedNoName            int
	SkippedForeignRoster     int
	SkippedQuarantined       int
	Errors                   int
	SkippedClaimedProbable   int
	SkippedRetiredExactSquat int
	AttachedExisting         int
	AliasesCreated           int
	PortraitCandidates       int
}

func (s *RosterStats) add(o RosterStats) {
	s.CharactersCreated += o.CharactersCreated
	s.EdgesWritten += o.EdgesWritten
	s.Already += o.Already
	s.SkippedNoWorkAnchor += o.SkippedNoWorkAnchor
	s.SkippedNoName += o.SkippedNoName
	s.SkippedForeignRoster += o.SkippedForeignRoster
	s.SkippedQuarantined += o.SkippedQuarantined
	s.SkippedClaimedProbable += o.SkippedClaimedProbable
	s.SkippedRetiredExactSquat += o.SkippedRetiredExactSquat
	s.Errors += o.Errors
	s.AttachedExisting += o.AttachedExisting
	s.AliasesCreated += o.AliasesCreated
	s.PortraitCandidates += o.PortraitCandidates
}

func (im *Importer) RunRoster(source string) (RosterStats, error) {
	var total RosterStats
	if source == "bangumi" || source == "all" {
		s, err := im.runRosterBangumi()
		if err != nil {
			return total, fmt.Errorf("bangumi roster wave: %w", err)
		}
		total.add(s)
	}
	if source == "eg" || source == "all" {
		if im.eg == nil {
			return total, fmt.Errorf("eg roster wave requested but no erogamescape connection")
		}
		s, err := im.runRosterEG()
		if err != nil {
			return total, fmt.Errorf("eg roster wave: %w", err)
		}
		total.add(s)
	}
	if source == "vndb" || source == "all" {
		s, err := im.runRosterVNDB()
		if err != nil {
			return total, fmt.Errorf("vndb roster wave: %w", err)
		}
		total.add(s)
	}
	return total, nil
}

type rosterPlan struct {
	workID    int64
	charExtID string
	kind      int16
	spoiler   int16
}

func (im *Importer) loadExactWorkMap(source int16) (map[int64]int64, error) {
	var rows []struct {
		ExternalID int64 `gorm:"column:external_id"`
		WorkID     int64 `gorm:"column:work_id"`
	}
	if err := im.catalog.Raw(`
		SELECT external_id::bigint AS external_id, entity_id AS work_id
		FROM catalog_external_ref
		WHERE entity_type = ? AND source_id = ? AND link_kind = ?`,
		model.EntityTypeWork, source, model.LinkKindExact).Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[int64]int64, len(rows))
	for _, r := range rows {
		m[r.ExternalID] = r.WorkID
	}
	return m, nil
}

// A Bangumi or EG character stays its own entity until catalog-char-xsrc folds
// it into the cast a work already has, and nothing schedules that fold. Casting
// one beside another source's roster therefore lists the character twice: the
// 2026-09-16 backlog drain put 11,375 same-name twins on 2,358 works through
// the Bangumi lane and 22 through the EG lane. These two lanes only cast a work
// no other importer or editor has cast; the VNDB lane is not gated. A
// quarantined work is skipped for the same reason: it exists to be merged into
// a live work that may already have a cast, and the merge would bring its cast
// along.
func (im *Importer) quarantinedWorks() (map[int64]bool, error) {
	var ids []int64
	if err := im.catalog.Raw(`SELECT id FROM catalog_work WHERE status = ? AND deleted_at IS NULL`,
		model.WorkStatusQuarantine).Scan(&ids).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}

func (im *Importer) worksWithForeignRoster(own string) (map[int64]bool, error) {
	var ids []int64
	if err := im.catalog.Raw(`SELECT DISTINCT work_id FROM catalog_work_character WHERE matched_by <> ?`, own).
		Scan(&ids).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}

func (im *Importer) runRosterBangumi() (RosterStats, error) {
	var st RosterStats
	workMap, err := im.loadExactWorkMap(bangumiSource)
	if err != nil {
		return st, err
	}
	if im.limit > 0 {
		workMap = capMap(workMap, im.limit)
	}
	charAnchor, err := im.loadAnchors(model.EntityTypeCharacter)
	if err != nil {
		return st, err
	}
	foreign, err := im.worksWithForeignRoster(ruleRosterBangumi)
	if err != nil {
		return st, err
	}
	quarantined, err := im.quarantinedWorks()
	if err != nil {
		return st, err
	}

	type scRow struct {
		CharacterID int64   `gorm:"column:character_id"`
		SubjectID   int64   `gorm:"column:subject_id"`
		Type        int     `gorm:"column:type"`
		Name        *string `gorm:"column:name"`
	}
	var scs []scRow
	if err := im.catalog.Raw(`
		SELECT sc.character_id, sc.subject_id, sc.type, c.name
		FROM src_bangumi.subject_character sc
		JOIN catalog_external_ref r ON r.entity_type = ? AND r.source_id = ? AND r.link_kind = ? AND r.external_id = sc.subject_id::text
		LEFT JOIN src_bangumi.character c ON c.id = sc.character_id`,
		model.EntityTypeWork, bangumiSource, model.LinkKindExact,
	).Scan(&scs).Error; err != nil {
		return st, err
	}

	var newChars []charItem
	seenChar := map[int64]bool{}
	var plans []rosterPlan
	for _, sc := range scs {
		workID, ok := workMap[sc.SubjectID]
		if !ok {
			st.SkippedNoWorkAnchor++
			continue
		}
		if quarantined[workID] {
			st.SkippedQuarantined++
			continue
		}
		if foreign[workID] {
			st.SkippedForeignRoster++
			continue
		}
		if sc.Name == nil || strings.TrimSpace(*sc.Name) == "" {
			st.SkippedNoName++
			continue
		}
		name := *sc.Name
		ext := strconv.FormatInt(sc.CharacterID, 10)
		if !seenChar[sc.CharacterID] {
			seenChar[sc.CharacterID] = true
			if charAnchor[anchorKey(bangumiSource, ext)] == 0 {
				newChars = append(newChars, charItem{extID: ext, name: name, lang: "ja"})
			}
		}
		plans = append(plans, rosterPlan{workID: workID, charExtID: ext, kind: bangumiRosterKind(sc.Type)})
	}

	st.CharactersCreated = len(newChars)
	if im.dryRun {
		st.EdgesWritten = len(plans)
		return st, nil
	}

	err = im.catalog.Transaction(func(tx *gorm.DB) error {
		charIDs, err := im.createCharacters(tx, bangumiSource, ruleBangumiChar, newChars)
		if err != nil {
			return err
		}
		charResolve := resolver(charAnchor, bangumiSource, charIDs)
		edges, dropped := materializeRoster(plans, charResolve, ruleRosterBangumi)
		st.Errors += dropped
		written, err := insertRosterEdges(tx, edges)
		if err != nil {
			return err
		}
		st.EdgesWritten = written
		st.Already = len(edges) - written
		return nil
	})
	return st, err
}

func (im *Importer) runRosterEG() (RosterStats, error) {
	var st RosterStats
	workMap, err := im.loadExactWorkMap(egSource)
	if err != nil {
		return st, err
	}
	if im.limit > 0 {
		workMap = capMap(workMap, im.limit)
	}
	charAnchor, err := im.loadAnchors(model.EntityTypeCharacter)
	if err != nil {
		return st, err
	}
	foreign, err := im.worksWithForeignRoster(ruleRosterEG)
	if err != nil {
		return st, err
	}
	quarantined, err := im.quarantinedWorks()
	if err != nil {
		return st, err
	}

	var apps []struct {
		Game int64 `gorm:"column:game"`
		Char int64 `gorm:"column:character_id"`
	}
	if err := im.eg.Raw(
		`SELECT game, character_id FROM appearances WHERE game IS NOT NULL AND character_id IS NOT NULL`,
	).Scan(&apps).Error; err != nil {
		return st, err
	}
	charName, err := im.egNameMap(`SELECT id, raw->>'name' AS name FROM characters WHERE btrim(coalesce(raw->>'name',''))<>''`)
	if err != nil {
		return st, err
	}

	var newChars []charItem
	seenChar := map[int64]bool{}
	var plans []rosterPlan
	for _, a := range apps {
		workID, ok := workMap[a.Game]
		if !ok {
			st.SkippedNoWorkAnchor++
			continue
		}
		if quarantined[workID] {
			st.SkippedQuarantined++
			continue
		}
		if foreign[workID] {
			st.SkippedForeignRoster++
			continue
		}
		name, ok := charName[a.Char]
		if !ok {
			st.SkippedNoName++
			continue
		}
		ext := strconv.FormatInt(a.Char, 10)
		if !seenChar[a.Char] {
			seenChar[a.Char] = true
			if charAnchor[anchorKey(egSource, ext)] == 0 {
				newChars = append(newChars, charItem{extID: ext, name: name, lang: "ja"})
			}
		}
		plans = append(plans, rosterPlan{workID: workID, charExtID: ext, kind: model.WorkCharacterKindUnknown})
	}

	st.CharactersCreated = len(newChars)
	if im.dryRun {
		st.EdgesWritten = len(plans)
		return st, nil
	}

	err = im.catalog.Transaction(func(tx *gorm.DB) error {
		charIDs, err := im.createCharacters(tx, egSource, ruleEGChar, newChars)
		if err != nil {
			return err
		}
		charResolve := resolver(charAnchor, egSource, charIDs)
		edges, dropped := materializeRoster(plans, charResolve, ruleRosterEG)
		st.Errors += dropped
		written, err := insertRosterEdges(tx, edges)
		if err != nil {
			return err
		}
		st.EdgesWritten = written
		st.Already = len(edges) - written
		return nil
	})
	return st, err
}

func bangumiRosterKind(t int) int16 {
	switch t {
	case 1:
		return model.WorkCharacterKindMain
	case 2:
		return model.WorkCharacterKindSecondary
	case 3:
		return model.WorkCharacterKindAppears
	default:
		return model.WorkCharacterKindUnknown
	}
}

func materializeRoster(plans []rosterPlan, char func(string) (int64, bool), matchedBy string) ([]model.CatalogWorkCharacter, int) {
	out := make([]model.CatalogWorkCharacter, 0, len(plans))
	dropped := 0
	for _, p := range plans {
		chID, ok := char(p.charExtID)
		if !ok {
			dropped++
			continue
		}
		out = append(out, model.CatalogWorkCharacter{
			WorkID: p.workID, CharacterID: chID, Kind: p.kind, Spoiler: p.spoiler, MatchedBy: matchedBy,
		})
	}
	return out, dropped
}

func insertRosterEdges(tx *gorm.DB, edges []model.CatalogWorkCharacter) (int, error) {
	written := 0
	var touched []int64
	const batch = 1000
	for start := 0; start < len(edges); start += batch {
		end := min(start+batch, len(edges))
		var sb strings.Builder
		sb.WriteString(`INSERT INTO catalog_work_character (work_id, character_id, kind, spoiler, matched_by, created_at, updated_at) VALUES `)
		args := make([]any, 0, (end-start)*5)
		for i := start; i < end; i++ {
			e := edges[i]
			if i > start {
				sb.WriteString(",")
			}
			sb.WriteString("(?,?,?,?,?,now(),now())")
			args = append(args, e.WorkID, e.CharacterID, e.Kind, e.Spoiler, e.MatchedBy)
		}
		sb.WriteString(` ON CONFLICT (work_id, character_id) DO NOTHING RETURNING work_id`)
		var hosts []int64
		if err := tx.Raw(sb.String(), args...).Scan(&hosts).Error; err != nil {
			return written, err
		}
		written += len(hosts)
		touched = append(touched, hosts...)
	}
	return written, touchWorks(tx, touched)
}
