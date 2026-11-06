package importer

import (
	"api/internal/platform/catalog/model"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	ruleVNDBWork        = "rule:vndb-work-import"
	vndbWorksChunk      = 500
	vndbLangEnglish     = "en"
	vndbCollisionSample = 20
)

const (
	vndbDevstatusFinished int16 = 0
	vndbDevstatusInDev    int16 = 1
)

type VNDBWorkPlan struct {
	VID            string
	OLang          string
	Title          string
	ENTitle        string
	R18            bool
	CollidedWorkID int64
}

type VNDBWorkCollision struct {
	VID       string
	Title     string
	WorkID    int64
	WorkTitle string
}

type VNDBWorksStats struct {
	PoolUnanchored      int
	SkippedCancelled    int
	SkippedNoOLangTitle int
	Planned             int
	PlannedR18          int
	CappedByLimit       int
	TitleCollisions     int
	ToQuarantine        int
	Quarantined         int
	ScreenQuarantined   int

	WorksCreated     int
	TitlesCreated    int
	AnchorsCreated   int
	RevisionsCreated int
	Errors           int

	Plans            []VNDBWorkPlan
	CollisionSamples []VNDBWorkCollision
}

type vndbWorkRow struct {
	VID         string `gorm:"column:vid"`
	OLang       string `gorm:"column:olang"`
	Devstatus   int16  `gorm:"column:devstatus"`
	Title       string `gorm:"column:title"`
	ENTitle     string `gorm:"column:en_title"`
	TitleNorm   string `gorm:"column:title_norm"`
	ENTitleNorm string `gorm:"column:en_title_norm"`
	R18         bool   `gorm:"column:r18"`
}

// buildVNDBWorkPoolQuery lists every VN that no catalog_external_ref points at,
// at ANY link_kind. Reading only exact anchors would be the resurrection bug the
// roster tripwire exists for: a merge demotes both surviving exact anchors to
// probable, and a vid whose anchor was demoted would look unanchored and be
// re-minted as the work the merge just folded away. A ref under a soft-deleted
// work blocks for the same reason — retiring a work is a decision, not an
// invitation to re-create it — and dead_at is not consulted for the third: an
// upstream deletion does not free the vid, it only marks the link.
//
// r18 asks the releases, not the VN: vn carries no age rating at all. Patch
// releases are excluded because an 18+ patch for an all-ages VN is exactly the
// case where the release's rating is not the work's.
func buildVNDBWorkPoolQuery() string {
	return `SELECT v.id AS vid, v.olang AS olang, v.devstatus AS devstatus,
			coalesce(t.title, '') AS title,
			coalesce(en.title, '') AS en_title,
			coalesce(lower(normalize(t.title, NFKC)), '') AS title_norm,
			coalesce(lower(normalize(en.title, NFKC)), '') AS en_title_norm,
			EXISTS (SELECT 1 FROM src_vndb.releases_vn rv
				JOIN src_vndb.releases r ON r.id = rv.id
				WHERE rv.vid = v.id AND r.patch = false
					AND (r.minage >= 18 OR r.has_ero)) AS r18
		FROM src_vndb.vn v
		LEFT JOIN src_vndb.vn_titles t ON t.id = v.id AND t.lang = v.olang
		LEFT JOIN src_vndb.vn_titles en ON en.id = v.id AND en.lang = '` + vndbLangEnglish + `' AND en.official
		WHERE NOT EXISTS (SELECT 1 FROM catalog_external_ref e
			WHERE e.entity_type = ? AND e.source_id = ? AND e.external_id = v.id)
		ORDER BY nullif(regexp_replace(v.id, '^v', ''), '')::bigint`
}

func (im *Importer) RunVNDBWorks() (VNDBWorksStats, error) {
	var st VNDBWorksStats

	wt, err := im.loadExistingWorkTitleNorms()
	if err != nil {
		return st, err
	}

	var pool []vndbWorkRow
	if err := im.catalog.Raw(buildVNDBWorkPoolQuery(), model.EntityTypeWork, vndbSource).Scan(&pool).Error; err != nil {
		return st, err
	}
	st.PoolUnanchored = len(pool)

	for _, r := range pool {
		if r.Devstatus != vndbDevstatusFinished && r.Devstatus != vndbDevstatusInDev {
			st.SkippedCancelled++
			continue
		}
		if r.Title == "" {
			st.SkippedNoOLangTitle++
			continue
		}
		p := VNDBWorkPlan{VID: r.VID, OLang: r.OLang, Title: r.Title, R18: r.R18}
		if r.OLang != vndbLangEnglish && r.ENTitle != "" {
			p.ENTitle = r.ENTitle
		}
		if _, w, ok := firstCorpusHit(wt, r.TitleNorm, r.ENTitleNorm); ok {
			p.CollidedWorkID = w.workID
			st.TitleCollisions++
			if len(st.CollisionSamples) < vndbCollisionSample {
				st.CollisionSamples = append(st.CollisionSamples, VNDBWorkCollision{
					VID: r.VID, Title: r.Title, WorkID: w.workID, WorkTitle: w.title,
				})
			}
		}
		st.Plans = append(st.Plans, p)
	}
	if im.limit > 0 && im.limit < len(st.Plans) {
		st.CappedByLimit = len(st.Plans) - im.limit
		st.Plans = st.Plans[:im.limit]
	}
	st.Planned = len(st.Plans)
	for _, p := range st.Plans {
		if p.R18 {
			st.PlannedR18++
		}
		if p.CollidedWorkID != 0 {
			st.ToQuarantine++
		}
	}

	if im.dryRun {
		st.WorksCreated = st.Planned
		st.AnchorsCreated = st.Planned
		st.RevisionsCreated = st.Planned
		st.TitlesCreated = countVNDBWorkTitles(st.Plans)
		return st, nil
	}
	return st, im.createVNDBWorks(st.Plans, &st)
}

func countVNDBWorkTitles(plans []VNDBWorkPlan) int {
	n := 0
	for _, p := range plans {
		n++
		if p.ENTitle != "" {
			n++
		}
	}
	return n
}

func (im *Importer) createVNDBWorks(plans []VNDBWorkPlan, st *VNDBWorksStats) error {
	for start := 0; start < len(plans); start += vndbWorksChunk {
		end := min(start+vndbWorksChunk, len(plans))
		chunk := plans[start:end]
		if err := im.catalog.Transaction(func(tx *gorm.DB) error {
			return im.createVNDBWorkChunk(tx, chunk, st)
		}); err != nil {
			return err
		}
	}
	return nil
}

func (im *Importer) createVNDBWorkChunk(tx *gorm.DB, chunk []VNDBWorkPlan, st *VNDBWorksStats) error {
	works := make([]model.CatalogWork, len(chunk))
	for i, p := range chunk {
		cr := model.ContentRatingAllAges
		if p.R18 {
			cr = model.ContentRatingR18
		}
		status := model.WorkStatusLive
		if p.CollidedWorkID != 0 {
			status = model.WorkStatusQuarantine
		}
		works[i] = model.CatalogWork{
			MediumID: mediumGalgame, OLang: p.OLang, DisplayName: p.Title,
			ContentRating: cr, Status: status,
			Extra: datatypes.JSON(`{}`), FieldProvenance: datatypes.JSON(`{}`),
		}
	}
	if err := tx.CreateInBatches(works, vndbWorksChunk).Error; err != nil {
		return err
	}

	var titles []model.CatalogWorkTitle
	for i, p := range chunk {
		wid := works[i].ID
		titles = append(titles, model.CatalogWorkTitle{
			WorkID: wid, Lang: p.OLang, Title: p.Title,
			Kind: model.WorkTitleKindOfficial, Provenance: model.WorkTitleProvenanceSource,
		})
		if p.ENTitle != "" {
			titles = append(titles, model.CatalogWorkTitle{
				WorkID: wid, Lang: vndbLangEnglish, Title: p.ENTitle,
				Kind: model.WorkTitleKindOfficial, Provenance: model.WorkTitleProvenanceSource,
			})
		}
	}
	if err := tx.CreateInBatches(titles, vndbWorksChunk).Error; err != nil {
		return err
	}
	titlesByWork := make(map[int64][]model.CatalogWorkTitle, len(chunk))
	for _, t := range titles {
		titlesByWork[t.WorkID] = append(titlesByWork[t.WorkID], t)
	}

	refs := make([]model.CatalogExternalRef, len(chunk))
	revs := make([]model.CatalogRevision, len(chunk))
	for i, p := range chunk {
		wid := works[i].ID
		refs[i] = selfRef(model.EntityTypeWork, wid, vndbSource, p.VID, ruleVNDBWork)
		revs[i] = importedRev(model.EntityTypeWork, wid, workSnapshotJSON(works[i], titlesByWork[wid]))
	}
	if err := im.batchRefsRevs(tx, refs, revs); err != nil {
		return err
	}
	screened, err := screenMinted(tx, works)
	if err != nil {
		return err
	}
	st.ScreenQuarantined += screened

	var cands []model.CatalogMatchCandidate
	for i, p := range chunk {
		if p.CollidedWorkID == 0 {
			continue
		}
		a, b := works[i].ID, p.CollidedWorkID
		if b < a {
			a, b = b, a
		}
		cands = append(cands, model.CatalogMatchCandidate{
			EntityType: model.EntityTypeWork, AID: a, BID: b,
			Reason: model.CandidateReasonNameNormEqual,
			Status: model.CandidateStatusPending,
		})
		st.Quarantined++
	}
	if len(cands) > 0 {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&cands).Error; err != nil {
			return err
		}
	}

	st.WorksCreated += len(works)
	st.TitlesCreated += len(titles)
	st.AnchorsCreated += len(refs)
	st.RevisionsCreated += len(revs)
	return nil
}
