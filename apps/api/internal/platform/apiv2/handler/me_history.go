package handler

import (
	"context"
	"net/http"
	"strings"
	"time"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	catmodel "api/internal/platform/catalog/model"
	catsvc "api/internal/platform/catalog/service"
	"api/internal/platform/devapi"

	"github.com/danielgtaylor/huma/v2"
)

const (
	historyScopeApp = "app"
	historyScopeAll = "all"
)

// Exported because huma drops the query parameters of an unexported embedded
// struct; see CollectionInput.
type HistoryScopeInput struct {
	Scope string `query:"scope" maxLength:"8" example:"app" doc:"app (the default) acts on the history this app recorded. all acts on every app's history merged per work: listing it needs the history:read scope and the account's browsing_history_sharing set to shared; deleting it needs neither."`
}

type listHistoryInput struct {
	CollectionInput
	HistoryScopeInput
	ContentLimit string `query:"content_limit" maxLength:"32" example:"sfw" doc:"Comma-separated closed editorial axis: sfw, nsfw, as on /v2/catalog/works. Absent applies no gate. A site that only shows sfw works passes sfw, so a work the account opened on an adult site never reaches its page. total counts after it."`
}

type listHistoryOutput struct {
	Body repr.List[repr.BrowsingHistoryEntry]
}

type clearHistoryInput struct {
	HistoryScopeInput
}

type recordWorkViewInput struct {
	WorkID string `path:"work_id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Catalog work id."`
}

type forgetWorkViewInput struct {
	HistoryScopeInput
	WorkID string `path:"work_id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Catalog work id."`
}

const historyDoc = "Browsing history is kept per app: each app reads and deletes what it recorded, and entries older than 90 days or past the newest 500 per app are deleted. An account that switched browsing_history off on PATCH /v2/me/privacy has no history on the server. "

func registerMeHistory(api huma.API, cat *Catalog) {
	me := []string{"me"}
	errs := collectionErrors(http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusServiceUnavailable)
	huma.Register(api, huma.Operation{
		OperationID: "listMyBrowsingHistory", Method: http.MethodGet, Path: "/v2/me/history",
		Summary: "List my browsing history",
		Description: "The works the bearer opened, newest first. " + historyDoc +
			"scope=all merges every app's entries per work and is 403 BROWSING_HISTORY_NOT_SHARED unless the account set browsing_history_sharing to shared, and 403 SCOPE_REQUIRED without " + devapi.ScopeHistoryRead + ". " +
			"Works that are no longer live in the catalog are left out. Items carry ids only; read the works themselves from /v2/catalog/works?ids=. " +
			"Requires a user access token. With the default scope=app any app may call this and no scope is required.",
		Tags: me, Errors: errs, SkipValidateParams: true,
	}, listMyBrowsingHistory(cat))
	huma.Register(api, huma.Operation{
		OperationID: "clearMyBrowsingHistory", Method: http.MethodDelete, Path: "/v2/me/history",
		Summary: "Clear my browsing history",
		Description: "Deletes the bearer's browsing history: this app's with the default scope=app, every app's with scope=all. Recording goes on afterwards; to stop it, set browsing_history to off on PATCH /v2/me/privacy. 204 with no body. " +
			"Requires a user access token. Any app may call this; no scope is required.",
		Tags: me, Errors: errs, DefaultStatus: http.StatusNoContent, SkipValidateParams: true,
	}, clearMyBrowsingHistory(cat))
	huma.Register(api, huma.Operation{
		OperationID: "recordMyWorkView", Method: http.MethodPut, Path: "/v2/me/history/works/{work_id}",
		Summary: "Record that I opened a work",
		Description: "Moves the work to the top of this app's history for the bearer. Send it once the person has actually stayed on the work's page, not on prefetch or render. Visits less than a minute apart count once. " +
			"204 with no body, also when the account has browsing history off: then nothing is stored. 404 when the work is not live. " +
			"Requires a user access token. Any app may call this; no scope is required.",
		Tags: me, Errors: errs, DefaultStatus: http.StatusNoContent, SkipValidateParams: true,
	}, recordMyWorkView(cat))
	huma.Register(api, huma.Operation{
		OperationID: "forgetMyWorkView", Method: http.MethodDelete, Path: "/v2/me/history/works/{work_id}",
		Summary: "Remove one work from my browsing history",
		Description: "Deletes the work from this app's history with the default scope=app, from every app's with scope=all. 204 with no body, also when there was nothing to delete. " +
			"Requires a user access token. Any app may call this; no scope is required.",
		Tags: me, Errors: errs, DefaultStatus: http.StatusNoContent, SkipValidateParams: true,
	}, forgetMyWorkView(cat))
}

func (in HistoryScopeInput) parse() (string, *problem.Problem) {
	switch s := strings.TrimSpace(in.Scope); s {
	case "", historyScopeApp:
		return historyScopeApp, nil
	case historyScopeAll:
		return historyScopeAll, nil
	}
	return "", closedParam("scope", historyScopeApp+", "+historyScopeAll)
}

func listMyBrowsingHistory(cat *Catalog) func(context.Context, *listHistoryInput) (*listHistoryOutput, error) {
	return func(ctx context.Context, in *listHistoryInput) (*listHistoryOutput, error) {
		q, err := parseCatalogList(ctx, &in.CollectionInput, collect.BrowsingHistorySpec())
		if err != nil {
			return nil, err
		}
		scope, perr := in.parse()
		if perr != nil {
			return nil, catalogErr(ctx, perr)
		}
		limits, perr := closedCSV(in.ContentLimit, "content_limit", []string{
			catmodel.DisplayLimitKeySFW, catmodel.DisplayLimitKeyNSFW,
		})
		if perr != nil {
			return nil, catalogErr(ctx, perr)
		}
		page, lerr := cat.ListBrowsingHistory(ctx, scope, limits, q)
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listHistoryOutput{Body: page}, nil
	}
}

func clearMyBrowsingHistory(cat *Catalog) func(context.Context, *clearHistoryInput) (*struct{}, error) {
	return func(ctx context.Context, in *clearHistoryInput) (*struct{}, error) {
		scope, perr := in.parse()
		if perr != nil {
			return nil, catalogErr(ctx, perr)
		}
		if err := cat.ForgetBrowsingHistory(ctx, scope, 0); err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &struct{}{}, nil
	}
}

func recordMyWorkView(cat *Catalog) func(context.Context, *recordWorkViewInput) (*struct{}, error) {
	return func(ctx context.Context, in *recordWorkViewInput) (*struct{}, error) {
		id, ok := repr.ParseID(in.WorkID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.WorkID))
		}
		if err := cat.RecordWorkView(ctx, id); err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &struct{}{}, nil
	}
}

func forgetMyWorkView(cat *Catalog) func(context.Context, *forgetWorkViewInput) (*struct{}, error) {
	return func(ctx context.Context, in *forgetWorkViewInput) (*struct{}, error) {
		id, ok := repr.ParseID(in.WorkID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.WorkID))
		}
		scope, perr := in.parse()
		if perr != nil {
			return nil, catalogErr(ctx, perr)
		}
		if err := cat.ForgetBrowsingHistory(ctx, scope, id); err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &struct{}{}, nil
	}
}

func historyUnbound() *problem.Problem {
	return problem.New(problem.CodeServiceUnavailable, "", "", "browsing history is not bound.")
}

func browsingHistoryRepr(e catsvc.BrowsingHistoryEntry) repr.BrowsingHistoryEntry {
	return repr.BrowsingHistoryEntry{
		Object: "work_view", WorkID: repr.ID(e.WorkID),
		FirstViewedAt: e.FirstViewedAt.UTC().Format(time.RFC3339),
		LastViewedAt:  e.LastViewedAt.UTC().Format(time.RFC3339),
		ViewCount:     e.ViewCount,
	}
}

func (c *Catalog) ListBrowsingHistory(ctx context.Context, scope string, limits []string, q collect.Query) (repr.List[repr.BrowsingHistoryEntry], error) {
	var empty repr.List[repr.BrowsingHistoryEntry]
	if c == nil || c.BrowsingHistory == nil {
		return empty, historyUnbound()
	}
	uid, client, err := requireUser(ctx)
	if err != nil {
		return empty, err
	}
	f := catsvc.BrowsingHistoryFilter{ClientID: client, DisplayLimits: limits}
	if scope == historyScopeAll {
		settings, serr := c.BrowsingHistory.Settings(ctx, uid)
		if serr != nil {
			return empty, serr
		}
		if !settings.Shared {
			return empty, problem.New(problem.CodeBrowsingHistoryNotShared, "", "", "this account keeps its browsing history per app.")
		}
		f.ClientID = ""
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
	rows, err := c.BrowsingHistory.List(ctx, uid, f, before, beforeID, limit+1)
	if err != nil {
		return empty, err
	}
	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		next = timeIDCursor(last.LastViewedAt, last.WorkID)
	}
	items := make([]repr.BrowsingHistoryEntry, 0, len(rows))
	for _, r := range rows {
		items = append(items, browsingHistoryRepr(r))
	}
	var total int64
	if q.IncludeTotal {
		if total, err = c.BrowsingHistory.Count(ctx, uid, f); err != nil {
			return empty, err
		}
	}
	return finishList(items, next, total, q, nil), nil
}

func (c *Catalog) RecordWorkView(ctx context.Context, workID int64) error {
	if c == nil || c.BrowsingHistory == nil {
		return historyUnbound()
	}
	uid, client, err := requireUser(ctx)
	if err != nil {
		return err
	}
	return historyErr(c.BrowsingHistory.Record(ctx, uid, client, workID))
}

func (c *Catalog) ForgetBrowsingHistory(ctx context.Context, scope string, workID int64) error {
	if c == nil || c.BrowsingHistory == nil {
		return historyUnbound()
	}
	uid, client, err := requireUser(ctx)
	if err != nil {
		return err
	}
	if scope == historyScopeAll {
		client = ""
	}
	_, err = c.BrowsingHistory.Forget(ctx, uid, client, workID)
	return historyErr(err)
}

func historyErr(err error) error {
	switch {
	case err == nil:
		return nil
	case err == catsvc.ErrHistoryWorkUnavailable:
		return problem.New(problem.CodeNotFound, "", "", "work is not available for browsing history.")
	case err == catsvc.ErrHistoryActorRequired:
		return problem.New(problem.CodeUserIdentityRequired, "", "", err.Error())
	}
	return err
}
