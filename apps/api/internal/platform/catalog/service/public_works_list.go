package service

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"api/internal/platform/catalog/dto"
	"api/internal/platform/catalog/model"
)

type WorksListFilter struct {
	ContentRating  *int16
	Claimed        *bool
	ClaimStates    []string
	DisplayLimits  []string
	Site           string
	OwnerUID       int64
	Statuses       []int16
	LabelID        int64
	LabelRollup    bool
	TagIDs         []int64
	SeriesID       int64
	EngineID       int64
	Platform       string
	ReleasedAfter  int64
	ReleasedBefore int64
	IDs            []int64
	NSFW           bool
	Sort           string
	OLang          PublicOLang
	Include        WorksListInclude
	Fields         PublicFields
	IncludeTotal   bool
}

func (s *PublicService) WorksList(ctx context.Context, f WorksListFilter, cursor string, limit int) (dto.PublicWorksListData, error) {
	lane := worksSortLane(f.Sort)
	cur, err := decodePublicCursor(cursor, lane)
	if err != nil {
		return dto.PublicWorksListData{}, err
	}
	if limit <= 0 {
		limit = 20
	} else if limit > 100 {
		limit = 100
	}

	where, args := worksListWhere(f)

	var total int64
	if f.IncludeTotal {
		if total, err = s.taxonomyTotal(ctx, "catalog_work w", where, args); err != nil {
			return dto.PublicWorksListData{}, err
		}
	}

	var order string
	if lane == "updated" {
		if cur.Updated != "" {
			ts, perr := time.Parse(time.RFC3339Nano, cur.Updated)
			if perr != nil {
				return dto.PublicWorksListData{}, ErrBadCursor
			}
			where = append(where, "(w.updated_at, w.id) < (?, ?)")
			args = append(args, ts, cur.ID)
		}
		order = "ORDER BY w.updated_at DESC, w.id DESC"
	} else {
		if cur.ID > 0 {
			where = append(where, "w.id > ?")
			args = append(args, cur.ID)
		}
		order = "ORDER BY w.id ASC"
	}

	q := `SELECT w.id, w.medium_id, w.display_name, w.olang, w.content_rating, w.site, w.product_work_id, w.claim_state, w.created_at, w.updated_at
		FROM catalog_work w WHERE ` + strings.Join(where, " AND ") + " " + order
	q, args, paginated := applyBrowseLimit(q, args, limit, f.IDs)

	var rows []struct {
		ID          int64
		MediumID    int16
		DisplayName string
		// The explicit column tag is load-bearing: GORM snake-cases the field
		// to o_lang, which matches no result column, so the value silently
		// scanned as "" from W1 until A2-1a caught it.
		OLang         string `gorm:"column:olang"`
		ContentRating int16
		Site          *string
		ProductWorkID *int64
		ClaimState    *int16 `gorm:"column:claim_state"`
		CreatedAt     time.Time
		UpdatedAt     time.Time
	}
	if err := s.db.WithContext(ctx).Raw(q, args...).Scan(&rows).Error; err != nil {
		return dto.PublicWorksListData{}, err
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
	items, err := s.enrichWorkListItems(ctx, src, f.NSFW, f.Include, f.Fields)
	if err != nil {
		return dto.PublicWorksListData{}, err
	}
	if f.LabelID > 0 && f.LabelRollup && f.Fields.Wants("via_label") {
		ids := make([]int64, len(items))
		for i, it := range items {
			ids[i] = it.ID
		}
		via, verr := s.labelRollupVia(ctx, f.LabelID, ids)
		if verr != nil {
			return dto.PublicWorksListData{}, verr
		}
		for i := range items {
			if v, ok := via[items[i].ID]; ok {
				items[i].ViaLabel = &v
			}
		}
	}
	out := dto.PublicWorksListData{Items: items, Total: total}
	if paginated && len(rows) == limit {
		last := rows[len(rows)-1]
		c := publicCursor{Sort: lane, ID: last.ID}
		if lane == "updated" {
			c.Updated = last.UpdatedAt.UTC().Format(time.RFC3339Nano)
		}
		nc := encodePublicCursor(c)
		out.NextCursor = &nc
	}
	return out, nil
}

// The COUNT runs off this slice BEFORE the cursor predicate is appended: a
// total that shrank as the caller paged would be worse than no total at all.
func worksListWhere(f WorksListFilter) ([]string, []any) {
	statuses := f.Statuses
	if len(statuses) == 0 {
		statuses = []int16{model.WorkStatusLive}
	}
	where := []string{"w.deleted_at IS NULL", "w.status IN ?", "w.medium_id = ?"}
	args := []any{statuses, galgameMediumID}

	if !f.NSFW {
		where = append(where, "w.content_rating <> ?")
		args = append(args, model.ContentRatingR18)
	}
	if f.ContentRating != nil {
		where = append(where, "w.content_rating = ?")
		args = append(args, *f.ContentRating)
	}
	// claimedSQL, not a local spelling of it: claimed= tested only w.site while
	// claim_state= also requires product_work_id, so a row with a site and no
	// product id was claimed=true and claim_state=none at the same time.
	if f.Claimed != nil {
		if *f.Claimed {
			where = append(where, claimedSQL)
		} else {
			where = append(where, "NOT "+claimedSQL)
		}
	}
	if f.Site != "" {
		where = append(where, "w.site = ?")
		args = append(args, f.Site)
	}
	if f.OwnerUID > 0 {
		where = append(where, "w.owner_user_id = ?")
		args = append(args, f.OwnerUID)
	}
	if pred, pargs := claimStateWhere(f.ClaimStates); pred != "" {
		where = append(where, pred)
		args = append(args, pargs...)
	}
	// A ban only writes claim_state, never status, and the claim-state predicate
	// ran only when claim_state= was passed — so a banned work stayed in the
	// default GET /v2/catalog/works page. The decision face promises ban "hides
	// it from any state", so the exclusion is not conditional on the caller
	// having asked; only an explicit claim_state=hidden opts back in.
	if !slices.Contains(f.ClaimStates, model.ClaimStateKeyHidden) {
		pred, pargs := claimStateWhere([]string{model.ClaimStateKeyHidden})
		where = append(where, "NOT "+pred)
		args = append(args, pargs...)
	}
	if pred, pargs := displayLimitWhere(f.DisplayLimits); pred != "" {
		where = append(where, pred)
		args = append(args, pargs...)
	}
	if f.LabelID > 0 {
		if f.LabelRollup {
			where = append(where, `EXISTS (SELECT 1 FROM catalog_work_label wl
				WHERE wl.work_id = w.id AND (wl.label_id = ? OR wl.label_id IN (`+labelRollupChildren+`)))`)
			args = append(args, f.LabelID, f.LabelID, labelRollupRelations)
		} else {
			where = append(where, "EXISTS (SELECT 1 FROM catalog_work_label wl WHERE wl.work_id = w.id AND wl.label_id = ?)")
			args = append(args, f.LabelID)
		}
	}
	for _, tagID := range f.TagIDs {
		where = append(where, `EXISTS (SELECT 1 FROM catalog_work_tag wt
			JOIN catalog_tag_source_map m ON m.source_id = wt.source_id AND m.source_name = wt.name
			WHERE wt.work_id = w.id AND m.tag_id = ?)`)
		args = append(args, tagID)
	}
	if f.SeriesID > 0 {
		where = append(where, "EXISTS (SELECT 1 FROM catalog_series_member sm WHERE sm.work_id = w.id AND sm.series_id = ?)")
		args = append(args, f.SeriesID)
	}
	if f.EngineID > 0 {
		where = append(where, "EXISTS (SELECT 1 FROM catalog_work_engine we WHERE we.work_id = w.id AND we.engine_id = ?)")
		args = append(args, f.EngineID)
	}
	if f.Platform != "" {
		where = append(where, `(EXISTS (SELECT 1 FROM catalog_release r WHERE r.work_id = w.id AND r.deleted_at IS NULL AND r.platform = ?)
			OR EXISTS (SELECT 1 FROM catalog_work_platform wp WHERE wp.work_id = w.id AND wp.platform = ?))`)
		args = append(args, f.Platform, f.Platform)
	}
	if f.ReleasedAfter > 0 || f.ReleasedBefore > 0 {
		earliest := `(SELECT min(r.released_y::int*10000 + coalesce(r.released_m,0)::int*100 + coalesce(r.released_d,0)::int)
			FROM catalog_release r WHERE r.work_id = w.id AND r.released_y IS NOT NULL AND r.deleted_at IS NULL)`
		if f.ReleasedAfter > 0 {
			where = append(where, earliest+" >= ?")
			args = append(args, f.ReleasedAfter)
		}
		if f.ReleasedBefore > 0 {
			where = append(where, earliest+" <= ?")
			args = append(args, f.ReleasedBefore)
		}
	}
	if pred, pargs := f.OLang.predicate(); pred != "" {
		where = append(where, pred)
		args = append(args, pargs...)
	}
	if len(f.IDs) > 0 {
		where = append(where, "w.id IN ?")
		args = append(args, f.IDs)
	}
	return where, args
}

func (s *PublicService) Changes(ctx context.Context, cursor string, limit int) (dto.PublicChangesData, error) {
	cur, err := decodePublicCursor(cursor, "changes")
	if err != nil {
		return dto.PublicChangesData{}, err
	}
	if limit <= 0 {
		limit = 100
	} else if limit > 500 {
		limit = 500
	}
	// The population is every galgame row, not just the live ones: a mirror
	// that only ever hears about live works can never learn that a row left,
	// and the downstream lists kept showing merged-away and deleted ids until
	// their next full sweep.
	where := []string{"medium_id = ?", "updated_at < now() - interval '5 seconds'"}
	args := []any{model.WorkStatusLive, galgameMediumID}
	if cur.Updated != "" {
		ts, perr := time.Parse(time.RFC3339Nano, cur.Updated)
		if perr != nil {
			return dto.PublicChangesData{}, ErrBadCursor
		}
		where = append(where, "(updated_at, id) > (?, ?)")
		args = append(args, ts, cur.ID)
	}
	q := `SELECT id, updated_at, (deleted_at IS NOT NULL OR status <> ?) AS gone FROM catalog_work WHERE ` +
		strings.Join(where, " AND ") + ` ORDER BY updated_at ASC, id ASC LIMIT ?`
	args = append(args, limit)
	var rows []struct {
		ID        int64
		UpdatedAt time.Time
		Gone      bool
	}
	if err := s.db.WithContext(ctx).Raw(q, args...).Scan(&rows).Error; err != nil {
		return dto.PublicChangesData{}, err
	}
	out := dto.PublicChangesData{Items: make([]dto.PublicChangeItem, len(rows))}
	next := cur
	for i, r := range rows {
		out.Items[i] = dto.PublicChangeItem{
			EntityType: "work", ID: r.ID, Updated: r.UpdatedAt.UTC().Format(time.RFC3339), Gone: r.Gone,
		}
		next = publicCursor{Sort: "changes", ID: r.ID, Updated: r.UpdatedAt.UTC().Format(time.RFC3339Nano)}
	}
	out.NextCursor = encodePublicCursor(next)
	return out, nil
}

// The population is the one Changes pages over, counted before the cursor: a
// total that shrank as the mirror paged would be worse than no total at all.
func (s *PublicService) ChangesTotal(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Raw(
		`SELECT count(*) FROM catalog_work WHERE medium_id = ? AND updated_at < now() - interval '5 seconds'`,
		galgameMediumID).Scan(&n).Error
	return n, err
}

func worksSortLane(sort string) string {
	if sort == "updated" {
		return "updated"
	}
	return "id"
}

type workListSourceRow struct {
	ID            int64
	MediumID      int16
	DisplayName   string
	OLang         string
	ContentRating int16
	Site          *string
	ProductWorkID *int64
	ClaimState    *int16
	CreatedAt     string
	UpdatedAt     string
}

func (s *PublicService) enrichWorkListItems(ctx context.Context, rows []workListSourceRow, nsfw bool, inc WorksListInclude, sel PublicFields) ([]dto.PublicWorkListItem, error) {
	if len(rows) == 0 {
		return []dto.PublicWorkListItem{}, nil
	}
	inc = inc.intersect(sel)
	ids := make([]int64, len(rows))
	subjects := make([]claimSubject, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
		subjects[i] = claimSubject{WorkID: r.ID}
	}
	// The cover rows feed two different keys, and display_nsfw decides which
	// cover each of them may show, so both loads follow either key.
	wantCovers := sel.Wants("cover") || inc.Covers
	var coverSubjects, limitSubjects []claimSubject
	if wantCovers {
		coverSubjects = subjects
	}
	if wantCovers || sel.Wants("claimed_by") {
		limitSubjects = subjects
	}
	var dates map[int64]*string
	if sel.Wants("release_date") {
		var err error
		if dates, err = s.earliestReleaseDatesFor(ctx, ids); err != nil {
			return nil, err
		}
	}
	covers, err := s.read.loadWorkCovers(ctx, coverSubjects)
	if err != nil {
		return nil, err
	}
	limits, err := s.read.loadDisplayNSFW(ctx, limitSubjects)
	if err != nil {
		return nil, err
	}
	out := make([]dto.PublicWorkListItem, len(rows))
	for i, r := range rows {
		out[i] = dto.PublicWorkListItem{
			ID: r.ID, Medium: s.mediumKey(r.MediumID), DisplayName: r.DisplayName,
			ContentRating: contentRatingKey(r.ContentRating), OLang: r.OLang,
			ReleaseDate: dates[r.ID],
			ClaimedBy:   claimedBy(r.Site, r.ProductWorkID, r.ClaimState, limits[r.ID], r.ContentRating),
			Cover: s.pickListCover(covers[r.ID],
				nsfw && effectiveDisplayNSFW(r.Site, r.ProductWorkID, limits[r.ID], r.ContentRating)),
			Created: r.CreatedAt, Updated: r.UpdatedAt,
		}
	}
	if err := s.attachWorkListBlocks(ctx, out, rows, subjects, covers, inc, nsfw, limits); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *PublicService) earliestReleaseDatesFor(ctx context.Context, ids []int64) (map[int64]*string, error) {
	if len(ids) == 0 {
		return map[int64]*string{}, nil
	}
	var rows []struct {
		WorkID int64 `gorm:"column:work_id"`
		Ord    int64 `gorm:"column:ord"`
	}
	if err := s.db.WithContext(ctx).Raw(`
		SELECT work_id,
		       min(released_y::int * 10000 + coalesce(released_m,0)::int * 100 + coalesce(released_d,0)::int) AS ord
		FROM catalog_release
		WHERE work_id IN ? AND released_y IS NOT NULL AND deleted_at IS NULL
		GROUP BY work_id`, ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]*string, len(rows))
	for _, r := range rows {
		d := partialISOFromOrdinal(r.Ord)
		out[r.WorkID] = &d
	}
	return out, nil
}

func partialISOFromOrdinal(ord int64) string {
	y, m, d := ord/10000, (ord/100)%100, ord%100
	out := fmt.Sprintf("%04d", y)
	if m > 0 {
		out = fmt.Sprintf("%s-%02d", out, m)
		if d > 0 {
			out = fmt.Sprintf("%s-%02d", out, d)
		}
	}
	return out
}

// Same stand-in ladder as pickCoverSlots: censored ghosts serve only when no
// real row could. Ghosts are sexual=0, so left in the main scan one would
// shadow the real explicit cover an R18 viewer should get — and, row order
// permitting, even a real safe cover.
func (s *PublicService) pickListCover(rows []WorkCoverRow, allowSexual bool) string {
	real, ghosts := s.splitCensored(rows)
	if url := s.listCoverFrom(real, allowSexual); url != "" {
		return url
	}
	return s.listCoverFrom(ghosts, false)
}

func (s *PublicService) listCoverFrom(rows []WorkCoverRow, allowSexual bool) string {
	var fallback string
	for _, c := range rows {
		if !isCoverArt(c.Kind) {
			continue
		}
		if !allowSexual && c.Sexual >= model.SexualExplicit {
			continue
		}
		url := s.imageURL(c.ImageHash)
		if url == "" {
			continue
		}
		if c.PortraitPinned {
			return url
		}
		if fallback == "" {
			fallback = url
		}
	}
	return fallback
}
