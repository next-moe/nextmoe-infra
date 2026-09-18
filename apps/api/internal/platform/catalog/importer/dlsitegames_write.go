package importer

import (
	"encoding/json"
	"os"
	"sort"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/titlekey"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (im *Importer) writeDLsiteGames(snap *dlGamesSnap, plans []dlGamePlan, st *DLsiteGamesStats) error {
	makers := map[string]dlNamed{}
	makerKind := map[string]int16{}
	creaters := map[string]dlNamed{}
	for _, p := range plans {
		if p.kind != "attach" && p.kind != "mint" {
			continue
		}
		for _, m := range p.members {
			if m.makerExt != "" {
				if _, ok := makers[m.makerExt]; !ok {
					makers[m.makerExt] = dlNamed{ext: m.makerExt, name: firstNonEmptyStr(m.makerName, m.makerExt)}
					makerKind[m.makerExt] = egdlLabelKind(m.makerExt)
				}
			}
			for ext, n := range snap.creatersOf(m) {
				if _, ok := creaters[ext]; !ok {
					creaters[ext] = n
				}
			}
		}
	}
	labelAnchor, err := im.loadAnchors(model.EntityTypeLabel)
	if err != nil {
		return err
	}
	cnAnchor, err := im.loadAnchors(model.EntityTypeCreditName)
	if err != nil {
		return err
	}
	newLabels := filterNew(makers, labelAnchor, dlsiteSource)
	newNames := filterNew(creaters, cnAnchor, dlsiteSource)
	if err := im.createEGDLShared(newLabels, newNames, makerKind, labelAnchor, cnAnchor); err != nil {
		return err
	}
	cnResolve := resolver(cnAnchor, dlsiteSource, nil)

	var attachTargets []int64
	for _, p := range plans {
		if p.kind == "attach" && p.workID != 0 {
			attachTargets = append(attachTargets, p.workID)
		}
	}
	titleLoose, err := loadTitleLoose(im.catalog, attachTargets)
	if err != nil {
		return err
	}

	worknoToWork := map[string]int64{}
	var hosts []int64
	for start := 0; start < len(plans); start += dlChunk {
		end := min(start+dlChunk, len(plans))
		chunk := plans[start:end]
		err := im.catalog.Transaction(func(tx *gorm.DB) error {
			for _, p := range chunk {
				switch p.kind {
				case "attach":
					if len(p.members) == 0 || p.workID == 0 {
						continue
					}
					n, err := im.writeDLGameAttach(tx, p, cnResolve, titleLoose, st)
					if err != nil {
						return err
					}
					st.Written += n
					hosts = append(hosts, p.workID)
					for _, m := range p.members {
						worknoToWork[m.workno] = p.workID
					}
				case "mint":
					if len(p.members) == 0 {
						continue
					}
					wid, n, err := im.writeDLGameMint(tx, p, cnResolve, st)
					if err != nil {
						return err
					}
					st.Written += n
					hosts = append(hosts, wid)
					for _, m := range p.members {
						worknoToWork[m.workno] = wid
					}
				}
			}
			return nil
		})
		if err != nil {
			st.Errors++
			return err
		}
	}

	src := dlsiteSource
	var edges []model.CatalogWorkLabel
	for workno, wid := range worknoToWork {
		p, ok := snap.byWorkno[workno]
		if !ok || p.makerExt == "" {
			continue
		}
		lid := labelAnchor[anchorKey(dlsiteSource, p.makerExt)]
		if lid == 0 {
			continue
		}
		edges = append(edges, model.CatalogWorkLabel{
			WorkID: wid, LabelID: lid, Kind: dlEdgeKind(makerKind[p.makerExt]), SourceID: &src,
		})
	}
	if len(edges) > 0 {
		touched, err := insertWorkLabelEdges(im.catalog, edges)
		if err != nil {
			return err
		}
		st.Written += len(touched)
		hosts = append(hosts, touched...)
	}
	return touchWorks(im.catalog, hosts)
}

func (im *Importer) writeDLGameAttach(tx *gorm.DB, p dlGamePlan, cnResolve func(string) (int64, bool), titleLoose map[int64]map[string]struct{}, st *DLsiteGamesStats) (int, error) {
	releases := make([]model.CatalogRelease, len(p.members))
	for i, m := range p.members {
		rel := model.CatalogRelease{
			WorkID: p.workID, Kind: model.ReleaseKindDigital,
			ReleasedY: m.y, ReleasedM: m.m, ReleasedD: m.d, Extra: datatypes.JSON(`{}`),
		}
		if m.lang != "" {
			lang := m.lang
			rel.Lang = &lang
		}
		releases[i] = rel
	}
	if err := tx.CreateInBatches(releases, 1000).Error; err != nil {
		return 0, err
	}
	written := len(releases)
	var titles []model.CatalogWorkTitle
	var refs []model.CatalogExternalRef
	var revs []model.CatalogRevision
	var credits []model.CatalogCredit
	for i, m := range p.members {
		rel := releases[i]
		refs = append(refs, selfRef(model.EntityTypeRelease, rel.ID, dlsiteSource, m.workno, p.rule))
		revs = append(revs, importedRev(model.EntityTypeRelease, rel.ID, releaseSnapshotJSON(rel)))
		loose := titlekey.Loose(m.name)
		if _, has := titleLoose[p.workID][loose]; m.name != "" && loose != "" && !has {
			titles = append(titles, model.CatalogWorkTitle{
				WorkID: p.workID, Lang: "ja", Title: m.name, Kind: model.WorkTitleKindSearchHint,
			})
			if titleLoose[p.workID] == nil {
				titleLoose[p.workID] = map[string]struct{}{}
			}
			titleLoose[p.workID][loose] = struct{}{}
		}
		credits = append(credits, buildDLGameCredits(p.workID, m.credits, cnResolve, st)...)
	}
	if len(titles) > 0 {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(titles, 1000).Error; err != nil {
			return written, err
		}
		written += len(titles)
	}
	if err := im.batchRefsRevs(tx, refs, revs); err != nil {
		return written, err
	}
	written += len(refs) + len(revs)
	n, err := im.insertCredits(tx, credits)
	if err != nil {
		return written, err
	}
	return written + n, nil
}

func (im *Importer) writeDLGameMint(tx *gorm.DB, p dlGamePlan, cnResolve func(string) (int64, bool), st *DLsiteGamesStats) (int64, int, error) {
	g := dlGameGroup{pop: p.members, worknos: make([]string, len(p.members))}
	for i, m := range p.members {
		g.worknos[i] = m.workno
	}
	primary := g.primaryMember()
	stripped := titlekey.Strip(primary.name)
	if stripped == "" {
		stripped = primary.name
	}
	status := model.WorkStatusLive
	if p.quarantine {
		status = model.WorkStatusQuarantine
	}
	work := model.CatalogWork{
		MediumID: mediumGalgame, OLang: model.OLangDefault, DisplayName: stripped,
		ContentRating: dlContentRating(primary.age), Status: status,
		Extra: datatypes.JSON(`{}`), FieldProvenance: datatypes.JSON(`{}`),
	}
	if err := tx.Create(&work).Error; err != nil {
		return 0, 0, err
	}
	written := 1
	wid := work.ID
	var titles []model.CatalogWorkTitle
	titles = append(titles, model.CatalogWorkTitle{WorkID: wid, Lang: "ja", Title: stripped, Kind: model.WorkTitleKindOfficial})
	if primary.name != stripped {
		titles = append(titles, model.CatalogWorkTitle{WorkID: wid, Lang: "ja", Title: primary.name, Kind: model.WorkTitleKindSearchHint})
	}
	if primary.kana != "" && primary.kana != stripped && primary.kana != primary.name {
		titles = append(titles, model.CatalogWorkTitle{WorkID: wid, Lang: "ja", Title: primary.kana, Kind: model.WorkTitleKindSearchHint})
	}
	looseHave := map[string]struct{}{}
	for _, t := range titles {
		if k := titlekey.Loose(t.Title); k != "" {
			looseHave[k] = struct{}{}
		}
	}
	releases := make([]model.CatalogRelease, len(p.members))
	var credits []model.CatalogCredit
	for i, m := range p.members {
		rel := model.CatalogRelease{
			WorkID: wid, Kind: model.ReleaseKindDigital,
			ReleasedY: m.y, ReleasedM: m.m, ReleasedD: m.d, Extra: datatypes.JSON(`{}`),
		}
		if m.lang != "" {
			lang := m.lang
			rel.Lang = &lang
		}
		releases[i] = rel
		if k := titlekey.Loose(m.name); m.name != "" && k != "" {
			if _, ok := looseHave[k]; !ok {
				titles = append(titles, model.CatalogWorkTitle{
					WorkID: wid, Lang: "ja", Title: m.name, Kind: model.WorkTitleKindSearchHint,
				})
				looseHave[k] = struct{}{}
			}
		}
		credits = append(credits, buildDLGameCredits(wid, m.credits, cnResolve, st)...)
	}
	if err := tx.CreateInBatches(titles, 1000).Error; err != nil {
		return wid, written, err
	}
	written += len(titles)
	if err := tx.CreateInBatches(releases, 1000).Error; err != nil {
		return wid, written, err
	}
	written += len(releases)
	var refs []model.CatalogExternalRef
	var revs []model.CatalogRevision
	revs = append(revs, importedRev(model.EntityTypeWork, wid, workSnapshotJSON(work, titles)))
	for i, m := range p.members {
		rel := releases[i]
		refs = append(refs, selfRef(model.EntityTypeRelease, rel.ID, dlsiteSource, m.workno, ruleDLsiteGameImport))
		revs = append(revs, importedRev(model.EntityTypeRelease, rel.ID, releaseSnapshotJSON(rel)))
	}
	if err := im.batchRefsRevs(tx, refs, revs); err != nil {
		return wid, written, err
	}
	written += len(refs) + len(revs)
	if p.quarantine && len(p.hits) > 0 {
		var cands []model.CatalogMatchCandidate
		for _, hit := range p.hits {
			a, b := wid, hit
			if b < a {
				a, b = b, a
			}
			cands = append(cands, model.CatalogMatchCandidate{
				EntityType: model.EntityTypeWork, AID: a, BID: b,
				Reason: model.CandidateReasonNameNormEqual,
				Status: model.CandidateStatusPending,
			})
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&cands).Error; err != nil {
			return wid, written, err
		}
		written += len(cands)
	}
	n, err := im.insertCredits(tx, credits)
	if err != nil {
		return wid, written, err
	}
	return wid, written + n, nil
}

func buildDLGameCredits(workID int64, plan []dlCredit, cnResolve func(string) (int64, bool), st *DLsiteGamesStats) []model.CatalogCredit {
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

func loadTitleLoose(db *gorm.DB, workIDs []int64) (map[int64]map[string]struct{}, error) {
	out := map[int64]map[string]struct{}{}
	if len(workIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		WorkID int64  `gorm:"column:work_id"`
		Title  string `gorm:"column:title"`
	}
	if err := db.Raw(`SELECT work_id, title FROM catalog_work_title WHERE work_id IN ?`, workIDs).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		k := titlekey.Loose(r.Title)
		if k == "" {
			continue
		}
		if out[r.WorkID] == nil {
			out[r.WorkID] = map[string]struct{}{}
		}
		out[r.WorkID][k] = struct{}{}
	}
	return out, nil
}

func WriteDLsiteGamesReceipts(path string, receipts []DLsiteGamesReceipt) error {
	if path == "" {
		return nil
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	sort.SliceStable(receipts, func(i, j int) bool {
		if receipts[i].Action != receipts[j].Action {
			return receipts[i].Action < receipts[j].Action
		}
		ai, aj := "", ""
		if len(receipts[i].Worknos) > 0 {
			ai = receipts[i].Worknos[0]
		}
		if len(receipts[j].Worknos) > 0 {
			aj = receipts[j].Worknos[0]
		}
		return ai < aj
	})
	for _, r := range receipts {
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	return nil
}
