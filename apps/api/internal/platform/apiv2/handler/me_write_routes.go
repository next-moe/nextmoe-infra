package handler

import (
	"context"
	"net/http"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	catsvc "api/internal/platform/catalog/service"

	"github.com/danielgtaylor/huma/v2"
)

func registerMeWrite(api huma.API, cat *Catalog) {
	me := []string{"me"}
	mod := []string{"moderation"}
	errs := collectionErrors(http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusServiceUnavailable)
	writeErrs := append(errs, http.StatusUnprocessableEntity, http.StatusConflict, http.StatusPreconditionRequired, http.StatusPreconditionFailed)

	huma.Register(api, huma.Operation{
		OperationID: "batchMyPlaytimes", Method: http.MethodPost, Path: "/v2/me/playtimes",
		Summary: "Batch write playtimes", Description: "207 Multi-Status. Each item is a playtime or a problem. Requires a user access token. Any app may call this; playtime:write is not required.",
		Tags: me, Errors: writeErrs, DefaultStatus: 207, SkipValidateParams: true,
	}, batchMyPlaytimes(cat))
	huma.Register(api, huma.Operation{
		OperationID: "batchMyWorkStates", Method: http.MethodPost, Path: "/v2/me/work-states",
		Summary: "Batch write work states", Description: "207 Multi-Status. Each item is a work_state or a problem. Requires a user access token. Any app may call this; no scope is required.",
		Tags: me, Errors: writeErrs, DefaultStatus: 207, SkipValidateParams: true,
	}, batchMyWorkStates(cat))
	huma.Register(api, huma.Operation{
		OperationID: "createMyClaim", Method: http.MethodPost, Path: "/v2/me/claims",
		Summary:     "Submit a claim",
		Description: "Mint or claim a work. work_id claims an existing catalog work. refs= claims the work they resolve to, or mints one from display_name when none match. site_work_id with display_name and neither work_id nor refs mints a work anchored to the site's own id. field_values carries an editing-engine work field map onto any mint lane and may be sent alone, without work_id, refs or site_work_id; it is refused with work_id, and refs that already resolve to a work answer 409 instead of dropping it. released rides the same mint lanes under the same two refusals and becomes one curated catalog_release row on the minted work. A caller holding catalog.claim.trusted mints straight to live rather than pending. A mint whose display_name or catalog.work.titles match live works of the same medium is refused with 409 DUPLICATE_SUSPECTS naming them in suspects[] and nothing is written; re-send with confirm_duplicates=true to mint anyway. The claiming lanes — work_id, and refs that resolve — never hit this gate. Requires a user access token bound to a catalog site.",
		Tags:        me, Errors: writeErrs, DefaultStatus: http.StatusCreated, SkipValidateParams: true,
	}, createMyClaim(cat))
	huma.Register(api, huma.Operation{
		OperationID: "deleteMyClaim", Method: http.MethodDelete, Path: "/v2/me/claims/{id}",
		Summary:     "Delete a draft claim",
		Description: "Deletes a draft the caller owns; a live or pending claim must be withdrawn to draft first (PATCH state=withdrawn). This soft-deletes the catalog work row and writes no claim event. 204 with no body. Requires a user access token.",
		Tags:        me, Errors: writeErrs, DefaultStatus: http.StatusNoContent, SkipValidateParams: true,
	}, deleteMyClaim(cat))
	huma.Register(api, huma.Operation{
		OperationID: "getMyClaim", Method: http.MethodGet, Path: "/v2/me/claims/{id}",
		Summary: "Get one of my claims", Description: "id is the catalog work id. Requires a user access token.",
		Tags: me, Errors: errs, SkipValidateParams: true,
	}, getMyClaim(cat))
	huma.Register(api, huma.Operation{
		OperationID: "patchMyClaim", Method: http.MethodPatch, Path: "/v2/me/claims/{id}",
		Summary:     "Move a claim the caller owns",
		Description: "PATCH {state: live|pending|withdrawn}. live publishes a draft without review, pending submits it for review, withdrawn returns it to draft. The owner may act, and an unowned claim is adopted by its first claimant. If-Match required. Requires a user access token bound to a catalog site.",
		Tags:        me, Errors: writeErrs, SkipValidateParams: true,
	}, patchMyClaim(cat))
	huma.Register(api, huma.Operation{
		OperationID: "uploadMyEditImage", Method: http.MethodPost, Path: "/v2/me/edit-images",
		Summary:     "Upload an image for an edit proposal",
		Description: "multipart/form-data with preset and file. Returns the hash an edit proposal carries in a cover or screenshot row. Requires a user access token bound to a catalog site.",
		Tags:        me, Errors: writeErrs, DefaultStatus: http.StatusCreated, SkipValidateParams: true,
	}, uploadMyEditImage(cat))
	huma.Register(api, huma.Operation{
		OperationID: "listMyProposals", Method: http.MethodGet, Path: "/v2/me/proposals",
		Summary:     "List my proposals",
		Description: "The bearer's own proposals. state= is a closed vocabulary and an unknown value is 400. object= or entity_type= narrows to one family, entity_id= to one entity — on this lane entity_id= is accepted without a family because every row already belongs to the caller. Requires a user access token.",
		Tags:        me, Errors: errs, SkipValidateParams: true,
	}, listMyProposals(cat))
	huma.Register(api, huma.Operation{
		OperationID: "createMyProposal", Method: http.MethodPost, Path: "/v2/me/proposals",
		Summary: "File a proposal", Description: "Requires a user access token bound to a catalog site.",
		Tags: me, Errors: writeErrs, DefaultStatus: http.StatusCreated, SkipValidateParams: true,
	}, createMyProposal(cat))
	huma.Register(api, huma.Operation{
		OperationID: "getMyProposal", Method: http.MethodGet, Path: "/v2/me/proposals/{id}",
		Summary: "Get one of my proposals", Description: "Requires a user access token.",
		Tags: me, Errors: errs, SkipValidateParams: true,
	}, getMyProposal(cat))
	huma.Register(api, huma.Operation{
		OperationID: "patchMyProposal", Method: http.MethodPatch, Path: "/v2/me/proposals/{id}",
		Summary: "Amend or withdraw a proposal", Description: "If-Match required. Requires a user access token.",
		Tags: me, Errors: writeErrs, SkipValidateParams: true,
	}, patchMyProposal(cat))
	huma.Register(api, huma.Operation{
		OperationID: "amendMyProposal", Method: http.MethodPost, Path: "/v2/me/proposals/{id}/amendments",
		Summary: "Append an amendment", Description: "If-Match required. Requires a user access token.",
		Tags: me, Errors: writeErrs, DefaultStatus: http.StatusCreated, SkipValidateParams: true,
	}, amendMyProposal(cat))
	huma.Register(api, huma.Operation{
		OperationID: "getModerationClaim", Method: http.MethodGet, Path: "/v2/moderation/claims/{id}",
		Summary: "Get one moderation claim", Description: "id is the catalog work id. Site-fenced. Requires a user access token bound to a catalog site.",
		Tags: mod, Errors: errs, SkipValidateParams: true,
	}, getModerationClaim(cat))
	huma.Register(api, huma.Operation{
		OperationID: "decideModerationClaim", Method: http.MethodPost, Path: "/v2/moderation/claims/{id}/decisions",
		Summary:     "Decide a claim",
		Description: "decision=approve|decline|ban|unban. unban restores the state the claim was hidden from. If-Match required, and the ETag comes from GET /v2/moderation/claims/{id}. Requires the catalog.claim.review permission.",
		Tags:        mod, Errors: writeErrs, DefaultStatus: http.StatusCreated, SkipValidateParams: true,
	}, decideModerationClaim(cat))
	huma.Register(api, huma.Operation{
		OperationID: "listModerationProposals", Method: http.MethodGet, Path: "/v2/moderation/proposals",
		Summary:     "Moderation proposal queue",
		Description: "Open proposals on the token site. The whole queue requires a catalog review permission. object= (or entity_type=) with entity_id= narrows it to one entity, which that entity's owner may read without one — the same owner-review channel the editing engine resolves per field. entity_id= without a family is 422.",
		Tags:        mod, Errors: errs, SkipValidateParams: true,
	}, listModerationProposals(cat))
	huma.Register(api, huma.Operation{
		OperationID: "getModerationProposal", Method: http.MethodGet, Path: "/v2/moderation/proposals/{id}",
		Summary:     "Get one moderation proposal",
		Description: "Site-fenced. include=patch adds the proposed and effective patches a decision is taken on. The ETag is the validator POST /v2/moderation/proposals/{id}/decisions takes as If-Match.",
		Tags:        mod, Errors: errs, SkipValidateParams: true,
	}, getModerationProposal(cat))
	huma.Register(api, huma.Operation{
		OperationID: "decideModerationProposal", Method: http.MethodPost, Path: "/v2/moderation/proposals/{id}/decisions",
		Summary: "Decide a proposal", Description: "decision=merge|decline. If-Match required.",
		Tags: mod, Errors: writeErrs, DefaultStatus: http.StatusCreated, SkipValidateParams: true,
	}, decideModerationProposal(cat))
	huma.Register(api, huma.Operation{
		OperationID: "revertModeration", Method: http.MethodPost, Path: "/v2/moderation/reverts",
		Summary: "Revert to a revision", Description: "Body names revision_id. Requires a user access token bound to a catalog site.",
		Tags: mod, Errors: writeErrs, DefaultStatus: http.StatusCreated, SkipValidateParams: true,
	}, revertModeration(cat))
	huma.Register(api, huma.Operation{
		OperationID: "getModerationSnapshot", Method: http.MethodGet, Path: "/v2/moderation/snapshots/{object}/{id}",
		Summary: "Current edit snapshot", Description: "Registered field values. Requires a user access token.",
		Tags: mod, Errors: errs, SkipValidateParams: true,
	}, getModerationSnapshot(cat))
}

type batchPlaytimesInput struct {
	Body struct {
		Items []struct {
			WorkID  string `json:"work_id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Catalog work id."`
			Minutes int    `json:"minutes" minimum:"0" maximum:"60000" doc:"Absolute cumulative minutes."`
		} `json:"items" maxItems:"100" doc:"At most 100 items."`
	}
}
type batchPlaytimesOutput struct {
	Status int
	Body   repr.List[repr.PlaytimeBatchItem]
}

// Named, not anonymous — a second anonymous `Items []struct{…}` on /v2/me
// panics huma's schema registry at startup (deviation 110, "duplicate name:
// Item"); folderBatchEntry exists for the same reason.
type workStateBatchEntry struct {
	WorkID     string `json:"work_id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Catalog work id."`
	State      string `json:"state" enum:"wish,doing,done,on_hold,dropped" doc:"Closed vocabulary."`
	Completion string `json:"completion,omitempty" enum:"one_route,main,all" doc:"How much is finished. Absent clears it. Refused with wish."`
}
type batchWorkStatesInput struct {
	Body struct {
		Items []workStateBatchEntry `json:"items" maxItems:"100" doc:"At most 100 items."`
	}
}
type batchWorkStatesOutput struct {
	Status int
	Body   repr.List[repr.WorkStateBatchItem]
}
type claimRefBody struct {
	Source     string `json:"source" minLength:"1" maxLength:"64" doc:"Open vocabulary source key such as vndb. Must not be used as a discriminant."`
	ExternalID string `json:"external_id" minLength:"1" maxLength:"256" doc:"Verbatim upstream id. Must not be used as a discriminant beyond exact match."`
}

type claimReleasedBody struct {
	Y int16 `json:"y" minimum:"1970" maximum:"2200" doc:"Release year."`
	M int16 `json:"m,omitempty" minimum:"0" maximum:"12" doc:"Release month; 0 or absent means unknown."`
	D int16 `json:"d,omitempty" minimum:"0" maximum:"31" doc:"Release day; 0 or absent means unknown. Requires m."`
}

type createClaimInput struct {
	Body struct {
		SiteWorkID        string             `json:"site_work_id,omitempty" pattern:"^[0-9]+$" maxLength:"20" doc:"The site's own work id. Sent alone with display_name — no work_id, no refs — it mints a work anchored to that id."`
		WorkID            string             `json:"work_id,omitempty" pattern:"^[0-9]+$" maxLength:"20" doc:"Existing catalog work id to claim."`
		DisplayName       string             `json:"display_name,omitempty" maxLength:"512" doc:"Required to mint when refs do not match. Must not be used as a discriminant."`
		Refs              []claimRefBody     `json:"refs,omitempty" maxItems:"100" doc:"source:external_id anchors, at most 100. Used when work_id is absent."`
		FieldValues       map[string]any     `json:"field_values,omitempty" doc:"Editing-engine field key to value for the minted work, e.g. catalog.work.titles — the same shape GET /v2/moderation/snapshots/{object}/{id} answers. Sent alone it mints from the map; sent with work_id it is 422. Top-level display_name wins over catalog.work.display_name."`
		Released          *claimReleasedBody `json:"released,omitempty" doc:"Release date for the minted work, written as ONE curated catalog_release row — a freshly minted work has no release row for a catalog.release proposal to edit. Omit for TBA. Only mint lanes read it: with work_id it is 422, and refs that resolve to an existing work answer 409 rather than dropping it."`
		ConfirmDuplicates bool               `json:"confirm_duplicates,omitempty" doc:"Mint even when live works of the same medium share a submitted title. Without it such a mint is refused with 409 DUPLICATE_SUSPECTS and the works in suspects[]; nothing is written. Confirming still files the pairs for reconciliation. Only mint lanes read this."`
	}
}
type getClaimInput struct {
	ID string `path:"id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Catalog work id."`
}
type patchClaimInput struct {
	ID      string `path:"id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Catalog work id."`
	IfMatch string `header:"If-Match" required:"true" doc:"Current ETag. Required; its absence is 428 PRECONDITION_REQUIRED."`
	Body    struct {
		State string `json:"state" enum:"live,pending,withdrawn,draft" doc:"live publishes a draft, pending submits it for review, withdrawn returns a live or pending claim to draft. draft is the older spelling of withdrawn."`
	}
}
type getClaimOutput struct {
	ETag string `header:"ETag" doc:"Opaque validator. Send it back as If-Match to move this claim."`
	Body repr.ClaimRecord
}
type listProposalsInput struct {
	CollectionInput
	State      string `query:"state" maxLength:"16" doc:"Closed: open, pending, merged, declined, withdrawn. Unknown value is 400 UNKNOWN_ENUM_VALUE."`
	Object     string `query:"object" maxLength:"32" doc:"Closed family filter: work, company, character, release, tag, engine, series."`
	EntityType string `query:"entity_type" maxLength:"64" doc:"Editing-engine type, e.g. catalog.work. The same spelling POST /v2/me/proposals takes in its body. Names the same filter as object=; sending both with different families is 400."`
	EntityID   string `query:"entity_id" maxLength:"20" doc:"Catalog id of one entity. Accepted alone on this lane, which is already fenced to the bearer's own proposals; pair it with object= or entity_type= when ids collide across families."`
}
type listModerationProposalsInput struct {
	CollectionInput
	Object     string `query:"object" maxLength:"32" doc:"Closed family filter: work, company, character, release, tag, engine, series."`
	EntityType string `query:"entity_type" maxLength:"64" doc:"Editing-engine type, e.g. catalog.work. Names the same filter as object=; sending both with different families is 400."`
	EntityID   string `query:"entity_id" maxLength:"20" doc:"Catalog id of one entity. Requires object= or entity_type=. Narrows the queue to that entity, which its owner may read without a review permission."`
}
type listProposalsOutput struct {
	Body repr.List[repr.ProposalRecord]
}
type createProposalInput struct {
	Body struct {
		EntityType string         `json:"entity_type" maxLength:"64" doc:"Editing-engine type, e.g. catalog.work. Must not be used as a discriminant."`
		EntityID   string         `json:"entity_id" pattern:"^[0-9]+$" maxLength:"20" doc:"Target catalog id."`
		Patch      map[string]any `json:"patch" doc:"Field-key to new value."`
		Note       string         `json:"note,omitempty" maxLength:"2000" doc:"Must not be used as a discriminant."`
	}
}
type getProposalInput struct {
	ID      string `path:"id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Proposal id."`
	Include string `query:"include" maxLength:"1024" doc:"Comma-separated blocks: amendments, patch. Unknown token is 400 UNKNOWN_INCLUDE."`
	View    string `query:"view" maxLength:"16" doc:"basic (default) or full. full adds amendments and patch."`
}
type getProposalOutput struct {
	ETag string `header:"ETag" doc:"Opaque validator. Send it back as If-Match to amend, withdraw, or decide this proposal."`
	Body repr.ProposalRecord
}
type patchProposalInput struct {
	ID      string `path:"id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Proposal id."`
	IfMatch string `header:"If-Match" required:"true" doc:"Current ETag. Required; its absence is 428 PRECONDITION_REQUIRED."`
	Body    struct {
		State string         `json:"state,omitempty" enum:"withdrawn" doc:"Set withdrawn to withdraw."`
		Patch map[string]any `json:"patch,omitempty" doc:"Field-key to new value to amend."`
	}
}
type amendProposalInput struct {
	ID      string `path:"id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Proposal id."`
	IfMatch string `header:"If-Match" required:"true" doc:"Current ETag. Required; its absence is 428 PRECONDITION_REQUIRED."`
	Body    struct {
		Set   map[string]any `json:"set,omitempty" doc:"Field-key to corrected value."`
		Unset []string       `json:"unset,omitempty" maxItems:"100" doc:"Field keys to drop, at most 100."`
		Note  string         `json:"note,omitempty" maxLength:"2000" doc:"Must not be used as a discriminant."`
	}
}
type decideClaimInput struct {
	ID      string `path:"id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Catalog work id."`
	IfMatch string `header:"If-Match" required:"true" doc:"Current ETag. Required; its absence is 428 PRECONDITION_REQUIRED."`
	Body    struct {
		Decision string `json:"decision" enum:"approve,decline,ban,unban" doc:"approve publishes a pending claim, decline sends it back, ban hides it from any state, unban restores the state it was hidden from."`
		Note     string `json:"note,omitempty" maxLength:"2000" doc:"Must not be used as a discriminant. Required to decline."`
	}
}
type decideProposalInput struct {
	ID      string `path:"id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Proposal id."`
	IfMatch string `header:"If-Match" required:"true" doc:"Current ETag. Required; its absence is 428 PRECONDITION_REQUIRED."`
	Body    struct {
		Decision string `json:"decision" enum:"merge,decline" doc:"merge writes the effective patch and records a revision. decline closes the proposal."`
		Note     string `json:"note,omitempty" maxLength:"2000" doc:"Must not be used as a discriminant."`
	}
}
type claimDecisionOutput struct{ Body repr.ClaimDecisionRecord }
type proposalDecisionOutput struct{ Body repr.ProposalDecisionRecord }
type revertInput struct {
	Body struct {
		RevisionID string `json:"revision_id" pattern:"^[0-9]+$" maxLength:"20" doc:"edit_revision id."`
		Reason     string `json:"reason,omitempty" maxLength:"2000" doc:"Must not be used as a discriminant."`
	}
}
type snapshotInput struct {
	Object string `path:"object" maxLength:"32" doc:"Family: work, company, character, release, tag, engine, series."`
	ID     string `path:"id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Catalog id."`
}
type snapshotOutput struct{ Body repr.SnapshotRecord }

func batchMyPlaytimes(cat *Catalog) func(context.Context, *batchPlaytimesInput) (*batchPlaytimesOutput, error) {
	return func(ctx context.Context, in *batchPlaytimesInput) (*batchPlaytimesOutput, error) {
		if in == nil {
			in = &batchPlaytimesInput{}
		}
		items := make([]struct {
			WorkID  string
			Minutes int
		}, 0, len(in.Body.Items))
		for _, it := range in.Body.Items {
			items = append(items, struct {
				WorkID  string
				Minutes int
			}{it.WorkID, it.Minutes})
		}
		page, err := cat.BatchPlaytimes(ctx, items)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &batchPlaytimesOutput{Status: 207, Body: page}, nil
	}
}

func batchMyWorkStates(cat *Catalog) func(context.Context, *batchWorkStatesInput) (*batchWorkStatesOutput, error) {
	return func(ctx context.Context, in *batchWorkStatesInput) (*batchWorkStatesOutput, error) {
		if in == nil {
			in = &batchWorkStatesInput{}
		}
		page, err := cat.BatchWorkStates(ctx, in.Body.Items)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &batchWorkStatesOutput{Status: 207, Body: page}, nil
	}
}

func createMyClaim(cat *Catalog) func(context.Context, *createClaimInput) (*getClaimOutput, error) {
	return func(ctx context.Context, in *createClaimInput) (*getClaimOutput, error) {
		if in == nil {
			in = &createClaimInput{}
		}
		refs := make([]repr.Ref, 0, len(in.Body.Refs))
		for _, r := range in.Body.Refs {
			refs = append(refs, repr.Ref{Source: r.Source, ExternalID: r.ExternalID})
		}
		var released catsvc.ReleaseDate
		if r := in.Body.Released; r != nil {
			released = catsvc.ReleaseDate{Y: r.Y, M: r.M, D: r.D}
		}
		rec, err := cat.CreateClaim(ctx, in.Body.WorkID, in.Body.SiteWorkID, in.Body.DisplayName, refs, in.Body.FieldValues, released, in.Body.ConfirmDuplicates)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &getClaimOutput{ETag: claimETag(rec), Body: rec}, nil
	}
}

func getMyClaim(cat *Catalog) func(context.Context, *getClaimInput) (*getClaimOutput, error) {
	return func(ctx context.Context, in *getClaimInput) (*getClaimOutput, error) {
		if in == nil {
			in = &getClaimInput{}
		}
		id, ok := repr.ParseID(in.ID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.ID))
		}
		rec, err := cat.GetMyClaim(ctx, id)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &getClaimOutput{ETag: claimETag(rec), Body: rec}, nil
	}
}

func deleteMyClaim(cat *Catalog) func(context.Context, *getClaimInput) (*struct{}, error) {
	return func(ctx context.Context, in *getClaimInput) (*struct{}, error) {
		if in == nil {
			in = &getClaimInput{}
		}
		id, ok := repr.ParseID(in.ID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.ID))
		}
		if err := cat.DeleteClaim(ctx, id); err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &struct{}{}, nil
	}
}

func patchMyClaim(cat *Catalog) func(context.Context, *patchClaimInput) (*getClaimOutput, error) {
	return func(ctx context.Context, in *patchClaimInput) (*getClaimOutput, error) {
		if in == nil {
			in = &patchClaimInput{}
		}
		id, ok := repr.ParseID(in.ID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.ID))
		}
		rec, err := cat.PatchClaim(ctx, id, in.Body.State, in.IfMatch)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &getClaimOutput{ETag: claimETag(rec), Body: rec}, nil
	}
}

func listMyProposals(cat *Catalog) func(context.Context, *listProposalsInput) (*listProposalsOutput, error) {
	return func(ctx context.Context, in *listProposalsInput) (*listProposalsOutput, error) {
		if in == nil {
			in = &listProposalsInput{}
		}
		q, err := parseCatalogList(ctx, &in.CollectionInput, collect.ProposalListSpec())
		if err != nil {
			return nil, err
		}
		page, lerr := cat.ListMyProposals(ctx, q, proposalFilter{
			Object: in.Object, EntityType: in.EntityType, EntityID: in.EntityID, State: in.State,
		})
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listProposalsOutput{Body: page}, nil
	}
}

func createMyProposal(cat *Catalog) func(context.Context, *createProposalInput) (*getProposalOutput, error) {
	return func(ctx context.Context, in *createProposalInput) (*getProposalOutput, error) {
		if in == nil {
			in = &createProposalInput{}
		}
		rec, etag, err := cat.CreateProposal(ctx, in.Body.EntityType, in.Body.EntityID, in.Body.Patch, in.Body.Note)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &getProposalOutput{ETag: etag, Body: rec}, nil
	}
}

func proposalInclude(ctx context.Context, view, include string) ([]string, error) {
	q, perr := collect.Parse(collect.Raw{View: view, Include: include}, collect.ProposalSpec())
	if perr != nil {
		return nil, withIdent(ctx, perr)
	}
	return q.Include, nil
}

func getMyProposal(cat *Catalog) func(context.Context, *getProposalInput) (*getProposalOutput, error) {
	return func(ctx context.Context, in *getProposalInput) (*getProposalOutput, error) {
		if in == nil {
			in = &getProposalInput{}
		}
		id, ok := repr.ParseID(in.ID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.ID))
		}
		include, ierr := proposalInclude(ctx, in.View, in.Include)
		if ierr != nil {
			return nil, ierr
		}
		rec, etag, err := cat.GetMyProposal(ctx, id, include)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &getProposalOutput{ETag: etag, Body: rec}, nil
	}
}

func getModerationProposal(cat *Catalog) func(context.Context, *getProposalInput) (*getProposalOutput, error) {
	return func(ctx context.Context, in *getProposalInput) (*getProposalOutput, error) {
		if in == nil {
			in = &getProposalInput{}
		}
		id, ok := repr.ParseID(in.ID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.ID))
		}
		include, ierr := proposalInclude(ctx, in.View, in.Include)
		if ierr != nil {
			return nil, ierr
		}
		rec, etag, err := cat.GetModerationProposal(ctx, id, include)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &getProposalOutput{ETag: etag, Body: rec}, nil
	}
}

func patchMyProposal(cat *Catalog) func(context.Context, *patchProposalInput) (*getProposalOutput, error) {
	return func(ctx context.Context, in *patchProposalInput) (*getProposalOutput, error) {
		if in == nil {
			in = &patchProposalInput{}
		}
		id, ok := repr.ParseID(in.ID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.ID))
		}
		rec, etag, err := cat.PatchProposal(ctx, id, in.Body.State, in.Body.Patch, in.IfMatch)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &getProposalOutput{ETag: etag, Body: rec}, nil
	}
}

func amendMyProposal(cat *Catalog) func(context.Context, *amendProposalInput) (*getProposalOutput, error) {
	return func(ctx context.Context, in *amendProposalInput) (*getProposalOutput, error) {
		if in == nil {
			in = &amendProposalInput{}
		}
		id, ok := repr.ParseID(in.ID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.ID))
		}
		rec, etag, err := cat.AmendProposal(ctx, id, in.Body.Set, in.Body.Unset, in.Body.Note, in.IfMatch)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &getProposalOutput{ETag: etag, Body: rec}, nil
	}
}

func getModerationClaim(cat *Catalog) func(context.Context, *getClaimInput) (*getClaimOutput, error) {
	return func(ctx context.Context, in *getClaimInput) (*getClaimOutput, error) {
		if in == nil {
			in = &getClaimInput{}
		}
		id, ok := repr.ParseID(in.ID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.ID))
		}
		rec, err := cat.GetModerationClaim(ctx, id)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &getClaimOutput{ETag: claimETag(rec), Body: rec}, nil
	}
}

func decideModerationClaim(cat *Catalog) func(context.Context, *decideClaimInput) (*claimDecisionOutput, error) {
	return func(ctx context.Context, in *decideClaimInput) (*claimDecisionOutput, error) {
		if in == nil {
			in = &decideClaimInput{}
		}
		id, ok := repr.ParseID(in.ID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.ID))
		}
		rec, err := cat.DecideClaim(ctx, id, in.Body.Decision, in.Body.Note, in.IfMatch)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &claimDecisionOutput{Body: rec}, nil
	}
}

func listModerationProposals(cat *Catalog) func(context.Context, *listModerationProposalsInput) (*listProposalsOutput, error) {
	return func(ctx context.Context, in *listModerationProposalsInput) (*listProposalsOutput, error) {
		if in == nil {
			in = &listModerationProposalsInput{}
		}
		q, err := parseCatalogList(ctx, &in.CollectionInput, collect.ProposalListSpec())
		if err != nil {
			return nil, err
		}
		page, lerr := cat.ListModerationProposals(ctx, q, in.Object, in.EntityType, in.EntityID)
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listProposalsOutput{Body: page}, nil
	}
}

func decideModerationProposal(cat *Catalog) func(context.Context, *decideProposalInput) (*proposalDecisionOutput, error) {
	return func(ctx context.Context, in *decideProposalInput) (*proposalDecisionOutput, error) {
		if in == nil {
			in = &decideProposalInput{}
		}
		id, ok := repr.ParseID(in.ID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.ID))
		}
		rec, err := cat.DecideProposal(ctx, id, in.Body.Decision, in.Body.Note, in.IfMatch)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &proposalDecisionOutput{Body: rec}, nil
	}
}

func revertModeration(cat *Catalog) func(context.Context, *revertInput) (*getProposalOutput, error) {
	return func(ctx context.Context, in *revertInput) (*getProposalOutput, error) {
		if in == nil {
			in = &revertInput{}
		}
		rec, etag, err := cat.RevertRevision(ctx, in.Body.RevisionID, in.Body.Reason)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &getProposalOutput{ETag: etag, Body: rec}, nil
	}
}

type editImageForm struct {
	Preset string `form:"preset" required:"true" enum:"cover,screenshot" doc:"Which editor slot these bytes are for."`
	// Not an image/* contentType list: mime.Writer's CreateFormFile stamps every
	// part application/octet-stream, so a narrower list rejects the exact client
	// this face exists for. The image service validates the bytes it is given.
	File huma.FormFile `form:"file" required:"true" contentType:"application/octet-stream" doc:"The image bytes. The ceiling is the service's own body limit, not a field constraint."`
}

type uploadEditImageInput struct {
	RawBody huma.MultipartFormFiles[editImageForm]
}

type uploadEditImageOutput struct {
	Location string `header:"Location" doc:"Absolute URL of the stored image."`
	Body     repr.EditImage
}

func uploadMyEditImage(cat *Catalog) func(context.Context, *uploadEditImageInput) (*uploadEditImageOutput, error) {
	return func(ctx context.Context, in *uploadEditImageInput) (*uploadEditImageOutput, error) {
		if in == nil {
			in = &uploadEditImageInput{}
		}
		form := in.RawBody.Data()
		if form == nil || !form.File.IsSet {
			p := problem.New(problem.CodeValidationFailed, "", "", "a multipart file part named file is required.")
			p.Errors = []problem.FieldError{{Pointer: "/file", Reason: problem.ReasonRequired,
				Detail: "send multipart/form-data with a file part"}}
			return nil, catalogErr(ctx, p)
		}
		defer form.File.Close()
		rec, err := cat.UploadEditImage(ctx, form.Preset, form.File.Filename, form.File)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &uploadEditImageOutput{Location: rec.URL, Body: rec}, nil
	}
}

func getModerationSnapshot(cat *Catalog) func(context.Context, *snapshotInput) (*snapshotOutput, error) {
	return func(ctx context.Context, in *snapshotInput) (*snapshotOutput, error) {
		if in == nil {
			in = &snapshotInput{}
		}
		rec, err := cat.GetSnapshot(ctx, in.Object, in.ID)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &snapshotOutput{Body: rec}, nil
	}
}
