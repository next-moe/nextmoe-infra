package importer

import (
	"strconv"

	"api/internal/platform/catalog/model"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func xmediaMedium(stype, platform int) (int16, bool) {
	switch stype {
	case 2:
		return mediumAnime, true
	case 1:
		switch platform {
		case 1001:
			return mediumManga, true
		case 1002:
			return mediumNovel, true
		}
	}
	return 0, false
}

const (
	mediumManga int16 = 2
	mediumNovel int16 = 3
	mediumAnime int16 = 4
)

type XmediaStats struct {
	RegisteredAnime int
	RegisteredManga int
	RegisteredNovel int
	Edges           int
	EdgesWritten    int
	AlreadyEdge     int
	AlreadyWork     int
	SkippedPlatform int
	SkippedNoTitle  int
	SkippedSelf     int
	Errors          int
}

type xmediaSubject struct {
	sid      int64
	stype    int
	platform int
	name     string
	nameCN   string
	medium   int16
}

func (im *Importer) RunBangumiXmedia() (XmediaStats, error) {
	var st XmediaStats

	galgameAnchor, allAnchor, err := im.loadBangumiAnchorsByMedium()
	if err != nil {
		return st, err
	}

	var rows []struct {
		Subject int64 `gorm:"column:subject_id"`
		Related int64 `gorm:"column:related_subject_id"`
	}
	if err := im.catalog.Raw(`SELECT subject_id, related_subject_id
		FROM src_bangumi.subject_relation WHERE relation_type = 1`).Scan(&rows).Error; err != nil {
		return st, err
	}
	pairs := make(map[[2]int64]struct{})
	xSids := make(map[int64]struct{})
	consider := func(gW, xSid int64) {
		pairs[[2]int64{gW, xSid}] = struct{}{}
		xSids[xSid] = struct{}{}
	}
	for _, r := range rows {
		if gW, ok := galgameAnchor[r.Subject]; ok {
			consider(gW, r.Related)
		}
		if gW, ok := galgameAnchor[r.Related]; ok {
			consider(gW, r.Subject)
		}
	}

	subs, err := im.loadXmediaSubjects(xSids)
	if err != nil {
		return st, err
	}

	xWork := make(map[int64]int64, len(subs))
	var toRegister []xmediaSubject
	for _, s := range subs {
		if w, ok := allAnchor[s.sid]; ok {
			st.AlreadyWork++
			xWork[s.sid] = w
			continue
		}
		medium, ok := xmediaMedium(s.stype, s.platform)
		if !ok {
			st.SkippedPlatform++
			continue
		}
		if s.name == "" && s.nameCN == "" {
			st.SkippedNoTitle++
			continue
		}
		s.medium = medium
		toRegister = append(toRegister, s)
	}

	if !im.dryRun {
		if err := im.registerXmedia(toRegister, xWork, &st); err != nil {
			return st, err
		}
	} else {
		for i, s := range toRegister {
			countMedium(&st, s.medium)
			xWork[s.sid] = int64(-(i + 1))
		}
	}

	existing, err := im.loadExistingWorkRelations()
	if err != nil {
		return st, err
	}
	src := bangumiSource
	var edges []model.CatalogWorkRelation
	for p := range pairs {
		gW, xSid := p[0], p[1]
		xW, ok := xWork[xSid]
		if !ok {
			continue
		}
		// Work-dedup merges folded some registered stubs into the galgame
		// they adapt (红楼梦 31006 holds novel 123224 beside game 1179), so
		// both ends resolve to one work; on 2026-09-16 that row failed
		// chk_catalog_work_relation_distinct and took the whole batch with it.
		if xW == gW {
			st.SkippedSelf++
			continue
		}
		key := [3]int64{xW, gW, relAdaptationOf}
		if _, in := existing[key]; in {
			st.AlreadyEdge++
			continue
		}
		existing[key] = struct{}{}
		edges = append(edges, model.CatalogWorkRelation{
			AWorkID: xW, BWorkID: gW, RelationTypeID: relAdaptationOf, SourceID: &src,
		})
	}
	st.Edges = len(edges)
	if im.dryRun || len(edges) == 0 {
		return st, nil
	}
	res := im.catalog.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&edges, 1000)
	if res.Error != nil {
		return st, res.Error
	}
	st.EdgesWritten = int(res.RowsAffected)
	st.AlreadyEdge += st.Edges - st.EdgesWritten
	if err := touchWorks(im.catalog, relationEndpoints(edges)); err != nil {
		return st, err
	}
	return st, nil
}

func countMedium(st *XmediaStats, medium int16) {
	switch medium {
	case mediumAnime:
		st.RegisteredAnime++
	case mediumManga:
		st.RegisteredManga++
	case mediumNovel:
		st.RegisteredNovel++
	}
}

func (im *Importer) registerXmedia(subs []xmediaSubject, xWork map[int64]int64, st *XmediaStats) error {
	const chunk = 1000
	for start := 0; start < len(subs); start += chunk {
		end := min(start+chunk, len(subs))
		batch := subs[start:end]
		if err := im.catalog.Transaction(func(tx *gorm.DB) error {
			works := make([]model.CatalogWork, len(batch))
			for i, s := range batch {
				name := s.name
				if name == "" {
					name = s.nameCN
				}
				works[i] = model.CatalogWork{
					MediumID: s.medium, OLang: "ja", DisplayName: name,
					ContentRating: 0, Status: model.WorkStatusLive,
					Extra: datatypes.JSON(`{}`), FieldProvenance: datatypes.JSON(`{}`),
				}
			}
			if err := tx.CreateInBatches(works, 1000).Error; err != nil {
				return err
			}

			var titles []model.CatalogWorkTitle
			var refs []model.CatalogExternalRef
			var revs []model.CatalogRevision
			for i, s := range batch {
				wid := works[i].ID
				xWork[s.sid] = wid
				countMedium(st, s.medium)
				if s.name != "" {
					titles = append(titles, model.CatalogWorkTitle{WorkID: wid, Lang: "ja", Title: s.name, Kind: model.WorkTitleKindOfficial})
				}
				if s.nameCN != "" && s.nameCN != s.name {
					titles = append(titles, model.CatalogWorkTitle{WorkID: wid, Lang: "zh", Title: s.nameCN, Kind: model.WorkTitleKindOfficial})
				}
				refs = append(refs, selfRef(model.EntityTypeWork, wid, bangumiSource, strconv.FormatInt(s.sid, 10), ruleBangumiXmedia))
				revs = append(revs, importedRev(model.EntityTypeWork, wid, workSnapshotJSON(works[i], nil)))
			}
			if err := tx.CreateInBatches(titles, 1000).Error; err != nil {
				return err
			}
			return im.batchRefsRevs(tx, refs, revs)
		}); err != nil {
			return err
		}
	}
	return nil
}

func (im *Importer) loadBangumiAnchorsByMedium() (galgame, all map[int64]int64, err error) {
	var rows []struct {
		Ext      string `gorm:"column:external_id"`
		Wid      int64  `gorm:"column:entity_id"`
		Medium   int16  `gorm:"column:medium_id"`
		LinkKind int16  `gorm:"column:link_kind"`
	}
	if err := im.catalog.Raw(`SELECT r.external_id, r.entity_id, w.medium_id, r.link_kind
		FROM catalog_external_ref r JOIN catalog_work w ON w.id = r.entity_id
		WHERE r.source_id = ? AND r.entity_type = ?`,
		bangumiSource, model.EntityTypeWork).Scan(&rows).Error; err != nil {
		return nil, nil, err
	}
	galgame = make(map[int64]int64)
	all = make(map[int64]int64, len(rows))
	for _, r := range rows {
		sid, e := strconv.ParseInt(r.Ext, 10, 64)
		if e != nil {
			continue
		}
		if _, ok := all[sid]; !ok || r.LinkKind == model.LinkKindExact {
			all[sid] = r.Wid
		}
		if r.Medium == mediumGalgame && r.LinkKind == model.LinkKindExact {
			galgame[sid] = r.Wid
		}
	}
	return galgame, all, nil
}

func (im *Importer) loadXmediaSubjects(sids map[int64]struct{}) ([]xmediaSubject, error) {
	if len(sids) == 0 {
		return nil, nil
	}
	ids := make([]int64, 0, len(sids))
	for id := range sids {
		ids = append(ids, id)
	}
	var rows []struct {
		ID       int64  `gorm:"column:id"`
		Type     int    `gorm:"column:type"`
		Platform int    `gorm:"column:platform"`
		Name     string `gorm:"column:name"`
		NameCN   string `gorm:"column:name_cn"`
	}
	if err := im.catalog.Raw(`SELECT id, type, platform, name, name_cn
		FROM src_bangumi.subject WHERE id IN ? AND type IN (1,2)`, ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	subs := make([]xmediaSubject, len(rows))
	for i, r := range rows {
		subs[i] = xmediaSubject{sid: r.ID, stype: r.Type, platform: r.Platform, name: r.Name, nameCN: r.NameCN}
	}
	return subs, nil
}
