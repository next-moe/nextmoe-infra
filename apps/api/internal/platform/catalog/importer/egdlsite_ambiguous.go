package importer

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

type conflictRow struct {
	workno, name string
	y, m, d      *int16
	claimants    []conflictClaimant
}

type conflictClaimant struct {
	egGame, workID int64
}

func (im *Importer) resolveAmbiguousGroups(dlsiteDB *gorm.DB, ambiguousIDs []string,
	dlsiteToEG map[string][]int64, egWork map[int64][]int64, relAnchor map[string]int64,
	roleMap map[string]int64, collectMaker func(id, name string), creaters map[string]dlNamed,
	attach, mint *[]egdlItem, conflicts *[]conflictRow, st *EGDLsiteStats) error {

	rows, err := loadDLWorks(dlsiteDB, ambiguousIDs, 0)
	if err != nil {
		return err
	}
	found := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		found[r.Workno] = struct{}{}
	}
	for _, did := range ambiguousIDs {
		if _, ok := found[did]; !ok {
			st.Missing++
		}
	}

	for _, r := range rows {
		if relAnchor[anchorKey(dlsiteSource, r.Workno)] != 0 {
			st.Already++
			continue
		}
		dw := parseDLRow(r, roleMap, creaters, st)
		collectMaker(r.MakerID, r.MakerName)

		matched := map[int64]int64{}
		sawMulti := false
		for _, g := range dlsiteToEG[r.Workno] {
			works := egWork[g]
			if len(works) >= 2 {
				sawMulti = true
				continue
			}
			if len(works) == 1 {
				matched[works[0]] = g
			}
		}
		if sawMulti {
			st.SkippedMultiWork++
			continue
		}
		switch len(matched) {
		case 1:
			for w, g := range matched {
				*attach = append(*attach, egdlItem{dw: dw, egGame: g, workID: w, attach: true, nameFold: r.NameFold})
			}
			st.AmbB1++
		case 0:
			*mint = append(*mint, egdlItem{dw: dw, nameFold: r.NameFold, noEGRef: true})
			st.AmbB2++
		default:
			cr := conflictRow{workno: r.Workno, name: r.WorkName, y: dw.y, m: dw.m, d: dw.d}
			for _, g := range dlsiteToEG[r.Workno] {
				if works := egWork[g]; len(works) == 1 {
					cr.claimants = append(cr.claimants, conflictClaimant{egGame: g, workID: works[0]})
				}
			}
			*conflicts = append(*conflicts, cr)
			st.AmbConflicts++
		}
	}
	return nil
}

func (im *Importer) exportConflicts(conflicts []conflictRow, path string) error {
	workIDs := map[int64]struct{}{}
	for _, c := range conflicts {
		for _, cl := range c.claimants {
			workIDs[cl.workID] = struct{}{}
		}
	}
	titles := map[int64]string{}
	if len(workIDs) > 0 {
		ids := make([]int64, 0, len(workIDs))
		for id := range workIDs {
			ids = append(ids, id)
		}
		var rows []struct {
			ID    int64  `gorm:"column:id"`
			Title string `gorm:"column:display_name"`
		}
		if err := im.catalog.Raw(`SELECT id, display_name FROM catalog_work WHERE id IN ?`, ids).Scan(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			titles[r.ID] = r.Title
		}
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	bw := bufio.NewWriter(f)
	fmt.Fprintln(bw, strings.Join([]string{
		"workno", "dlsite_name", "released", "n_claimants", "eg_games", "wiki_work_ids", "wiki_titles", "decision", "notes",
	}, "\t"))
	for _, c := range conflicts {
		var games, works, tset []string
		for _, cl := range c.claimants {
			games = append(games, strconv.FormatInt(cl.egGame, 10))
			works = append(works, strconv.FormatInt(cl.workID, 10))
			tset = append(tset, cleanTSV(titles[cl.workID]))
		}
		fmt.Fprintln(bw, strings.Join([]string{
			c.workno, cleanTSV(c.name), releasedStr(c.y, c.m, c.d), strconv.Itoa(len(c.claimants)),
			strings.Join(games, "|"), strings.Join(works, "|"), strings.Join(tset, " | "), "", "",
		}, "\t"))
	}
	if err := bw.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "wrote %s: %d conflict rows\n", path, len(conflicts))
	return nil
}

func releasedStr(y, m, d *int16) string {
	if y == nil {
		return ""
	}
	s := strconv.Itoa(int(*y))
	if m != nil {
		s += fmt.Sprintf("-%02d", *m)
		if d != nil {
			s += fmt.Sprintf("-%02d", *d)
		}
	}
	return s
}

func cleanTSV(s string) string {
	return strings.NewReplacer("\t", " ", "\n", " ", "\r", " ").Replace(s)
}
