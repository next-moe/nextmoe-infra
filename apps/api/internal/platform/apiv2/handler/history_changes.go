package handler

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/repr"

	"github.com/danielgtaylor/huma/v2"
)

const historyChangesPath = "/v2/browsing-history/changes"

const historyChangesDefaultLimit = 100

type listHistoryChangesInput struct {
	Cursor string `query:"cursor" maxLength:"512" doc:"Opaque keyset cursor from a prior next_cursor."`
	Limit  string `query:"limit" maxLength:"8" doc:"Page size 1-100, default 100. Values above 100 are 400 LIMIT_TOO_LARGE, not clamped."`
}

type listHistoryChangesOutput struct {
	Body repr.List[repr.BrowsingHistoryChange]
}

func registerHistoryChanges(api huma.API, cat *Catalog) {
	huma.Register(api, huma.Operation{
		OperationID: "listBrowsingHistoryChanges", Method: http.MethodGet, Path: historyChangesPath,
		Summary: "Browsing history switches and clears feed",
		Description: "Every account whose browsing history each app must act on, oldest change first, keyset-paginated: it switched browsing_history off or back on, or cleared every app's history. " +
			"**This is the channel for a site that keeps its own browsing history** of things the catalog does not hold, such as forum topics or resources. " +
			"Apply each entry as the account's current state: browsing_history off means delete everything you keep for the account and record nothing until an entry says on; " +
			"either way delete the account's entries viewed at or before cleared_at. An account appears once, at its latest change, so applying entries in order is idempotent. " +
			"Bootstrap from an empty cursor (the first drain lists every account that ever switched off or cleared), then poll at your own cadence. " +
			"next_cursor comes only with a full page, so keep the last one you received and poll from it: the partial page after it is read again. " +
			"A change surfaces five seconds after it commits. Deleted accounts are not listed; they come from the sign-in service's deleted-accounts feed. " +
			"Requires an application key with the history_changes:read scope on top of catalog:read; a user access token is refused. " +
			"The scope is granted by an operator, not self-service, because the answer names accounts and when they cleared their history.",
		Tags:               []string{"browsing-history"},
		Errors:             collectionErrors(http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable),
		SkipValidateParams: true,
	}, listHistoryChanges(cat))
}

func listHistoryChanges(cat *Catalog) func(context.Context, *listHistoryChangesInput) (*listHistoryChangesOutput, error) {
	return func(ctx context.Context, in *listHistoryChangesInput) (*listHistoryChangesOutput, error) {
		limit := in.Limit
		if strings.TrimSpace(limit) == "" {
			limit = strconv.Itoa(historyChangesDefaultLimit)
		}
		q, perr := collect.Parse(collect.Raw{Cursor: in.Cursor, Limit: limit}, collect.BrowsingHistoryChangeSpec())
		if perr != nil {
			return nil, withIdent(ctx, perr)
		}
		page, err := cat.ListBrowsingHistoryChanges(ctx, q)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &listHistoryChangesOutput{Body: page}, nil
	}
}

func (c *Catalog) ListBrowsingHistoryChanges(ctx context.Context, q collect.Query) (repr.List[repr.BrowsingHistoryChange], error) {
	var empty repr.List[repr.BrowsingHistoryChange]
	if c == nil || c.BrowsingHistory == nil {
		return empty, historyUnbound()
	}
	if _, perr := requireAppClient(ctx); perr != nil {
		return empty, perr
	}
	var after time.Time
	var afterUID int64
	if q.Cursor != "" {
		var ok bool
		if after, afterUID, ok = parseTimeIDCursor(q.Cursor); !ok {
			return empty, collectInvalidCursor()
		}
	}
	limit := q.Limit
	if limit <= 0 {
		limit = historyChangesDefaultLimit
	}
	rows, err := c.BrowsingHistory.Changes(ctx, after, afterUID, limit)
	if err != nil {
		return empty, err
	}
	items := make([]repr.BrowsingHistoryChange, 0, len(rows))
	for _, r := range rows {
		state := "on"
		if r.Off {
			state = "off"
		}
		items = append(items, repr.BrowsingHistoryChange{
			Object: "browsing_history_change", UserID: repr.ID(r.UID), BrowsingHistory: state,
			ClearedAt: r.ClearedAt.UTC().Format(time.RFC3339Nano),
			ChangedAt: r.ChangedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	var next *string
	if len(rows) == limit {
		last := rows[len(rows)-1]
		next = timeIDCursor(last.ChangedAt, last.UID)
	}
	return finishList(items, next, 0, q, nil), nil
}
