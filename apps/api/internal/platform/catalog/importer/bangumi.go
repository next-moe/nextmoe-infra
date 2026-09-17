package importer

import (
	"fmt"
	"strconv"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

const (
	bangumiTypeIndividual = 1
	bangumiTypeCompany    = 2
	bangumiTypeGroup      = 3
)

type creditPlan struct {
	workID    int64
	cnExtID   string
	labelExt  string
	roleID    int64
	charExtID string
	note      string
}

func (im *Importer) runBangumi() (Stats, error) {
	var st Stats
	workMap, err := im.loadExactWorkMap(bangumiSource)
	if err != nil {
		return st, err
	}
	if im.limit > 0 {
		workMap = capMap(workMap, im.limit)
	}
	bids := int64Keys(workMap)

	roleMap, err := im.roleMap(bangumiSource)
	if err != nil {
		return st, err
	}
	cnAnchor, err := im.loadAnchors(model.EntityTypeCreditName)
	if err != nil {
		return st, err
	}
	labelAnchor, err := im.loadAnchors(model.EntityTypeLabel)
	if err != nil {
		return st, err
	}
	charAnchor, err := im.loadAnchors(model.EntityTypeCharacter)
	if err != nil {
		return st, err
	}

	type spRow struct {
		PersonID  int64 `gorm:"column:person_id"`
		SubjectID int64 `gorm:"column:subject_id"`
		Position  int   `gorm:"column:position"`
	}
	var sps []spRow
	if err := chunkBids(bids, func(chunk []int64) error {
		var batch []spRow
		if err := im.catalog.Raw(`SELECT person_id, subject_id, position FROM src_bangumi.subject_person WHERE subject_id IN ?`, chunk).Scan(&batch).Error; err != nil {
			return err
		}
		sps = append(sps, batch...)
		return nil
	}); err != nil {
		return st, err
	}
	type personRow struct {
		ID   int64  `gorm:"column:id"`
		Type int    `gorm:"column:type"`
		Name string `gorm:"column:name"`
	}
	var persons []personRow
	if err := chunkBids(bids, func(chunk []int64) error {
		var batch []personRow
		if err := im.catalog.Raw(`SELECT id, type, name FROM src_bangumi.person WHERE id IN (
			SELECT person_id FROM src_bangumi.subject_person WHERE subject_id IN ?
			UNION SELECT person_id FROM src_bangumi.person_character WHERE subject_id IN ?)`, chunk, chunk).Scan(&batch).Error; err != nil {
			return err
		}
		persons = append(persons, batch...)
		return nil
	}); err != nil {
		return st, err
	}
	personByID := map[int64]personRow{}
	for _, p := range persons {
		personByID[p.ID] = p
	}
	type scRow struct {
		CharacterID int64 `gorm:"column:character_id"`
		SubjectID   int64 `gorm:"column:subject_id"`
		Type        int   `gorm:"column:type"`
	}
	var scs []scRow
	if err := chunkBids(bids, func(chunk []int64) error {
		var batch []scRow
		if err := im.catalog.Raw(`SELECT character_id, subject_id, type FROM src_bangumi.subject_character WHERE subject_id IN ?`, chunk).Scan(&batch).Error; err != nil {
			return err
		}
		scs = append(scs, batch...)
		return nil
	}); err != nil {
		return st, err
	}
	charRoleNote := map[string]string{}
	for _, sc := range scs {
		charRoleNote[fmt.Sprintf("%d|%d", sc.SubjectID, sc.CharacterID)] = charTypeNote(sc.Type)
	}
	type charRow struct {
		ID   int64  `gorm:"column:id"`
		Name string `gorm:"column:name"`
	}
	var chars []charRow
	if err := chunkBids(bids, func(chunk []int64) error {
		var batch []charRow
		if err := im.catalog.Raw(`SELECT id, name FROM src_bangumi.character WHERE id IN (
			SELECT character_id FROM src_bangumi.subject_character WHERE subject_id IN ?
			UNION SELECT character_id FROM src_bangumi.person_character WHERE subject_id IN ?)`, chunk, chunk).Scan(&batch).Error; err != nil {
			return err
		}
		chars = append(chars, batch...)
		return nil
	}); err != nil {
		return st, err
	}
	type pcRow struct {
		PersonID    int64 `gorm:"column:person_id"`
		SubjectID   int64 `gorm:"column:subject_id"`
		CharacterID int64 `gorm:"column:character_id"`
	}
	var pcs []pcRow
	if err := chunkBids(bids, func(chunk []int64) error {
		var batch []pcRow
		if err := im.catalog.Raw(`SELECT person_id, subject_id, character_id FROM src_bangumi.person_character WHERE subject_id IN ?`, chunk).Scan(&batch).Error; err != nil {
			return err
		}
		pcs = append(pcs, batch...)
		return nil
	}); err != nil {
		return st, err
	}

	var plans []creditPlan
	for _, sp := range sps {
		workID, ok := workMap[sp.SubjectID]
		if !ok {
			st.SkippedGate++
			continue
		}
		roleID, ok := roleMap[fmt.Sprintf("4:%d", sp.Position)]
		if !ok {
			st.SkippedUnmappedRole++
			continue
		}
		p := personByID[sp.PersonID]
		ext := strconv.FormatInt(sp.PersonID, 10)
		plan := creditPlan{workID: workID, cnExtID: ext, roleID: roleID}
		if p.Type == bangumiTypeCompany || p.Type == bangumiTypeGroup {
			plan.labelExt = ext
		}
		plans = append(plans, plan)
	}
	for _, pc := range pcs {
		workID, ok := workMap[pc.SubjectID]
		if !ok {
			st.SkippedGate++
			continue
		}
		plans = append(plans, creditPlan{
			workID: workID, cnExtID: strconv.FormatInt(pc.PersonID, 10), roleID: roleVoiceActor,
			charExtID: strconv.FormatInt(pc.CharacterID, 10),
			note:      charRoleNote[fmt.Sprintf("%d|%d", pc.SubjectID, pc.CharacterID)],
		})
	}

	// On 2026-09-17, the week after 975 anime and manga stubs gained bangumi
	// refs, a dry run of this lane planned 3,183 names and 182 labels against a
	// usual 37-94 and 1-5: every staff member and company on those subjects,
	// whose positions map to no role and so earn no credit. Only a person or a
	// character that a credit will point at is minted.
	used := referencedBy(plans)
	var newNames []nameItem
	var newLabels []labelItem
	seenName, seenLabel := map[int64]bool{}, map[int64]bool{}
	for _, p := range persons {
		ext := strconv.FormatInt(p.ID, 10)
		if !used.names[ext] {
			continue
		}
		if !seenName[p.ID] && cnAnchor[anchorKey(bangumiSource, ext)] == 0 {
			seenName[p.ID] = true
			newNames = append(newNames, nameItem{extID: ext, name: p.Name, lang: "ja"})
		}
		if used.labels[ext] && !seenLabel[p.ID] && labelAnchor[anchorKey(bangumiSource, ext)] == 0 {
			seenLabel[p.ID] = true
			newLabels = append(newLabels, labelItem{extID: ext, name: p.Name, lang: "ja", kind: labelKind(p.Type)})
		}
	}
	var newChars []charItem
	seenChar := map[int64]bool{}
	for _, c := range chars {
		ext := strconv.FormatInt(c.ID, 10)
		if !used.chars[ext] {
			continue
		}
		if !seenChar[c.ID] && charAnchor[anchorKey(bangumiSource, ext)] == 0 {
			seenChar[c.ID] = true
			newChars = append(newChars, charItem{extID: ext, name: c.Name, lang: "ja"})
		}
	}

	st.NamesCreated = len(newNames)
	st.LabelsCreated = len(newLabels)
	st.CharactersCreated = len(newChars)
	if im.dryRun {
		st.CreditsWritten = len(plans)
		return st, nil
	}

	err = im.catalog.Transaction(func(tx *gorm.DB) error {
		nameIDs, err := im.createCreditNames(tx, bangumiSource, ruleBangumiPerson, newNames)
		if err != nil {
			return err
		}
		labelIDs, err := im.createLabels(tx, bangumiSource, ruleBangumiPerson, newLabels)
		if err != nil {
			return err
		}
		charIDs, err := im.createCharacters(tx, bangumiSource, ruleBangumiChar, newChars)
		if err != nil {
			return err
		}
		cnResolve := resolver(cnAnchor, bangumiSource, nameIDs)
		labelResolve := resolver(labelAnchor, bangumiSource, labelIDs)
		charResolve := resolver(charAnchor, bangumiSource, charIDs)

		credits, dropped := materialize(plans, cnResolve, labelResolve, charResolve, bangumiSource)
		st.Errors += dropped
		written, err := im.insertCredits(tx, credits)
		if err != nil {
			return err
		}
		st.CreditsWritten = written
		st.Already = len(credits) - written
		return nil
	})
	return st, err
}

type mintSet struct{ names, labels, chars map[string]bool }

func referencedBy(plans []creditPlan) mintSet {
	s := mintSet{names: map[string]bool{}, labels: map[string]bool{}, chars: map[string]bool{}}
	for _, p := range plans {
		s.names[p.cnExtID] = true
		if p.labelExt != "" {
			s.labels[p.labelExt] = true
		}
		if p.charExtID != "" {
			s.chars[p.charExtID] = true
		}
	}
	return s
}

func charTypeNote(t int) string {
	switch t {
	case 1:
		return "主角"
	case 2:
		return "配角"
	case 3:
		return "客串"
	default:
		return ""
	}
}

func labelKind(personType int) int16 {
	if personType == bangumiTypeGroup {
		return model.LabelKindGroup
	}
	return model.LabelKindPublisher
}

var bidChunkSize = 10000

func chunkBids(bids []int64, fn func(chunk []int64) error) error {
	for start := 0; start < len(bids); start += bidChunkSize {
		end := min(start+bidChunkSize, len(bids))
		if err := fn(bids[start:end]); err != nil {
			return err
		}
	}
	return nil
}
