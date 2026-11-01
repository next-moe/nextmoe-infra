package llmsuggest

import (
	"sort"
	"strings"
	"time"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/service"

	"gorm.io/gorm"
)

// pairDateNearDays is work-dedup's dateNearDays: the two must agree on what
// counts as one edition dated by two sources.
const pairDateNearDays = 62

var storeRecordSourceIDs = []int16{8, 15}

// The two corroborators below stand on a shared fact plus a release date both
// sides agree on. Neither fact is enough alone: ラブレッスン (1999) and Lessons
// in Love (2022) are the sole holders of a shared title, and a base game and
// its DLC set can share one store id.
func (e pairEvidence) exclusiveTitle() bool {
	return e.DatesNear && e.TitleName != "" && e.TitleHolders == exclusiveNameHolders
}

func (e pairEvidence) exclusiveCrossStore() bool {
	return e.DatesNear && e.StoreSourceKey != "" && e.StoreID != "" && e.StoreHolders == exclusiveNameHolders
}

func attachDatedEvidence(db *gorm.DB, rows []QueueVerdict, out map[int64]pairEvidence) error {
	ids := verdictWorkIDs(rows)
	if len(ids) == 0 {
		return nil
	}
	dates, err := loadPairDates(db, ids)
	if err != nil {
		return err
	}
	titles, err := sharedTitleHolders(db, rows)
	if err != nil {
		return err
	}
	stores, err := crossStoreHolders(db, rows)
	if err != nil {
		return err
	}
	for _, r := range rows {
		ev := out[r.ID]
		ev.DatesNear = datesWithin(dates[r.AID], dates[r.BID], pairDateNearDays)
		if t, ok := titles[[2]int64{r.AID, r.BID}]; ok {
			ev.TitleName, ev.TitleHolders = t.n, t.holders
		}
		if s, ok := stores[[2]int64{r.AID, r.BID}]; ok {
			ev.StoreSourceKey, ev.StoreID, ev.StoreHolders = s.sourceKey, s.ext, s.holders
		}
		out[r.ID] = ev
	}
	return nil
}

func datesWithin(a, b *time.Time, days int) bool {
	if a == nil || b == nil {
		return false
	}
	d := a.Sub(*b)
	if d < 0 {
		d = -d
	}
	return d <= time.Duration(days)*24*time.Hour
}

func loadPairDates(db *gorm.DB, ids []int64) (map[int64]*time.Time, error) {
	out := map[int64]*time.Time{}
	for _, chunk := range chunkBy(ids, 500) {
		var found []struct {
			WorkID int64      `gorm:"column:work_id"`
			D      *time.Time `gorm:"column:d"`
		}
		if err := db.Raw(`
			WITH rd AS (
				SELECT work_id, min(make_date(released_y, released_m, coalesce(nullif(released_d, 0), 1))) AS d
				FROM catalog_release
				WHERE deleted_at IS NULL AND released_y IS NOT NULL AND released_m BETWEEN 1 AND 12 AND work_id IN ?
				GROUP BY work_id
			),
			bd AS (
				SELECT r.entity_id AS work_id, min(s.date::date) AS d
				FROM catalog_external_ref r
				JOIN src_bangumi.subject s ON s.id = r.external_id::bigint
				WHERE r.entity_type = ? AND r.link_kind = ? AND r.dead_at IS NULL AND r.source_id = 3
				  AND r.entity_id IN ? AND s.date ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$'
				GROUP BY r.entity_id
			)
			SELECT w.id AS work_id, coalesce(rd.d, bd.d) AS d
			FROM catalog_work w
			LEFT JOIN rd ON rd.work_id = w.id
			LEFT JOIN bd ON bd.work_id = w.id
			WHERE w.id IN ?`,
			chunk, model.EntityTypeWork, model.LinkKindExact, chunk, chunk).Scan(&found).Error; err != nil {
			return nil, err
		}
		for _, f := range found {
			out[f.WorkID] = f.D
		}
	}
	return out, nil
}

type titleHit struct {
	n       string
	holders int
}

// sharedTitleHolders finds a source title of one work that the other also
// carries, official on at least one side. 61790 and 273420 were one game: the
// bangumi mint's display name was an alias of 61790, which the display-only
// name gate cannot see. Machine titles are left out of the pair side because
// they collide by translation (16532 and 27910 both carry a machine 恋爱课程), and
// kept in the holder count, which only makes exclusivity harder to reach.
func sharedTitleHolders(db *gorm.DB, rows []QueueVerdict) (map[[2]int64]titleHit, error) {
	vals, args := pairValues(rows)
	if len(vals) == 0 {
		return nil, nil
	}
	ids := verdictWorkIDs(rows)
	foldTitle := service.WorkTitleFoldSQL("t.title_norm")
	foldDisplay := service.WorkTitleFoldSQL("lower(normalize(w.display_name, NFKC))")
	args = append(args, ids, model.WorkTitleProvenanceSource, ids)
	var found []struct {
		AID     int64  `gorm:"column:a_id"`
		BID     int64  `gorm:"column:b_id"`
		N       string `gorm:"column:n"`
		Holders int    `gorm:"column:holders"`
	}
	if err := db.Raw(`
		WITH pair(a_id, b_id) AS (VALUES `+strings.Join(vals, ", ")+`),
		own AS (
			SELECT t.work_id, `+foldTitle+` AS n, (t.kind = 0) AS official
			FROM catalog_work_title t
			WHERE t.work_id IN ? AND t.kind IN (0, 1) AND t.provenance = ?
			  AND `+service.WorkDupeNormEligibleSQL(foldTitle)+`
			  AND `+editspec.NotSuppressedWorkTitleSQL("t")+`
			UNION
			SELECT w.id, `+foldDisplay+`, true
			FROM catalog_work w
			WHERE w.id IN ? AND w.deleted_at IS NULL AND `+service.WorkDupeNormEligibleSQL(foldDisplay)+`
		),
		shared AS (
			SELECT p.a_id, p.b_id, oa.n
			FROM pair p
			JOIN own oa ON oa.work_id = p.a_id
			JOIN own ob ON ob.work_id = p.b_id AND ob.n = oa.n
			GROUP BY p.a_id, p.b_id, oa.n
			HAVING bool_or(oa.official OR ob.official)
		),
		corpus AS (`+service.WorkDupeCorpusSQL()+`)
		SELECT s.a_id, s.b_id, s.n, count(DISTINCT c.work_id) AS holders
		FROM shared s JOIN corpus c ON c.n = s.n
		GROUP BY 1, 2, 3`, args...).Scan(&found).Error; err != nil {
		return nil, err
	}
	sort.Slice(found, func(i, j int) bool { return found[i].N < found[j].N })
	out := map[[2]int64]titleHit{}
	for _, f := range found {
		k := [2]int64{f.AID, f.BID}
		if _, done := out[k]; done || f.Holders != exclusiveNameHolders {
			continue
		}
		out[k] = titleHit{n: f.N, holders: f.Holders}
	}
	return out, nil
}

type storeHit struct {
	sourceKey string
	ext       string
	holders   int
}

// crossStoreHolders finds a store id one work holds on a release and the other
// on the work itself. Two works holding it at the same grain stay out: two EG
// edition entries naming one DMM product derive the same id twice from one
// registry, while a release-grain id comes from VNDB and a work-grain one from
// EG or Bangumi. 6253 (VNDB) and 217311 (EG) both name dmm hobc_0507 that way.
func crossStoreHolders(db *gorm.DB, rows []QueueVerdict) (map[[2]int64]storeHit, error) {
	vals, args := pairValues(rows)
	if len(vals) == 0 {
		return nil, nil
	}
	args = append(args,
		model.EntityTypeWork, storeRecordSourceIDs,
		model.EntityTypeRelease, model.LinkKindExact, storeRecordSourceIDs)
	var found []struct {
		AID       int64  `gorm:"column:a_id"`
		BID       int64  `gorm:"column:b_id"`
		SourceKey string `gorm:"column:source_key"`
		Ext       string `gorm:"column:ext"`
		Holders   int    `gorm:"column:holders"`
	}
	if err := db.Raw(`
		WITH pair(a_id, b_id) AS (VALUES `+strings.Join(vals, ", ")+`),
		held AS (
			SELECT r.entity_id AS work_id, r.source_id, lower(r.external_id) AS ext, false AS on_release
			FROM catalog_external_ref r
			WHERE r.entity_type = ? AND r.dead_at IS NULL AND r.link_kind IN (0, 1) AND r.source_id IN ?
			UNION
			SELECT rel.work_id, r.source_id, lower(r.external_id), true
			FROM catalog_external_ref r
			JOIN catalog_release rel ON rel.id = r.entity_id AND rel.deleted_at IS NULL
			WHERE r.entity_type = ? AND r.dead_at IS NULL AND r.link_kind = ? AND r.source_id IN ?
		),
		live AS (
			SELECT h.* FROM held h JOIN catalog_work w ON w.id = h.work_id AND w.deleted_at IS NULL
		),
		shared AS (
			SELECT DISTINCT p.a_id, p.b_id, ha.source_id, ha.ext
			FROM pair p
			JOIN live ha ON ha.work_id = p.a_id
			JOIN live hb ON hb.work_id = p.b_id AND hb.source_id = ha.source_id AND hb.ext = ha.ext
			  AND hb.on_release <> ha.on_release
		)
		SELECT s.a_id, s.b_id, cs.key AS source_key, s.ext, count(DISTINCT l.work_id) AS holders
		FROM shared s
		JOIN live l ON l.source_id = s.source_id AND l.ext = s.ext
		JOIN catalog_source cs ON cs.id = s.source_id
		GROUP BY 1, 2, 3, 4`, args...).Scan(&found).Error; err != nil {
		return nil, err
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].SourceKey != found[j].SourceKey {
			return found[i].SourceKey < found[j].SourceKey
		}
		return found[i].Ext < found[j].Ext
	})
	out := map[[2]int64]storeHit{}
	for _, f := range found {
		k := [2]int64{f.AID, f.BID}
		if _, done := out[k]; done || f.Holders != exclusiveNameHolders {
			continue
		}
		out[k] = storeHit{sourceKey: f.SourceKey, ext: f.Ext, holders: f.Holders}
	}
	return out, nil
}

func pairValues(rows []QueueVerdict) ([]string, []any) {
	seen := map[[2]int64]struct{}{}
	var vals []string
	var args []any
	for _, r := range rows {
		k := [2]int64{r.AID, r.BID}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		vals = append(vals, "(CAST(? AS bigint), CAST(? AS bigint))")
		args = append(args, r.AID, r.BID)
	}
	return vals, args
}
