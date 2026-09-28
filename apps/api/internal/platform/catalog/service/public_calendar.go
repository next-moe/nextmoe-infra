package service

import (
	"context"
	"strconv"
	"strings"
	"time"

	"api/internal/platform/catalog/dto"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/search/spec"
)

const (
	calendarLaneMonth   = "calendar"
	calendarLanePending = "calendar-pending"
	calendarLaneTBA     = "calendar-tba"
)

type CalendarBucketKind int8

const (
	CalendarMonthBucket CalendarBucketKind = iota
	CalendarPendingBucket
	CalendarTBABucket
)

type CalendarBucket struct {
	Kind  CalendarBucketKind
	Year  int
	Month int
}

func (b CalendarBucket) window() (lo, hi int64, ok bool) {
	switch b.Kind {
	case CalendarMonthBucket:
		lo = int64(b.Year)*10000 + int64(b.Month)*100
		return lo, lo + 99, true
	case CalendarPendingBucket:
		lo = int64(b.Year) * 10000
		return lo, lo, true
	default:
		return 0, 0, false
	}
}

func (b CalendarBucket) lane() string {
	switch b.Kind {
	case CalendarMonthBucket:
		return calendarLaneMonth
	case CalendarPendingBucket:
		return calendarLanePending
	default:
		return calendarLaneTBA
	}
}

type PublicOLang struct {
	All    bool
	Values []string
}

func (o PublicOLang) predicate() (string, []any) {
	switch {
	case o.All:
		return "", nil
	case len(o.Values) > 0:
		return "w.olang IN ?", []any{o.Values}
	default:
		return "(w.olang = ? OR w.olang LIKE ?)", []any{"ja", "zh%"}
	}
}

func (o PublicOLang) Spec() spec.OLang {
	return spec.OLang{All: o.All, Values: o.Values}
}

func (o PublicOLang) Key() string {
	switch {
	case o.All:
		return "all"
	case len(o.Values) > 0:
		return strings.Join(o.Values, "+")
	default:
		return "jazh"
	}
}

type CalendarFilter struct {
	NSFW                bool
	OLang               PublicOLang
	DisplayLimits       []string
	ExcludeCompanyKinds []string
	Include             WorksListInclude
}

func (f CalendarFilter) PopulationKey() string {
	gate := "sfw"
	if f.NSFW {
		gate = "nsfw"
	}
	limit := "all"
	if len(f.DisplayLimits) > 0 {
		limit = strings.Join(f.DisplayLimits, "+")
	}
	key := gate + "-" + f.OLang.Key() + "-" + limit
	if len(f.ExcludeCompanyKinds) > 0 {
		key += "-xck-" + strings.Join(f.ExcludeCompanyKinds, "+")
	}
	return key
}

func (f CalendarFilter) population() (where []string, args []any) {
	where = []string{"w.deleted_at IS NULL", "w.status = ?", "w.medium_id = ?"}
	args = []any{model.WorkStatusLive, galgameMediumID}
	if !f.NSFW {
		where = append(where, "w.content_rating <> ?")
		args = append(args, model.ContentRatingR18)
	}
	if pred, pargs := f.OLang.predicate(); pred != "" {
		where = append(where, pred)
		args = append(args, pargs...)
	}
	if pred, pargs := displayLimitWhere(f.DisplayLimits); pred != "" {
		where = append(where, pred)
		args = append(args, pargs...)
	}
	return where, args
}

// One NOT EXISTS, not NOT EXISTS(excluded) OR EXISTS(other): the OR form is
// probed per work ahead of the release joins and took the undated bucket from
// 54 ms to 321 ms on a production copy; as an anti-join it is 125 ms.
func (f CalendarFilter) companyKindExclusion(workID string) (string, []any) {
	kinds := make([]int16, 0, len(f.ExcludeCompanyKinds))
	for _, k := range f.ExcludeCompanyKinds {
		if v, ok := LabelKindFromKey(k); ok {
			kinds = append(kinds, v)
		}
	}
	if len(kinds) == 0 {
		return "", nil
	}
	return `NOT EXISTS (SELECT 1 FROM catalog_work_label xwl
		JOIN catalog_label xl ON xl.id = xwl.label_id AND xl.deleted_at IS NULL
		WHERE xwl.work_id = ` + workID + ` AND xl.kind IN ?
		  AND NOT EXISTS (SELECT 1 FROM catalog_work_label kwl
			JOIN catalog_label kl ON kl.id = kwl.label_id AND kl.deleted_at IS NULL
			WHERE kwl.work_id = ` + workID + ` AND kl.kind NOT IN ?))`, []any{kinds, kinds}
}

func (s *PublicService) CalendarMeta(ctx context.Context, b CalendarBucket, f CalendarFilter) (int64, time.Time, error) {
	from, where, args := calendarSource(b, f)
	var row struct {
		N          int64     `gorm:"column:n"`
		MaxUpdated time.Time `gorm:"column:max_updated"`
	}
	q := `SELECT count(*) AS n, coalesce(max(w.updated_at), to_timestamp(0)) AS max_updated ` +
		from + ` WHERE ` + strings.Join(where, " AND ")
	if err := s.db.WithContext(ctx).Raw(q, args...).Scan(&row).Error; err != nil {
		return 0, time.Time{}, err
	}
	return row.N, row.MaxUpdated, nil
}

func (s *PublicService) CalendarPage(ctx context.Context, b CalendarBucket, f CalendarFilter, cursor string, limit int) (dto.PublicCalendarData, error) {
	cur, err := decodePublicCursor(cursor, b.lane())
	if err != nil {
		return dto.PublicCalendarData{}, err
	}
	limit = clampBrowseLimit(limit)

	from, where, args := calendarSource(b, f)
	sel := `SELECT w.id, w.medium_id, w.display_name, w.olang, w.content_rating, w.site, w.product_work_id, w.claim_state, w.created_at, w.updated_at`
	order := ` ORDER BY w.id ASC`
	if b.Kind == CalendarMonthBucket {
		sel += `, e.ord`
		order = ` ORDER BY e.ord ASC, w.id ASC`
		if cur.ID > 0 {
			where = append(where, "(e.ord, w.id) > (?, ?)")
			args = append(args, cur.Ord, cur.ID)
		}
	} else if cur.ID > 0 {
		where = append(where, "w.id > ?")
		args = append(args, cur.ID)
	}
	args = append(args, limit)

	var rows []struct {
		ID          int64
		MediumID    int16
		DisplayName string
		// Explicit column tag: GORM snake-cases the field to o_lang, which
		// matches no result column (the W1 works-list trap, fixed in 0dce1f36).
		OLang         string `gorm:"column:olang"`
		ContentRating int16
		Site          *string
		ProductWorkID *int64
		ClaimState    *int16 `gorm:"column:claim_state"`
		CreatedAt     time.Time
		UpdatedAt     time.Time
		Ord           int64 `gorm:"column:ord"`
	}
	q := sel + " " + from + ` WHERE ` + strings.Join(where, " AND ") + order + ` LIMIT ?`
	if err := s.db.WithContext(ctx).Raw(q, args...).Scan(&rows).Error; err != nil {
		return dto.PublicCalendarData{}, err
	}

	src := make([]workListSourceRow, len(rows))
	for i, r := range rows {
		src[i] = workListSourceRow{
			ID: r.ID, MediumID: r.MediumID, DisplayName: r.DisplayName, OLang: r.OLang,
			ContentRating: r.ContentRating, Site: r.Site, ProductWorkID: r.ProductWorkID,
			ClaimState: r.ClaimState, CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt: r.UpdatedAt.UTC().Format(time.RFC3339),
		}
	}
	items, err := s.enrichWorkListItems(ctx, src, f.NSFW, f.Include, PublicFields{})
	if err != nil {
		return dto.PublicCalendarData{}, err
	}
	out := dto.PublicCalendarData{Items: items}
	if len(rows) == limit {
		last := rows[len(rows)-1]
		nc := encodePublicCursor(publicCursor{Sort: b.lane(), ID: last.ID, Ord: last.Ord})
		out.NextCursor = &nc
	}
	return out, nil
}

func calendarSource(b CalendarBucket, f CalendarFilter) (from string, where []string, args []any) {
	from = `FROM catalog_work w`
	if lo, hi, dated := b.window(); dated {
		from += `
			JOIN (SELECT r.work_id, min(` + releaseOrd("r") + `) AS ord
				FROM catalog_release r
				WHERE r.released_y IS NOT NULL AND r.deleted_at IS NULL
				  AND r.work_id IN (SELECT c.work_id FROM catalog_release c
					WHERE c.released_y IS NOT NULL AND c.deleted_at IS NULL
					  AND ` + releaseOrd("c") + ` BETWEEN ? AND ?)
				GROUP BY r.work_id
				HAVING min(` + releaseOrd("r") + `) BETWEEN ? AND ?) e ON e.work_id = w.id`
		args = append(args, lo, hi, lo, hi)
	}

	where, pargs := f.population()
	args = append(args, pargs...)
	if pred, pargs := f.companyKindExclusion("w.id"); pred != "" {
		where = append(where, pred)
		args = append(args, pargs...)
	}
	if b.Kind == CalendarTBABucket {
		where = append(where,
			"EXISTS (SELECT 1 FROM catalog_release r WHERE r.work_id = w.id AND r.deleted_at IS NULL)",
			"NOT EXISTS (SELECT 1 FROM catalog_release r WHERE r.work_id = w.id AND r.deleted_at IS NULL AND r.released_y IS NOT NULL)")
	}
	return from, where, args
}

func releaseOrd(alias string) string {
	return alias + ".released_y::int * 10000 + coalesce(" + alias + ".released_m,0)::int * 100 + coalesce(" + alias + ".released_d,0)::int"
}

// Walking in from each end (ORDER BY ord OFFSET 0 in a subquery, the rule as an
// outer filter, LIMIT 1) was faster and wrong: nothing keeps the subquery's
// order once the planner runs the rule as a hash anti-join, and CI got an
// arbitrary kept row back as max_month. The bounds are one population-wide
// answer shared by every month, so they are cached for the totals TTL instead.
func (s *PublicService) CalendarBounds(ctx context.Context, f CalendarFilter) (minOrd, maxOrd int64, found bool, err error) {
	key := "calendar-bounds\x00" + f.PopulationKey()
	if lo, ok := s.totals.get(key + "\x00min"); ok {
		if hi, ok := s.totals.get(key + "\x00max"); ok {
			return lo, hi, lo != 0, nil
		}
	}
	where, args := f.population()
	if pred, pargs := f.companyKindExclusion("w.id"); pred != "" {
		where = append(where, pred)
		args = append(args, pargs...)
	}
	var row struct {
		MinOrd *int64 `gorm:"column:min_ord"`
		MaxOrd *int64 `gorm:"column:max_ord"`
	}
	q := `SELECT min(e.ord) AS min_ord, max(e.ord) AS max_ord
		FROM (SELECT min(` + releaseOrd("r") + `) AS ord
			FROM catalog_release r
			JOIN catalog_work w ON w.id = r.work_id
			WHERE r.released_y IS NOT NULL AND r.deleted_at IS NULL
				AND ` + strings.Join(where, " AND ") + `
			GROUP BY r.work_id) e`
	if err := s.db.WithContext(ctx).Raw(q, args...).Scan(&row).Error; err != nil {
		return 0, 0, false, err
	}
	if row.MinOrd != nil && row.MaxOrd != nil {
		minOrd, maxOrd, found = (*row.MinOrd/100)*100, (*row.MaxOrd/100)*100, true
	}
	s.totals.put(key+"\x00min", minOrd)
	s.totals.put(key+"\x00max", maxOrd)
	return minOrd, maxOrd, found, nil
}

func CalendarETag(bucketKey, populationKey string, count int64, maxUpdated time.Time) string {
	return `W/"cal-` + bucketKey + `-` + populationKey + `-` +
		strconv.FormatInt(count, 10) + `-` + strconv.FormatInt(maxUpdated.Unix(), 10) + `"`
}
