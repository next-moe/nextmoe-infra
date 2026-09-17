package importer

import (
	"fmt"

	"api/internal/platform/catalog/model"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (im *Importer) loadEGDlsiteClaims() (map[string][]int64, error) {
	var rows []struct {
		ID     int64  `gorm:"column:id"`
		Dlsite string `gorm:"column:dlsite_id"`
	}
	if err := im.eg.Raw(`SELECT id, dlsite_id FROM games WHERE dlsite_id IS NOT NULL AND dlsite_id <> ''`).
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load eg dlsite claims: %w", err)
	}
	m := make(map[string][]int64, len(rows))
	for _, r := range rows {
		m[r.Dlsite] = append(m[r.Dlsite], r.ID)
	}
	return m, nil
}

func loadDLWorks(dlsiteDB *gorm.DB, worknos []string, limit int) ([]dlRow, error) {
	if len(worknos) == 0 {
		return nil, nil
	}
	var rows []dlRow
	if err := dlsiteDB.Raw(`
		SELECT workno, work_name, coalesce(work_name_kana,'') AS kana,
		       coalesce(maker_id,'') AS maker_id, coalesce(maker_name,'') AS maker_name,
		       coalesce(age_category,'') AS age_category, regist_date,
		       coalesce(product_json->'creaters','{}') AS creaters,
		       lower(normalize(work_name, NFKC)) AS name_fold
		FROM works WHERE status = 'fetched' AND workno IN ? ORDER BY workno`, worknos).
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load dlsite works: %w", err)
	}
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}

func (im *Importer) createEGDLShared(newLabels, newNames []dlNamed, makerKind map[string]int16, labelAnchor, cnAnchor map[string]int64) error {
	labelItems := make([]labelItem, len(newLabels))
	for i, m := range newLabels {
		labelItems[i] = labelItem{extID: m.ext, name: m.name, lang: "ja", kind: makerKind[m.ext]}
	}
	nameItems := make([]nameItem, len(newNames))
	for i, m := range newNames {
		nameItems[i] = nameItem{extID: m.ext, name: m.name, lang: "ja"}
	}
	return im.catalog.Transaction(func(tx *gorm.DB) error {
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
}

func (im *Importer) mintEGDLWorks(items []egdlItem, cnResolve func(string) (int64, bool), st *EGDLsiteStats) error {
	for start := 0; start < len(items); start += dlChunk {
		end := min(start+dlChunk, len(items))
		chunk := items[start:end]
		if err := im.catalog.Transaction(func(tx *gorm.DB) error {
			return im.createMintChunk(tx, chunk, cnResolve, st)
		}); err != nil {
			return err
		}
	}
	return nil
}

func (im *Importer) createMintChunk(tx *gorm.DB, chunk []egdlItem, cnResolve func(string) (int64, bool), st *EGDLsiteStats) error {
	dl, eg := dlsiteSource, egSource
	works := make([]model.CatalogWork, len(chunk))
	for i, it := range chunk {
		status := model.WorkStatusLive
		switch {
		case it.dw.stub:
			status = model.WorkStatusStub
			st.Stubs++
		case it.collidedWorkID != 0:
			status = model.WorkStatusQuarantine
		}
		works[i] = model.CatalogWork{
			MediumID: mediumGalgame, OLang: model.OLangDefault, DisplayName: it.dw.name,
			ContentRating: it.dw.contentRating, Status: status,
			Extra: datatypes.JSON(`{}`), FieldProvenance: datatypes.JSON(`{}`),
		}
	}
	if err := tx.CreateInBatches(works, 1000).Error; err != nil {
		return err
	}

	var titles []model.CatalogWorkTitle
	releases := make([]model.CatalogRelease, len(chunk))
	var credits []model.CatalogCredit
	for i, it := range chunk {
		wid := works[i].ID
		titles = append(titles, model.CatalogWorkTitle{WorkID: wid, Lang: "ja", Title: it.dw.name, Kind: model.WorkTitleKindOfficial})
		if it.dw.kana != "" && it.dw.kana != it.dw.name {
			titles = append(titles, model.CatalogWorkTitle{WorkID: wid, Lang: "ja", Title: it.dw.kana, Kind: model.WorkTitleKindSearchHint})
		}
		releases[i] = model.CatalogRelease{
			WorkID: wid, Kind: model.ReleaseKindDigital,
			ReleasedY: it.dw.y, ReleasedM: it.dw.m, ReleasedD: it.dw.d, Extra: datatypes.JSON(`{}`),
		}
		credits = append(credits, buildDLCredits(wid, it.dw.credits, cnResolve, st)...)
	}
	if err := tx.CreateInBatches(titles, 1000).Error; err != nil {
		return err
	}
	if err := tx.CreateInBatches(releases, 1000).Error; err != nil {
		return err
	}

	var refs []model.CatalogExternalRef
	var revs []model.CatalogRevision
	for i, it := range chunk {
		wid := works[i].ID
		rel := releases[i]
		refs = append(refs, selfRef(model.EntityTypeRelease, rel.ID, dl, it.dw.workno, ruleEGDLsite))
		if !it.noEGRef {
			refs = append(refs, model.CatalogExternalRef{
				EntityType: model.EntityTypeWork, EntityID: wid, SourceID: eg,
				ExternalID: fmt.Sprint(it.egGame), LinkKind: model.LinkKindProbable, MatchedBy: ruleEGDLsite,
			})
			st.EGRefsWritten++
		}
		revs = append(revs,
			importedRev(model.EntityTypeWork, wid, releaseSnapshotJSON(rel)),
			importedRev(model.EntityTypeRelease, rel.ID, releaseSnapshotJSON(rel)),
		)
	}
	if err := im.batchRefsRevs(tx, refs, revs); err != nil {
		return err
	}
	var cands []model.CatalogMatchCandidate
	for i, it := range chunk {
		if it.collidedWorkID == 0 {
			continue
		}
		a, b := works[i].ID, it.collidedWorkID
		if b < a {
			a, b = b, a
		}
		cands = append(cands, model.CatalogMatchCandidate{
			EntityType: model.EntityTypeWork, AID: a, BID: b,
			Reason: model.CandidateReasonNameNormEqual,
			Status: model.CandidateStatusPending,
		})
	}
	if len(cands) > 0 {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&cands).Error; err != nil {
			return err
		}
		st.Quarantined += len(cands)
	}
	written, err := im.insertCredits(tx, credits)
	if err != nil {
		return err
	}
	st.ReleasesCreated += len(releases)
	st.TitlesCreated += len(titles)
	st.CreditsWritten += written
	return nil
}

func (im *Importer) attachEGDLReleases(items []egdlItem, cnResolve func(string) (int64, bool), st *EGDLsiteStats) error {
	ids := make([]int64, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.workID)
	}
	titleFolds, err := loadTitleFolds(im.catalog, ids)
	if err != nil {
		return err
	}
	for start := 0; start < len(items); start += dlChunk {
		end := min(start+dlChunk, len(items))
		chunk := items[start:end]
		if err := im.catalog.Transaction(func(tx *gorm.DB) error {
			return im.createAttachChunk(tx, chunk, cnResolve, titleFolds, st)
		}); err != nil {
			return err
		}
	}
	return nil
}

func (im *Importer) createAttachChunk(tx *gorm.DB, chunk []egdlItem, cnResolve func(string) (int64, bool), titleFolds map[int64]map[string]bool, st *EGDLsiteStats) error {
	dl := dlsiteSource
	releases := make([]model.CatalogRelease, len(chunk))
	for i, it := range chunk {
		releases[i] = model.CatalogRelease{
			WorkID: it.workID, Kind: model.ReleaseKindDigital,
			ReleasedY: it.dw.y, ReleasedM: it.dw.m, ReleasedD: it.dw.d, Extra: datatypes.JSON(`{}`),
		}
	}
	if err := tx.CreateInBatches(releases, 1000).Error; err != nil {
		return err
	}

	var titles []model.CatalogWorkTitle
	var refs []model.CatalogExternalRef
	var revs []model.CatalogRevision
	var credits []model.CatalogCredit
	for i, it := range chunk {
		rel := releases[i]
		refs = append(refs, selfRef(model.EntityTypeRelease, rel.ID, dl, it.dw.workno, ruleEGDLsite))
		revs = append(revs, importedRev(model.EntityTypeRelease, rel.ID, releaseSnapshotJSON(rel)))
		if it.nameFold != "" && !titleFolds[it.workID][it.nameFold] {
			titles = append(titles, model.CatalogWorkTitle{
				WorkID: it.workID, Lang: "ja", Title: it.dw.name, Kind: model.WorkTitleKindSearchHint,
			})
			if titleFolds[it.workID] == nil {
				titleFolds[it.workID] = map[string]bool{}
			}
			titleFolds[it.workID][it.nameFold] = true
		}
		credits = append(credits, buildDLCredits(it.workID, it.dw.credits, cnResolve, st)...)
	}
	if len(titles) > 0 {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(titles, 1000).Error; err != nil {
			return err
		}
	}
	if err := im.batchRefsRevs(tx, refs, revs); err != nil {
		return err
	}
	written, err := im.insertCredits(tx, credits)
	if err != nil {
		return err
	}
	st.ReleasesCreated += len(releases)
	st.TitlesCreated += len(titles)
	st.CreditsWritten += written
	hosts := make([]int64, 0, len(chunk))
	for _, it := range chunk {
		hosts = append(hosts, it.workID)
	}
	return touchWorks(tx, hosts)
}

func (im *Importer) emitEGDLEdges(items []egdlItem, labelAnchor map[string]int64, makerKind map[string]int16, st *EGDLsiteStats) error {
	var rows []struct {
		Workno string `gorm:"column:external_id"`
		WorkID int64  `gorm:"column:work_id"`
	}
	if err := im.catalog.Raw(`SELECT r.external_id, rel.work_id
		FROM catalog_external_ref r JOIN catalog_release rel ON rel.id = r.entity_id
		WHERE r.entity_type = ? AND r.source_id = ? AND r.matched_by = ?`,
		model.EntityTypeRelease, dlsiteSource, ruleEGDLsite).Scan(&rows).Error; err != nil {
		return err
	}
	worknoToWork := make(map[string]int64, len(rows))
	for _, r := range rows {
		worknoToWork[r.Workno] = r.WorkID
	}
	src := dlsiteSource
	var edges []model.CatalogWorkLabel
	for _, it := range items {
		if it.dw.makerExt == "" {
			continue
		}
		wid, ok := worknoToWork[it.dw.workno]
		if !ok {
			continue
		}
		lid := labelAnchor[anchorKey(dlsiteSource, it.dw.makerExt)]
		if lid == 0 {
			continue
		}
		edges = append(edges, model.CatalogWorkLabel{
			WorkID: wid, LabelID: lid, Kind: dlEdgeKind(makerKind[it.dw.makerExt]), SourceID: &src,
		})
	}
	if len(edges) == 0 {
		return nil
	}
	res := im.catalog.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&edges, 1000)
	if res.Error != nil {
		return res.Error
	}
	st.EdgesWritten = int(res.RowsAffected)
	return nil
}

func buildDLCredits(workID int64, plan []dlCredit, cnResolve func(string) (int64, bool), st *EGDLsiteStats) []model.CatalogCredit {
	src := dlsiteSource
	out := make([]model.CatalogCredit, 0, len(plan))
	for _, c := range plan {
		cnID, ok := cnResolve(c.createrExt)
		if !ok {
			st.Errors++
			continue
		}
		out = append(out, model.CatalogCredit{
			WorkID: workID, CreditNameID: cnID, RoleID: c.roleID, Spoiler: model.SpoilerNone, SourceID: &src,
		})
	}
	return out
}

func loadTitleFolds(db *gorm.DB, workIDs []int64) (map[int64]map[string]bool, error) {
	out := map[int64]map[string]bool{}
	if len(workIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		WorkID int64  `gorm:"column:work_id"`
		Fold   string `gorm:"column:fold"`
	}
	if err := db.Raw(`SELECT work_id, lower(normalize(title, NFKC)) AS fold
		FROM catalog_work_title WHERE work_id IN ?`, workIDs).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load title folds: %w", err)
	}
	for _, r := range rows {
		if out[r.WorkID] == nil {
			out[r.WorkID] = map[string]bool{}
		}
		out[r.WorkID][r.Fold] = true
	}
	return out, nil
}
