package importer

import (
	"log/slog"
	"strconv"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

type egMusicRole struct{ table, sourceRole string }

var egMusicRoles = []egMusicRole{
	{"singers", "singers"},
	{"lyricists", "lyricists"},
	{"composers", "composers"},
	{"arrangers", "arrangers"},
}

type musicRoleStat struct {
	table         string
	roleID        int64
	candidates    int
	noCreaterName int
	noAnchor      int
	featuring     int
	skipDup       int
	planned       int
	written       int
	already       int
}

type egMusicRow struct {
	Creater   int64 `gorm:"column:creater_id"`
	Music     int64 `gorm:"column:music"`
	Featuring bool  `gorm:"column:featuring"`
}

func (im *Importer) runEGMusic() (Stats, error) {
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
	createrName, err := im.egNameMap(`SELECT id, raw->>'name' AS name FROM creaters WHERE btrim(coalesce(raw->>'name',''))<>''`)
	if err != nil {
		return st, err
	}
	musicToWorks, err := im.egMusicWorkMap(workMap)
	if err != nil {
		return st, err
	}

	newNames, seenName := []nameItem{}, map[int64]bool{}
	addCreater := func(id int64) {
		if seenName[id] {
			return
		}
		seenName[id] = true
		ext := strconv.FormatInt(id, 10)
		if cnAnchor[anchorKey(egSource, ext)] == 0 {
			newNames = append(newNames, nameItem{extID: ext, name: createrName[id], lang: "ja"})
		}
	}

	stats := make([]musicRoleStat, len(egMusicRoles))
	plansByRole := make([][]creditPlan, len(egMusicRoles))
	for i, mr := range egMusicRoles {
		stats[i].table = mr.table
		roleID, ok := roleMap[mr.sourceRole]
		if !ok {
			st.SkippedUnmappedRole++
			continue
		}
		stats[i].roleID = roleID
		rows, err := im.egMusicCredits(mr.table)
		if err != nil {
			return st, err
		}
		var plans []creditPlan
		seenWC := map[[2]int64]bool{}
		for _, r := range rows {
			stats[i].candidates++
			if r.Featuring {
				stats[i].featuring++
			}
			if _, ok := createrName[r.Creater]; !ok {
				stats[i].noCreaterName++
				continue
			}
			works := musicToWorks[r.Music]
			if len(works) == 0 {
				stats[i].noAnchor++
				continue
			}
			for _, w := range works {
				key := [2]int64{w, r.Creater}
				if seenWC[key] {
					stats[i].skipDup++
					continue
				}
				seenWC[key] = true
				addCreater(r.Creater)
				plans = append(plans, creditPlan{workID: w, cnExtID: strconv.FormatInt(r.Creater, 10), roleID: roleID})
			}
		}
		stats[i].planned = len(plans)
		plansByRole[i] = plans
	}

	st.NamesCreated = len(newNames)
	if im.dryRun {
		for i := range stats {
			st.CreditsWritten += stats[i].planned
		}
		logEGMusic(stats, len(newNames), true)
		return st, nil
	}

	err = im.catalog.Transaction(func(tx *gorm.DB) error {
		nameIDs, err := im.createCreditNames(tx, egSource, ruleEGCreater, newNames)
		if err != nil {
			return err
		}
		cnResolve := resolver(cnAnchor, egSource, nameIDs)
		noLabel := func(string) (int64, bool) { return 0, false }
		noChar := func(string) (int64, bool) { return 0, false }
		for i := range stats {
			credits, dropped := materialize(plansByRole[i], cnResolve, noLabel, noChar, egSource)
			st.Errors += dropped
			written, err := im.insertCredits(tx, credits)
			if err != nil {
				return err
			}
			stats[i].written = written
			stats[i].already = len(credits) - written
			st.CreditsWritten += written
			st.Already += len(credits) - written
		}
		return nil
	})
	if err != nil {
		return st, err
	}
	logEGMusic(stats, len(newNames), false)
	return st, nil
}

func (im *Importer) egMusicWorkMap(workMap map[int64]int64) (map[int64][]int64, error) {
	var rows []struct {
		Music int64 `gorm:"column:music"`
		Game  int64 `gorm:"column:game"`
	}
	if err := im.eg.Raw(`SELECT music, game FROM game_music WHERE music IS NOT NULL AND game IS NOT NULL`).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64][]int64)
	seen := make(map[[2]int64]bool)
	for _, r := range rows {
		workID, ok := workMap[r.Game]
		if !ok {
			continue
		}
		key := [2]int64{r.Music, workID}
		if seen[key] {
			continue
		}
		seen[key] = true
		out[r.Music] = append(out[r.Music], workID)
	}
	return out, nil
}

func (im *Importer) egMusicCredits(table string) ([]egMusicRow, error) {
	var rows []egMusicRow
	q := `SELECT creater_id, music, coalesce((raw->>'featuring')::bool, false) AS featuring FROM ` + table +
		` WHERE creater_id IS NOT NULL AND creater_id <> 0 AND music IS NOT NULL`
	if err := im.eg.Raw(q).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func logEGMusic(stats []musicRoleStat, namesToCreate int, dry bool) {
	for _, s := range stats {
		slog.Info("eg music credits (per role)",
			"table", s.table, "role_id", s.roleID,
			"candidates", s.candidates, "no_creater_name", s.noCreaterName,
			"no_anchor", s.noAnchor, "featuring", s.featuring,
			"skip_dup", s.skipDup, "planned", s.planned,
			"written", s.written, "already", s.already)
	}
	slog.Info("eg music credits summary", "names_to_create", namesToCreate, "dry_run", dry)
}
