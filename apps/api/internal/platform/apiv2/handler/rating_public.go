package handler

import (
	"context"
	"net/http"
	"strings"
	"time"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	catsvc "api/internal/platform/catalog/service"

	"github.com/danielgtaylor/huma/v2"
)

type listRatingsInput struct {
	CollectionInput
	WorkID string `query:"work_id" maxLength:"20" doc:"The ratings of one work. At least one of work_id and user_id is required."`
	UserID string `query:"user_id" maxLength:"20" doc:"The ratings one account wrote — the central sign-in user id. At least one of work_id and user_id is required."`
}
type ratingPathInput struct {
	ID string `path:"id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Rating id."`
}
type listRatingChangesOutput struct {
	Body repr.List[repr.RatingChange]
}

func registerPublicRatings(api huma.API, cat *Catalog) {
	tags := []string{"ratings"}
	errs := collectionErrors(http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusServiceUnavailable)

	huma.Register(api, huma.Operation{
		OperationID: "listRatingChanges", Method: http.MethodGet, Path: "/v2/ratings/changes",
		Summary:     "Rating changes feed",
		Description: "Every rating written or deleted on any NextMoe site, oldest first, keyset-paginated. **This is the mirror channel** for a site that keeps its own copy of the ratings: bootstrap from an empty cursor (the first drain is the whole population), then poll at your own cadence. next_cursor comes only with a full page, so keep the last one you received and poll from it: the partial page after it is read again, which an upsert absorbs. A written entry carries the rating as it stands; an entry with gone: true is a deletion, so drop the mirrored row. A change surfaces five seconds after it commits. ids= is not accepted. Requires an application key or a user access token with catalog:read.",
		Tags:        tags, Errors: errs, SkipValidateParams: true,
	}, listRatingChanges(cat))

	huma.Register(api, huma.Operation{
		OperationID: "listRatings", Method: http.MethodGet, Path: "/v2/ratings",
		Summary:     "List ratings of a work or by an account",
		Description: "Newest first by creation, so an edit does not move a rating. work_id= or user_id= is required, and both together answer the one rating that account gave that work. No editorial gate applies: a caller that hides r18 works filters when it hydrates them. Requires an application key or a user access token with catalog:read.",
		Tags:        tags, Errors: errs, SkipValidateParams: true,
	}, listRatings(cat))

	huma.Register(api, huma.Operation{
		OperationID: "getRating", Method: http.MethodGet, Path: "/v2/ratings/{id}",
		Summary:     "Get one rating",
		Description: "404 when no rating has this id, including one that was deleted. Requires an application key or a user access token with catalog:read.",
		Tags:        tags, Errors: errs, SkipValidateParams: true,
	}, getRating(cat))
}

func listRatings(cat *Catalog) func(context.Context, *listRatingsInput) (*listMyRatingsOutput, error) {
	return func(ctx context.Context, in *listRatingsInput) (*listMyRatingsOutput, error) {
		q, err := parseCatalogList(ctx, &in.CollectionInput, collect.RatingSpec())
		if err != nil {
			return nil, err
		}
		var f catsvc.RatingFilter
		if in.WorkID != "" {
			if f.WorkID, err = requiredIDParam("work_id", in.WorkID); err != nil {
				return nil, catalogErr(ctx, err)
			}
		}
		if in.UserID != "" {
			if f.UserID, err = requiredIDParam("user_id", in.UserID); err != nil {
				return nil, catalogErr(ctx, err)
			}
		}
		if f.WorkID == 0 && f.UserID == 0 {
			p := problem.New(problem.CodeInvalidParameter, "", "", "work_id or user_id is required.")
			p.Errors = []problem.FieldError{{Parameter: "work_id", Reason: problem.ReasonRequired, Detail: "send work_id, user_id, or both"}}
			return nil, catalogErr(ctx, p)
		}
		page, lerr := cat.ListRatings(ctx, q, f)
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listMyRatingsOutput{Body: page}, nil
	}
}

func getRating(cat *Catalog) func(context.Context, *ratingPathInput) (*userRatingOutput, error) {
	return func(ctx context.Context, in *ratingPathInput) (*userRatingOutput, error) {
		id, ok := repr.ParseID(in.ID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.ID))
		}
		if cat == nil || cat.Ratings == nil {
			return nil, catalogErr(ctx, problem.New(problem.CodeServiceUnavailable, "", "", "ratings are not bound."))
		}
		rec, err := cat.Ratings.Get(ctx, id)
		if err != nil {
			return nil, catalogErr(ctx, ratingErr(err, ""))
		}
		return &userRatingOutput{Body: userRatingRepr(*rec)}, nil
	}
}

func listRatingChanges(cat *Catalog) func(context.Context, *CollectionInput) (*listRatingChangesOutput, error) {
	return func(ctx context.Context, in *CollectionInput) (*listRatingChangesOutput, error) {
		q, err := parseCatalogList(ctx, in, collect.RatingChangeSpec())
		if err != nil {
			return nil, err
		}
		page, lerr := cat.ListRatingChanges(ctx, q)
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listRatingChangesOutput{Body: page}, nil
	}
}

func parseTimeIDCursor(cursor string) (time.Time, int64, bool) {
	ts, id, ok := strings.Cut(cursor, "|")
	n, idOK := repr.ParseID(id)
	t, err := time.Parse(time.RFC3339Nano, ts)
	return t, n, ok && idOK && err == nil
}

func timeIDCursor(t time.Time, id int64) *string {
	s := t.UTC().Format(time.RFC3339Nano) + "|" + repr.ID(id)
	return &s
}

func (c *Catalog) ListRatings(ctx context.Context, q collect.Query, f catsvc.RatingFilter) (repr.List[repr.UserRating], error) {
	var empty repr.List[repr.UserRating]
	if c == nil || c.Ratings == nil {
		return empty, problem.New(problem.CodeServiceUnavailable, "", "", "ratings are not bound.")
	}
	var before time.Time
	var beforeID int64
	if q.Cursor != "" {
		var ok bool
		if before, beforeID, ok = parseTimeIDCursor(q.Cursor); !ok {
			return empty, collectInvalidCursor()
		}
	}
	limit := q.Limit
	if limit <= 0 {
		limit = collect.DefaultLimit
	}
	rows, err := c.Ratings.List(ctx, f, before, beforeID, limit+1)
	if err != nil {
		return empty, err
	}
	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		next = timeIDCursor(last.CreatedAt, last.ID)
	}
	items := make([]repr.UserRating, 0, len(rows))
	for _, r := range rows {
		items = append(items, userRatingRepr(r))
	}
	var total int64
	if q.IncludeTotal {
		if total, err = c.Ratings.Count(ctx, f); err != nil {
			return empty, err
		}
	}
	return finishList(items, next, total, q, nil), nil
}

func (c *Catalog) ListRatingChanges(ctx context.Context, q collect.Query) (repr.List[repr.RatingChange], error) {
	var empty repr.List[repr.RatingChange]
	if c == nil || c.Ratings == nil {
		return empty, problem.New(problem.CodeServiceUnavailable, "", "", "ratings are not bound.")
	}
	var after time.Time
	var afterID int64
	if q.Cursor != "" {
		var ok bool
		if after, afterID, ok = parseTimeIDCursor(q.Cursor); !ok {
			return empty, collectInvalidCursor()
		}
	}
	limit := q.Limit
	if limit <= 0 {
		limit = collect.DefaultLimit
	}
	rows, err := c.Ratings.Changes(ctx, after, afterID, limit)
	if err != nil {
		return empty, err
	}
	items := make([]repr.RatingChange, 0, len(rows))
	for _, r := range rows {
		items = append(items, repr.RatingChange{
			Object: "rating_change", ID: repr.ID(r.ID), WorkID: repr.ID(r.WorkID), Gone: r.Gone,
			ChangedAt: r.ChangedAt.UTC().Format(time.RFC3339Nano), Rating: userRatingPtr(r.Rating),
		})
	}
	var next *string
	if len(rows) == limit {
		last := rows[len(rows)-1]
		next = timeIDCursor(last.ChangedAt, last.ID)
	}
	var total int64
	if q.IncludeTotal {
		if total, err = c.Ratings.ChangesTotal(ctx); err != nil {
			return empty, err
		}
	}
	return finishList(items, next, total, q, nil), nil
}
