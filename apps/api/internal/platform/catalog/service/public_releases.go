package service

import (
	"context"
	"strconv"
	"strings"
	"time"

	"api/internal/platform/catalog/dto"
	"api/internal/platform/catalog/model"

	"gorm.io/datatypes"
)

const (
	releaseFeedLaneDesc = "releases-date-desc"
	releaseFeedLaneAsc  = "releases-date-asc"
)

const (
	ReleaseFeedSortDateDesc = "date_desc"
	ReleaseFeedSortDateAsc  = "date_asc"
)

func ReleaseKindFromKey(k string) (int16, bool) {
	switch k {
	case "default":
		return model.ReleaseKindDefault, true
	case "digital":
		return model.ReleaseKindDigital, true
	case "physical":
		return model.ReleaseKindPhysical, true
	case "trial":
		return model.ReleaseKindTrial, true
	case "patch":
		return model.ReleaseKindPatch, true
	default:
		return 0, false
	}
}

var DefaultReleaseFeedKinds = []int16{
	model.ReleaseKindDefault, model.ReleaseKindDigital, model.ReleaseKindPhysical,
}

type ReleaseFeedFilter struct {
	NSFW          bool
	OLang         PublicOLang
	DisplayLimits []string
	Kinds         []int16
	Langs         []string
	Official      *bool
	Platform      string
	DateFrom      int64
	DateTo        int64
	Sort          string
	Include       WorksListInclude
	IDs           []int64
}

func (f ReleaseFeedFilter) lane() string {
	if f.Sort == ReleaseFeedSortDateAsc {
		return releaseFeedLaneAsc
	}
	return releaseFeedLaneDesc
}

func (f ReleaseFeedFilter) kinds() []int16 {
	if len(f.Kinds) > 0 {
		return f.Kinds
	}
	return DefaultReleaseFeedKinds
}

func (f ReleaseFeedFilter) PopulationKey() string {
	gate := "sfw"
	if f.NSFW {
		gate = "nsfw"
	}
	limits := "all"
	if len(f.DisplayLimits) > 0 {
		limits = strings.Join(f.DisplayLimits, "+")
	}
	kinds := make([]string, 0, len(f.kinds()))
	for _, k := range f.kinds() {
		kinds = append(kinds, strconv.FormatInt(int64(k), 10))
	}
	langs := "all"
	if len(f.Langs) > 0 {
		langs = strings.Join(f.Langs, "+")
	}
	official := "any"
	if f.Official != nil {
		official = strconv.FormatBool(*f.Official)
	}
	return strings.Join([]string{
		gate, f.OLang.Key(), limits, strings.Join(kinds, "+"), langs, official,
		f.Platform, strconv.FormatInt(f.DateFrom, 10), strconv.FormatInt(f.DateTo, 10),
	}, "-")
}

func (s *PublicService) ReleaseFeedMeta(ctx context.Context, f ReleaseFeedFilter) (int64, time.Time, int64, error) {
	from, where, args := releaseFeedSource(f)
	var row struct {
		N          int64     `gorm:"column:n"`
		MaxCreated time.Time `gorm:"column:max_created"`
		MaxID      int64     `gorm:"column:max_id"`
	}
	q := `SELECT count(*) AS n,
			coalesce(max(r.created_at), to_timestamp(0)) AS max_created,
			coalesce(max(r.id), 0) AS max_id ` +
		from + ` WHERE ` + strings.Join(where, " AND ")
	if err := s.db.WithContext(ctx).Raw(q, args...).Scan(&row).Error; err != nil {
		return 0, time.Time{}, 0, err
	}
	return row.N, row.MaxCreated, row.MaxID, nil
}

type releaseFeedRow struct {
	ID        int64
	Kind      int16
	Title     *string
	Lang      *string
	Platform  *string
	EngineID  *int64 `gorm:"column:engine_id"`
	ReleasedY *int16 `gorm:"column:released_y"`
	ReleasedM *int16 `gorm:"column:released_m"`
	ReleasedD *int16 `gorm:"column:released_d"`
	Extra     datatypes.JSON
	Ord       int64 `gorm:"column:ord"`
	IsFirst   bool  `gorm:"column:is_first"`

	WorkID      int64 `gorm:"column:work_id"`
	MediumID    int16
	DisplayName string
	// Explicit column tag: GORM snake-cases the field to o_lang, which matches
	// no result column (the W1 works-list trap, fixed in 0dce1f36).
	OLang         string `gorm:"column:olang"`
	ContentRating int16
	Site          *string
	ProductWorkID *int64
	ClaimState    *int16 `gorm:"column:claim_state"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (s *PublicService) ReleaseFeed(ctx context.Context, f ReleaseFeedFilter, cursor string, limit int) (dto.PublicReleaseFeedData, error) {
	lane := f.lane()
	cur, err := decodePublicCursor(cursor, lane)
	if err != nil {
		return dto.PublicReleaseFeedData{}, err
	}
	limit = clampBrowseLimit(limit)

	from, where, args := releaseFeedSource(f)
	ord := releaseOrd("r")
	if cur.ID > 0 {
		if lane == releaseFeedLaneAsc {
			where = append(where, "(("+ord+") > ? OR (("+ord+") = ? AND r.id > ?))")
		} else {
			where = append(where, "(("+ord+") < ? OR (("+ord+") = ? AND r.id > ?))")
		}
		args = append(args, cur.Ord, cur.Ord, cur.ID)
	}
	dir := "DESC"
	if lane == releaseFeedLaneAsc {
		dir = "ASC"
	}

	q := `SELECT r.id, r.kind, r.title, r.lang, r.platform, r.engine_id,
			r.released_y, r.released_m, r.released_d, r.extra,
			` + ord + ` AS ord,
			(` + ord + `) = (SELECT min(` + releaseOrd("r2") + `) FROM catalog_release r2
				WHERE r2.work_id = r.work_id AND r2.deleted_at IS NULL
				  AND r2.released_y IS NOT NULL AND r2.released_m IS NOT NULL) AS is_first,
			w.id AS work_id, w.medium_id, w.display_name, w.olang, w.content_rating,
			w.site, w.product_work_id, w.claim_state, w.created_at, w.updated_at ` +
		from + ` WHERE ` + strings.Join(where, " AND ") +
		` ORDER BY ord ` + dir + `, r.id ASC`
	q, args, paginated := applyBrowseLimit(q, args, limit, f.IDs)

	var rows []releaseFeedRow
	if err := s.db.WithContext(ctx).Raw(q, args...).Scan(&rows).Error; err != nil {
		return dto.PublicReleaseFeedData{}, err
	}
	items, err := s.buildReleaseFeedItems(ctx, rows, f)
	if err != nil {
		return dto.PublicReleaseFeedData{}, err
	}
	out := dto.PublicReleaseFeedData{Items: items}
	if paginated && len(rows) == limit {
		last := rows[len(rows)-1]
		nc := encodePublicCursor(publicCursor{Sort: lane, ID: last.ID, Ord: last.Ord})
		out.NextCursor = &nc
	}
	return out, nil
}

func (s *PublicService) buildReleaseFeedItems(ctx context.Context, rows []releaseFeedRow, f ReleaseFeedFilter) ([]dto.PublicReleaseFeedItem, error) {
	if len(rows) == 0 {
		return []dto.PublicReleaseFeedItem{}, nil
	}
	releaseIDs := make([]int64, len(rows))
	src := make([]workListSourceRow, 0, len(rows))
	seen := make(map[int64]struct{}, len(rows))
	for i, r := range rows {
		releaseIDs[i] = r.ID
		if _, dup := seen[r.WorkID]; dup {
			continue
		}
		seen[r.WorkID] = struct{}{}
		src = append(src, workListSourceRow{
			ID: r.WorkID, MediumID: r.MediumID, DisplayName: r.DisplayName, OLang: r.OLang,
			ContentRating: r.ContentRating, Site: r.Site, ProductWorkID: r.ProductWorkID,
			ClaimState: r.ClaimState, CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt: r.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
	refs, err := s.entityRefsFor(ctx, model.EntityTypeRelease, releaseIDs)
	if err != nil {
		return nil, err
	}
	labels, err := s.releaseLabelsFor(ctx, releaseIDs)
	if err != nil {
		return nil, err
	}
	var engineIDs []int64
	for _, r := range rows {
		if r.EngineID != nil {
			engineIDs = append(engineIDs, *r.EngineID)
		}
	}
	engines, err := engineNames(ctx, s.db, engineIDs)
	if err != nil {
		return nil, err
	}
	works, err := s.enrichWorkListItems(ctx, src, f.NSFW, f.Include, PublicFields{})
	if err != nil {
		return nil, err
	}
	byWork := make(map[int64]dto.PublicWorkListItem, len(works))
	for _, w := range works {
		byWork[w.ID] = w
	}
	out := make([]dto.PublicReleaseFeedItem, len(rows))
	labelBlocks := make([][]dto.PublicWorkLabel, 0, len(rows))
	for i, r := range rows {
		item := dto.PublicReleaseFeedItem{
			ID: r.ID, Kind: releaseKindKey(r.Kind), Title: derefStrPub(r.Title),
			Lang: derefStrPub(r.Lang), Platform: derefStrPub(r.Platform),
			Platforms: publicPlatformsFromExtra(r.Extra),
			IsFirst:   r.IsFirst, Work: byWork[r.WorkID],
			Refs: refs[r.ID], Labels: labels[r.ID],
		}
		if r.ReleasedY != nil {
			d := partialISOFromOrdinal(r.Ord)
			item.Date = &d
		}
		if r.EngineID != nil {
			item.Engine = &dto.PublicEngineRef{ID: *r.EngineID, Name: engines[*r.EngineID]}
		}
		if item.Refs == nil {
			item.Refs = []dto.PublicCatalogRef{}
		}
		if item.Labels == nil {
			item.Labels = []dto.PublicWorkLabel{}
		}
		labelBlocks = append(labelBlocks, item.Labels)
		out[i] = item
	}
	if err := s.fillWorkLabelCounts(ctx, labelBlocks, f.NSFW); err != nil {
		return nil, err
	}
	return out, nil
}

func releaseFeedSource(f ReleaseFeedFilter) (from string, where []string, args []any) {
	from = `FROM catalog_release r JOIN catalog_work w ON w.id = r.work_id`

	where = []string{
		"r.deleted_at IS NULL", "w.deleted_at IS NULL", "w.status = ?", "w.medium_id = ?",
	}
	args = append(args, model.WorkStatusLive, galgameMediumID)
	if len(f.IDs) == 0 {
		where = append(where, "r.released_y IS NOT NULL", "r.released_m IS NOT NULL")
	}
	if len(f.IDs) > 0 {
		where = append(where, "r.id IN ?")
		args = append(args, f.IDs)
	}

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

	where = append(where, "r.kind IN ?")
	args = append(args, f.kinds())

	if len(f.Langs) > 0 {
		where = append(where, "COALESCE(r.lang, w.olang) IN ?")
		args = append(args, f.Langs)
	}
	if f.Official != nil {
		// extra->>'official' is written by the VNDB release lane only, where
		// false means "fan translation / unofficial edition". A row WITHOUT the
		// key is official: every other lane materialises store SKUs (dlsite
		// worknos, getchu product pages), which are official by construction.
		// Hence IS DISTINCT FROM rather than <> — a NULL must count as official,
		// and <> would silently drop all 159,080 keyless rows.
		if *f.Official {
			where = append(where, "r.extra->>'official' IS DISTINCT FROM 'false'")
		} else {
			where = append(where, "r.extra->>'official' = 'false'")
		}
	}
	if f.Platform != "" {
		where = append(where, "r.platform = ?")
		args = append(args, f.Platform)
	}
	if f.DateFrom > 0 {
		where = append(where, "("+releaseOrd("r")+") >= ?")
		args = append(args, f.DateFrom)
	}
	if f.DateTo > 0 {
		where = append(where, "("+releaseOrd("r")+") <= ?")
		args = append(args, f.DateTo)
	}
	return from, where, args
}

func ReleaseFeedETag(populationKey string, count int64, maxCreated time.Time, maxID int64) string {
	return `W/"relfeed-` + populationKey + `-` +
		strconv.FormatInt(count, 10) + `-` +
		strconv.FormatInt(maxCreated.Unix(), 10) + `-` +
		strconv.FormatInt(maxID, 10) + `"`
}
