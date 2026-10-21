package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	"api/internal/platform/catalog/model"
	catsvc "api/internal/platform/catalog/service"
	"api/internal/platform/settings/keys"

	"github.com/danielgtaylor/huma/v2"
)

var splitRequestStates = map[string]int16{
	"open":      model.ProposalStatusOpen,
	"approved":  model.ProposalStatusApproved,
	"executed":  model.ProposalStatusExecuted,
	"rejected":  model.ProposalStatusRejected,
	"withdrawn": model.ProposalStatusWithdrawn,
	"undone":    model.ProposalStatusUndone,
}

func splitRequestState(status int16) string {
	for name, s := range splitRequestStates {
		if s == status {
			return name
		}
	}
	return "open"
}

var splitTitleKinds = map[string]int16{
	"official":     model.WorkTitleKindOfficial,
	"alias":        model.WorkTitleKindAlias,
	"abbreviation": model.WorkTitleKindAbbreviation,
}

type splitDraftBody struct {
	ReleaseIDs  []string          `json:"release_ids" minItems:"1" maxItems:"200" doc:"Releases of the work that move to the new work. At least one release has to stay."`
	DisplayName string            `json:"display_name" minLength:"1" maxLength:"512" doc:"The new work's display name. Must not be used as a discriminant."`
	OLang       string            `json:"olang" minLength:"2" maxLength:"16" format:"bcp47" doc:"The new work's original language, BCP-47."`
	Titles      []repr.SplitTitle `json:"titles,omitempty" maxItems:"32" doc:"Titles the new work starts with."`
	PromoteRefs []repr.SplitRef   `json:"promote_refs,omitempty" maxItems:"8" doc:"Bangumi or ErogameScape entries linked to the work as related or probable that become the new work's identity."`
}

type createSplitRequestInput struct {
	Body struct {
		Object   string `json:"object" enum:"work" doc:"Family of the entry. Only works can be split today."`
		TargetID string `json:"target_id" pattern:"^[0-9]+$" maxLength:"20" doc:"The work to split."`
		splitDraftBody
		Note string `json:"note" minLength:"4" maxLength:"2000" doc:"Why these releases are a work of their own. Must not be used as a discriminant."`
	}
}

type amendSplitRequestInput struct {
	ID   string `path:"id" pattern:"^[0-9]+$" maxLength:"20" doc:"Split request id."`
	Body splitDraftBody
}

type listSplitRequestsInput struct {
	State      string `query:"state" enum:"open,approved,executed,rejected,withdrawn,undone" doc:"Narrow to one state."`
	TargetID   string `query:"target_id" pattern:"^[0-9]+$" maxLength:"20" doc:"Narrow to requests about one work."`
	OriginSite string `query:"origin_site" enum:"kungal,moyu,letmoe,nextmoe,editor" doc:"Narrow to requests filed from one first-party property."`
	Cursor     string `query:"cursor" pattern:"^cur_[0-9]+$" maxLength:"32" doc:"Opaque keyset cursor from next_cursor."`
	Limit      int    `query:"limit" minimum:"1" maximum:"100" doc:"Page size, 1-100. Default 20."`
}

type splitRequestIDInput struct {
	ID string `path:"id" pattern:"^[0-9]+$" maxLength:"20" doc:"Split request id."`
}

type patchSplitRequestInput struct {
	ID   string `path:"id" pattern:"^[0-9]+$" maxLength:"20" doc:"Split request id."`
	Body struct {
		State string `json:"state" enum:"withdrawn" doc:"Set withdrawn to withdraw an open request you filed."`
	}
}

type decideSplitRequestInput struct {
	ID   string `path:"id" pattern:"^[0-9]+$" maxLength:"20" doc:"Split request id."`
	Body struct {
		Decision string `json:"decision" enum:"approve,reject,execute,undo" doc:"approve starts the cooling-off (catalog.merge_cooling_off_hours) after which the catalog-split-requests job executes it; reject closes it; execute runs an approved request whose cooling-off has ended without waiting for the job; undo puts an executed split back while undo_until has not passed: the releases return and the new work is merged into the original."`
		Reason   string `json:"reason,omitempty" maxLength:"2000" doc:"Required to reject and to undo. Must not be used as a discriminant."`
	}
}

type splitPreviewInput struct {
	Object     string `query:"object" enum:"work" doc:"Family of the entry."`
	TargetID   string `query:"target_id" pattern:"^[0-9]+$" maxLength:"20" doc:"The work."`
	ReleaseIDs string `query:"release_ids" maxLength:"4000" pattern:"^[0-9]+(,[0-9]+)*$" doc:"Comma-separated release ids chosen to move."`
	Promote    string `query:"promote" maxLength:"1000" pattern:"^[a-z_]+:[^,]+(,[a-z_]+:[^,]+)*$" doc:"Comma-separated source:external_id entries chosen to promote."`
	RequestID  string `query:"request_id" pattern:"^[0-9]+$" maxLength:"20" doc:"The split request being reviewed, left out of the one-pending-request check so its own page does not report it."`
}

type listSplitEventsInput struct {
	Cursor string `query:"cursor" pattern:"^sp_[0-9]+_[0-9]+$" maxLength:"48" doc:"Opaque keyset cursor from next_cursor. Omit to start from the first split."`
	Limit  int    `query:"limit" minimum:"1" maximum:"100" doc:"Page size, 1-100. Default 50."`
}

type splitRequestOutput struct {
	Body repr.SplitRequestRecord
}

type splitRequestListOutput struct {
	Body repr.List[repr.SplitRequestRecord]
}

type splitPreviewOutput struct {
	Body repr.SplitPreview
}

type splitEventListOutput struct {
	Body repr.List[repr.SplitEvent]
}

func registerSplitRequests(api huma.API, cat *Catalog) {
	errs := collectionErrors(http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity, http.StatusTooManyRequests,
		http.StatusServiceUnavailable)
	me, mod := []string{"me"}, []string{"moderation"}
	const firstParty = " Requires a user access token bound to a first-party catalog site. The token must carry the catalog:edit scope."
	huma.Register(api, huma.Operation{
		OperationID: "createMySplitRequest", Method: http.MethodPost, Path: "/v2/me/split-requests",
		Summary: "Request a split",
		Description: "Asks reviewers to move some releases of a work into a work of their own, for an edition that deserves its own entry although VNDB lists it under the same VN. Nothing changes on filing: a reviewer approves (and may amend the selection), the cooling-off runs, then the new work is created. " +
			"One open or approved request per work; another is 409. Quotas are tiered: catalog.split_requests_per_day, _editor, _reviewer inside catalog.write_quota_window_hours; past it 429 QUOTA_EXCEEDED." + firstParty,
		Tags: me, Errors: errs, DefaultStatus: http.StatusCreated, SkipValidateParams: true,
	}, createMySplitRequest(cat))
	huma.Register(api, huma.Operation{
		OperationID: "listMySplitRequests", Method: http.MethodGet, Path: "/v2/me/split-requests",
		Summary:     "List my split requests",
		Description: "The bearer's own split requests, newest first; follow next_cursor for more." + firstParty,
		Tags:        me, Errors: errs, SkipValidateParams: true,
	}, listMySplitRequests(cat))
	huma.Register(api, huma.Operation{
		OperationID: "getMySplitRequest", Method: http.MethodGet, Path: "/v2/me/split-requests/{id}",
		Summary:     "Get one split request",
		Description: "Any split request, for its page in the editor." + firstParty,
		Tags:        me, Errors: errs, SkipValidateParams: true,
	}, getMySplitRequest(cat))
	huma.Register(api, huma.Operation{
		OperationID: "patchMySplitRequest", Method: http.MethodPatch, Path: "/v2/me/split-requests/{id}",
		Summary:     "Withdraw a split request",
		Description: "Withdraws an open request the bearer filed." + firstParty,
		Tags:        me, Errors: errs, SkipValidateParams: true,
	}, patchMySplitRequest(cat))
	huma.Register(api, huma.Operation{
		OperationID: "getMySplitPreview", Method: http.MethodGet, Path: "/v2/me/split-preview",
		Summary:     "Preview a split",
		Description: "Every live release of the work, the registry entries that could become the new work's identity, what moves and what stays, titles suggested from the chosen releases, and the blockers, warnings and registry facts for the choice in release_ids= and promote=." + firstParty,
		Tags:        me, Errors: errs, SkipValidateParams: true,
	}, getMySplitPreview(cat))
	huma.Register(api, huma.Operation{
		OperationID: "listModerationSplitRequests", Method: http.MethodGet, Path: "/v2/moderation/split-requests",
		Summary:     "Split request queue",
		Description: "Split requests, newest first; follow next_cursor for more. Requires the catalog.merge.review permission.",
		Tags:        mod, Errors: errs, SkipValidateParams: true,
	}, listModerationSplitRequests(cat))
	huma.Register(api, huma.Operation{
		OperationID: "getModerationSplitRequest", Method: http.MethodGet, Path: "/v2/moderation/split-requests/{id}",
		Summary:     "Get one split request for review",
		Description: "Requires the catalog.merge.review permission.",
		Tags:        mod, Errors: errs, SkipValidateParams: true,
	}, getModerationSplitRequest(cat))
	huma.Register(api, huma.Operation{
		OperationID: "amendModerationSplitRequest", Method: http.MethodPatch, Path: "/v2/moderation/split-requests/{id}",
		Summary:     "Amend a split request",
		Description: "Replaces the chosen releases, the new work's name, language and titles, and the entries to promote, while the request is open or approved. Validated like a filing; records who amended. Requires the catalog.merge.review permission.",
		Tags:        mod, Errors: errs, SkipValidateParams: true,
	}, amendModerationSplitRequest(cat))
	huma.Register(api, huma.Operation{
		OperationID: "decideModerationSplitRequest", Method: http.MethodPost, Path: "/v2/moderation/split-requests/{id}/decisions",
		Summary:     "Decide a split request",
		Description: "decision=approve|reject|execute|undo. Requires the catalog.merge.review permission.",
		Tags:        mod, Errors: errs, DefaultStatus: http.StatusCreated, SkipValidateParams: true,
	}, decideModerationSplitRequest(cat))
	huma.Register(api, huma.Operation{
		OperationID: "listCatalogSplits", Method: http.MethodGet, Path: "/v2/catalog/splits",
		Summary: "Work split feed",
		Description: "Executed and undone splits, oldest change first. Keyset-paginated. A split moves releases from original_id to a new work: the new id also appears on /v2/catalog/changes, and the moved releases keep their ids. A mirror that hangs its own data off releases or editions should follow them to new_work_id. An undo re-surfaces the split with state=undone: the releases are back on original_id and new_work_id redirects there (/v2/catalog/redirects). " +
			"Anonymous calls are allowed and limited per IP address; an application key or a user access token with catalog:read gets its own, higher limit.",
		Tags: []string{"catalog"}, Errors: errs, SkipValidateParams: true,
	}, listCatalogSplits(cat))
}

func (c *Catalog) splitsBound() error {
	if c == nil || c.Splits == nil {
		return problem.New(problem.CodeServiceUnavailable, "", "", "split requests are not bound.")
	}
	return nil
}

func splitRequestQuota(ctx context.Context) int64 {
	switch editTier(ctx) {
	case tierReviewer:
		return keys.CatalogSplitRequestsPerDayReviewer.Get()
	case tierEditor:
		return keys.CatalogSplitRequestsPerDayEditor.Get()
	}
	return keys.CatalogSplitRequestsPerDay.Get()
}

func splitInputFrom(workID int64, b splitDraftBody) (catsvc.SplitInput, error) {
	in := catsvc.SplitInput{WorkID: workID, DisplayName: b.DisplayName, OLang: b.OLang}
	for i, s := range b.ReleaseIDs {
		id, ok := repr.ParseID(s)
		if !ok {
			pr := problem.New(problem.CodeValidationFailed, "", "", "release_ids must be catalog ids.")
			pr.Errors = []problem.FieldError{{Pointer: "/release_ids/" + strconv.Itoa(i), Reason: problem.ReasonInvalidFormat, Detail: "not a catalog id"}}
			return in, pr
		}
		in.ReleaseIDs = append(in.ReleaseIDs, id)
	}
	for _, t := range b.Titles {
		kind, ok := splitTitleKinds[t.TitleKind]
		if !ok {
			kind = model.WorkTitleKindOfficial
		}
		in.Titles = append(in.Titles, model.SplitTitle{Lang: t.Lang, Title: t.Title, Kind: kind})
	}
	for _, r := range b.PromoteRefs {
		in.PromoteRefs = append(in.PromoteRefs, model.SplitRef{Source: r.Source, ExternalID: r.ExternalID})
	}
	return in, nil
}

func createMySplitRequest(cat *Catalog) func(context.Context, *createSplitRequestInput) (*splitRequestOutput, error) {
	return func(ctx context.Context, in *createSplitRequestInput) (*splitRequestOutput, error) {
		if err := cat.splitsBound(); err != nil {
			return nil, catalogErr(ctx, err)
		}
		uid, err := requireFirstPartyUser(ctx)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		workID, ok := repr.ParseID(in.Body.TargetID)
		if !ok {
			return nil, catalogErr(ctx, problem.New(problem.CodeValidationFailed, "", "", "target_id must be a catalog id."))
		}
		window := quotaWindow()
		used, err := cat.Splits.CountSince(ctx, uid, time.Now().Add(-window))
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		if limit := splitRequestQuota(ctx); used >= limit {
			return nil, catalogErr(ctx, quotaExceeded("the split request limit for this account is exhausted", used, limit, window))
		}
		sin, err := splitInputFrom(workID, in.Body.splitDraftBody)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		sin.Note = in.Body.Note
		r, err := cat.Splits.Request(ctx, sin, uid)
		if err != nil {
			return nil, catalogErr(ctx, splitRequestErr(err))
		}
		return &splitRequestOutput{Body: cat.splitRequestRecord(ctx, r)}, nil
	}
}

func listMySplitRequests(cat *Catalog) func(context.Context, *listSplitRequestsInput) (*splitRequestListOutput, error) {
	return func(ctx context.Context, in *listSplitRequestsInput) (*splitRequestListOutput, error) {
		if err := checkSplitListQuery(in); err != nil {
			return nil, catalogErr(ctx, err)
		}
		if err := cat.splitsBound(); err != nil {
			return nil, catalogErr(ctx, err)
		}
		uid, err := requireFirstPartyUser(ctx)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		f := splitRequestFilter(in)
		f.ProposedBy = &uid
		page, err := cat.listSplitRequests(ctx, f, in)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &splitRequestListOutput{Body: page}, nil
	}
}

func getMySplitRequest(cat *Catalog) func(context.Context, *splitRequestIDInput) (*splitRequestOutput, error) {
	return func(ctx context.Context, in *splitRequestIDInput) (*splitRequestOutput, error) {
		if err := cat.splitsBound(); err != nil {
			return nil, catalogErr(ctx, err)
		}
		if _, err := requireFirstPartyUser(ctx); err != nil {
			return nil, catalogErr(ctx, err)
		}
		id, _ := repr.ParseID(in.ID)
		return cat.splitRequestAfterWrite(ctx, id)
	}
}

func patchMySplitRequest(cat *Catalog) func(context.Context, *patchSplitRequestInput) (*splitRequestOutput, error) {
	return func(ctx context.Context, in *patchSplitRequestInput) (*splitRequestOutput, error) {
		if err := cat.splitsBound(); err != nil {
			return nil, catalogErr(ctx, err)
		}
		uid, err := requireFirstPartyUser(ctx)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		id, _ := repr.ParseID(in.ID)
		r, err := cat.Splits.Get(ctx, id)
		if err != nil {
			return nil, catalogErr(ctx, splitRequestErr(err))
		}
		if r.ProposedBy != uid {
			return nil, catalogErr(ctx, problem.New(problem.CodePermissionRequired, "", "", "only the requester may withdraw a split request."))
		}
		if err := cat.Splits.Withdraw(ctx, id); err != nil {
			return nil, catalogErr(ctx, splitRequestErr(err))
		}
		return cat.splitRequestAfterWrite(ctx, id)
	}
}

func getMySplitPreview(cat *Catalog) func(context.Context, *splitPreviewInput) (*splitPreviewOutput, error) {
	return func(ctx context.Context, in *splitPreviewInput) (*splitPreviewOutput, error) {
		if err := cat.splitsBound(); err != nil {
			return nil, catalogErr(ctx, err)
		}
		if _, err := requireFirstPartyUser(ctx); err != nil {
			return nil, catalogErr(ctx, err)
		}
		workID, ok := repr.ParseID(in.TargetID)
		if !ok {
			pr := problem.New(problem.CodeValidationFailed, "", "", "target_id is required.")
			pr.Errors = []problem.FieldError{{Parameter: "target_id", Reason: problem.ReasonRequired, Detail: "the work to split"}}
			return nil, catalogErr(ctx, pr)
		}
		var selected []int64
		for _, s := range strings.Split(in.ReleaseIDs, ",") {
			if id, ok := repr.ParseID(s); ok && s != "" {
				selected = append(selected, id)
			}
		}
		var promote []model.SplitRef
		for _, s := range strings.Split(in.Promote, ",") {
			if source, ext, ok := strings.Cut(s, ":"); ok {
				promote = append(promote, model.SplitRef{Source: source, ExternalID: ext})
			}
		}
		own, _ := repr.ParseID(in.RequestID)
		p, err := cat.Splits.Preview(ctx, workID, selected, promote, own)
		if err != nil {
			return nil, catalogErr(ctx, splitRequestErr(err))
		}
		return &splitPreviewOutput{Body: splitPreviewFrom(p)}, nil
	}
}

func listModerationSplitRequests(cat *Catalog) func(context.Context, *listSplitRequestsInput) (*splitRequestListOutput, error) {
	return func(ctx context.Context, in *listSplitRequestsInput) (*splitRequestListOutput, error) {
		if err := checkSplitListQuery(in); err != nil {
			return nil, catalogErr(ctx, err)
		}
		if err := cat.splitsBound(); err != nil {
			return nil, catalogErr(ctx, err)
		}
		if err := requireMergeReviewer(ctx); err != nil {
			return nil, catalogErr(ctx, err)
		}
		page, err := cat.listSplitRequests(ctx, splitRequestFilter(in), in)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &splitRequestListOutput{Body: page}, nil
	}
}

func getModerationSplitRequest(cat *Catalog) func(context.Context, *splitRequestIDInput) (*splitRequestOutput, error) {
	return func(ctx context.Context, in *splitRequestIDInput) (*splitRequestOutput, error) {
		if err := cat.splitsBound(); err != nil {
			return nil, catalogErr(ctx, err)
		}
		if err := requireMergeReviewer(ctx); err != nil {
			return nil, catalogErr(ctx, err)
		}
		id, _ := repr.ParseID(in.ID)
		return cat.splitRequestAfterWrite(ctx, id)
	}
}

func amendModerationSplitRequest(cat *Catalog) func(context.Context, *amendSplitRequestInput) (*splitRequestOutput, error) {
	return func(ctx context.Context, in *amendSplitRequestInput) (*splitRequestOutput, error) {
		if err := cat.splitsBound(); err != nil {
			return nil, catalogErr(ctx, err)
		}
		if err := requireMergeReviewer(ctx); err != nil {
			return nil, catalogErr(ctx, err)
		}
		id, _ := repr.ParseID(in.ID)
		sin, err := splitInputFrom(0, in.Body)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		if err := cat.Splits.Amend(ctx, id, userIDFrom(ctx), sin); err != nil {
			return nil, catalogErr(ctx, splitRequestErr(err))
		}
		return cat.splitRequestAfterWrite(ctx, id)
	}
}

func decideModerationSplitRequest(cat *Catalog) func(context.Context, *decideSplitRequestInput) (*splitRequestOutput, error) {
	return func(ctx context.Context, in *decideSplitRequestInput) (*splitRequestOutput, error) {
		if err := cat.splitsBound(); err != nil {
			return nil, catalogErr(ctx, err)
		}
		if err := requireMergeReviewer(ctx); err != nil {
			return nil, catalogErr(ctx, err)
		}
		uid := userIDFrom(ctx)
		id, _ := repr.ParseID(in.ID)
		needReason := func(what string) error {
			pr := problem.New(problem.CodeValidationFailed, "", "", what+" needs a reason.")
			pr.Errors = []problem.FieldError{{Pointer: "/reason", Reason: problem.ReasonRequired, Detail: "required to " + strings.TrimPrefix(what, "an ")}}
			return pr
		}
		var err error
		switch in.Body.Decision {
		case "approve":
			err = cat.Splits.Approve(ctx, id, uid)
		case "reject":
			if strings.TrimSpace(in.Body.Reason) == "" {
				return nil, catalogErr(ctx, needReason("a rejection"))
			}
			err = cat.Splits.Reject(ctx, id, uid, in.Body.Reason)
		case "execute":
			_, err = cat.Splits.Execute(ctx, id, &uid)
		case "undo":
			if strings.TrimSpace(in.Body.Reason) == "" {
				return nil, catalogErr(ctx, needReason("an undo"))
			}
			err = cat.Splits.Undo(ctx, id, uid, in.Body.Reason)
		default:
			return nil, catalogErr(ctx, problem.New(problem.CodeValidationFailed, "", "", "decision must be approve, reject, execute or undo."))
		}
		if err != nil {
			return nil, catalogErr(ctx, splitRequestErr(err))
		}
		return cat.splitRequestAfterWrite(ctx, id)
	}
}

func listCatalogSplits(cat *Catalog) func(context.Context, *listSplitEventsInput) (*splitEventListOutput, error) {
	return func(ctx context.Context, in *listSplitEventsInput) (*splitEventListOutput, error) {
		limit := in.Limit
		if limit > 100 {
			return nil, catalogErr(ctx, problem.New(problem.CodeLimitTooLarge, "", "", "limit is at most 100."))
		}
		if limit <= 0 {
			limit = 50
		}
		var after time.Time
		var afterID int64
		if in.Cursor != "" {
			parts := strings.Split(strings.TrimPrefix(in.Cursor, "sp_"), "_")
			micros, err1 := strconv.ParseInt(parts[0], 10, 64)
			id, err2 := strconv.ParseInt(parts[len(parts)-1], 10, 64)
			if len(parts) != 2 || err1 != nil || err2 != nil {
				return nil, catalogErr(ctx, problem.New(problem.CodeInvalidCursor, "", "", "cursor is not one this face issued."))
			}
			after, afterID = time.UnixMicro(micros), id
		}
		if err := cat.splitsBound(); err != nil {
			return nil, catalogErr(ctx, err)
		}
		rows, err := cat.Splits.Events(ctx, after, afterID, limit+1)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		out := repr.List[repr.SplitEvent]{Object: "list", Items: make([]repr.SplitEvent, 0, len(rows))}
		if len(rows) > limit {
			rows = rows[:limit]
			last := rows[len(rows)-1]
			next := "sp_" + strconv.FormatInt(last.ChangedAt.UnixMicro(), 10) + "_" + strconv.FormatInt(last.ID, 10)
			out.NextCursor = &next
		}
		for i := range rows {
			r := &rows[i]
			if r.NewWorkID == nil || r.ChangedAt == nil {
				continue
			}
			in, _ := catsvc.SplitInputOf(r)
			out.Items = append(out.Items, repr.SplitEvent{
				Object: "split", ID: repr.ID(r.ID), State: splitRequestState(r.Status),
				OriginalID: repr.ID(r.EntityID), NewWorkID: repr.ID(*r.NewWorkID),
				ReleaseIDs: idStrings(in.ReleaseIDs), ChangedAt: rfc3339(*r.ChangedAt),
			})
		}
		return &splitEventListOutput{Body: out}, nil
	}
}

func checkSplitListQuery(in *listSplitRequestsInput) error {
	if _, err := originSiteParam(in.OriginSite); err != nil {
		return err
	}
	if in.Limit > 100 {
		return problem.New(problem.CodeLimitTooLarge, "", "", "limit is at most 100.")
	}
	if in.Limit < 0 {
		return problem.New(problem.CodeInvalidParameter, "", "", "limit is at least 1.")
	}
	return nil
}

func splitRequestFilter(in *listSplitRequestsInput) catsvc.SplitRequestFilter {
	var f catsvc.SplitRequestFilter
	if st, ok := splitRequestStates[in.State]; ok {
		f.Status = &st
	}
	if id, ok := repr.ParseID(in.TargetID); ok && in.TargetID != "" {
		f.EntityID = &id
	}
	f.OriginSite = in.OriginSite
	return f
}

func (c *Catalog) listSplitRequests(ctx context.Context, f catsvc.SplitRequestFilter, in *listSplitRequestsInput) (repr.List[repr.SplitRequestRecord], error) {
	limit := in.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if in.Cursor != "" {
		before, err := strconv.ParseInt(strings.TrimPrefix(in.Cursor, "cur_"), 10, 64)
		if err != nil || before <= 0 {
			return repr.List[repr.SplitRequestRecord]{}, problem.New(problem.CodeInvalidCursor, "", "", "cursor is not one this face issued.")
		}
		f.BeforeID = before
	}
	f.Limit = limit + 1
	rows, err := c.Splits.List(ctx, f)
	if err != nil {
		return repr.List[repr.SplitRequestRecord]{}, err
	}
	out := repr.List[repr.SplitRequestRecord]{Object: "list"}
	if len(rows) > limit {
		rows = rows[:limit]
		next := "cur_" + strconv.FormatInt(rows[len(rows)-1].ID, 10)
		out.NextCursor = &next
	}
	out.Items = make([]repr.SplitRequestRecord, 0, len(rows))
	for i := range rows {
		out.Items = append(out.Items, splitRequestFrom(&rows[i]))
	}
	c.attachSplitRequestPeople(ctx, out.Items)
	c.attachSplitRequestNames(ctx, out.Items)
	return out, nil
}

func (c *Catalog) splitRequestAfterWrite(ctx context.Context, id int64) (*splitRequestOutput, error) {
	r, err := c.Splits.Get(ctx, id)
	if err != nil {
		return nil, catalogErr(ctx, splitRequestErr(err))
	}
	return &splitRequestOutput{Body: c.splitRequestRecord(ctx, r)}, nil
}

func (c *Catalog) splitRequestRecord(ctx context.Context, r *model.CatalogSplitRequest) repr.SplitRequestRecord {
	items := []repr.SplitRequestRecord{splitRequestFrom(r)}
	c.attachSplitRequestPeople(ctx, items)
	c.attachSplitRequestNames(ctx, items)
	return items[0]
}

func idStrings(ids []int64) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, repr.ID(id))
	}
	return out
}

func splitRequestFrom(r *model.CatalogSplitRequest) repr.SplitRequestRecord {
	in, _ := catsvc.SplitInputOf(r)
	rec := repr.SplitRequestRecord{
		Object: "split_request", ID: repr.ID(r.ID), State: splitRequestState(r.Status),
		TargetObject: "work", TargetID: repr.ID(r.EntityID), NewWorkID: optIDPtr(r.NewWorkID),
		ReleaseIDs: idStrings(in.ReleaseIDs), DisplayName: r.DisplayName, OLang: r.OLang,
		Titles: make([]repr.SplitTitle, 0, len(in.Titles)), PromoteRefs: make([]repr.SplitRef, 0, len(in.PromoteRefs)),
		Note: r.Note, DecisionNote: r.DecisionNote, UndoNote: r.UndoNote,
		ProposedByUID: repr.ID(r.ProposedBy), AmendedByUID: optIDPtr(r.AmendedBy), DecidedByUID: optIDPtr(r.DecidedBy),
		UndoneByUID: optIDPtr(r.UndoneBy), OriginSite: r.OriginSite, ProposedAt: rfc3339(r.ProposedAt),
		AmendedAt: optRFC3339(r.AmendedAt), DecidedAt: optRFC3339(r.DecidedAt), ExecuteAfter: optRFC3339(r.ExecuteAfter),
		ExecutedAt: optRFC3339(r.ExecutedAt), UndoneAt: optRFC3339(r.UndoneAt), UndoMergeID: optIDPtr(r.UndoMergeID),
		Moved: []repr.SplitMovedCount{},
	}
	for _, t := range in.Titles {
		rec.Titles = append(rec.Titles, repr.SplitTitle{Lang: t.Lang, Title: t.Title, TitleKind: titleKindToken(t.Kind)})
	}
	for _, ref := range in.PromoteRefs {
		rec.PromoteRefs = append(rec.PromoteRefs, repr.SplitRef{Source: ref.Source, ExternalID: ref.ExternalID})
	}
	if r.Status == model.ProposalStatusExecuted && r.ExecutedAt != nil {
		until := r.ExecutedAt.Add(time.Duration(keys.CatalogMergeUndoWindowDays.Get()) * 24 * time.Hour)
		s := rfc3339(until)
		rec.UndoUntil = &s
	}
	var exec catsvc.SplitExecution
	if len(r.Execution) > 0 && json.Unmarshal(r.Execution, &exec) == nil && r.ExecutedAt != nil {
		for _, m := range []struct {
			key string
			n   int
		}{
			{"releases", len(exec.Releases)}, {"tombstones", len(exec.Tombstones)}, {"titles", len(exec.Titles)},
			{"credits", len(exec.Credits)}, {"refs", len(exec.Refs)}, {"rejections", len(exec.Rejections)},
		} {
			rec.Moved = append(rec.Moved, repr.SplitMovedCount{Key: m.key, Count: int64(m.n)})
		}
	}
	return rec
}

func (c *Catalog) attachSplitRequestNames(ctx context.Context, items []repr.SplitRequestRecord) {
	var ids []int64
	for _, it := range items {
		if id, ok := repr.ParseID(it.TargetID); ok {
			ids = append(ids, id)
		}
		if it.NewWorkID != nil {
			if id, ok := repr.ParseID(*it.NewWorkID); ok {
				ids = append(ids, id)
			}
		}
	}
	names, err := c.Splits.WorkNames(ctx, ids)
	if err != nil {
		return
	}
	for i := range items {
		if id, ok := repr.ParseID(items[i].TargetID); ok {
			items[i].TargetName = names[id]
		}
		if items[i].NewWorkID != nil {
			if id, ok := repr.ParseID(*items[i].NewWorkID); ok {
				items[i].NewWorkName = names[id]
			}
		}
	}
}

func (c *Catalog) attachSplitRequestPeople(ctx context.Context, items []repr.SplitRequestRecord) {
	if c.UsersOf == nil || len(items) == 0 {
		return
	}
	seen := map[int64]struct{}{}
	var ids []int64
	add := func(s *string) {
		if s == nil {
			return
		}
		if id, ok := repr.ParseID(*s); ok && id > 0 {
			if _, dup := seen[id]; !dup {
				seen[id] = struct{}{}
				ids = append(ids, id)
			}
		}
	}
	for i := range items {
		add(&items[i].ProposedByUID)
		add(items[i].AmendedByUID)
		add(items[i].DecidedByUID)
		add(items[i].UndoneByUID)
	}
	if len(ids) == 0 {
		return
	}
	users, err := c.UsersOf(ctx, ids)
	if err != nil {
		return
	}
	lookup := func(s *string) *repr.UserRef {
		if s == nil {
			return nil
		}
		id, ok := repr.ParseID(*s)
		if !ok {
			return nil
		}
		if u, ok := users[id]; ok {
			return &u
		}
		return nil
	}
	for i := range items {
		items[i].ProposedBy = lookup(&items[i].ProposedByUID)
		items[i].AmendedBy = lookup(items[i].AmendedByUID)
		items[i].DecidedBy = lookup(items[i].DecidedByUID)
		items[i].UndoneBy = lookup(items[i].UndoneByUID)
	}
}

func splitPreviewFrom(p *catsvc.SplitPreview) repr.SplitPreview {
	out := repr.SplitPreview{
		Object: "split_preview", TargetObject: "work", TargetID: repr.ID(p.WorkID), TargetName: p.WorkName,
		OLang: p.OLang, SuggestedName: p.SuggestedName, SuggestedLang: p.SuggestedLang,
		Releases: make([]repr.SplitPreviewRelease, 0, len(p.Releases)), Candidates: make([]repr.SplitPreviewRef, 0, len(p.Candidates)),
		Titles: make([]repr.SplitTitle, 0, len(p.Titles)), Moves: make([]repr.SplitPreviewCount, 0, len(p.Moves)),
		Stays: make([]repr.SplitPreviewCount, 0, len(p.Stays)), Blockers: issuesOf(p.Blockers),
		Warnings: issuesOf(p.Warnings), Facts: issuesOf(p.Facts), PendingID: optIDPtr(p.PendingID),
	}
	if p.VNDBAnchor != "" {
		v := p.VNDBAnchor
		out.VNDBAnchor = &v
	}
	for _, r := range p.Releases {
		rel := repr.SplitPreviewRelease{ID: repr.ID(r.ID), Title: r.Title, ReleaseKind: r.KindKey, Platform: r.Platform,
			Lang: r.Lang, VNDBRid: optString(r.VNDBID), Sources: r.Sources, LabelCount: r.Labels, CreditCount: r.Credits,
			Selected: r.Selected, Tombstone: r.Tombstoned}
		if rel.Sources == nil {
			rel.Sources = []string{}
		}
		rel.ReleaseDate, rel.ReleaseDatePrecision = catsvc.CalendarDate(r.ReleasedY, r.ReleasedM, r.ReleasedD)
		out.Releases = append(out.Releases, rel)
	}
	for _, c := range p.Candidates {
		kind := "related"
		if c.LinkKind == model.LinkKindProbable {
			kind = "probable"
		}
		out.Candidates = append(out.Candidates, repr.SplitPreviewRef{Source: c.Source, ExternalID: c.ExternalID,
			AnchorKind: kind, HeldByID: optIDPtr(c.HeldBy), Selected: c.Selected})
	}
	for _, t := range p.Titles {
		out.Titles = append(out.Titles, repr.SplitTitle{Lang: t.Lang, Title: t.Title, TitleKind: titleKindToken(t.Kind)})
	}
	for _, c := range p.Moves {
		out.Moves = append(out.Moves, repr.SplitPreviewCount{Key: c.Key, Count: c.Count})
	}
	for _, c := range p.Stays {
		out.Stays = append(out.Stays, repr.SplitPreviewCount{Key: c.Key, Count: c.Count})
	}
	return out
}

func issuesOf(in []catsvc.PreviewIssue) []repr.MergePreviewIssue {
	out := make([]repr.MergePreviewIssue, 0, len(in))
	for _, i := range in {
		out = append(out, repr.MergePreviewIssue{Code: i.Code, Message: i.Message})
	}
	return out
}

func splitRequestErr(err error) error {
	switch {
	case errors.Is(err, catsvc.ErrNotFound):
		return problem.New(problem.CodeNotFound, "", "", err.Error())
	case errors.Is(err, catsvc.ErrSplitPending):
		return problem.New(problem.CodeAlreadyExists, "", "", err.Error())
	case errors.Is(err, catsvc.ErrProposalState), errors.Is(err, catsvc.ErrSplitUndo):
		return problem.New(problem.CodeInvalidStateTransition, "", "", err.Error())
	case errors.Is(err, catsvc.ErrCoolingOff):
		return problem.New(problem.CodeInvalidStateTransition, "", "", "the cooling-off has not ended yet.")
	case errors.Is(err, catsvc.ErrSplitReleases), errors.Is(err, catsvc.ErrSplitRef), errors.Is(err, catsvc.ErrSplitDraft),
		errors.Is(err, catsvc.ErrSplitFamily), errors.Is(err, catsvc.ErrEntityNotLive):
		return problem.New(problem.CodeValidationFailed, "", "", err.Error())
	}
	return err
}
