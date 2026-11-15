package handler

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/parse"
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
	Record               string `query:"record" maxLength:"16" example:"playtime" doc:"Which works make up the list. state, the default, lists the works with a play state; playtime the works with a playtime, play state or not; any the works with a play state, a playtime or a rating."`
	State                string `query:"state" maxLength:"64" example:"doing,done" doc:"Comma-separated closed vocabulary: wish, doing, done, on_hold, dropped, and none for a work with no play state, which only record=playtime and record=any list. Absent lists every state."`
	IncludePlaytimeTotal string `query:"include_playtime_total" maxLength:"5" example:"true" doc:"true adds playtime_total_minutes: the sum of every listed work's playtime.minutes after the same filters as the items."`
	ContentLimit         string `query:"content_limit" maxLength:"32" example:"sfw" doc:"Comma-separated closed editorial axis: sfw, nsfw, as on /v2/catalog/works. Absent applies no gate. total and the state facet count after it, so a reader who hides nsfw works gets counts that match what it shows."`
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
	Body repr.ListWorkRecord
}

const workRecordListDoc = "record= picks the works listed: state (the default) lists only works with a play state, so a work with only a playtime or only a rating is left out; playtime lists every work with a playtime; any lists every work with a play state, a playtime or a rating. Under playtime and any a work without a play state has a null work_state. Works that are no longer live in the catalog are left out. " +
	"sort=updated_desc, the default, is newest first by the play state's updated_at, or for a work with no play state the newer of its playtime's and its rating's updated_at; updated_asc is the reverse. playtime_desc and playtime_asc order by playtime.minutes, rating_desc and rating_asc by rating.overall; works with no such value come last in both directions, and work_id breaks ties in the sort's direction. A cursor belongs to the sort that issued it; another sort's cursor is 400 INVALID_CURSOR. " +
	"facets=state counts the works per state after the same filters as the items, state= included, as every v2 facet does: all five states are present, 0 when empty, and under record=playtime or record=any a sixth bucket none counts the works with no play state. Ask for it on a request without state= to fill a row of tabs. include_total counts the items; include_playtime_total=true adds playtime_total_minutes. "

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

var workRecordSets = []string{catsvc.WorkRecordSetState, catsvc.WorkRecordSetPlaytime, catsvc.WorkRecordSetAny}

func (in WorkRecordFilterInput) filter() (catsvc.PublicWorkRecordFilter, error) {
	record := catsvc.WorkRecordSetState
	if in.Record != "" {
		v, perr := parse.Enum(in.Record, "record", workRecordSets)
		if perr != nil {
			return catsvc.PublicWorkRecordFilter{}, perr
		}
		record = v
	}
	stateTokens := workStateTokens
	if record != catsvc.WorkRecordSetState {
		stateTokens = append(slices.Clone(workStateTokens), "none")
	}
	states, perr := closedCSV(in.State, "state", stateTokens)
	if perr != nil {
		return catsvc.PublicWorkRecordFilter{}, perr
	}
	limits, perr := closedCSV(in.ContentLimit, "content_limit", []string{
		catmodel.DisplayLimitKeySFW, catmodel.DisplayLimitKeyNSFW,
	})
	if perr != nil {
		return catsvc.PublicWorkRecordFilter{}, perr
	}
	f := catsvc.PublicWorkRecordFilter{Record: record, DisplayLimits: limits}
	for _, s := range states {
		if s == "none" {
			f.NoState = true
			continue
		}
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
		page, lerr := cat.ListWorkRecords(ctx, uid, in.WorkRecordFilterInput, q)
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
		page, lerr := cat.ListMyWorkRecords(ctx, in.WorkRecordFilterInput, q)
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listWorkRecordsOutput{Body: page}, nil
	}
}

func (c *Catalog) ListWorkRecords(ctx context.Context, uid int64, in WorkRecordFilterInput, q collect.Query) (repr.ListWorkRecord, error) {
	if c == nil || c.WorkRecords == nil {
		return repr.ListWorkRecord{}, problem.New(problem.CodeServiceUnavailable, "", "", "work records are not bound.")
	}
	if userIDFrom(ctx) != uid {
		privacy, err := c.WorkRecords.Privacy(ctx, uid)
		if err != nil {
			return repr.ListWorkRecord{}, err
		}
		if privacy.Private {
			return repr.ListWorkRecord{}, problem.New(problem.CodePlayRecordsPrivate, "", "", "this account keeps its play records private.")
		}
	}
	return c.listWorkRecordsOf(ctx, uid, in, q)
}

func (c *Catalog) ListMyWorkRecords(ctx context.Context, in WorkRecordFilterInput, q collect.Query) (repr.ListWorkRecord, error) {
	if c == nil || c.WorkRecords == nil {
		return repr.ListWorkRecord{}, problem.New(problem.CodeServiceUnavailable, "", "", "work records are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return repr.ListWorkRecord{}, err
	}
	return c.listWorkRecordsOf(ctx, uid, in, q)
}

func workRecordSort(token string) catsvc.WorkRecordSort {
	key, dir, _ := strings.Cut(token, "_")
	return catsvc.WorkRecordSort{Key: key, Desc: dir == "desc"}
}

// The default sort keeps the bare time|id cursor it has always issued, so a
// cursor handed out before sort= existed still pages.
func parseWorkRecordCursor(token, cursor string) (*catsvc.WorkRecordKey, bool) {
	if token == "updated_desc" {
		at, id, ok := parseTimeIDCursor(cursor)
		return &catsvc.WorkRecordKey{At: at, WorkID: id}, ok
	}
	rest, ok := strings.CutPrefix(cursor, token+":")
	if !ok {
		return nil, false
	}
	val, idRaw, ok := strings.Cut(rest, "|")
	id, idOK := repr.ParseID(idRaw)
	if !ok || !idOK {
		return nil, false
	}
	k := &catsvc.WorkRecordKey{WorkID: id}
	switch {
	case workRecordSort(token).Key == catsvc.WorkRecordSortUpdated:
		at, err := time.Parse(time.RFC3339Nano, val)
		k.At = at
		return k, err == nil
	case val == "-":
		k.Null = true
		return k, true
	default:
		n, err := strconv.ParseInt(val, 10, 64)
		k.Value = n
		return k, err == nil
	}
}

func workRecordCursor(token string, k catsvc.WorkRecordKey) *string {
	if token == "updated_desc" {
		return timeIDCursor(k.At, k.WorkID)
	}
	val := "-"
	switch {
	case workRecordSort(token).Key == catsvc.WorkRecordSortUpdated:
		val = k.At.UTC().Format(time.RFC3339Nano)
	case !k.Null:
		val = strconv.FormatInt(k.Value, 10)
	}
	s := token + ":" + val + "|" + repr.ID(k.WorkID)
	return &s
}

func (c *Catalog) listWorkRecordsOf(ctx context.Context, uid int64, in WorkRecordFilterInput, q collect.Query) (repr.ListWorkRecord, error) {
	var empty repr.ListWorkRecord
	f, err := in.filter()
	if err != nil {
		return empty, err
	}
	var withPlaytimeTotal bool
	if in.IncludePlaytimeTotal != "" {
		v, perr := parse.Bool(in.IncludePlaytimeTotal, "include_playtime_total")
		if perr != nil {
			return empty, perr
		}
		withPlaytimeTotal = v
	}
	sort := workRecordSort(q.Sort)
	var after *catsvc.WorkRecordKey
	if q.Cursor != "" {
		var ok bool
		if after, ok = parseWorkRecordCursor(q.Sort, q.Cursor); !ok {
			return empty, collectInvalidCursor()
		}
	}
	limit := q.Limit
	if limit <= 0 {
		limit = collect.DefaultLimit
	}
	rows, err := c.WorkRecords.ListPublic(ctx, uid, f, sort, after, limit+1)
	if err != nil {
		return empty, err
	}
	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		next = workRecordCursor(q.Sort, rows[len(rows)-1].Key)
	}
	items := make([]repr.WorkRecord, 0, len(rows))
	for _, r := range rows {
		items = append(items, workRecordRepr(r.WorkRecord))
	}
	var counts catsvc.WorkRecordCounts
	if q.IncludeTotal || len(q.Facets) > 0 || withPlaytimeTotal {
		if counts, err = c.WorkRecords.CountPublic(ctx, uid, f); err != nil {
			return empty, err
		}
	}
	total := counts.NoState
	for _, n := range counts.ByState {
		total += n
	}
	out := repr.ListWorkRecord{List: finishList(items, next, total, q, nil)}
	if withPlaytimeTotal {
		out.PlaytimeTotalMinutes = &counts.PlaytimeMinutes
	}
	if len(q.Facets) > 0 {
		buckets := make([]repr.FacetValue, 0, len(workStateTokens)+1)
		for _, tok := range workStateTokens {
			st, _ := workStateFromWords(tok, "")
			buckets = append(buckets, repr.FacetValue{Value: tok, DisplayName: tok, Count: int(counts.ByState[st])})
		}
		if f.Record != catsvc.WorkRecordSetState {
			buckets = append(buckets, repr.FacetValue{Value: "none", DisplayName: "none", Count: int(counts.NoState)})
		}
		out.Facets = &map[string][]repr.FacetValue{"state": buckets}
	}
	return out, nil
}
