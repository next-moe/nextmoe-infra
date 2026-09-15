package llmsuggest

import (
	"sort"
	"strconv"
	"strings"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

func loadWorkSides(db *gorm.DB, ids []int64) (map[int64]workSideDossier, error) {
	out := map[int64]workSideDossier{}
	if len(ids) == 0 {
		return out, nil
	}
	for _, chunk := range chunkBy(ids, 500) {
		var works []struct {
			ID            int64   `gorm:"column:id"`
			DisplayName   string  `gorm:"column:display_name"`
			OLang         string  `gorm:"column:olang"`
			ContentRating int16   `gorm:"column:content_rating"`
			Site          *string `gorm:"column:site"`
			ClaimState    *int16  `gorm:"column:claim_state"`
			MediumKey     string  `gorm:"column:medium_key"`
		}
		if err := db.Raw(`SELECT w.id, w.display_name, w.olang, w.content_rating, w.site, w.claim_state, m.key AS medium_key
			FROM catalog_work w JOIN catalog_medium m ON m.id = w.medium_id
			WHERE w.id IN ?`, chunk).Scan(&works).Error; err != nil {
			return nil, err
		}
		for _, w := range works {
			side := workSideDossier{
				ID: w.ID, DisplayName: w.DisplayName, MediumKey: w.MediumKey,
				OLang: w.OLang, ContentRating: w.ContentRating,
				Titles: []workTitleEv{}, Refs: []string{}, Labels: []string{},
			}
			if siteClaimed(w.Site) {
				side.Site = derefStr(w.Site)
				side.ClaimState = w.ClaimState
			}
			out[w.ID] = side
		}

		var titles []struct {
			WorkID int64  `gorm:"column:work_id"`
			Title  string `gorm:"column:title"`
			Lang   string `gorm:"column:lang"`
			Kind   int16  `gorm:"column:kind"`
			Norm   string `gorm:"column:title_norm"`
		}
		if err := db.Raw(`SELECT work_id, title, lang, kind, title_norm
			FROM catalog_work_title WHERE work_id IN ? ORDER BY work_id, kind, id`, chunk).
			Scan(&titles).Error; err != nil {
			return nil, err
		}
		titleN := map[int64]int{}
		for _, t := range titles {
			if titleN[t.WorkID] >= 5 {
				continue
			}
			side := out[t.WorkID]
			side.Titles = append(side.Titles, workTitleEv{
				Title: t.Title, Lang: t.Lang, Official: t.Kind == model.WorkTitleKindOfficial, Norm: t.Norm,
			})
			titleN[t.WorkID]++
			out[t.WorkID] = side
		}

		var years []struct {
			WorkID int64 `gorm:"column:work_id"`
			Y      int   `gorm:"column:y"`
		}
		if err := db.Raw(`SELECT work_id, min(released_y) AS y FROM catalog_release
			WHERE deleted_at IS NULL AND released_y IS NOT NULL AND work_id IN ?
			GROUP BY work_id`, chunk).Scan(&years).Error; err != nil {
			return nil, err
		}
		for _, y := range years {
			side := out[y.WorkID]
			yy := y.Y
			side.MinReleaseYear = &yy
			out[y.WorkID] = side
		}

		var refs []struct {
			EntityID   int64  `gorm:"column:entity_id"`
			SourceKey  string `gorm:"column:source_key"`
			ExternalID string `gorm:"column:external_id"`
		}
		if err := db.Raw(`SELECT r.entity_id, s.key AS source_key, r.external_id
			FROM catalog_external_ref r
			JOIN catalog_source s ON s.id = r.source_id
			WHERE r.entity_type = ? AND r.link_kind IN (?, ?) AND r.dead_at IS NULL AND r.entity_id IN ?
			ORDER BY r.entity_id, r.link_kind, s.key, r.external_id`,
			model.EntityTypeWork, model.LinkKindExact, model.LinkKindProbable, chunk).
			Scan(&refs).Error; err != nil {
			return nil, err
		}
		refN := map[int64]int{}
		for _, r := range refs {
			if refN[r.EntityID] >= 8 {
				continue
			}
			side := out[r.EntityID]
			side.Refs = append(side.Refs, r.SourceKey+":"+r.ExternalID)
			refN[r.EntityID]++
			out[r.EntityID] = side
		}

		var labels []struct {
			WorkID int64  `gorm:"column:work_id"`
			Name   string `gorm:"column:display_name"`
		}
		if err := db.Raw(`SELECT wl.work_id, l.display_name
			FROM catalog_work_label wl
			JOIN catalog_label l ON l.id = wl.label_id AND l.deleted_at IS NULL
			WHERE wl.work_id IN ? ORDER BY wl.work_id, l.display_name`, chunk).
			Scan(&labels).Error; err != nil {
			return nil, err
		}
		labN := map[int64]int{}
		seenLab := map[string]struct{}{}
		for _, l := range labels {
			key := strconvWorkLabel(l.WorkID, l.Name)
			if _, ok := seenLab[key]; ok {
				continue
			}
			seenLab[key] = struct{}{}
			if labN[l.WorkID] >= 3 {
				continue
			}
			side := out[l.WorkID]
			side.Labels = append(side.Labels, l.Name)
			labN[l.WorkID]++
			out[l.WorkID] = side
		}
	}
	return out, nil
}

func strconvWorkLabel(id int64, name string) string {
	return strconv.FormatInt(id, 10) + "\x00" + name
}

// refKey is the identifier as the table stores it. Keeping source_id and
// external_id apart rather than folding them into one "key:id" string is what
// lets the fan-out count use idx_catalog_external_ref_source_ext; filtering on
// the concatenation instead reads all 1,221,512 rows, every night, for nothing.
type refKey struct {
	SourceID   int16
	ExternalID string
	Display    string
}

// loadSharedRefFanOut answers, for every pair, which identifiers both sides
// hold and how many live works hold each one.
//
// It reads link_kind 2 as well, which the dossier's own ref list does not.
// Related is where official_site lives - 60,543 rows - and the partial unique
// uq_catalog_external_ref_exact only covers link_kind 0, so a shared identifier
// is structurally expressible there and nowhere else. Leaving it out is why the
// model kept writing "no shared refs" about pairs that share an exclusive
// product page: it was never shown one.
//
// The fan-out is the whole point. Measured over the 693 undecided pairs on
// 2026-09-15: 131 share an identifier no other live work holds, while
// twitter:frontwingint and youtube.com/@frontwing_1999 are each held by 43
// works. Both look identical in this table without the count.
//
// Intersecting here rather than over workSideDossier.Refs is deliberate: that
// list is truncated to 8 for the token budget, and a shared identifier dropped
// by the cap would silently read as no shared identifier at all.
func loadSharedRefFanOut(db *gorm.DB, pairs [][2]int64) (map[[2]int64][]sharedRefEv, error) {
	out := map[[2]int64][]sharedRefEv{}
	if len(pairs) == 0 {
		return out, nil
	}
	ids := make([]int64, 0, len(pairs)*2)
	seen := map[int64]struct{}{}
	for _, p := range pairs {
		for _, id := range p {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}

	held := map[int64]map[string]refKey{}
	for _, chunk := range chunkBy(ids, 500) {
		var rows []struct {
			EntityID   int64  `gorm:"column:entity_id"`
			SourceID   int16  `gorm:"column:source_id"`
			SourceKey  string `gorm:"column:source_key"`
			ExternalID string `gorm:"column:external_id"`
		}
		if err := db.Raw(`SELECT r.entity_id, r.source_id, s.key AS source_key, r.external_id
			FROM catalog_external_ref r JOIN catalog_source s ON s.id = r.source_id
			WHERE r.entity_type = ? AND r.dead_at IS NULL AND r.entity_id IN ?`,
			model.EntityTypeWork, chunk).Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			if held[r.EntityID] == nil {
				held[r.EntityID] = map[string]refKey{}
			}
			display := r.SourceKey + ":" + r.ExternalID
			held[r.EntityID][display] = refKey{SourceID: r.SourceID, ExternalID: r.ExternalID, Display: display}
		}
	}

	shared := map[[2]int64][]refKey{}
	keys := map[string]refKey{}
	for _, p := range pairs {
		for display, k := range held[p[0]] {
			if _, ok := held[p[1]][display]; !ok {
				continue
			}
			shared[p] = append(shared[p], k)
			keys[display] = k
		}
	}
	if len(keys) == 0 {
		return out, nil
	}
	flat := make([]refKey, 0, len(keys))
	for _, k := range keys {
		flat = append(flat, k)
	}

	fan := map[string]int{}
	for _, chunk := range chunkBy(flat, 500) {
		ph := make([]string, 0, len(chunk))
		args := []any{model.EntityTypeWork}
		for _, k := range chunk {
			ph = append(ph, "(?,?)")
			args = append(args, k.SourceID, k.ExternalID)
		}
		var rows []struct {
			SourceID   int16  `gorm:"column:source_id"`
			ExternalID string `gorm:"column:external_id"`
			N          int    `gorm:"column:n"`
		}
		if err := db.Raw(`SELECT r.source_id, r.external_id, count(DISTINCT r.entity_id) AS n
			FROM catalog_external_ref r
			JOIN catalog_work w ON w.id = r.entity_id AND w.deleted_at IS NULL
			WHERE r.entity_type = ? AND r.dead_at IS NULL
			  AND (r.source_id, r.external_id) IN (`+strings.Join(ph, ",")+`)
			GROUP BY 1, 2`, args...).Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			fan[strconv.FormatInt(int64(r.SourceID), 10)+":"+r.ExternalID] = r.N
		}
	}
	for p, ks := range shared {
		sort.Slice(ks, func(i, j int) bool { return ks[i].Display < ks[j].Display })
		evs := make([]sharedRefEv, 0, len(ks))
		for _, k := range capN(ks, 8) {
			evs = append(evs, sharedRefEv{
				Ref:          k.Display,
				WorksHolding: fan[strconv.FormatInt(int64(k.SourceID), 10)+":"+k.ExternalID],
			})
		}
		out[p] = evs
	}
	return out, nil
}
