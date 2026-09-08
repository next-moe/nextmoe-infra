package handler

import (
	"context"
	"net/http"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"

	"github.com/danielgtaylor/huma/v2"
)

type listPlaytimesInput struct {
	CollectionInput
	WorkIDs string `query:"work_ids" maxLength:"4096" doc:"Comma-separated work ids, max 100. Batch read, no pagination."`
}
type listPlaytimesOutput struct {
	Body repr.List[repr.UserPlaytime]
}
type getPlaytimeInput struct {
	WorkID string `path:"work_id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Catalog work id."`
}
type getPlaytimeOutput struct {
	Body repr.UserPlaytime
}
type putPlaytimeInput struct {
	WorkID string `path:"work_id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Catalog work id."`
	Body   struct {
		Minutes int `json:"minutes" minimum:"0" maximum:"60000" doc:"Absolute cumulative minutes."`
	}
}
type deletePlaytimeInput struct {
	WorkID string `path:"work_id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Catalog work id."`
}
type listWorkStatesInput struct {
	CollectionInput
	WorkIDs string `query:"work_ids" maxLength:"4096" doc:"Comma-separated work ids, max 100. Batch read, no pagination."`
}
type listWorkStatesOutput struct {
	Body repr.List[repr.UserWorkState]
}
type getWorkStateInput struct {
	WorkID string `path:"work_id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Catalog work id."`
}
type getWorkStateOutput struct {
	Body repr.UserWorkState
}
type putWorkStateInput struct {
	WorkID string `path:"work_id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Catalog work id."`
	Body   struct {
		State      string `json:"state" enum:"wish,doing,done,on_hold,dropped" doc:"Closed vocabulary."`
		Completion string `json:"completion,omitempty" enum:"one_route,main,all" doc:"How much is finished. Absent clears it. Refused with wish."`
	}
}
type deleteWorkStateInput struct {
	WorkID string `path:"work_id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Catalog work id."`
}
type listCoverVotesOutput struct {
	Body repr.List[repr.CoverVote]
}
type putCoverVoteInput struct {
	CoverID string `path:"cover_id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Catalog cover row id."`
	Body    struct {
		Vote string `json:"vote" enum:"up" doc:"Only up is stored."`
	}
}
type deleteCoverVoteInput struct {
	CoverID string `path:"cover_id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Catalog cover row id."`
}
type getCoverVoteOutput struct {
	Body repr.CoverVote
}

func registerMe(api huma.API, cat *Catalog) {
	me := []string{"me"}
	errs := collectionErrors(http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusServiceUnavailable)
	huma.Register(api, huma.Operation{
		OperationID: "listMyPlaytimes", Method: http.MethodGet, Path: "/v2/me/playtimes",
		Summary: "List my playtimes", Description: "The bearer user's playtime rows. work_ids= is a batch read. Requires a user access token. Any app may call this; playtime:read is not required.",
		Tags: me, Errors: errs, SkipValidateParams: true,
	}, listMyPlaytimes(cat))
	huma.Register(api, huma.Operation{
		OperationID: "getMyPlaytime", Method: http.MethodGet, Path: "/v2/me/playtimes/{work_id}",
		Summary: "Get my playtime on one work", Description: "404 when the user has never reported. Requires a user access token. Any app may call this; playtime:read is not required.",
		Tags: me, Errors: errs, SkipValidateParams: true,
	}, getMyPlaytime(cat))
	huma.Register(api, huma.Operation{
		OperationID: "putMyPlaytime", Method: http.MethodPut, Path: "/v2/me/playtimes/{work_id}",
		Summary: "Replace my playtime on one work", Description: "Absolute minutes. Naturally idempotent. Requires a user access token. Any app may call this; playtime:write is not required.",
		Tags: me, Errors: errs, SkipValidateParams: true,
	}, putMyPlaytime(cat))
	huma.Register(api, huma.Operation{
		OperationID: "deleteMyPlaytime", Method: http.MethodDelete, Path: "/v2/me/playtimes/{work_id}",
		Summary: "Delete my playtime on one work", Description: "204 with no body. Requires a user access token. Any app may call this; playtime:write is not required.",
		Tags: me, Errors: errs, DefaultStatus: http.StatusNoContent, SkipValidateParams: true,
	}, deleteMyPlaytime(cat))
	huma.Register(api, huma.Operation{
		OperationID: "listMyWorkStates", Method: http.MethodGet, Path: "/v2/me/work-states",
		Summary: "List my work states", Description: "The bearer user's per-work states. work_ids= is a batch read. Requires a user access token. Any app may call this; no scope is required.",
		Tags: me, Errors: errs, SkipValidateParams: true,
	}, listMyWorkStates(cat))
	huma.Register(api, huma.Operation{
		OperationID: "getMyWorkState", Method: http.MethodGet, Path: "/v2/me/work-states/{work_id}",
		Summary: "Get my state on one work", Description: "404 when the user has never set one. Requires a user access token. Any app may call this; no scope is required.",
		Tags: me, Errors: errs, SkipValidateParams: true,
	}, getMyWorkState(cat))
	huma.Register(api, huma.Operation{
		OperationID: "putMyWorkState", Method: http.MethodPut, Path: "/v2/me/work-states/{work_id}",
		Summary: "Replace my state on one work", Description: "Body carries state and optionally completion; an absent completion clears it. Naturally idempotent. Requires a user access token. Any app may call this; no scope is required.",
		Tags: me, Errors: errs, SkipValidateParams: true,
	}, putMyWorkState(cat))
	huma.Register(api, huma.Operation{
		OperationID: "deleteMyWorkState", Method: http.MethodDelete, Path: "/v2/me/work-states/{work_id}",
		Summary: "Delete my state on one work", Description: "204 with no body. Requires a user access token. Any app may call this; no scope is required.",
		Tags: me, Errors: errs, DefaultStatus: http.StatusNoContent, SkipValidateParams: true,
	}, deleteMyWorkState(cat))
	huma.Register(api, huma.Operation{
		OperationID: "listMyCoverVotes", Method: http.MethodGet, Path: "/v2/me/cover-votes",
		Summary: "List my cover votes", Description: "Every cover the bearer has voted up. Requires a user access token.",
		Tags: me, Errors: errs, SkipValidateParams: true,
	}, listMyCoverVotes(cat))
	huma.Register(api, huma.Operation{
		OperationID: "putMyCoverVote", Method: http.MethodPut, Path: "/v2/me/cover-votes/{cover_id}",
		Summary: "Cast a cover vote", Description: "Only vote=up is stored. One ballot per work. Requires a user access token.",
		Tags: me, Errors: errs, SkipValidateParams: true,
	}, putMyCoverVote(cat))
	huma.Register(api, huma.Operation{
		OperationID: "listMyClaims", Method: http.MethodGet, Path: "/v2/me/claims",
		Summary:     "List my claims",
		Description: "Claims the bearer acted on. kind=submitted (the default) keeps the ones the bearer owns, kind=audited the ones the bearer only reviewed, kind=all everything they touched. claim_state= and site= narrow further, and site= also scopes first_acted_at/acted_count. Requires a user access token.",
		Tags:        me, Errors: errs, SkipValidateParams: true,
	}, listMyClaims(cat))
	huma.Register(api, huma.Operation{
		OperationID: "listModerationClaims", Method: http.MethodGet, Path: "/v2/moderation/claims",
		Summary:     "Moderation claim queue",
		Description: "Claims on the token site awaiting a decision. claim_state= selects which states the queue lists and defaults to pending; the decision face also acts on live, draft and declined (ban) and on hidden (unban), so those are listable here too. Oldest submission first. ids= and refs= are not accepted. Requires a user access token with review authority.",
		Tags:        []string{"moderation"}, Errors: errs, SkipValidateParams: true,
	}, listModerationClaims(cat))
	huma.Register(api, huma.Operation{
		OperationID: "deleteMyCoverVote", Method: http.MethodDelete, Path: "/v2/me/cover-votes/{cover_id}",
		Summary: "Withdraw a cover vote", Description: "204 with no body. Requires a user access token.",
		Tags: me, Errors: errs, DefaultStatus: http.StatusNoContent, SkipValidateParams: true,
	}, deleteMyCoverVote(cat))
}

func listMyPlaytimes(cat *Catalog) func(context.Context, *listPlaytimesInput) (*listPlaytimesOutput, error) {
	return func(ctx context.Context, in *listPlaytimesInput) (*listPlaytimesOutput, error) {
		if in == nil {
			in = &listPlaytimesInput{}
		}
		q, err := parseCatalogList(ctx, &in.CollectionInput, collect.PlaytimeSpec())
		if err != nil {
			return nil, err
		}
		page, lerr := cat.ListPlaytimes(ctx, q, splitWorkIDs(in.WorkIDs))
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listPlaytimesOutput{Body: page}, nil
	}
}

func getMyPlaytime(cat *Catalog) func(context.Context, *getPlaytimeInput) (*getPlaytimeOutput, error) {
	return func(ctx context.Context, in *getPlaytimeInput) (*getPlaytimeOutput, error) {
		if in == nil {
			in = &getPlaytimeInput{}
		}
		id, ok := repr.ParseID(in.WorkID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.WorkID))
		}
		rec, err := cat.GetPlaytime(ctx, id)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &getPlaytimeOutput{Body: rec}, nil
	}
}

func putMyPlaytime(cat *Catalog) func(context.Context, *putPlaytimeInput) (*getPlaytimeOutput, error) {
	return func(ctx context.Context, in *putPlaytimeInput) (*getPlaytimeOutput, error) {
		if in == nil {
			in = &putPlaytimeInput{}
		}
		id, ok := repr.ParseID(in.WorkID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.WorkID))
		}
		rec, err := cat.PutPlaytime(ctx, id, in.Body.Minutes)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &getPlaytimeOutput{Body: rec}, nil
	}
}

func deleteMyPlaytime(cat *Catalog) func(context.Context, *deletePlaytimeInput) (*struct{}, error) {
	return func(ctx context.Context, in *deletePlaytimeInput) (*struct{}, error) {
		if in == nil {
			in = &deletePlaytimeInput{}
		}
		id, ok := repr.ParseID(in.WorkID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.WorkID))
		}
		if err := cat.DeletePlaytime(ctx, id); err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &struct{}{}, nil
	}
}

func listMyWorkStates(cat *Catalog) func(context.Context, *listWorkStatesInput) (*listWorkStatesOutput, error) {
	return func(ctx context.Context, in *listWorkStatesInput) (*listWorkStatesOutput, error) {
		if in == nil {
			in = &listWorkStatesInput{}
		}
		q, err := parseCatalogList(ctx, &in.CollectionInput, collect.WorkStateSpec())
		if err != nil {
			return nil, err
		}
		page, lerr := cat.ListWorkStates(ctx, q, splitWorkIDs(in.WorkIDs))
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listWorkStatesOutput{Body: page}, nil
	}
}

func getMyWorkState(cat *Catalog) func(context.Context, *getWorkStateInput) (*getWorkStateOutput, error) {
	return func(ctx context.Context, in *getWorkStateInput) (*getWorkStateOutput, error) {
		if in == nil {
			in = &getWorkStateInput{}
		}
		id, ok := repr.ParseID(in.WorkID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.WorkID))
		}
		rec, err := cat.GetWorkState(ctx, id)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &getWorkStateOutput{Body: rec}, nil
	}
}

func putMyWorkState(cat *Catalog) func(context.Context, *putWorkStateInput) (*getWorkStateOutput, error) {
	return func(ctx context.Context, in *putWorkStateInput) (*getWorkStateOutput, error) {
		if in == nil {
			in = &putWorkStateInput{}
		}
		id, ok := repr.ParseID(in.WorkID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.WorkID))
		}
		rec, err := cat.PutWorkState(ctx, id, in.Body.State, in.Body.Completion)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &getWorkStateOutput{Body: rec}, nil
	}
}

func deleteMyWorkState(cat *Catalog) func(context.Context, *deleteWorkStateInput) (*struct{}, error) {
	return func(ctx context.Context, in *deleteWorkStateInput) (*struct{}, error) {
		if in == nil {
			in = &deleteWorkStateInput{}
		}
		id, ok := repr.ParseID(in.WorkID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.WorkID))
		}
		if err := cat.DeleteWorkState(ctx, id); err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &struct{}{}, nil
	}
}

func listMyCoverVotes(cat *Catalog) func(context.Context, *struct{}) (*listCoverVotesOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*listCoverVotesOutput, error) {
		page, err := cat.ListCoverVotes(ctx)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &listCoverVotesOutput{Body: page}, nil
	}
}

func putMyCoverVote(cat *Catalog) func(context.Context, *putCoverVoteInput) (*getCoverVoteOutput, error) {
	return func(ctx context.Context, in *putCoverVoteInput) (*getCoverVoteOutput, error) {
		if in == nil {
			in = &putCoverVoteInput{}
		}
		id, ok := repr.ParseID(in.CoverID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.CoverID))
		}
		rec, err := cat.PutCoverVote(ctx, id, in.Body.Vote)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &getCoverVoteOutput{Body: rec}, nil
	}
}

func deleteMyCoverVote(cat *Catalog) func(context.Context, *deleteCoverVoteInput) (*struct{}, error) {
	return func(ctx context.Context, in *deleteCoverVoteInput) (*struct{}, error) {
		if in == nil {
			in = &deleteCoverVoteInput{}
		}
		id, ok := repr.ParseID(in.CoverID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.CoverID))
		}
		if err := cat.DeleteCoverVote(ctx, id); err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &struct{}{}, nil
	}
}

type listClaimsOutput struct {
	Body repr.List[repr.ClaimRecord]
}

type listMyClaimsInput struct {
	CollectionInput
	ClaimState string `query:"claim_state" maxLength:"128" doc:"Comma-separated closed states: none, live, draft, pending, declined, hidden."`
	Kind       string `query:"kind" maxLength:"16" doc:"submitted (default) keeps works the bearer owns, audited keeps works the bearer reviewed but does not own, all keeps everything the bearer touched."`
	Site       string `query:"site" maxLength:"64" doc:"Claiming site key. Open vocabulary; unknown values match nothing. Also scopes first_acted_at and acted_count."`
}

func listMyClaims(cat *Catalog) func(context.Context, *listMyClaimsInput) (*listClaimsOutput, error) {
	return func(ctx context.Context, in *listMyClaimsInput) (*listClaimsOutput, error) {
		if in == nil {
			in = &listMyClaimsInput{}
		}
		q, err := parseCatalogList(ctx, &in.CollectionInput, collect.ClaimSpec())
		if err != nil {
			return nil, err
		}
		f, ferr := parseMyClaimFilter(in.ClaimState, in.Kind, in.Site)
		if ferr != nil {
			return nil, withIdent(ctx, ferr)
		}
		page, lerr := cat.ListMyClaims(ctx, q, f)
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listClaimsOutput{Body: page}, nil
	}
}

type listModerationClaimsInput struct {
	CollectionInput
	ClaimState string `query:"claim_state" maxLength:"128" doc:"Comma-separated closed states: live, draft, pending, declined, hidden. Default pending. hidden is how a banned claim is found for unban."`
}

func listModerationClaims(cat *Catalog) func(context.Context, *listModerationClaimsInput) (*listClaimsOutput, error) {
	return func(ctx context.Context, in *listModerationClaimsInput) (*listClaimsOutput, error) {
		if in == nil {
			in = &listModerationClaimsInput{}
		}
		q, err := parseCatalogList(ctx, &in.CollectionInput, collect.ClaimSpec())
		if err != nil {
			return nil, err
		}
		page, lerr := cat.ListModerationClaims(ctx, q, in.ClaimState)
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listClaimsOutput{Body: page}, nil
	}
}

func problemInvalidID(raw string) error {
	p := problem.New(problem.CodeInvalidParameter, "", "", "id must be a positive decimal catalog id.")
	p.Errors = []problem.FieldError{{Parameter: "id", Reason: problem.ReasonInvalidFormat, Detail: raw}}
	return p
}
