package importer

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/sourcedate"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type DLsiteStats struct {
	WorksCreated        int
	ReleasesCreated     int
	TitlesCreated       int
	LabelsCreated       int
	NamesCreated        int
	CreditsWritten      int
	Stubs               int
	SkippedInfoOnly     int
	SkippedUnmappedRole int
	Already             int
	Errors              int
	EdgesConsidered     int
	EdgesWritten        int
}

const dlChunk = 2000

type dlWork struct {
	workno        string
	name          string
	kana          string
	makerExt      string
	contentRating int16
	stub          bool
	y, m, d       *int16
	credits       []dlCredit
}

type dlCredit struct {
	createrExt string
	roleID     int64
}

type dlNamed struct {
	ext, name string
}

func (im *Importer) RunDLsite(dlsiteDB *gorm.DB) (DLsiteStats, error) {
	var st DLsiteStats
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

	q := `SELECT workno, work_name, coalesce(work_name_kana,'') AS kana, maker_id, coalesce(maker_name,'') AS maker_name,
		age_category, ` + sourcedate.DLsiteDaySQL + ` AS regist_ymd, coalesce(product_json->'creaters','{}') AS creaters
		FROM works WHERE work_type_string='ボイス・ASMR' AND status='fetched' ORDER BY workno`
	if im.limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", im.limit)
	}
	var rows []struct {
		Workno    string         `gorm:"column:workno"`
		WorkName  string         `gorm:"column:work_name"`
		Kana      string         `gorm:"column:kana"`
		MakerID   string         `gorm:"column:maker_id"`
		MakerName string         `gorm:"column:maker_name"`
		Age       string         `gorm:"column:age_category"`
		RegistYMD string         `gorm:"column:regist_ymd"`
		Creaters  datatypes.JSON `gorm:"column:creaters"`
	}
	if err := dlsiteDB.Raw(q).Scan(&rows).Error; err != nil {
		return st, err
	}

	makers := map[string]dlNamed{}
	makerKind := map[string]int16{}
	creaters := map[string]dlNamed{}
	var works []dlWork
	for _, r := range rows {
		if _, ok := makers[r.MakerID]; !ok && r.MakerID != "" {
			makers[r.MakerID] = dlNamed{ext: r.MakerID, name: firstNonEmptyStr(r.MakerName, r.MakerID)}
			makerKind[r.MakerID] = dlLabelKind(r.MakerID)
		}
		y, mo, d := ymdParts(r.RegistYMD)
		w := dlWork{
			workno: r.Workno, name: r.WorkName, kana: r.Kana, makerExt: r.MakerID,
			contentRating: dlContentRating(r.Age), stub: strings.TrimSpace(r.WorkName) == "",
			y: y, m: mo, d: d,
		}
		for _, c := range parseCreaters(r.Creaters) {
			roleID, ok := roleMap[c.classification]
			if !ok {
				st.SkippedUnmappedRole++
				continue
			}
			if _, ok := creaters[c.id]; !ok {
				creaters[c.id] = dlNamed{ext: c.id, name: c.name}
			}
			w.credits = append(w.credits, dlCredit{createrExt: c.id, roleID: roleID})
		}
		works = append(works, w)
	}

	newLabels := filterNew(makers, labelAnchor, dlsiteSource)
	newNames := filterNew(creaters, cnAnchor, dlsiteSource)
	var todo []dlWork
	for _, w := range works {
		if relAnchor[anchorKey(dlsiteSource, w.workno)] != 0 {
			st.Already++
			continue
		}
		todo = append(todo, w)
	}

	if im.dryRun {
		st.LabelsCreated = len(newLabels)
		st.NamesCreated = len(newNames)
		st.WorksCreated = len(todo)
		st.ReleasesCreated = len(todo)
		for _, w := range todo {
			if w.stub {
				st.Stubs++
			}
			st.TitlesCreated++
			if w.kana != "" && w.kana != w.name {
				st.TitlesCreated++
			}
			st.CreditsWritten += len(w.credits)
		}
		for _, w := range works {
			if w.makerExt != "" {
				st.EdgesConsidered++
			}
		}
		return st, nil
	}

	labelItems := make([]labelItem, len(newLabels))
	for i, m := range newLabels {
		labelItems[i] = labelItem{extID: m.ext, name: m.name, lang: "ja", kind: makerKind[m.ext]}
	}
	nameItems := make([]nameItem, len(newNames))
	for i, m := range newNames {
		nameItems[i] = nameItem{extID: m.ext, name: m.name, lang: "ja"}
	}
	err = im.catalog.Transaction(func(tx *gorm.DB) error {
		lids, err := im.createLabels(tx, dlsiteSource, ruleDLsiteMaker, labelItems)
		if err != nil {
			return err
		}
		nids, err := im.createCreditNames(tx, dlsiteSource, ruleDLsiteCreater, nameItems)
		if err != nil {
			return err
		}
		for k, v := range lids {
			labelAnchor[anchorKey(dlsiteSource, k)] = v
		}
		for k, v := range nids {
			cnAnchor[anchorKey(dlsiteSource, k)] = v
		}
		return nil
	})
	if err != nil {
		return st, err
	}
	st.LabelsCreated = len(newLabels)
	st.NamesCreated = len(newNames)
	cnResolve := resolver(cnAnchor, dlsiteSource, nil)

	for start := 0; start < len(todo); start += dlChunk {
		end := min(start+dlChunk, len(todo))
		chunk := todo[start:end]
		err := im.catalog.Transaction(func(tx *gorm.DB) error {
			return im.createDLWorkChunk(tx, chunk, cnResolve, &st)
		})
		if err != nil {
			return st, err
		}
	}

	if err := im.emitDLWorkLabels(works, labelAnchor, makerKind, &st); err != nil {
		return st, err
	}
	return st, nil
}

func (im *Importer) emitDLWorkLabels(works []dlWork, labelAnchor map[string]int64, makerKind map[string]int16, st *DLsiteStats) error {
	var rows []struct {
		Workno string `gorm:"column:external_id"`
		WorkID int64  `gorm:"column:work_id"`
	}
	if err := im.catalog.Raw(`SELECT r.external_id, rel.work_id
		FROM catalog_external_ref r JOIN catalog_release rel ON rel.id = r.entity_id
		WHERE r.entity_type = ? AND r.source_id = ?`, model.EntityTypeRelease, dlsiteSource).Scan(&rows).Error; err != nil {
		return err
	}
	worknoToWork := make(map[string]int64, len(rows))
	for _, r := range rows {
		worknoToWork[r.Workno] = r.WorkID
	}

	src := dlsiteSource
	var edges []model.CatalogWorkLabel
	for _, w := range works {
		if w.makerExt == "" {
			continue
		}
		wid, ok := worknoToWork[w.workno]
		if !ok {
			continue
		}
		lid := labelAnchor[anchorKey(dlsiteSource, w.makerExt)]
		if lid == 0 {
			continue
		}
		edges = append(edges, model.CatalogWorkLabel{
			WorkID: wid, LabelID: lid, Kind: dlEdgeKind(makerKind[w.makerExt]), SourceID: &src,
		})
	}
	if len(edges) == 0 {
		return nil
	}
	touched, err := insertWorkLabelEdges(im.catalog, edges)
	if err != nil {
		return err
	}
	st.EdgesWritten = len(touched)
	return touchWorks(im.catalog, touched)
}

func dlEdgeKind(labelKind int16) int16 {
	if labelKind == model.LabelKindPublisher {
		return model.WorkLabelKindPublisher
	}
	return model.WorkLabelKindCircle
}

func (im *Importer) createDLWorkChunk(tx *gorm.DB, chunk []dlWork, cnResolve func(string) (int64, bool), st *DLsiteStats) error {
	src := dlsiteSource
	workRows := make([]model.CatalogWork, len(chunk))
	for i, w := range chunk {
		status := model.WorkStatusLive
		if w.stub {
			status = model.WorkStatusStub
			st.Stubs++
		}
		workRows[i] = model.CatalogWork{
			MediumID: mediumASMR, OLang: "ja", DisplayName: w.name,
			ContentRating: w.contentRating, Status: status,
			Extra: datatypes.JSON(`{}`), FieldProvenance: datatypes.JSON(`{}`),
		}
	}
	if err := tx.CreateInBatches(workRows, 1000).Error; err != nil {
		return err
	}

	var titles []model.CatalogWorkTitle
	var releases []model.CatalogRelease
	relIdx := make([]int, len(chunk))
	var credits []model.CatalogCredit
	for i := range chunk {
		w := chunk[i]
		workID := workRows[i].ID
		titles = append(titles, model.CatalogWorkTitle{WorkID: workID, Lang: "ja", Title: w.name, Kind: model.WorkTitleKindOfficial})
		if w.kana != "" && w.kana != w.name {
			titles = append(titles, model.CatalogWorkTitle{WorkID: workID, Lang: "ja", Title: w.kana, Kind: model.WorkTitleKindSearchHint})
		}
		relIdx[i] = len(releases)
		releases = append(releases, model.CatalogRelease{
			WorkID: workID, Kind: model.ReleaseKindDigital,
			ReleasedY: w.y, ReleasedM: w.m, ReleasedD: w.d, Extra: datatypes.JSON(`{}`),
		})
		for _, c := range w.credits {
			cnID, ok := cnResolve(c.createrExt)
			if !ok {
				st.Errors++
				continue
			}
			credits = append(credits, model.CatalogCredit{
				WorkID: workID, CreditNameID: cnID, RoleID: c.roleID,
				Spoiler: model.SpoilerNone, SourceID: &src,
			})
		}
	}
	if err := tx.CreateInBatches(titles, 1000).Error; err != nil {
		return err
	}
	if err := tx.CreateInBatches(releases, 1000).Error; err != nil {
		return err
	}

	titlesByWork := map[int64][]model.CatalogWorkTitle{}
	for _, t := range titles {
		titlesByWork[t.WorkID] = append(titlesByWork[t.WorkID], t)
	}
	var refs []model.CatalogExternalRef
	var revs []model.CatalogRevision
	for i := range chunk {
		w := chunk[i]
		workID := workRows[i].ID
		rel := releases[relIdx[i]]
		refs = append(refs, selfRef(model.EntityTypeRelease, rel.ID, dlsiteSource, w.workno, ruleDLsiteWork))
		revs = append(revs,
			importedRev(model.EntityTypeWork, workID, workSnapshotJSON(workRows[i], titlesByWork[workID])),
			importedRev(model.EntityTypeRelease, rel.ID, releaseSnapshotJSON(rel)),
		)
	}
	if err := im.batchRefsRevs(tx, refs, revs); err != nil {
		return err
	}

	written, err := im.insertCredits(tx, credits)
	if err != nil {
		return err
	}
	st.WorksCreated += len(chunk)
	st.ReleasesCreated += len(releases)
	st.TitlesCreated += len(titles)
	st.CreditsWritten += written
	return nil
}

type createrJSON struct {
	id, name, classification string
}

func parseCreaters(raw datatypes.JSON) []createrJSON {
	var obj map[string][]struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		Classification string `json:"classification"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil
	}
	var out []createrJSON
	for cls, arr := range obj {
		for _, c := range arr {
			if c.ID == "" {
				continue
			}
			classification := c.Classification
			if classification == "" {
				classification = cls
			}
			out = append(out, createrJSON{id: c.ID, name: c.Name, classification: classification})
		}
	}
	return out
}

func dlContentRating(age string) int16 {
	switch strings.TrimSpace(age) {
	case "1":
		return model.ContentRatingAllAges
	case "2":
		return model.ContentRatingSensitive
	case "3":
		return model.ContentRatingR18
	default:
		return model.ContentRatingAllAges
	}
}

func dlLabelKind(makerID string) int16 {
	if strings.HasPrefix(makerID, "BG") {
		return model.LabelKindPublisher
	}
	return model.LabelKindDoujinCircle
}

func ymdParts(ymd string) (y, m, d *int16) {
	if len(ymd) < 10 {
		return nil, nil, nil
	}
	yy, err1 := strconv.Atoi(ymd[0:4])
	mm, err2 := strconv.Atoi(ymd[5:7])
	dd, err3 := strconv.Atoi(ymd[8:10])
	if err1 != nil || err2 != nil || err3 != nil {
		return nil, nil, nil
	}
	y16, m16, d16 := int16(yy), int16(mm), int16(dd)
	return &y16, &m16, &d16
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func filterNew(m map[string]dlNamed, anchors map[string]int64, source int16) []dlNamed {
	out := make([]dlNamed, 0, len(m))
	for ext, n := range m {
		if anchors[anchorKey(source, ext)] == 0 {
			out = append(out, n)
		}
	}
	return out
}

func workSnapshotJSON(w model.CatalogWork, titles []model.CatalogWorkTitle) datatypes.JSON {
	b, _ := json.Marshal(map[string]any{"work": w, "titles": titles})
	return b
}

func releaseSnapshotJSON(r model.CatalogRelease) datatypes.JSON {
	b, _ := json.Marshal(map[string]any{"release": r})
	return b
}
