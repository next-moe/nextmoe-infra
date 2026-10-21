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

var deletionRequestStates = map[string]int16{
	"open":      model.ProposalStatusOpen,
	"approved":  model.ProposalStatusApproved,
	"executed":  model.ProposalStatusExecuted,
	"rejected":  model.ProposalStatusRejected,
	"withdrawn": model.ProposalStatusWithdrawn,
	"restored":  model.DeletionStatusRestored,
}

func deletionRequestState(status int16) string {
	for name, s := range deletionRequestStates {
		if s == status {
			return name
		}
	}
	return "open"
}

type createDeletionRequestInput struct {
	Body struct {
		Object   string `json:"object" enum:"work" doc:"Family of the entry. Only works can be deleted today."`
		TargetID string `json:"target_id" pattern:"^[0-9]+$" maxLength:"20" doc:"The entry to delete."`
		Reason   string `json:"reason" enum:"not_a_work,out_of_scope,spam_or_test,other,duplicate" doc:"Why it should go. duplicate is refused 422: a duplicate is merged into the entry it duplicates through /v2/me/merge-requests."`
		Note     string `json:"note" minLength:"4" maxLength:"2000" doc:"Evidence reviewers can check. Must not be used as a discriminant."`
	}
}

type listDeletionRequestsInput struct {
	State    string `query:"state" enum:"open,approved,executed,rejected,withdrawn,restored" doc:"Narrow to one state."`
	TargetID string `query:"target_id" pattern:"^[0-9]+$" maxLength:"20" doc:"Narrow to requests about one work."`
	Cursor   string `query:"cursor" pattern:"^cur_[0-9]+$" maxLength:"32" doc:"Opaque keyset cursor from next_cursor."`
	Limit    int    `query:"limit" minimum:"1" maximum:"100" doc:"Page size, 1-100. Default 20."`
}

type deletionRequestIDInput struct {
	ID string `path:"id" pattern:"^[0-9]+$" maxLength:"20" doc:"Deletion request id."`
}

type patchDeletionRequestInput struct {
	ID   string `path:"id" pattern:"^[0-9]+$" maxLength:"20" doc:"Deletion request id."`
	Body struct {
		State string `json:"state" enum:"withdrawn" doc:"Set withdrawn to withdraw an open request you filed."`
	}
}

type decideDeletionRequestInput struct {
	ID   string `path:"id" pattern:"^[0-9]+$" maxLength:"20" doc:"Deletion request id."`
	Body struct {
		Decision string `json:"decision" enum:"approve,reject,execute,restore" doc:"approve starts the cooling-off (catalog.deletion_cooling_off_hours) after which the catalog-deletion-requests job deletes the entry; reject closes it; execute runs an approved request whose cooling-off has ended without waiting for the job; restore brings an executed deletion back under the same id."`
		Reason   string `json:"reason,omitempty" maxLength:"2000" doc:"Required to reject; an optional note to restore. Must not be used as a discriminant."`
	}
}

type deletionPreviewInput struct {
	Object   string `query:"object" enum:"work" doc:"Family of the entry."`
	TargetID string `query:"target_id" pattern:"^[0-9]+$" maxLength:"20" doc:"The entry."`
}

type deletionRequestOutput struct {
	Body repr.DeletionRequestRecord
}

type deletionRequestListOutput struct {
	Body repr.List[repr.DeletionRequestRecord]
}

type deletionPreviewOutput struct {
	Body repr.DeletionPreview
}

func registerDeletionRequests(api huma.API, cat *Catalog) {
	errs := collectionErrors(http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity, http.StatusTooManyRequests,
		http.StatusServiceUnavailable)
	me, mod := []string{"me"}, []string{"moderation"}
	huma.Register(api, huma.Operation{
		OperationID: "createMyDeletionRequest", Method: http.MethodPost, Path: "/v2/me/deletion-requests",
		Summary: "Request a deletion",
		Description: "Asks reviewers to delete a catalog entry; only works can be deleted today. Nothing is deleted on filing: a reviewer approves, the cooling-off runs, then the entry is soft-deleted. Its links to source entries stay as a tombstone so the importers do not create it again, and user records on it are kept but hidden until a reviewer restores it under the same id. " +
			"A duplicate is refused 422: merge it with /v2/me/merge-requests instead. One open or approved request per entry; another is 409. Quotas are tiered: catalog.deletion_requests_per_day, _editor, _reviewer inside catalog.write_quota_window_hours; past it 429 QUOTA_EXCEEDED. " +
			"Requires a user access token bound to a first-party catalog site. The token must carry the catalog:edit scope.",
		Tags: me, Errors: errs, DefaultStatus: http.StatusCreated, SkipValidateParams: true,
	}, createMyDeletionRequest(cat))
	huma.Register(api, huma.Operation{
		OperationID: "listMyDeletionRequests", Method: http.MethodGet, Path: "/v2/me/deletion-requests",
		Summary:     "List my deletion requests",
		Description: "The bearer's own deletion requests, newest first; follow next_cursor for more. Requires a user access token bound to a first-party catalog site. The token must carry the catalog:edit scope.",
		Tags:        me, Errors: errs, SkipValidateParams: true,
	}, listMyDeletionRequests(cat))
	huma.Register(api, huma.Operation{
		OperationID: "patchMyDeletionRequest", Method: http.MethodPatch, Path: "/v2/me/deletion-requests/{id}",
		Summary:     "Withdraw a deletion request",
		Description: "Withdraws an open request the bearer filed. Requires a user access token bound to a first-party catalog site. The token must carry the catalog:edit scope.",
		Tags:        me, Errors: errs, SkipValidateParams: true,
	}, patchMyDeletionRequest(cat))
	huma.Register(api, huma.Operation{
		OperationID: "getMyDeletionPreview", Method: http.MethodGet, Path: "/v2/me/deletion-preview",
		Summary:     "Preview what a deletion affects",
		Description: "What deleting the entry would touch: its links to source entries, releases, user records that would be hidden, open edit proposals that would be declined, the claiming site, and any pending request. Also answers for an entry that is already deleted. Requires a user access token bound to a first-party catalog site. The token must carry the catalog:edit scope.",
		Tags:        me, Errors: errs, SkipValidateParams: true,
	}, getMyDeletionPreview(cat))
	huma.Register(api, huma.Operation{
		OperationID: "listModerationDeletionRequests", Method: http.MethodGet, Path: "/v2/moderation/deletion-requests",
		Summary:     "Deletion request queue",
		Description: "Deletion requests, newest first; follow next_cursor for more. Requires the catalog.delete.review permission.",
		Tags:        mod, Errors: errs, SkipValidateParams: true,
	}, listModerationDeletionRequests(cat))
	huma.Register(api, huma.Operation{
		OperationID: "getModerationDeletionRequest", Method: http.MethodGet, Path: "/v2/moderation/deletion-requests/{id}",
		Summary:     "Get one deletion request",
		Description: "Requires the catalog.delete.review permission.",
		Tags:        mod, Errors: errs, SkipValidateParams: true,
	}, getModerationDeletionRequest(cat))
	huma.Register(api, huma.Operation{
		OperationID: "decideModerationDeletionRequest", Method: http.MethodPost, Path: "/v2/moderation/deletion-requests/{id}/decisions",
		Summary:     "Decide a deletion request",
		Description: "decision=approve|reject|execute|restore. Requires the catalog.delete.review permission.",
		Tags:        mod, Errors: errs, DefaultStatus: http.StatusCreated, SkipValidateParams: true,
	}, decideModerationDeletionRequest(cat))
}

func (c *Catalog) deletionsBound() error {
	if c == nil || c.Deletions == nil {
		return problem.New(problem.CodeServiceUnavailable, "", "", "deletion requests are not bound.")
	}
	return nil
}

func deletionRequestQuota(ctx context.Context) int64 {
	switch editTier(ctx) {
	case tierReviewer:
		return keys.CatalogDeletionRequestsPerDayReviewer.Get()
	case tierEditor:
		return keys.CatalogDeletionRequestsPerDayEditor.Get()
	}
	return keys.CatalogDeletionRequestsPerDay.Get()
}

func createMyDeletionRequest(cat *Catalog) func(context.Context, *createDeletionRequestInput) (*deletionRequestOutput, error) {
	return func(ctx context.Context, in *createDeletionRequestInput) (*deletionRequestOutput, error) {
		if err := cat.deletionsBound(); err != nil {
			return nil, catalogErr(ctx, err)
		}
		uid, err := requireFirstPartyUser(ctx)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		id, ok := repr.ParseID(in.Body.TargetID)
		if !ok {
			return nil, catalogErr(ctx, problem.New(problem.CodeValidationFailed, "", "", "target_id must be a catalog id."))
		}
		window := quotaWindow()
		used, err := cat.Deletions.CountSince(ctx, uid, time.Now().Add(-window))
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		if limit := deletionRequestQuota(ctx); used >= limit {
			return nil, catalogErr(ctx, quotaExceeded("the deletion request limit for this account is exhausted", used, limit, window))
		}
		r, err := cat.Deletions.Request(ctx, model.EntityTypeWork, id, uid, in.Body.Reason, in.Body.Note)
		if err != nil {
			return nil, catalogErr(ctx, deletionRequestErr(err))
		}
		return &deletionRequestOutput{Body: cat.deletionRequestRecord(ctx, r)}, nil
	}
}

func listMyDeletionRequests(cat *Catalog) func(context.Context, *listDeletionRequestsInput) (*deletionRequestListOutput, error) {
	return func(ctx context.Context, in *listDeletionRequestsInput) (*deletionRequestListOutput, error) {
		if err := checkDeletionListQuery(in); err != nil {
			return nil, catalogErr(ctx, err)
		}
		if err := cat.deletionsBound(); err != nil {
			return nil, catalogErr(ctx, err)
		}
		uid, err := requireFirstPartyUser(ctx)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		f := deletionRequestFilter(in)
		f.ProposedBy = &uid
		page, err := cat.listDeletionRequests(ctx, f, in)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &deletionRequestListOutput{Body: page}, nil
	}
}

func patchMyDeletionRequest(cat *Catalog) func(context.Context, *patchDeletionRequestInput) (*deletionRequestOutput, error) {
	return func(ctx context.Context, in *patchDeletionRequestInput) (*deletionRequestOutput, error) {
		if err := cat.deletionsBound(); err != nil {
			return nil, catalogErr(ctx, err)
		}
		uid, err := requireFirstPartyUser(ctx)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		id, _ := repr.ParseID(in.ID)
		r, err := cat.Deletions.Get(ctx, id)
		if err != nil {
			return nil, catalogErr(ctx, deletionRequestErr(err))
		}
		if r.ProposedBy != uid {
			return nil, catalogErr(ctx, problem.New(problem.CodePermissionRequired, "", "", "only the requester may withdraw a deletion request."))
		}
		if err := cat.Deletions.Withdraw(ctx, id); err != nil {
			return nil, catalogErr(ctx, deletionRequestErr(err))
		}
		return cat.deletionRequestAfterWrite(ctx, id)
	}
}

func getMyDeletionPreview(cat *Catalog) func(context.Context, *deletionPreviewInput) (*deletionPreviewOutput, error) {
	return func(ctx context.Context, in *deletionPreviewInput) (*deletionPreviewOutput, error) {
		if err := cat.deletionsBound(); err != nil {
			return nil, catalogErr(ctx, err)
		}
		if _, err := requireFirstPartyUser(ctx); err != nil {
			return nil, catalogErr(ctx, err)
		}
		id, ok := repr.ParseID(in.TargetID)
		if !ok {
			pr := problem.New(problem.CodeValidationFailed, "", "", "target_id is required.")
			pr.Errors = []problem.FieldError{{Parameter: "target_id", Reason: problem.ReasonRequired, Detail: "the work to preview"}}
			return nil, catalogErr(ctx, pr)
		}
		p, err := cat.Deletions.Preview(ctx, id)
		if err != nil {
			return nil, catalogErr(ctx, deletionRequestErr(err))
		}
		return &deletionPreviewOutput{Body: deletionPreviewFrom(p)}, nil
	}
}

func listModerationDeletionRequests(cat *Catalog) func(context.Context, *listDeletionRequestsInput) (*deletionRequestListOutput, error) {
	return func(ctx context.Context, in *listDeletionRequestsInput) (*deletionRequestListOutput, error) {
		if err := checkDeletionListQuery(in); err != nil {
			return nil, catalogErr(ctx, err)
		}
		if err := cat.deletionsBound(); err != nil {
			return nil, catalogErr(ctx, err)
		}
		if err := requireDeletionReviewer(ctx); err != nil {
			return nil, catalogErr(ctx, err)
		}
		page, err := cat.listDeletionRequests(ctx, deletionRequestFilter(in), in)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &deletionRequestListOutput{Body: page}, nil
	}
}

func getModerationDeletionRequest(cat *Catalog) func(context.Context, *deletionRequestIDInput) (*deletionRequestOutput, error) {
	return func(ctx context.Context, in *deletionRequestIDInput) (*deletionRequestOutput, error) {
		if err := cat.deletionsBound(); err != nil {
			return nil, catalogErr(ctx, err)
		}
		if err := requireDeletionReviewer(ctx); err != nil {
			return nil, catalogErr(ctx, err)
		}
		id, _ := repr.ParseID(in.ID)
		return cat.deletionRequestAfterWrite(ctx, id)
	}
}

func decideModerationDeletionRequest(cat *Catalog) func(context.Context, *decideDeletionRequestInput) (*deletionRequestOutput, error) {
	return func(ctx context.Context, in *decideDeletionRequestInput) (*deletionRequestOutput, error) {
		if err := cat.deletionsBound(); err != nil {
			return nil, catalogErr(ctx, err)
		}
		if err := requireDeletionReviewer(ctx); err != nil {
			return nil, catalogErr(ctx, err)
		}
		uid := userIDFrom(ctx)
		id, _ := repr.ParseID(in.ID)
		var err error
		switch in.Body.Decision {
		case "approve":
			err = cat.Deletions.Approve(ctx, id, uid)
		case "reject":
			if strings.TrimSpace(in.Body.Reason) == "" {
				pr := problem.New(problem.CodeValidationFailed, "", "", "a rejection needs a reason.")
				pr.Errors = []problem.FieldError{{Pointer: "/reason", Reason: problem.ReasonRequired, Detail: "required to reject"}}
				return nil, catalogErr(ctx, pr)
			}
			err = cat.Deletions.Reject(ctx, id, uid, in.Body.Reason)
		case "execute":
			var closed bool
			closed, err = cat.Deletions.Execute(ctx, id, &uid)
			if err == nil && closed {
				out, getErr := cat.deletionRequestAfterWrite(ctx, id)
				if getErr != nil {
					return nil, getErr
				}
				return out, nil
			}
		case "restore":
			err = cat.Deletions.Restore(ctx, id, uid, in.Body.Reason)
		default:
			return nil, catalogErr(ctx, problem.New(problem.CodeValidationFailed, "", "", "decision must be approve, reject, execute or restore."))
		}
		if err != nil {
			return nil, catalogErr(ctx, deletionRequestErr(err))
		}
		return cat.deletionRequestAfterWrite(ctx, id)
	}
}

func checkDeletionListQuery(in *listDeletionRequestsInput) error {
	if in.Limit > 100 {
		return problem.New(problem.CodeLimitTooLarge, "", "", "limit is at most 100.")
	}
	if in.Limit < 0 {
		return problem.New(problem.CodeInvalidParameter, "", "", "limit is at least 1.")
	}
	return nil
}

func requireDeletionReviewer(ctx context.Context) error {
	if _, err := requireFirstPartyUser(ctx); err != nil {
		return err
	}
	return requireModerationKey(ctx, catalogPerm.DeleteReview, rolesFrom(ctx))
}

func deletionRequestFilter(in *listDeletionRequestsInput) catsvc.DeletionRequestFilter {
	var f catsvc.DeletionRequestFilter
	if st, ok := deletionRequestStates[in.State]; ok {
		f.Status = &st
	}
	if id, ok := repr.ParseID(in.TargetID); ok && in.TargetID != "" {
		et := model.EntityTypeWork
		f.EntityType = &et
		f.EntityID = &id
	}
	return f
}

func (c *Catalog) listDeletionRequests(ctx context.Context, f catsvc.DeletionRequestFilter, in *listDeletionRequestsInput) (repr.List[repr.DeletionRequestRecord], error) {
	limit := in.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if in.Cursor != "" {
		before, err := strconv.ParseInt(strings.TrimPrefix(in.Cursor, "cur_"), 10, 64)
		if err != nil || before <= 0 {
			return repr.List[repr.DeletionRequestRecord]{}, problem.New(problem.CodeInvalidCursor, "", "", "cursor is not one this face issued.")
		}
		f.BeforeID = before
	}
	f.Limit = limit + 1
	rows, err := c.Deletions.List(ctx, f)
	if err != nil {
		return repr.List[repr.DeletionRequestRecord]{}, err
	}
	out := repr.List[repr.DeletionRequestRecord]{Object: "list"}
	if len(rows) > limit {
		rows = rows[:limit]
		next := "cur_" + strconv.FormatInt(rows[len(rows)-1].ID, 10)
		out.NextCursor = &next
	}
	out.Items = make([]repr.DeletionRequestRecord, 0, len(rows))
	for i := range rows {
		out.Items = append(out.Items, deletionRequestFrom(&rows[i]))
	}
	c.attachDeletionRequestPeople(ctx, out.Items)
	return out, nil
}

func (c *Catalog) deletionRequestAfterWrite(ctx context.Context, id int64) (*deletionRequestOutput, error) {
	r, err := c.Deletions.Get(ctx, id)
	if err != nil {
		return nil, catalogErr(ctx, deletionRequestErr(err))
	}
	return &deletionRequestOutput{Body: c.deletionRequestRecord(ctx, r)}, nil
}

func (c *Catalog) deletionRequestRecord(ctx context.Context, r *model.CatalogDeletionRequest) repr.DeletionRequestRecord {
	items := []repr.DeletionRequestRecord{deletionRequestFrom(r)}
	c.attachDeletionRequestPeople(ctx, items)
	return items[0]
}

func optRFC3339(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := rfc3339(*t)
	return &s
}

func optIDPtr(id *int64) *string {
	if id == nil || *id <= 0 {
		return nil
	}
	s := repr.ID(*id)
	return &s
}

func deletionRequestFrom(r *model.CatalogDeletionRequest) repr.DeletionRequestRecord {
	return repr.DeletionRequestRecord{
		Object: "deletion_request", ID: repr.ID(r.ID), State: deletionRequestState(r.Status),
		TargetObject: "work", TargetID: repr.ID(r.EntityID),
		Reason: r.Reason, Note: r.Note, DecisionNote: r.DecisionNote, RestoreNote: r.RestoreNote,
		ProposedByUID: repr.ID(r.ProposedBy), DecidedByUID: optIDPtr(r.DecidedBy), RestoredByUID: optIDPtr(r.RestoredBy),
		OriginSite: r.OriginSite, ProposedAt: rfc3339(r.ProposedAt), DecidedAt: optRFC3339(r.DecidedAt),
		ExecuteAfter: optRFC3339(r.ExecuteAfter), ExecutedAt: optRFC3339(r.ExecutedAt),
		RestoredAt: optRFC3339(r.RestoredAt),
	}
}

func (c *Catalog) attachDeletionRequestPeople(ctx context.Context, items []repr.DeletionRequestRecord) {
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
		add(items[i].DecidedByUID)
		add(items[i].RestoredByUID)
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
		items[i].DecidedBy = lookup(items[i].DecidedByUID)
		items[i].RestoredBy = lookup(items[i].RestoredByUID)
	}
}

var anchorKinds = map[int16]string{
	model.LinkKindExact:    "identity",
	model.LinkKindProbable: "probable",
	model.LinkKindRelated:  "related",
}

func deletionPreviewFrom(p *catsvc.DeletionPreview) repr.DeletionPreview {
	out := repr.DeletionPreview{
		Object: "deletion_preview", TargetObject: "work", TargetID: repr.ID(p.WorkID),
		DisplayName: p.DisplayName, Deleted: p.Deleted, MergedIntoID: optIDPtr(p.MergedInto),
		Anchors: make([]repr.DeletionAnchor, 0, len(p.Anchors)), Releases: p.Releases,
		UserData: repr.DeletionUserData{
			Ratings: p.Ratings, FolderItems: p.FolderItems, Playtimes: p.Playtimes, WorkStates: p.WorkStates,
		},
		OpenProposals: p.OpenProposals, PendingRequestID: optIDPtr(p.PendingRequestID),
	}
	if p.ClaimSite != "" {
		site := p.ClaimSite
		out.ClaimSite = &site
	}
	for _, a := range p.Anchors {
		kind, ok := anchorKinds[a.LinkKind]
		if !ok {
			continue
		}
		out.Anchors = append(out.Anchors, repr.DeletionAnchor{Source: a.Source, ExternalID: a.ExternalID, Kind: kind})
	}
	return out
}

func deletionRequestErr(err error) error {
	switch {
	case errors.Is(err, catsvc.ErrNotFound):
		return problem.New(problem.CodeNotFound, "", "", err.Error())
	case errors.Is(err, catsvc.ErrDeletionPending):
		return problem.New(problem.CodeAlreadyExists, "", "", err.Error())
	case errors.Is(err, catsvc.ErrProposalState), errors.Is(err, catsvc.ErrDeletionNotDeleted):
		return problem.New(problem.CodeInvalidStateTransition, "", "", err.Error())
	case errors.Is(err, catsvc.ErrCoolingOff):
		return problem.New(problem.CodeInvalidStateTransition, "", "", "the cooling-off has not ended yet.")
	case errors.Is(err, catsvc.ErrDeletionDuplicate):
		pr := problem.New(problem.CodeValidationFailed, "", "", err.Error()+"; file a merge request instead.")
		pr.Errors = []problem.FieldError{{Pointer: "/reason", Reason: problem.ReasonNotAllowedValue, Detail: "a duplicate is merged through /v2/me/merge-requests"}}
		return pr
	case errors.Is(err, catsvc.ErrDeletionReason), errors.Is(err, catsvc.ErrDeletionFamily),
		errors.Is(err, catsvc.ErrEntityNotLive):
		return problem.New(problem.CodeValidationFailed, "", "", err.Error())
	}
	return err
}

func (c *Catalog) missingWork(ctx context.Context, id int64) error {
	if c.Deletions != nil {
		gone, err := c.Deletions.DeletedWorkInfo(ctx, id)
		if err != nil {
			return err
		}
		if gone != nil {
			return problem.Deleted("work", gone.Reason, rfc3339(gone.DeletedAt), "", "",
				"work "+repr.ID(id)+" was deleted by deletion request "+repr.ID(gone.RequestID)+".")
		}
	}
	return c.mergedOrNotFound(ctx, model.EntityTypeWork, "work", id)
}
