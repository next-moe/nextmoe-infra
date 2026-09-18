package llmsuggest

import (
	"sort"

	"api/internal/platform/catalog/dlsitecode"
	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

const (
	sourceIDBangumi int16 = 3
	sourceIDDLsite  int16 = 4
)

type declaredHit struct {
	subject string
	workno  string
}

type DeclaredIndex struct {
	declarers map[string]map[int64]struct{}
	anchors   map[string]map[int64]struct{}
	byWork    map[int64][]declaredHit
}

func attachDeclaredEvidence(db *gorm.DB, rows []QueueVerdict, out map[int64]pairEvidence) error {
	idx, err := LoadDeclaredIndex(db)
	if err != nil {
		return err
	}
	for _, r := range rows {
		sub, wn := idx.Hit(r.AID, r.BID)
		if sub == "" {
			continue
		}
		ev := out[r.ID]
		ev.DeclaredSubject = sub
		ev.DeclaredWorkno = wn
		out[r.ID] = ev
	}
	return nil
}

// A Bangumi subject's infobox_raw often names a DLsite workno that is not an
// external ref. For worknos already anchored to a work by an exact DLsite
// release ref, the work holding the naming subject's exact Bangumi ref is that
// work 9,384 of 10,175 times (measured 2026-09-18); a 30-row sample of the
// other 791 was the same game held twice.
func LoadDeclaredIndex(db *gorm.DB) (*DeclaredIndex, error) {
	idx := &DeclaredIndex{
		declarers: map[string]map[int64]struct{}{},
		anchors:   map[string]map[int64]struct{}{},
		byWork:    map[int64][]declaredHit{},
	}
	var decls []struct {
		WorkID     int64  `gorm:"column:work_id"`
		ExternalID string `gorm:"column:external_id"`
		Infobox    string `gorm:"column:infobox"`
	}
	if err := db.Raw(`
		SELECT r.entity_id AS work_id, r.external_id, COALESCE(s.infobox_raw, '') AS infobox
		FROM catalog_external_ref r
		JOIN catalog_work w ON w.id = r.entity_id AND w.deleted_at IS NULL
		LEFT JOIN src_bangumi.subject s ON s.id::text = r.external_id
		WHERE r.entity_type = ? AND r.source_id = ? AND r.link_kind = ? AND r.dead_at IS NULL`,
		model.EntityTypeWork, sourceIDBangumi, model.LinkKindExact).Scan(&decls).Error; err != nil {
		return nil, err
	}
	for _, d := range decls {
		for _, wn := range dlsitecode.Worknos(d.Infobox) {
			addKeyHolder(idx.declarers, wn, d.WorkID)
			idx.byWork[d.WorkID] = append(idx.byWork[d.WorkID], declaredHit{subject: d.ExternalID, workno: wn})
		}
	}

	var ancs []struct {
		WorkID     int64  `gorm:"column:work_id"`
		ExternalID string `gorm:"column:external_id"`
	}
	if err := db.Raw(`
		SELECT rel.work_id, r.external_id
		FROM catalog_external_ref r
		JOIN catalog_release rel ON rel.id = r.entity_id AND rel.deleted_at IS NULL
		JOIN catalog_work w ON w.id = rel.work_id AND w.deleted_at IS NULL
		WHERE r.entity_type = ? AND r.source_id = ? AND r.link_kind = ? AND r.dead_at IS NULL`,
		model.EntityTypeRelease, sourceIDDLsite, model.LinkKindExact).Scan(&ancs).Error; err != nil {
		return nil, err
	}
	for _, a := range ancs {
		for _, wn := range dlsitecode.Worknos(a.ExternalID) {
			addKeyHolder(idx.anchors, wn, a.WorkID)
		}
	}
	return idx, nil
}

func (idx *DeclaredIndex) Hit(a, b int64) (subject, workno string) {
	hits := idx.directedHits(a, b)
	hits = append(hits, idx.directedHits(b, a)...)
	if len(hits) == 0 {
		return "", ""
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].subject != hits[j].subject {
			return hits[i].subject < hits[j].subject
		}
		return hits[i].workno < hits[j].workno
	})
	return hits[0].subject, hits[0].workno
}

func (idx *DeclaredIndex) directedHits(declarer, anchor int64) []declaredHit {
	var out []declaredHit
	for _, h := range idx.byWork[declarer] {
		if len(idx.declarers[h.workno]) != 1 || len(idx.anchors[h.workno]) != 1 {
			continue
		}
		if _, ok := idx.anchors[h.workno][anchor]; !ok {
			continue
		}
		out = append(out, h)
	}
	return out
}

func (idx *DeclaredIndex) ExclusiveWorkPairs() [][2]int64 {
	seen := map[[2]int64]struct{}{}
	var out [][2]int64
	for wn, ds := range idx.declarers {
		if len(ds) != 1 || len(idx.anchors[wn]) != 1 {
			continue
		}
		var d, a int64
		for id := range ds {
			d = id
		}
		for id := range idx.anchors[wn] {
			a = id
		}
		if d == a {
			continue
		}
		lo, hi := d, a
		if lo > hi {
			lo, hi = hi, lo
		}
		k := [2]int64{lo, hi}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	return out
}
