package handler

import (
	"context"
	"fmt"
	"time"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	"api/internal/platform/catalog/model"
	catsvc "api/internal/platform/catalog/service"
)

func companyFollowRepr(r catsvc.EntityFollowRecord) repr.CompanyFollow {
	return repr.CompanyFollow{
		Object: "company_follow", CompanyID: repr.ID(r.EntityID),
		CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func (c *Catalog) ListCompanyFollows(ctx context.Context, q collect.Query, companyIDs []string) (repr.List[repr.CompanyFollow], error) {
	if c == nil || c.CompanyFollows == nil {
		return repr.List[repr.CompanyFollow]{}, problem.New(problem.CodeServiceUnavailable, "", "", "company follows are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return repr.List[repr.CompanyFollow]{}, err
	}
	if len(companyIDs) > 0 {
		ids, perr := parseCompanyIDs(companyIDs)
		if perr != nil {
			return repr.List[repr.CompanyFollow]{}, perr
		}
		rows, gerr := c.CompanyFollows.ListMineFor(ctx, uid, ids)
		if gerr != nil {
			return repr.List[repr.CompanyFollow]{}, companyFollowErr(gerr)
		}
		byCompany := make(map[int64]catsvc.EntityFollowRecord, len(rows))
		for _, r := range rows {
			byCompany[r.EntityID] = r
		}
		items := make([]repr.CompanyFollow, 0, len(ids))
		var missing []string
		for i, id := range ids {
			r, ok := byCompany[id]
			if !ok {
				missing = append(missing, companyIDs[i])
				continue
			}
			items = append(items, companyFollowRepr(r))
		}
		return finishList(items, nil, int64(len(items)), collect.Query{Batch: true, IncludeTotal: q.IncludeTotal}, missing), nil
	}
	var before int64
	if q.Cursor != "" {
		id, ok := repr.ParseID(q.Cursor)
		if !ok {
			return repr.List[repr.CompanyFollow]{}, collectInvalidCursor()
		}
		before = id
	}
	limit := q.Limit
	if limit <= 0 {
		limit = collect.DefaultLimit
	}
	rows, lerr := c.CompanyFollows.ListMine(ctx, uid, before, limit+1)
	if lerr != nil {
		return repr.List[repr.CompanyFollow]{}, companyFollowErr(lerr)
	}
	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		s := repr.ID(rows[len(rows)-1].ID)
		next = &s
	}
	items := make([]repr.CompanyFollow, 0, len(rows))
	for _, r := range rows {
		items = append(items, companyFollowRepr(r))
	}
	var total int64
	if q.IncludeTotal {
		if total, lerr = c.CompanyFollows.CountMine(ctx, uid); lerr != nil {
			return repr.List[repr.CompanyFollow]{}, companyFollowErr(lerr)
		}
	}
	return finishList(items, next, total, q, nil), nil
}

func (c *Catalog) GetCompanyFollow(ctx context.Context, companyID int64) (repr.CompanyFollow, error) {
	if c == nil || c.CompanyFollows == nil {
		return repr.CompanyFollow{}, problem.New(problem.CodeServiceUnavailable, "", "", "company follows are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return repr.CompanyFollow{}, err
	}
	rows, gerr := c.CompanyFollows.ListMineFor(ctx, uid, []int64{companyID})
	if gerr != nil {
		return repr.CompanyFollow{}, companyFollowErr(gerr)
	}
	if len(rows) == 0 {
		return repr.CompanyFollow{}, problem.New(problem.CodeNotFound, "", "", "Not following this company.")
	}
	return companyFollowRepr(rows[0]), nil
}

func (c *Catalog) PutCompanyFollow(ctx context.Context, companyID int64) (repr.CompanyFollow, error) {
	if c == nil || c.CompanyFollows == nil {
		return repr.CompanyFollow{}, problem.New(problem.CodeServiceUnavailable, "", "", "company follows are not bound.")
	}
	uid, clientID, err := requireUser(ctx)
	if err != nil {
		return repr.CompanyFollow{}, err
	}
	rec, gerr := c.CompanyFollows.FollowCompany(ctx, uid, companyID, clientID, siteFrom(ctx))
	if gerr != nil {
		if gerr == catsvc.ErrFollowCompanyUnavailable {
			return repr.CompanyFollow{}, c.mergedOrNotFound(ctx, model.EntityTypeLabel, "company", companyID)
		}
		return repr.CompanyFollow{}, companyFollowErr(gerr)
	}
	return companyFollowRepr(*rec), nil
}

func (c *Catalog) DeleteCompanyFollow(ctx context.Context, companyID int64) error {
	if c == nil || c.CompanyFollows == nil {
		return problem.New(problem.CodeServiceUnavailable, "", "", "company follows are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return err
	}
	return companyFollowErr(c.CompanyFollows.UnfollowCompany(ctx, uid, companyID))
}

func parseCompanyIDs(companyIDs []string) ([]int64, error) {
	if len(companyIDs) > collect.MaxBatchItems {
		return nil, problem.New(problem.CodeTooManyIDs, "", "", "company_ids named more than 100 items.")
	}
	ids := make([]int64, 0, len(companyIDs))
	for _, s := range companyIDs {
		id, ok := repr.ParseID(s)
		if !ok {
			p := problem.New(problem.CodeInvalidParameter, "", "", "company_ids values must be decimal catalog ids.")
			p.Errors = []problem.FieldError{{Parameter: "company_ids", Reason: problem.ReasonInvalidFormat, Detail: s}}
			return nil, p
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func companyFollowErr(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case err == catsvc.ErrFollowLimit:
		return problem.New(problem.CodeValidationFailed, "", "", fmt.Sprintf("a user may follow at most %d companies.", model.EntityFollowsPerUserMax))
	case err == catsvc.ErrFollowActorRequired:
		return problem.New(problem.CodeUserIdentityRequired, "", "", err.Error())
	case err == catsvc.ErrFollowCompanyUnavailable:
		return problem.New(problem.CodeNotFound, "", "", "company not found.")
	}
	return err
}
