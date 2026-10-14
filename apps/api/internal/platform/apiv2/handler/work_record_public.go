package handler

import (
	"context"
	"net/http"
	"time"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	catmodel "api/internal/platform/catalog/model"
	catsvc "api/internal/platform/catalog/service"

	"github.com/danielgtaylor/huma/v2"
)

var workStateTokens = []string{"wish", "doing", "done", "on_hold", "dropped"}

// Exported because huma drops the query parameters of an unexported embedded
// struct; see CollectionInput.
type WorkRecordFilterInput struct {
	State        string `query:"state" maxLength:"64" example:"doing,done" doc:"Comma-separated closed vocabulary: wish, doing, done, on_hold, dropped. Absent lists every state."`
	ContentLimit string `query:"content_limit" maxLength:"32" example:"sfw" doc:"Comma-separated closed editorial axis: sfw, nsfw, as on /v2/catalog/works. Absent applies no gate. total and the state facet count after it, so a reader who hides nsfw works gets counts that match what it shows."`
}

type listWorkRecordsInput struct {
	CollectionInput
	WorkRecordFilterInput
	UserID string `query:"user_id" required:"true" minLength:"1" maxLength:"20" example:"2" doc:"The account whose play records to list — the central sign-in user id, the same one a rating reports as user_id."`
}

type listMyWorkRecordsInput struct {
	CollectionInput
	WorkRecordFilterInput
}

type listWorkRecordsOutput struct {
	Body repr.List[repr.WorkRecord]
}

const workRecordListDoc = "Newest first by the play state's updated_at; a work with only a playtime or only a rating is not listed. Works that are no longer live in the catalog are left out. " +
	"facets=state counts the works per state after the same filters as the items, state= included, as every v2 facet does: all five states are present, 0 when empty. Ask for it on a request without state= to fill a row of tabs. include_total counts the items. "

func registerPublicWorkRecords(api huma.API, cat *Catalog) {
	errs := collectionErrors(http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable)
	huma.Register(api, huma.Operation{
		OperationID: "listWorkRecords", Method: http.MethodGet, Path: "/v2/work-records",
		Summary: "List an account's play records",
		Description: "The works one account has a play state on — wish, doing, done, on_hold or dropped — each with its playtime and rating, the shape /v2/me/work-records/{work_id} answers its owner. This is the face a profile page's game library reads. " +
			workRecordListDoc +
			"An account that keeps its play records private (PATCH /v2/me/privacy) is 403 PLAY_RECORDS_PRIVATE, except to its own user token; the account reads its own list, private or not, on /v2/me/work-records, which needs no scope. Play records are public by default. " +
			"Requires an application key or a user access token with catalog:read.",
		Tags:               []string{"ratings"},
		Errors:             errs,
		SkipValidateParams: true,
	}, listWorkRecords(cat))
	huma.Register(api, huma.Operation{
		OperationID: "listMyWorkRecords", Method: http.MethodGet, Path: "/v2/me/work-records",
		Summary: "List my play records",
		Description: "The bearer's own play records: the works they have a play state on, each with its playtime and rating, with the filters and facets of /v2/work-records. The privacy switch does not apply: this is the owner's view of their own profile. " +
			workRecordListDoc +
			"Requires a user access token. Any app may call this; no scope is required.",
		Tags:               []string{"me"},
		Errors:             errs,
		SkipValidateParams: true,
	}, listMyWorkRecords(cat))
}

func (in WorkRecordFilterInput) filter() (catsvc.PublicWorkRecordFilter, error) {
	states, perr := closedCSV(in.State, "state", workStateTokens)
	if perr != nil {
		return catsvc.PublicWorkRecordFilter{}, perr
	}
	limits, perr := closedCSV(in.ContentLimit, "content_limit", []string{
		catmodel.DisplayLimitKeySFW, catmodel.DisplayLimitKeyNSFW,
	})
	if perr != nil {
		return catsvc.PublicWorkRecordFilter{}, perr
	}
	f := catsvc.PublicWorkRecordFilter{DisplayLimits: limits}
	for _, s := range states {
		st, _ := workStateFromWords(s, "")
		f.States = append(f.States, st)
	}
	return f, nil
}

func listWorkRecords(cat *Catalog) func(context.Context, *listWorkRecordsInput) (*listWorkRecordsOutput, error) {
	return func(ctx context.Context, in *listWorkRecordsInput) (*listWorkRecordsOutput, error) {
		q, err := parseCatalogList(ctx, &in.CollectionInput, collect.WorkRecordSpec())
		if err != nil {
			return nil, err
		}
		uid, err := requiredIDParam("user_id", in.UserID)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		f, err := in.filter()
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		page, lerr := cat.ListWorkRecords(ctx, uid, f, q)
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listWorkRecordsOutput{Body: page}, nil
	}
}

func listMyWorkRecords(cat *Catalog) func(context.Context, *listMyWorkRecordsInput) (*listWorkRecordsOutput, error) {
	return func(ctx context.Context, in *listMyWorkRecordsInput) (*listWorkRecordsOutput, error) {
		q, err := parseCatalogList(ctx, &in.CollectionInput, collect.WorkRecordSpec())
		if err != nil {
			return nil, err
		}
		f, err := in.filter()
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		page, lerr := cat.ListMyWorkRecords(ctx, f, q)
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listWorkRecordsOutput{Body: page}, nil
	}
}

func (c *Catalog) ListWorkRecords(ctx context.Context, uid int64, f catsvc.PublicWorkRecordFilter, q collect.Query) (repr.List[repr.WorkRecord], error) {
	if c == nil || c.WorkRecords == nil {
		return repr.List[repr.WorkRecord]{}, problem.New(problem.CodeServiceUnavailable, "", "", "work records are not bound.")
	}
	if userIDFrom(ctx) != uid {
		privacy, err := c.WorkRecords.Privacy(ctx, uid)
		if err != nil {
			return repr.List[repr.WorkRecord]{}, err
		}
		if privacy.Private {
			return repr.List[repr.WorkRecord]{}, problem.New(problem.CodePlayRecordsPrivate, "", "", "this account keeps its play records private.")
		}
	}
	return c.listWorkRecordsOf(ctx, uid, f, q)
}

func (c *Catalog) ListMyWorkRecords(ctx context.Context, f catsvc.PublicWorkRecordFilter, q collect.Query) (repr.List[repr.WorkRecord], error) {
	if c == nil || c.WorkRecords == nil {
		return repr.List[repr.WorkRecord]{}, problem.New(problem.CodeServiceUnavailable, "", "", "work records are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return repr.List[repr.WorkRecord]{}, err
	}
	return c.listWorkRecordsOf(ctx, uid, f, q)
}

func (c *Catalog) listWorkRecordsOf(ctx context.Context, uid int64, f catsvc.PublicWorkRecordFilter, q collect.Query) (repr.List[repr.WorkRecord], error) {
	var empty repr.List[repr.WorkRecord]
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
	rows, err := c.WorkRecords.ListPublic(ctx, uid, f, before, beforeID, limit+1)
	if err != nil {
		return empty, err
	}
	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1].WorkState
		next = timeIDCursor(last.UpdatedAt, last.WorkID)
	}
	items := make([]repr.WorkRecord, 0, len(rows))
	for _, r := range rows {
		items = append(items, workRecordRepr(r))
	}
	var total int64
	var counts map[int16]int64
	if q.IncludeTotal || len(q.Facets) > 0 {
		if counts, err = c.WorkRecords.CountPublicByState(ctx, uid, f); err != nil {
			return empty, err
		}
		for _, n := range counts {
			total += n
		}
	}
	out := finishList(items, next, total, q, nil)
	if len(q.Facets) > 0 {
		buckets := make([]repr.FacetValue, 0, len(workStateTokens))
		for _, tok := range workStateTokens {
			st, _ := workStateFromWords(tok, "")
			buckets = append(buckets, repr.FacetValue{Value: tok, DisplayName: tok, Count: int(counts[st])})
		}
		out.Facets = &map[string][]repr.FacetValue{"state": buckets}
	}
	return out, nil
}
