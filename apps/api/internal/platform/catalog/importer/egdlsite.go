package importer

import (
	"fmt"
	"strings"

	"api/internal/platform/catalog/model"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	mediumGalgame int16 = 1
	ruleEGDLsite        = "rule:eg-dlsite-rosetta"
)

type EGDLsiteStats struct {
	Attached  int
	Minted    int
	Already   int
	Ambiguous int
	Missing   int

	AmbB1        int
	AmbB2        int
	AmbConflicts int

	ReleasesCreated     int
	TitlesCreated       int
	LabelsCreated       int
	NamesCreated        int
	CreditsWritten      int
	EdgesWritten        int
	EGRefsWritten       int
	Stubs               int
	SkippedUnmappedRole int
	Errors              int

	TitleCollisions       int
	Quarantined           int
	SkippedIntraCollision int
	EGAliases             int
	SkippedMultiWork      int
}

type egdlItem struct {
	dw             dlWork
	egGame         int64
	workID         int64
	attach         bool
	nameFold       string
	noEGRef        bool
	collidedWorkID int64
	egName         string
	egNameFold     string
}

type dlRow struct {
	Workno    string         `gorm:"column:workno"`
	WorkName  string         `gorm:"column:work_name"`
	Kana      string         `gorm:"column:kana"`
	MakerID   string         `gorm:"column:maker_id"`
	MakerName string         `gorm:"column:maker_name"`
	Age       string         `gorm:"column:age_category"`
	RegistYMD string         `gorm:"column:regist_ymd"`
	Creaters  datatypes.JSON `gorm:"column:creaters"`
	NameFold  string         `gorm:"column:name_fold"`
}

func (im *Importer) RunEGDLsite(dlsiteDB *gorm.DB) (EGDLsiteStats, error) {
	var st EGDLsiteStats
	if im.eg == nil {
		return st, fmt.Errorf("eg connection required")
	}
	roleMap, err := im.roleMap(dlsiteSource)
	if err != nil {
		return st, err
	}
	relAnchor, err := im.loadAnchors(model.EntityTypeRelease)
	if err != nil {
		return st, err
	}
	labelAnchor, err := im.loadAnchors(model.EntityTypeLabel)
	if err != nil {
		return st, err
	}
	cnAnchor, err := im.loadAnchors(model.EntityTypeCreditName)
	if err != nil {
		return st, err
	}
	egWork, err := im.loadEGKnownWorks()
	if err != nil {
		return st, err
	}

	dlsiteToEG, err := im.loadEGDlsiteClaims()
	if err != nil {
		return st, err
	}
	candidates := make([]string, 0, len(dlsiteToEG))
	var ambiguousIDs []string
	for did, games := range dlsiteToEG {
		if len(games) > 1 {
			ambiguousIDs = append(ambiguousIDs, did)
			continue
		}
		candidates = append(candidates, did)
	}
	if !im.resolveAmbiguous {
		st.Ambiguous = len(ambiguousIDs)
	}

	rows, err := loadDLWorks(dlsiteDB, candidates, im.limit)
	if err != nil {
		return st, err
	}
	found := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		found[r.Workno] = struct{}{}
	}
	for _, did := range candidates {
		if _, ok := found[did]; !ok {
			st.Missing++
		}
	}

	makers := map[string]dlNamed{}
	makerKind := map[string]int16{}
	creaters := map[string]dlNamed{}
	var attach, mint []egdlItem
	collectMaker := func(id, name string) {
		if id == "" {
			return
		}
		if _, ok := makers[id]; !ok {
			makers[id] = dlNamed{ext: id, name: firstNonEmptyStr(name, id)}
			makerKind[id] = egdlLabelKind(id)
		}
	}
	for _, r := range rows {
		if relAnchor[anchorKey(dlsiteSource, r.Workno)] != 0 {
			st.Already++
			continue
		}
		dw := parseDLRow(r, roleMap, creaters, &st)
		collectMaker(r.MakerID, r.MakerName)
		eg := dlsiteToEG[r.Workno][0]
		item := egdlItem{dw: dw, egGame: eg, nameFold: r.NameFold}
		switch works := egWork[eg]; len(works) {
		case 1:
			item.attach, item.workID = true, works[0]
			attach = append(attach, item)
		case 0:
			mint = append(mint, item)
		default:
			st.SkippedMultiWork++
		}
	}

	var conflicts []conflictRow
	if im.resolveAmbiguous {
		if err := im.resolveAmbiguousGroups(dlsiteDB, ambiguousIDs, dlsiteToEG, egWork, relAnchor,
			roleMap, collectMaker, creaters, &attach, &mint, &conflicts, &st); err != nil {
			return st, err
		}
	}

	if err := im.ensureEGDLAliases(&st); err != nil {
		return st, err
	}
	if len(mint) > 0 {
		if err := im.attachEGNames(mint); err != nil {
			return st, err
		}
		wt, err := im.loadExistingWorkTitleNorms()
		if err != nil {
			return st, err
		}
		mint = gateEGDLMints(mint, wt, &st)
	}

	st.Attached = len(attach)
	st.Minted = len(mint)

	newLabels := filterNew(makers, labelAnchor, dlsiteSource)
	newNames := filterNew(creaters, cnAnchor, dlsiteSource)

	if im.conflictsOut != "" {
		if err := im.exportConflicts(conflicts, im.conflictsOut); err != nil {
			return st, err
		}
	}

	if im.dryRun {
		st.LabelsCreated = len(newLabels)
		st.NamesCreated = len(newNames)
		st.ReleasesCreated = len(attach) + len(mint)
		for _, it := range mint {
			if it.dw.stub {
				st.Stubs++
			}
			st.TitlesCreated++
			if _, ok := egAliasTitle(0, it); ok {
				st.TitlesCreated++
			}
			if !it.noEGRef {
				st.EGRefsWritten++
			}
			st.CreditsWritten += len(it.dw.credits)
			if it.collidedWorkID != 0 {
				st.Quarantined++
			}
		}
		for _, it := range attach {
			st.CreditsWritten += len(it.dw.credits)
			st.TitlesCreated++
		}
		st.EdgesWritten = len(attach) + len(mint)
		return st, nil
	}

	if err := im.createEGDLShared(newLabels, newNames, makerKind, labelAnchor, cnAnchor); err != nil {
		return st, err
	}
	st.LabelsCreated = len(newLabels)
	st.NamesCreated = len(newNames)
	cnResolve := resolver(cnAnchor, dlsiteSource, nil)

	if err := im.mintEGDLWorks(mint, cnResolve, &st); err != nil {
		return st, err
	}
	if err := im.attachEGDLReleases(attach, cnResolve, &st); err != nil {
		return st, err
	}
	if err := im.emitEGDLEdges(append(attach, mint...), labelAnchor, makerKind, &st); err != nil {
		return st, err
	}
	return st, nil
}

// gateEGDLMints holds this lane to the title gate the other minting lanes use.
// It had none: on 2026-09-16, 23 of its 69 pending mints already existed as
// live works under the same title, because an EG game that no work carries an
// EG ref for looks unanchored. A collision mints the work quarantined with
// a pending candidate, so the work-pair judge either merges or releases it; two
// pending mints that spell one title are both held back.
func gateEGDLMints(mint []egdlItem, wt map[string]wtNorm, st *EGDLsiteStats) []egdlItem {
	keysOf := func(it egdlItem) map[string]bool {
		keys := map[string]bool{}
		for _, n := range []string{it.nameFold, it.egNameFold} {
			for _, key := range gateKeys(n) {
				keys[key] = true
			}
		}
		return keys
	}
	perKey := map[string]int{}
	for _, it := range mint {
		for key := range keysOf(it) {
			perKey[key]++
		}
	}
	out := mint[:0]
	for _, it := range mint {
		shared := false
		for key := range keysOf(it) {
			if perKey[key] > 1 {
				shared = true
			}
		}
		if shared {
			st.SkippedIntraCollision++
			continue
		}
		if _, w, ok := firstCorpusHit(wt, it.nameFold, it.egNameFold); ok {
			it.collidedWorkID = w.workID
			st.TitleCollisions++
		}
		out = append(out, it)
	}
	return out
}

func parseDLRow(r dlRow, roleMap map[string]int64, creaters map[string]dlNamed, st *EGDLsiteStats) dlWork {
	dw := dlWork{
		workno: r.Workno, name: r.WorkName, kana: r.Kana, makerExt: r.MakerID,
		contentRating: dlContentRating(r.Age), stub: strings.TrimSpace(r.WorkName) == "",
	}
	dw.y, dw.m, dw.d = ymdParts(r.RegistYMD)
	for _, c := range parseCreaters(r.Creaters) {
		roleID, ok := roleMap[c.classification]
		if !ok {
			st.SkippedUnmappedRole++
			continue
		}
		if _, ok := creaters[c.id]; !ok {
			creaters[c.id] = dlNamed{ext: c.id, name: c.name}
		}
		dw.credits = append(dw.credits, dlCredit{createrExt: c.id, roleID: roleID})
	}
	return dw
}

func egdlLabelKind(makerID string) int16 {
	if strings.HasPrefix(makerID, "VG") || strings.HasPrefix(makerID, "BG") {
		return model.LabelKindPublisher
	}
	return model.LabelKindDoujinCircle
}
