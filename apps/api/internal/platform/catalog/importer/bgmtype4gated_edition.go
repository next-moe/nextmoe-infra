package importer

import (
	"fmt"
	"slices"
	"strconv"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const ruleBgmEditionGuard = "rule:bgm-edition-guard"

type BgmGatedEdition struct {
	SubjectID   int64
	Name        string
	NameCN      string
	MatchedNorm string
	WorkIDs     []int64
}

type editionAttach struct {
	subjectID int64
	workID    int64
}

type editionCorpus struct {
	releaseWorks map[string][]int64
	rejected     map[editionAttach]struct{}
}

func (ec editionCorpus) editionHit(r poolRow) (string, []int64) {
	var matched string
	var works []int64
	for _, norm := range []string{r.NameNorm, r.NameCNNorm} {
		key, ok := foldedGateKey(norm)
		if !ok {
			continue
		}
		for _, w := range ec.releaseWorks[key] {
			if _, no := ec.rejected[editionAttach{subjectID: r.ID, workID: w}]; no || slices.Contains(works, w) {
				continue
			}
			if matched == "" {
				matched = key
			}
			works = append(works, w)
		}
	}
	slices.Sort(works)
	return matched, works
}

func (im *Importer) loadEditionCorpus() (editionCorpus, error) {
	var titles []struct {
		Norm   string `gorm:"column:title_norm"`
		WorkID int64  `gorm:"column:work_id"`
	}
	if err := im.catalog.Raw(`
		SELECT DISTINCT lower(normalize(rel.title, NFKC)) AS title_norm, rel.work_id
		FROM catalog_release rel
		JOIN catalog_work w ON w.id = rel.work_id AND w.deleted_at IS NULL AND w.status = ? AND w.medium_id = ?
		WHERE rel.deleted_at IS NULL AND rel.title IS NOT NULL
		  AND EXISTS (SELECT 1 FROM catalog_external_ref r
			WHERE r.entity_type = ? AND r.entity_id = rel.id AND r.source_id = ? AND r.dead_at IS NULL)`,
		model.WorkStatusLive, mediumGalgame, model.EntityTypeRelease, vndbSource).Scan(&titles).Error; err != nil {
		return editionCorpus{}, fmt.Errorf("release titles: %w", err)
	}
	ec := editionCorpus{releaseWorks: make(map[string][]int64, len(titles)), rejected: map[editionAttach]struct{}{}}
	for _, t := range titles {
		key, ok := foldedGateKey(t.Norm)
		if !ok || slices.Contains(ec.releaseWorks[key], t.WorkID) {
			continue
		}
		ec.releaseWorks[key] = append(ec.releaseWorks[key], t.WorkID)
	}

	var rejections []struct {
		EntityID   int64  `gorm:"column:entity_id"`
		ExternalID string `gorm:"column:external_id"`
	}
	if err := im.catalog.Raw(`
		SELECT entity_id, external_id FROM catalog_match_rejection
		WHERE entity_type = ? AND source_id = ?`,
		model.EntityTypeWork, bangumiSource).Scan(&rejections).Error; err != nil {
		return editionCorpus{}, fmt.Errorf("rejections: %w", err)
	}
	for _, r := range rejections {
		if sid, err := strconv.ParseInt(r.ExternalID, 10, 64); err == nil {
			ec.rejected[editionAttach{subjectID: sid, workID: r.EntityID}] = struct{}{}
		}
	}
	return ec, nil
}

func (im *Importer) attachEditionRefs(attaches []editionAttach, st *BgmGatedStats) error {
	if len(attaches) == 0 {
		return nil
	}
	refs := make([]model.CatalogExternalRef, len(attaches))
	hosts := make([]int64, len(attaches))
	for i, a := range attaches {
		refs[i] = model.CatalogExternalRef{
			EntityType: model.EntityTypeWork, EntityID: a.workID, SourceID: bangumiSource,
			ExternalID: strconv.FormatInt(a.subjectID, 10),
			LinkKind:   model.LinkKindRelated, MatchedBy: ruleBgmEditionGuard,
		}
		hosts[i] = a.workID
	}
	return im.catalog.Transaction(func(tx *gorm.DB) error {
		res := tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(refs, bgmGatedChunk)
		if res.Error != nil {
			return res.Error
		}
		st.EditionRefsCreated += int(res.RowsAffected)
		return touchWorks(tx, hosts)
	})
}
