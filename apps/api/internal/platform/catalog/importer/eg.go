package importer

import (
	"strconv"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

func (im *Importer) runEG() (Stats, error) {
	var st Stats
	workMap, err := im.loadExactLiveWorkMap(egSource)
	if err != nil {
		return st, err
	}
	if im.limit > 0 {
		workMap = capMap(workMap, im.limit)
	}
	roleMap, err := im.roleMap(egSource)
	if err != nil {
		return st, err
	}
	cnAnchor, err := im.loadAnchors(model.EntityTypeCreditName)
	if err != nil {
		return st, err
	}
	charAnchor, err := im.loadAnchors(model.EntityTypeCharacter)
	if err != nil {
		return st, err
	}

	var staff []struct {
		Game    int64 `gorm:"column:game"`
		Creater int64 `gorm:"column:creater_id"`
		Shubetu *int  `gorm:"column:shubetu"`
	}
	if err := im.eg.Raw(`SELECT game, creater_id, shubetu FROM staff WHERE game IS NOT NULL AND creater_id IS NOT NULL`).Scan(&staff).Error; err != nil {
		return st, err
	}
	var apps []struct {
		Game int64 `gorm:"column:game"`
		Char int64 `gorm:"column:character_id"`
	}
	if err := im.eg.Raw(`SELECT game, character_id FROM appearances WHERE game IS NOT NULL AND character_id IS NOT NULL`).Scan(&apps).Error; err != nil {
		return st, err
	}
	var vas []struct {
		Game  int64 `gorm:"column:game"`
		Char  int64 `gorm:"column:character_id"`
		Actor int64 `gorm:"column:actor_id"`
	}
	if err := im.eg.Raw(`SELECT game, character_id, actor_id FROM appearance_actors WHERE game IS NOT NULL AND character_id IS NOT NULL AND actor_id IS NOT NULL`).Scan(&vas).Error; err != nil {
		return st, err
	}
	createrName, err := im.egNameMap(`SELECT id, raw->>'name' AS name FROM creaters WHERE btrim(coalesce(raw->>'name',''))<>''`)
	if err != nil {
		return st, err
	}
	charName, err := im.egNameMap(`SELECT id, raw->>'name' AS name FROM characters WHERE btrim(coalesce(raw->>'name',''))<>''`)
	if err != nil {
		return st, err
	}

	newNames, seenName := []nameItem{}, map[int64]bool{}
	addCreater := func(id int64) {
		if seenName[id] {
			return
		}
		name, ok := createrName[id]
		if !ok {
			return
		}
		seenName[id] = true
		ext := strconv.FormatInt(id, 10)
		if cnAnchor[anchorKey(egSource, ext)] == 0 {
			newNames = append(newNames, nameItem{extID: ext, name: name, lang: "ja"})
		}
	}
	newChars, seenChar := []charItem{}, map[int64]bool{}
	addChar := func(id int64) {
		if seenChar[id] {
			return
		}
		name, ok := charName[id]
		if !ok {
			return
		}
		seenChar[id] = true
		ext := strconv.FormatInt(id, 10)
		if charAnchor[anchorKey(egSource, ext)] == 0 {
			newChars = append(newChars, charItem{extID: ext, name: name, lang: "ja"})
		}
	}

	var plans []creditPlan
	for _, s := range staff {
		workID, ok := workMap[s.Game]
		if !ok {
			continue
		}
		if s.Shubetu == nil {
			st.SkippedUnmappedRole++
			continue
		}
		roleID, ok := roleMap[strconv.Itoa(*s.Shubetu)]
		if !ok {
			st.SkippedUnmappedRole++
			continue
		}
		if _, ok := createrName[s.Creater]; !ok {
			continue
		}
		addCreater(s.Creater)
		plans = append(plans, creditPlan{workID: workID, cnExtID: strconv.FormatInt(s.Creater, 10), roleID: roleID})
	}
	for _, a := range apps {
		if _, ok := workMap[a.Game]; ok {
			addChar(a.Char)
		}
	}
	for _, v := range vas {
		workID, ok := workMap[v.Game]
		if !ok {
			continue
		}
		if _, ok := createrName[v.Actor]; !ok {
			continue
		}
		if _, ok := charName[v.Char]; !ok {
			continue
		}
		addCreater(v.Actor)
		addChar(v.Char)
		plans = append(plans, creditPlan{
			workID: workID, cnExtID: strconv.FormatInt(v.Actor, 10), roleID: roleVoiceActor,
			charExtID: strconv.FormatInt(v.Char, 10),
		})
	}

	st.NamesCreated = len(newNames)
	st.CharactersCreated = len(newChars)
	if im.dryRun {
		st.CreditsWritten = len(plans)
		return st, nil
	}

	err = im.catalog.Transaction(func(tx *gorm.DB) error {
		nameIDs, err := im.createCreditNames(tx, egSource, ruleEGCreater, newNames)
		if err != nil {
			return err
		}
		charIDs, err := im.createCharacters(tx, egSource, ruleEGChar, newChars)
		if err != nil {
			return err
		}
		cnResolve := resolver(cnAnchor, egSource, nameIDs)
		charResolve := resolver(charAnchor, egSource, charIDs)
		noLabel := func(string) (int64, bool) { return 0, false }

		credits, dropped := materialize(plans, cnResolve, noLabel, charResolve, egSource)
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

func (im *Importer) egNameMap(query string) (map[int64]string, error) {
	var rows []struct {
		ID   int64  `gorm:"column:id"`
		Name string `gorm:"column:name"`
	}
	if err := im.eg.Raw(query).Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[int64]string, len(rows))
	for _, r := range rows {
		m[r.ID] = r.Name
	}
	return m, nil
}
