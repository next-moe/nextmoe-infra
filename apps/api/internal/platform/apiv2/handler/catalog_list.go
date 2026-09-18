package handler

import (
	"context"
	"errors"
	"strconv"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	catmodel "api/internal/platform/catalog/model"
	catsvc "api/internal/platform/catalog/service"
)

func (c *Catalog) ListCompanies(ctx context.Context, q collect.Query, hasWorks bool) (repr.List[repr.Company], error) {
	if c == nil || c.Public == nil {
		return repr.List[repr.Company]{}, problem.New(problem.CodeServiceUnavailable, "", "", "catalog read is not bound.")
	}
	ids, missing, err := c.batchEntityIDs(ctx, q, catmodel.EntityTypeLabel)
	if err != nil {
		return repr.List[repr.Company]{}, err
	}
	if q.Batch && len(ids) == 0 {
		return finishList([]repr.Company{}, nil, 0, q, missing), nil
	}
	if q.Batch && companyWantsDetail(q.Include) {
		items := make([]repr.Company, 0, len(ids))
		seen := map[int64]bool{}
		for _, id := range ids {
			rec, found, derr := c.Public.Label(ctx, id, false, q.NSFW, 0, 0)
			if derr != nil {
				return repr.List[repr.Company]{}, derr
			}
			if !found || seen[id] {
				continue
			}
			items = append(items, companyFromDetail(rec, q.Include, c.Public.ImageURL(rec.LogoHash)))
			seen[id] = true
		}
		missing = appendUnseen(missing, ids, seen)
		return finishList(items, nil, int64(len(items)), q, missing), nil
	}
	f := catsvc.LabelsListFilter{NSFW: q.NSFW, IDs: ids, HasWorks: hasWorks}
	limit := q.Limit
	if q.Batch {
		limit = 100
		q.Cursor = ""
	}
	data, lerr := c.Public.LabelsList(ctx, f, q.Cursor, limit)
	if lerr != nil {
		return repr.List[repr.Company]{}, listCursorErr(lerr)
	}
	items := make([]repr.Company, 0, len(data.Items))
	seen := map[int64]bool{}
	for _, it := range data.Items {
		items = append(items, companyFromListItem(it, q.Include, c.Public.ImageURL(it.LogoHash)))
		seen[it.ID] = true
	}
	missing = appendUnseen(missing, ids, seen)
	return finishList(items, data.NextCursor, data.Total, q, missing), nil
}

func companyWantsDetail(include []string) bool {
	for _, t := range include {
		if t == "intros" || t == "links" {
			return true
		}
	}
	return false
}

func (c *Catalog) ListTags(ctx context.Context, q collect.Query, hasWorks bool) (repr.List[repr.Tag], error) {
	if c == nil || c.Public == nil {
		return repr.List[repr.Tag]{}, problem.New(problem.CodeServiceUnavailable, "", "", "catalog read is not bound.")
	}
	ids, missing, err := c.batchEntityIDs(ctx, q, catmodel.EntityTypeTag)
	if err != nil {
		return repr.List[repr.Tag]{}, err
	}
	if q.Batch && len(ids) == 0 {
		return finishList([]repr.Tag{}, nil, 0, q, missing), nil
	}
	f := catsvc.TagsListFilter{NSFW: q.NSFW, IDs: ids, HasWorks: hasWorks}
	limit := q.Limit
	if q.Batch {
		limit = 100
		q.Cursor = ""
	}
	data, lerr := c.Public.TagsList(ctx, f, q.Cursor, limit)
	if lerr != nil {
		return repr.List[repr.Tag]{}, listCursorErr(lerr)
	}
	items := make([]repr.Tag, 0, len(data.Items))
	seen := map[int64]bool{}
	for _, it := range data.Items {
		items = append(items, tagFromListItem(it))
		seen[it.ID] = true
	}
	missing = appendUnseen(missing, ids, seen)
	return finishList(items, data.NextCursor, data.Total, q, missing), nil
}

func (c *Catalog) ListEngines(ctx context.Context, q collect.Query) (repr.List[repr.Engine], error) {
	if c == nil || c.Public == nil {
		return repr.List[repr.Engine]{}, problem.New(problem.CodeServiceUnavailable, "", "", "catalog read is not bound.")
	}
	ids, missing, err := c.batchEntityIDs(ctx, q, catmodel.EntityTypeEngine)
	if err != nil {
		return repr.List[repr.Engine]{}, err
	}
	if q.Batch && len(ids) == 0 {
		return finishList([]repr.Engine{}, nil, 0, q, missing), nil
	}
	f := catsvc.EnginesListFilter{NSFW: q.NSFW, IDs: ids}
	limit := q.Limit
	if q.Batch {
		limit = 100
		q.Cursor = ""
	}
	data, lerr := c.Public.EnginesList(ctx, f, q.Cursor, limit)
	if lerr != nil {
		return repr.List[repr.Engine]{}, listCursorErr(lerr)
	}
	items := make([]repr.Engine, 0, len(data.Items))
	seen := map[int64]bool{}
	for _, it := range data.Items {
		items = append(items, engineFromListItem(it))
		seen[it.ID] = true
	}
	missing = appendUnseen(missing, ids, seen)
	return finishList(items, data.NextCursor, data.Total, q, missing), nil
}

func (c *Catalog) ListRoles(ctx context.Context, q collect.Query) (repr.List[repr.Role], error) {
	if c == nil || c.Public == nil {
		return repr.List[repr.Role]{}, problem.New(problem.CodeServiceUnavailable, "", "", "catalog read is not bound.")
	}
	ids, missing, err := c.batchEntityIDs(ctx, q, entityTypeNone)
	if err != nil {
		return repr.List[repr.Role]{}, err
	}
	if q.Batch && len(ids) == 0 {
		return finishList([]repr.Role{}, nil, 0, q, missing), nil
	}
	f := catsvc.RolesListFilter{IDs: ids}
	limit := q.Limit
	if q.Batch {
		limit = 100
		q.Cursor = ""
	}
	data, lerr := c.Public.RolesList(ctx, f, q.Cursor, limit)
	if lerr != nil {
		return repr.List[repr.Role]{}, listCursorErr(lerr)
	}
	items := make([]repr.Role, 0, len(data.Items))
	seen := map[int64]bool{}
	for _, it := range data.Items {
		items = append(items, roleFromListItem(it))
		seen[it.ID] = true
	}
	missing = appendUnseen(missing, ids, seen)
	return finishList(items, data.NextCursor, data.Total, q, missing), nil
}

func (c *Catalog) ListSeries(ctx context.Context, q collect.Query) (repr.List[repr.Series], error) {
	if c == nil || c.Public == nil {
		return repr.List[repr.Series]{}, problem.New(problem.CodeServiceUnavailable, "", "", "catalog read is not bound.")
	}
	ids, missing, err := c.batchEntityIDs(ctx, q, entityTypeNone)
	if err != nil {
		return repr.List[repr.Series]{}, err
	}
	if q.Batch {
		items := make([]repr.Series, 0, len(ids))
		seen := map[int64]bool{}
		for _, id := range ids {
			rec, found, derr := c.Public.SeriesDetail(ctx, id, false, q.NSFW, 0, 0)
			if derr != nil {
				return repr.List[repr.Series]{}, derr
			}
			if !found || seen[id] {
				continue
			}
			items = append(items, seriesFromDetail(rec, nil))
			seen[id] = true
		}
		missing = appendUnseen(missing, ids, seen)
		return finishList(items, nil, int64(len(items)), q, missing), nil
	}
	data, lerr := c.Public.SeriesList(ctx, q.NSFW, q.Cursor, "", q.Limit)
	if lerr != nil {
		return repr.List[repr.Series]{}, listCursorErr(lerr)
	}
	items := make([]repr.Series, 0, len(data.Items))
	for _, it := range data.Items {
		items = append(items, seriesFromListItem(it))
	}
	return finishList(items, data.NextCursor, data.Total, q, missing), nil
}

func releaseFeedKinds() []int16 {
	return []int16{
		catmodel.ReleaseKindDefault, catmodel.ReleaseKindDigital, catmodel.ReleaseKindPhysical,
		catmodel.ReleaseKindTrial, catmodel.ReleaseKindPatch,
	}
}

func (c *Catalog) ListReleases(ctx context.Context, q collect.Query) (repr.List[repr.Release], error) {
	if c == nil || c.Public == nil {
		return repr.List[repr.Release]{}, problem.New(problem.CodeServiceUnavailable, "", "", "catalog read is not bound.")
	}
	ids, missing, err := c.batchEntityIDs(ctx, q, catmodel.EntityTypeRelease)
	if err != nil {
		return repr.List[repr.Release]{}, err
	}
	if q.Batch && len(ids) == 0 {
		return finishList([]repr.Release{}, nil, 0, q, missing), nil
	}
	sort := q.Sort
	if sort == "" {
		sort = catsvc.ReleaseFeedSortDateDesc
	}
	// OLang must be spelled out: the zero PublicOLang is not "no language gate",
	// it is ja+zh. Leaving it zero here narrowed the whole releases lane to the
	// home population, so an en work's release was reachable from the work
	// sub-face and 404'd on /v2/catalog/releases/{id} (PR #87's defect again).
	f := catsvc.ReleaseFeedFilter{
		NSFW: q.NSFW, Sort: sort, Kinds: releaseFeedKinds(), IDs: ids,
		OLang: catsvc.PublicOLang{All: true},
	}
	limit := q.Limit
	if q.Batch {
		limit = 100
		q.Cursor = ""
	}
	data, lerr := c.Public.ReleaseFeed(ctx, f, q.Cursor, limit)
	if lerr != nil {
		return repr.List[repr.Release]{}, listCursorErr(lerr)
	}
	items := make([]repr.Release, 0, len(data.Items))
	seen := map[int64]bool{}
	for _, it := range data.Items {
		items = append(items, releaseFromFeed(it))
		seen[it.ID] = true
	}
	missing = appendUnseen(missing, ids, seen)
	var total int64
	if q.IncludeTotal {
		n, _, _, merr := c.Public.ReleaseFeedMeta(ctx, f)
		if merr != nil {
			return repr.List[repr.Release]{}, merr
		}
		total = n
	}
	return finishList(items, data.NextCursor, total, q, missing), nil
}

const entityTypeNone int16 = -1

func (c *Catalog) batchEntityIDs(ctx context.Context, q collect.Query, entityType int16) ([]int64, []string, error) {
	if !q.Batch {
		return nil, nil, nil
	}
	var ids []int64
	var missing []string
	for _, s := range q.IDs {
		n, ok := repr.ParseID(s)
		if !ok {
			p := problem.New(problem.CodeInvalidParameter, "", "", "ids= values must be decimal catalog ids.")
			p.Errors = []problem.FieldError{{Parameter: "ids", Reason: problem.ReasonInvalidFormat, Detail: s}}
			return nil, nil, p
		}
		ids = append(ids, n)
	}
	for _, r := range q.Refs {
		ref := r.Source + ":" + r.ExternalID
		if entityType < 0 || c == nil || c.Public == nil {
			missing = append(missing, ref)
			continue
		}
		id, err := c.Public.LookupEntityID(ctx, r.Source, r.ExternalID, entityType)
		if err != nil {
			return nil, nil, err
		}
		if id == 0 {
			missing = append(missing, ref)
			continue
		}
		ids = append(ids, id)
	}
	return ids, missing, nil
}

func listCursorErr(err error) error {
	if errors.Is(err, catsvc.ErrBadCursor) {
		return collectInvalidCursor()
	}
	return err
}

func appendUnseen(missing []string, ids []int64, seen map[int64]bool) []string {
	for _, id := range ids {
		if !seen[id] {
			missing = append(missing, repr.ID(id))
		}
	}
	return missing
}

func finishList[T any](items []T, next *string, total int64, q collect.Query, missing []string) repr.List[T] {
	var enc *string
	if next != nil && *next != "" && !q.Batch {
		e := collect.EncodeCursor(*next)
		enc = &e
	}
	out := repr.NewList(items, enc)
	if q.IncludeTotal {
		n := total
		out.Total = &n
	}
	if missing != nil {
		m := missing
		out.Missing = &m
	}
	return out
}

func finishPageList[T any](items []T, total int64) repr.List[T] {
	out := repr.NewList(items, nil)
	n := total
	out.Total = &n
	rel := "eq"
	out.TotalRelation = &rel
	return out
}

func searchPage(q collect.Query) (int, *problem.Problem) {
	if q.Page > 0 {
		return q.Page, nil
	}
	page := 1
	if q.Cursor != "" {
		n, err := strconv.Atoi(q.Cursor)
		if err != nil || n < 1 {
			return 0, collectInvalidCursor()
		}
		page = n
	}
	return page, nil
}
