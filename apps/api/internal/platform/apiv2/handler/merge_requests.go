package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	"api/internal/platform/catalog/model"
	catalogPerm "api/internal/platform/catalog/perm"
	catsvc "api/internal/platform/catalog/service"
	"api/internal/platform/settings/keys"

	"github.com/danielgtaylor/huma/v2"
)

var mergeRequestStates = map[string]int16{
	"open":      model.ProposalStatusOpen,
	"approved":  model.ProposalStatusApproved,
	"executed":  model.ProposalStatusExecuted,
	"rejected":  model.ProposalStatusRejected,
	"withdrawn": model.ProposalStatusWithdrawn,
	"undone":    model.ProposalStatusUndone,
}

func mergeRequestState(status int16) string {
	for name, s := range mergeRequestStates {
		if s == status {
			return name
		}
	}
	return "open"
}

func mergeObjectOf(entityType int16) string {
	switch entityType {
	case model.EntityTypeWork:
		return "work"
	case model.EntityTypeLabel:
		return "company"
	case model.EntityTypeCharacter:
		return "character"
	case model.EntityTypePerson:
		return "person"
	case model.EntityTypeCreditName:
		return "credit_name"
	}
	return ""
}

func mergeEntityTypeOf(object string) (int16, bool) {
	switch object {
	case "work":
		return model.EntityTypeWork, true
	case "company":
		return model.EntityTypeLabel, true
	case "character":
		return model.EntityTypeCharacter, true
	case "person":
		return model.EntityTypePerson, true
	case "credit_name":
		return model.EntityTypeCreditName, true
	}
	return 0, false
}

type createMergeRequestInput struct {
	Body struct {
		Object   string `json:"object" enum:"work,company,character,person,credit_name" doc:"Family of the two entries."`
		SourceID string `json:"source_id" pattern:"^[0-9]+$" maxLength:"20" doc:"The entry to fold away."`
		TargetID string `json:"target_id" pattern:"^[0-9]+$" maxLength:"20" doc:"The entry that survives."`
		Reason   string `json:"reason" minLength:"4" maxLength:"2000" doc:"Why these are the same entry. Reviewers read it. Must not be used as a discriminant."`
		mergeChoicesBody
	}
}

type mergeChoicesBody struct {
	FieldResolution map[string]string `json:"field_resolution,omitempty" doc:"Optional. Field → source|target for the fields GET /v2/me/merge-preview lists under fields. A field not named keeps that preview's default. 422 for a field the family cannot resolve."`
	HideReleaseIDs  []string          `json:"hide_release_ids,omitempty" maxItems:"500" doc:"Optional, works only. Source releases to hide once they land on the target — normally the ones the preview marks duplicate_of. 422 for a release that is not the source's own."`
}

func (b mergeChoicesBody) choices() (catsvc.MergeChoices, error) {
	c := catsvc.MergeChoices{Fields: b.FieldResolution}
	for _, raw := range b.HideReleaseIDs {
		id, ok := repr.ParseID(raw)
		if !ok {
			return c, problem.New(problem.CodeValidationFailed, "", "", "hide_release_ids must be release ids.")
		}
		c.HideReleases = append(c.HideReleases, id)
	}
	return c, nil
}

type listMergeRequestsInput struct {
	State      string `query:"state" enum:"open,approved,executed,rejected,withdrawn,undone" doc:"Narrow to one state."`
	Object     string `query:"object" enum:"work,company,character,person,credit_name" doc:"Narrow to one family."`
	OriginSite string `query:"origin_site" enum:"kungal,moyu,letmoe,nextmoe,editor" doc:"Narrow to requests filed from one first-party property."`
	Cursor     string `query:"cursor" pattern:"^cur_[0-9]+$" maxLength:"32" doc:"Opaque keyset cursor from next_cursor."`
	Limit      int    `query:"limit" minimum:"1" maximum:"100" doc:"Page size, 1-100. Default 20."`
}

type mergeRequestIDInput struct {
	ID string `path:"id" pattern:"^[0-9]+$" maxLength:"20" doc:"Merge request id."`
}

type patchMergeRequestInput struct {
	ID   string `path:"id" pattern:"^[0-9]+$" maxLength:"20" doc:"Merge request id."`
	Body struct {
		State string `json:"state" enum:"withdrawn" doc:"Set withdrawn to withdraw an open request you filed."`
	}
}

type decideMergeRequestInput struct {
	ID   string `path:"id" pattern:"^[0-9]+$" maxLength:"20" doc:"Merge request id."`
	Body struct {
		Decision string `json:"decision" enum:"approve,reject,execute,undo" doc:"approve starts the cooling-off (catalog.merge_cooling_off_hours) after which the catalog-merge-requests job executes it; reject closes it; execute runs an approved request whose cooling-off has ended without waiting for the job; undo takes an executed merge back under the source's original id while undo_until has not passed."`
		Reason   string `json:"reason,omitempty" maxLength:"2000" doc:"Required to reject and to undo. Must not be used as a discriminant."`
	}
}

type mergeRequestOutput struct {
	Body repr.MergeRequestRecord
}

type mergeRequestListOutput struct {
	Body repr.List[repr.MergeRequestRecord]
}

func registerMergeRequests(api huma.API, cat *Catalog) {
	errs := collectionErrors(http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity, http.StatusTooManyRequests,
		http.StatusServiceUnavailable)
	me, mod := []string{"me"}, []string{"moderation"}
	huma.Register(api, huma.Operation{
		OperationID: "createMyMergeRequest", Method: http.MethodPost, Path: "/v2/me/merge-requests",
		Summary: "Request a merge",
		Description: "Asks reviewers to merge two entries of one family. Nothing merges on filing: a reviewer approves, the cooling-off runs, then the merge executes through the catalog merge machinery (redirect from source to target, survivorship, archive for unmerge). " +
			"Two works of different mediums are refused 422. Quotas are tiered: catalog.merge_requests_per_day, _editor, _reviewer inside catalog.write_quota_window_hours; past it 429 QUOTA_EXCEEDED. " +
			"Requires a user access token bound to a catalog site. The token must carry the catalog:edit scope.",
		Tags: me, Errors: errs, DefaultStatus: http.StatusCreated, SkipValidateParams: true,
	}, createMyMergeRequest(cat))
	huma.Register(api, huma.Operation{
		OperationID: "listMyMergeRequests", Method: http.MethodGet, Path: "/v2/me/merge-requests",
		Summary:     "List my merge requests",
		Description: "The bearer's own merge requests, newest first; follow next_cursor for more. Requires a user access token bound to a catalog site. The token must carry the catalog:edit scope.",
		Tags:        me, Errors: errs, SkipValidateParams: true,
	}, listMyMergeRequests(cat))
	huma.Register(api, huma.Operation{
		OperationID: "patchMyMergeRequest", Method: http.MethodPatch, Path: "/v2/me/merge-requests/{id}",
		Summary:     "Withdraw a merge request",
		Description: "Withdraws an open request the bearer filed. Requires a user access token bound to a catalog site. The token must carry the catalog:edit scope.",
		Tags:        me, Errors: errs, SkipValidateParams: true,
	}, patchMyMergeRequest(cat))
	huma.Register(api, huma.Operation{
		OperationID: "listModerationMergeRequests", Method: http.MethodGet, Path: "/v2/moderation/merge-requests",
		Summary:     "Merge request queue",
		Description: "Merge requests filed through the editor, newest first; follow next_cursor for more. Requires the catalog.merge.review permission.",
		Tags:        mod, Errors: errs, SkipValidateParams: true,
	}, listModerationMergeRequests(cat))
	huma.Register(api, huma.Operation{
		OperationID: "getModerationMergeRequest", Method: http.MethodGet, Path: "/v2/moderation/merge-requests/{id}",
		Summary:     "Get one merge request",
		Description: "Requires the catalog.merge.review permission.",
		Tags:        mod, Errors: errs, SkipValidateParams: true,
	}, getModerationMergeRequest(cat))
	huma.Register(api, huma.Operation{
		OperationID: "decideModerationMergeRequest", Method: http.MethodPost, Path: "/v2/moderation/merge-requests/{id}/decisions",
		Summary:     "Decide a merge request",
		Description: "decision=approve|reject|execute|undo. undo revives the source under its own id, moves back what the merge moved, restores what it archived and revokes the redirect (the redirects feed then lists it with revoked=true); the response carries undo_report. A merge executed before undo was recorded, or past catalog.merge_undo_window_days, is refused 409. Requires the catalog.merge.review permission.",
		Tags:        mod, Errors: errs, DefaultStatus: http.StatusCreated, SkipValidateParams: true,
	}, decideModerationMergeRequest(cat))
	registerMergeWorkbench(api, cat, errs)
}

func (c *Catalog) mergesBound() error {
	if c == nil || c.Merges == nil {
		return problem.New(problem.CodeServiceUnavailable, "", "", "merge requests are not bound.")
	}
	return nil
}

func requireFirstPartyUser(ctx context.Context) (int64, error) {
	uid, _, err := requireUser(ctx)
	if err != nil {
		return 0, err
	}
	if _, err := requireSite(ctx); err != nil {
		return 0, err
	}
	if thirdPartyFrom(ctx) {
		return 0, problem.New(problem.CodePermissionRequired, "", "", "merge requests are open to first-party sites only.")
	}
	return uid, nil
}

func mergeRequestQuota(ctx context.Context) int64 {
	switch editTier(ctx) {
	case tierReviewer:
		return keys.CatalogMergeRequestsPerDayReviewer.Get()
	case tierEditor:
		return keys.CatalogMergeRequestsPerDayEditor.Get()
	}
	return keys.CatalogMergeRequestsPerDay.Get()
}

func createMyMergeRequest(cat *Catalog) func(context.Context, *createMergeRequestInput) (*mergeRequestOutput, error) {
	return func(ctx context.Context, in *createMergeRequestInput) (*mergeRequestOutput, error) {
		if err := cat.mergesBound(); err != nil {
			return nil, catalogErr(ctx, err)
		}
		uid, err := requireFirstPartyUser(ctx)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		et, ok := mergeEntityTypeOf(in.Body.Object)
		if !ok {
			return nil, catalogErr(ctx, problem.New(problem.CodeNotFound, "", "", "entries of family "+in.Body.Object+" cannot be merged."))
		}
		src, ok1 := repr.ParseID(in.Body.SourceID)
		dst, ok2 := repr.ParseID(in.Body.TargetID)
		if !ok1 || !ok2 {
			return nil, catalogErr(ctx, problem.New(problem.CodeValidationFailed, "", "", "source_id and target_id must be catalog ids."))
		}
		choices, err := in.Body.choices()
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		window := quotaWindow()
		used, err := cat.Merges.CountMergeRequestsSince(ctx, uid, time.Now().Add(-window))
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		if limit := mergeRequestQuota(ctx); used >= limit {
			return nil, catalogErr(ctx, quotaExceeded("the merge request limit for this account is exhausted", used, limit, window))
		}
		p, err := cat.Merges.RequestMerge(ctx, et, src, dst, uid, in.Body.Reason, choices)
		if err != nil {
			return nil, catalogErr(ctx, mergeRequestErr(err))
		}
		return &mergeRequestOutput{Body: cat.mergeRequestRecord(ctx, p)}, nil
	}
}

func listMyMergeRequests(cat *Catalog) func(context.Context, *listMergeRequestsInput) (*mergeRequestListOutput, error) {
	return func(ctx context.Context, in *listMergeRequestsInput) (*mergeRequestListOutput, error) {
		if err := checkMergeListQuery(in); err != nil {
			return nil, catalogErr(ctx, err)
		}
		if err := cat.mergesBound(); err != nil {
			return nil, catalogErr(ctx, err)
		}
		uid, err := requireFirstPartyUser(ctx)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		f := mergeRequestFilter(in)
		f.ProposedBy = &uid
		page, err := cat.listMergeRequests(ctx, f, in)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &mergeRequestListOutput{Body: page}, nil
	}
}

func patchMyMergeRequest(cat *Catalog) func(context.Context, *patchMergeRequestInput) (*mergeRequestOutput, error) {
	return func(ctx context.Context, in *patchMergeRequestInput) (*mergeRequestOutput, error) {
		if err := cat.mergesBound(); err != nil {
			return nil, catalogErr(ctx, err)
		}
		uid, err := requireFirstPartyUser(ctx)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		id, _ := repr.ParseID(in.ID)
		p, err := cat.Merges.GetMergeRequest(ctx, id)
		if err != nil {
			return nil, catalogErr(ctx, mergeRequestErr(err))
		}
		if p.ProposedBy != uid || p.Origin != catsvc.OriginEditor {
			return nil, catalogErr(ctx, problem.New(problem.CodePermissionRequired, "", "", "only the requester may withdraw a merge request."))
		}
		if err := cat.Merges.WithdrawMerge(ctx, id, uid); err != nil {
			return nil, catalogErr(ctx, mergeRequestErr(err))
		}
		return cat.mergeRequestAfterWrite(ctx, id)
	}
}

func listModerationMergeRequests(cat *Catalog) func(context.Context, *listMergeRequestsInput) (*mergeRequestListOutput, error) {
	return func(ctx context.Context, in *listMergeRequestsInput) (*mergeRequestListOutput, error) {
		if err := checkMergeListQuery(in); err != nil {
			return nil, catalogErr(ctx, err)
		}
		if err := cat.mergesBound(); err != nil {
			return nil, catalogErr(ctx, err)
		}
		if err := requireMergeReviewer(ctx); err != nil {
			return nil, catalogErr(ctx, err)
		}
		f := mergeRequestFilter(in)
		f.Origin = catsvc.OriginEditor
		page, err := cat.listMergeRequests(ctx, f, in)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &mergeRequestListOutput{Body: page}, nil
	}
}

func getModerationMergeRequest(cat *Catalog) func(context.Context, *mergeRequestIDInput) (*mergeRequestOutput, error) {
	return func(ctx context.Context, in *mergeRequestIDInput) (*mergeRequestOutput, error) {
		if err := cat.mergesBound(); err != nil {
			return nil, catalogErr(ctx, err)
		}
		if err := requireMergeReviewer(ctx); err != nil {
			return nil, catalogErr(ctx, err)
		}
		id, _ := repr.ParseID(in.ID)
		return cat.mergeRequestAfterWrite(ctx, id)
	}
}

func decideModerationMergeRequest(cat *Catalog) func(context.Context, *decideMergeRequestInput) (*mergeRequestOutput, error) {
	return func(ctx context.Context, in *decideMergeRequestInput) (*mergeRequestOutput, error) {
		if err := cat.mergesBound(); err != nil {
			return nil, catalogErr(ctx, err)
		}
		if err := requireMergeReviewer(ctx); err != nil {
			return nil, catalogErr(ctx, err)
		}
		uid := userIDFrom(ctx)
		id, _ := repr.ParseID(in.ID)
		p, err := cat.Merges.GetMergeRequest(ctx, id)
		if err != nil {
			return nil, catalogErr(ctx, mergeRequestErr(err))
		}
		if p.Origin != catsvc.OriginEditor {
			return nil, catalogErr(ctx, problem.New(problem.CodeNotFound, "", "", "no merge request with that id was filed through the editor."))
		}
		switch in.Body.Decision {
		case "approve":
			err = cat.Merges.ApproveMerge(ctx, id, uid)
		case "reject":
			if len(in.Body.Reason) == 0 {
				pr := problem.New(problem.CodeValidationFailed, "", "", "a rejection needs a reason.")
				pr.Errors = []problem.FieldError{{Pointer: "/reason", Reason: problem.ReasonRequired, Detail: "required to reject"}}
				return nil, catalogErr(ctx, pr)
			}
			err = cat.Merges.RejectMerge(ctx, id, uid, in.Body.Reason)
		case "execute":
			err = cat.Merges.ExecuteMerge(ctx, id, &uid)
		case "undo":
			if len(in.Body.Reason) == 0 {
				pr := problem.New(problem.CodeValidationFailed, "", "", "an undo needs a reason.")
				pr.Errors = []problem.FieldError{{Pointer: "/reason", Reason: problem.ReasonRequired, Detail: "required to undo"}}
				return nil, catalogErr(ctx, pr)
			}
			rep, uerr := cat.Merges.UndoMerge(ctx, id, uid, in.Body.Reason)
			if uerr != nil {
				return nil, catalogErr(ctx, mergeRequestErr(uerr))
			}
			out, oerr := cat.mergeRequestAfterWrite(ctx, id)
			if oerr != nil {
				return nil, oerr
			}
			out.Body.UndoReport = undoReportFrom(rep)
			return out, nil
		default:
			return nil, catalogErr(ctx, problem.New(problem.CodeValidationFailed, "", "", "decision must be approve, reject, execute or undo."))
		}
		if err != nil {
			return nil, catalogErr(ctx, mergeRequestErr(err))
		}
		return cat.mergeRequestAfterWrite(ctx, id)
	}
}

func checkMergeListQuery(in *listMergeRequestsInput) error {
	if in.Limit > 100 {
		return problem.New(problem.CodeLimitTooLarge, "", "", "limit is at most 100.")
	}
	if in.Limit < 0 {
		return problem.New(problem.CodeInvalidParameter, "", "", "limit is at least 1.")
	}
	return nil
}

func requireMergeReviewer(ctx context.Context) error {
	if _, err := requireFirstPartyUser(ctx); err != nil {
		return err
	}
	return requireModerationKey(ctx, catalogPerm.MergeReview, rolesFrom(ctx))
}

func mergeRequestFilter(in *listMergeRequestsInput) catsvc.MergeRequestFilter {
	var f catsvc.MergeRequestFilter
	if st, ok := mergeRequestStates[in.State]; ok {
		f.Status = &st
	}
	if et, ok := mergeEntityTypeOf(in.Object); ok {
		f.EntityType = &et
	}
	f.OriginSite = in.OriginSite
	return f
}

func (c *Catalog) listMergeRequests(ctx context.Context, f catsvc.MergeRequestFilter, in *listMergeRequestsInput) (repr.List[repr.MergeRequestRecord], error) {
	limit := in.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if in.Cursor != "" {
		before, err := strconv.ParseInt(strings.TrimPrefix(in.Cursor, "cur_"), 10, 64)
		if err != nil || before <= 0 {
			return repr.List[repr.MergeRequestRecord]{}, problem.New(problem.CodeInvalidCursor, "", "", "cursor is not one this face issued.")
		}
		f.BeforeID = before
	}
	f.Limit = limit + 1
	rows, err := c.Merges.ListMergeRequests(ctx, f)
	if err != nil {
		return repr.List[repr.MergeRequestRecord]{}, err
	}
	out := repr.List[repr.MergeRequestRecord]{Object: "list"}
	if len(rows) > limit {
		rows = rows[:limit]
		next := "cur_" + strconv.FormatInt(rows[len(rows)-1].ID, 10)
		out.NextCursor = &next
	}
	deadlines, err := c.Merges.UndoDeadlines(ctx, rows)
	if err != nil {
		return repr.List[repr.MergeRequestRecord]{}, err
	}
	out.Items = make([]repr.MergeRequestRecord, 0, len(rows))
	for i := range rows {
		rec := mergeRequestFrom(&rows[i])
		if at, ok := deadlines[rows[i].ID]; ok {
			s := rfc3339(at)
			rec.UndoUntil = &s
		}
		out.Items = append(out.Items, rec)
	}
	c.attachMergeRequestPeople(ctx, out.Items)
	return out, nil
}

func (c *Catalog) mergeRequestAfterWrite(ctx context.Context, id int64) (*mergeRequestOutput, error) {
	p, err := c.Merges.GetMergeRequest(ctx, id)
	if err != nil {
		return nil, catalogErr(ctx, mergeRequestErr(err))
	}
	return &mergeRequestOutput{Body: c.mergeRequestRecord(ctx, p)}, nil
}

func (c *Catalog) mergeRequestRecord(ctx context.Context, p *model.CatalogMergeProposal) repr.MergeRequestRecord {
	items := []repr.MergeRequestRecord{mergeRequestFrom(p)}
	if deadlines, err := c.Merges.UndoDeadlines(ctx, []model.CatalogMergeProposal{*p}); err == nil {
		if at, ok := deadlines[p.ID]; ok {
			s := rfc3339(at)
			items[0].UndoUntil = &s
		}
	}
	if p.Status == model.ProposalStatusExecuted || p.Status == model.ProposalStatusUndone {
		if moved, err := c.Merges.MovedCounts(ctx, p.ID); err == nil {
			items[0].Moved = moved
		}
	}
	c.attachMergeRequestPeople(ctx, items)
	return items[0]
}

func mergeRequestFrom(p *model.CatalogMergeProposal) repr.MergeRequestRecord {
	rec := repr.MergeRequestRecord{
		Object: "merge_request", ID: repr.ID(p.ID), State: mergeRequestState(p.Status),
		TargetObject: mergeObjectOf(p.EntityType),
		SourceID:     repr.ID(p.SourceEntityID), TargetID: repr.ID(p.TargetEntityID),
		Note: p.Note, ProposedByUID: repr.ID(p.ProposedBy), Origin: p.Origin, OriginSite: p.OriginSite,
		ProposedAt: rfc3339(p.ProposedAt),
	}
	if p.ExecuteAfter != nil {
		at := rfc3339(*p.ExecuteAfter)
		rec.ExecuteAfter = &at
	}
	if p.ExecutedAt != nil {
		at := rfc3339(*p.ExecutedAt)
		rec.ExecutedAt = &at
	}
	rec.FieldResolution, rec.HideReleaseIDs, rec.Moved = map[string]string{}, []string{}, map[string]int64{}
	if choices, err := catsvc.MergeChoicesOf(p); err == nil {
		for k, v := range choices.Fields {
			rec.FieldResolution[k] = v
		}
		for _, id := range choices.HideReleases {
			rec.HideReleaseIDs = append(rec.HideReleaseIDs, repr.ID(id))
		}
	}
	if p.AmendedBy != nil {
		uid := repr.ID(*p.AmendedBy)
		rec.AmendedByUID = &uid
	}
	if p.AmendedAt != nil {
		at := rfc3339(*p.AmendedAt)
		rec.AmendedAt = &at
	}
	if p.UndoneBy != nil {
		uid := repr.ID(*p.UndoneBy)
		rec.UndoneByUID = &uid
	}
	if p.UndoneAt != nil {
		at := rfc3339(*p.UndoneAt)
		rec.UndoneAt = &at
	}
	return rec
}

func (c *Catalog) attachMergeRequestPeople(ctx context.Context, items []repr.MergeRequestRecord) {
	if c.UsersOf == nil || len(items) == 0 {
		return
	}
	seen := map[int64]struct{}{}
	var ids []int64
	add := func(raw string) {
		if id, ok := repr.ParseID(raw); ok && id > 0 {
			if _, dup := seen[id]; !dup {
				seen[id] = struct{}{}
				ids = append(ids, id)
			}
		}
	}
	for _, it := range items {
		add(it.ProposedByUID)
		if it.AmendedByUID != nil {
			add(*it.AmendedByUID)
		}
		if it.UndoneByUID != nil {
			add(*it.UndoneByUID)
		}
	}
	if len(ids) == 0 {
		return
	}
	users, err := c.UsersOf(ctx, ids)
	if err != nil {
		return
	}
	lookup := func(raw *string) *repr.UserRef {
		if raw == nil {
			return nil
		}
		if id, ok := repr.ParseID(*raw); ok {
			if u, ok := users[id]; ok {
				return &u
			}
		}
		return nil
	}
	for i := range items {
		items[i].ProposedBy = lookup(&items[i].ProposedByUID)
		items[i].AmendedBy = lookup(items[i].AmendedByUID)
		items[i].UndoneBy = lookup(items[i].UndoneByUID)
	}
}

func mergeRequestErr(err error) error {
	switch {
	case errors.Is(err, catsvc.ErrNotFound):
		return problem.New(problem.CodeNotFound, "", "", err.Error())
	case errors.Is(err, catsvc.ErrDuplicateOpenProposal):
		return problem.New(problem.CodeAlreadyExists, "", "", err.Error())
	case errors.Is(err, catsvc.ErrProposalState):
		return problem.New(problem.CodeInvalidStateTransition, "", "", err.Error())
	case errors.Is(err, catsvc.ErrCoolingOff):
		return problem.New(problem.CodeInvalidStateTransition, "", "", "the cooling-off has not ended yet.")
	case errors.Is(err, catsvc.ErrSameEntity), errors.Is(err, catsvc.ErrCrossMedium),
		errors.Is(err, catsvc.ErrEntityNotLive), errors.Is(err, catsvc.ErrHiddenSurvivor),
		errors.Is(err, catsvc.ErrMergeChoice):
		return problem.New(problem.CodeValidationFailed, "", "", err.Error())
	case errors.Is(err, catsvc.ErrUndoUnavailable), errors.Is(err, catsvc.ErrUndoExpired), errors.Is(err, catsvc.ErrUndoBlocked):
		return problem.New(problem.CodeInvalidStateTransition, "", "", err.Error())
	}
	return err
}
