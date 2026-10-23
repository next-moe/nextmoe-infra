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

type listMyRatingsInput struct {
	CollectionInput
	WorkIDs string `query:"work_ids" maxLength:"4096" doc:"Comma-separated work ids, max 100. Batch read, no pagination."`
}
type listMyRatingsOutput struct {
	Body repr.List[repr.UserRating]
}
type myRatingPathInput struct {
	WorkID string `path:"work_id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Catalog work id."`
}
type putMyRatingInput struct {
	WorkID string `path:"work_id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Catalog work id."`
	Body   ratingBody
}
type userRatingOutput struct {
	Body repr.UserRating
}

type workRecordState struct {
	State      string `json:"state" enum:"wish,doing,done,on_hold,dropped" example:"done" doc:"Closed vocabulary, as on /v2/me/work-states."`
	Completion string `json:"completion,omitempty" enum:"one_route,main,all" example:"all" doc:"How much is finished. Absent means unstated. Refused with wish."`
}
type workRecordPlaytime struct {
	Minutes int `json:"minutes" minimum:"0" maximum:"60000" example:"720" doc:"Absolute cumulative minutes."`
}
type putWorkRecordInput struct {
	WorkID string `path:"work_id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Catalog work id."`
	Body   struct {
		WorkState *workRecordState    `json:"work_state,omitempty" doc:"The play state. Absent or null deletes it."`
		Playtime  *workRecordPlaytime `json:"playtime,omitempty" doc:"This app's playtime report. Absent or null deletes this app's row; other apps' rows stay."`
		Rating    *ratingBody         `json:"rating,omitempty" doc:"The rating. Absent or null deletes it."`
	}
}
type workRecordOutput struct {
	Body repr.WorkRecord
}

func registerMeRatings(api huma.API, cat *Catalog) {
	me := []string{"me"}
	errs := collectionErrors(http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusServiceUnavailable)
	writeErrs := append(errs, http.StatusUnprocessableEntity)
	huma.Register(api, huma.Operation{
		OperationID: "listMyRatings", Method: http.MethodGet, Path: "/v2/me/ratings",
		Summary:     "List my ratings",
		Description: "The bearer user's ratings, in the order the catalog last changed them, oldest first; next_cursor is the incremental-sync watermark. work_ids= is a batch read. A rating is one per person per work across every NextMoe site, so this answers what the user rated anywhere. Requires a user access token. Any app may call this; no scope is required.",
		Tags:        me, Errors: errs, SkipValidateParams: true,
	}, listMyRatings(cat))
	huma.Register(api, huma.Operation{
		OperationID: "getMyRating", Method: http.MethodGet, Path: "/v2/me/ratings/{work_id}",
		Summary:     "Get my rating of one work",
		Description: "404 when the user has not rated the work. Requires a user access token. Any app may call this; no scope is required.",
		Tags:        me, Errors: errs, SkipValidateParams: true,
	}, getMyRating(cat))
	huma.Register(api, huma.Operation{
		OperationID: "putMyRating", Method: http.MethodPut, Path: "/v2/me/ratings/{work_id}",
		Summary:     "Rate a work, or replace my rating of it",
		Description: "Creates the rating or replaces every field of it; the id, client_id and site stay those of the first write. Sending the rating as it already stands writes nothing. The work's nextmoe score on /v2/catalog/works/{id}/ratings follows at once. Any app may write a rating; it is public on every site. A first-party site checks short_summary itself before sending it. For a third-party application's token the catalog checks a new or changed short_summary: a refused one is 422 VALIDATION_FAILED at /short_summary with reason NOT_ALLOWED_VALUE, and nothing is written. Requires a user access token; no scope is required.",
		Tags:        me, Errors: writeErrs, SkipValidateParams: true,
	}, putMyRating(cat))
	huma.Register(api, huma.Operation{
		OperationID: "deleteMyRating", Method: http.MethodDelete, Path: "/v2/me/ratings/{work_id}",
		Summary:     "Delete my rating of one work",
		Description: "204 with no body, also when there was none. The deletion reaches /v2/ratings/changes as a gone entry. Requires a user access token. Any app may call this; no scope is required.",
		Tags:        me, Errors: errs, DefaultStatus: http.StatusNoContent, SkipValidateParams: true,
	}, deleteMyRating(cat))
	huma.Register(api, huma.Operation{
		OperationID: "getMyWorkRecord", Method: http.MethodGet, Path: "/v2/me/work-records/{work_id}",
		Summary:     "My play state, playtime and rating on one work",
		Description: "The three parts a rate-this-game form edits together, as /v2/me/work-states, /v2/me/playtimes and /v2/me/ratings answer them. 200 with null parts when nothing is recorded. Requires a user access token. Any app may call this; no scope is required.",
		Tags:        me, Errors: errs, SkipValidateParams: true,
	}, getMyWorkRecord(cat))
	huma.Register(api, huma.Operation{
		OperationID: "putMyWorkRecord", Method: http.MethodPut, Path: "/v2/me/work-records/{work_id}",
		Summary:     "Replace my play state, playtime and rating on one work",
		Description: "One transaction for the three parts: either all are written or none is. PUT replaces the whole record, so a part that is absent or null is deleted: send every part the form shows. One exception: a third-party application's PUT without a rating leaves the rating as it is; DELETE /v2/me/ratings/{work_id} removes it. playtime is this app's own report, and the record answers the most minutes any app reported. A third-party application's new or changed short_summary is checked as on PUT /v2/me/ratings/{work_id}; a refused one is 422 at /rating/short_summary and no part is written. Requires a user access token; no scope is required.",
		Tags:        me, Errors: writeErrs, SkipValidateParams: true,
	}, putMyWorkRecord(cat))
}

func listMyRatings(cat *Catalog) func(context.Context, *listMyRatingsInput) (*listMyRatingsOutput, error) {
	return func(ctx context.Context, in *listMyRatingsInput) (*listMyRatingsOutput, error) {
		q, err := parseCatalogList(ctx, &in.CollectionInput, collect.MyRatingSpec())
		if err != nil {
			return nil, err
		}
		page, lerr := cat.ListMyRatings(ctx, q, splitWorkIDs(in.WorkIDs))
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listMyRatingsOutput{Body: page}, nil
	}
}

func getMyRating(cat *Catalog) func(context.Context, *myRatingPathInput) (*userRatingOutput, error) {
	return func(ctx context.Context, in *myRatingPathInput) (*userRatingOutput, error) {
		id, ok := repr.ParseID(in.WorkID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.WorkID))
		}
		rec, err := cat.GetMyRating(ctx, id)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &userRatingOutput{Body: rec}, nil
	}
}

func putMyRating(cat *Catalog) func(context.Context, *putMyRatingInput) (*userRatingOutput, error) {
	return func(ctx context.Context, in *putMyRatingInput) (*userRatingOutput, error) {
		id, ok := repr.ParseID(in.WorkID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.WorkID))
		}
		rec, err := cat.PutMyRating(ctx, id, in.Body)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &userRatingOutput{Body: rec}, nil
	}
}

func deleteMyRating(cat *Catalog) func(context.Context, *myRatingPathInput) (*struct{}, error) {
	return func(ctx context.Context, in *myRatingPathInput) (*struct{}, error) {
		id, ok := repr.ParseID(in.WorkID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.WorkID))
		}
		if err := cat.DeleteMyRating(ctx, id); err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &struct{}{}, nil
	}
}

func getMyWorkRecord(cat *Catalog) func(context.Context, *myRatingPathInput) (*workRecordOutput, error) {
	return func(ctx context.Context, in *myRatingPathInput) (*workRecordOutput, error) {
		id, ok := repr.ParseID(in.WorkID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.WorkID))
		}
		rec, err := cat.GetWorkRecord(ctx, id)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &workRecordOutput{Body: rec}, nil
	}
}

func putMyWorkRecord(cat *Catalog) func(context.Context, *putWorkRecordInput) (*workRecordOutput, error) {
	return func(ctx context.Context, in *putWorkRecordInput) (*workRecordOutput, error) {
		id, ok := repr.ParseID(in.WorkID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.WorkID))
		}
		var rec catsvc.WorkRecordInput
		if s := in.Body.WorkState; s != nil {
			st, comp := workStateFromWords(s.State, s.Completion)
			rec.WorkState = &catsvc.WorkStateInput{State: st, Completion: comp}
		}
		if p := in.Body.Playtime; p != nil {
			rec.Minutes = &p.Minutes
		}
		if r := in.Body.Rating; r != nil {
			v := r.input()
			rec.Rating = &v
		} else {
			rec.KeepRating = thirdPartyFrom(ctx)
		}
		out, err := cat.PutWorkRecord(ctx, id, rec)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &workRecordOutput{Body: out}, nil
	}
}

func workRecordRepr(r catsvc.WorkRecord) repr.WorkRecord {
	out := repr.WorkRecord{Object: "work_record", WorkID: repr.ID(r.WorkID), Rating: userRatingPtr(r.Rating)}
	if r.WorkState != nil {
		v := userWorkStateRepr(*r.WorkState)
		out.WorkState = &v
	}
	if r.Playtime != nil {
		out.Playtime = &repr.UserPlaytime{Object: "playtime", WorkID: repr.ID(r.WorkID), Minutes: r.Playtime.Minutes}
	}
	return out
}

func (c *Catalog) GetWorkRecord(ctx context.Context, workID int64) (repr.WorkRecord, error) {
	if c == nil || c.WorkRecords == nil {
		return repr.WorkRecord{}, problem.New(problem.CodeServiceUnavailable, "", "", "work records are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return repr.WorkRecord{}, err
	}
	rec, gerr := c.WorkRecords.Get(ctx, uid, workID)
	if gerr != nil {
		return repr.WorkRecord{}, gerr
	}
	return workRecordRepr(*rec), nil
}

func (c *Catalog) PutWorkRecord(ctx context.Context, workID int64, in catsvc.WorkRecordInput) (repr.WorkRecord, error) {
	if c == nil || c.WorkRecords == nil {
		return repr.WorkRecord{}, problem.New(problem.CodeServiceUnavailable, "", "", "work records are not bound.")
	}
	uid, client, err := requireUser(ctx)
	if err != nil {
		return repr.WorkRecord{}, err
	}
	var scan summaryScan
	if in.Rating != nil {
		if err := in.Validate(); err != nil {
			return repr.WorkRecord{}, workRecordErr(err)
		}
		if scan, err = c.checkRatingSummary(ctx, uid, workID, in.Rating.ShortSummary, "/rating/short_summary"); err != nil {
			return repr.WorkRecord{}, err
		}
	}
	rec, gerr := c.WorkRecords.Put(ctx, uid, workID, client, siteFrom(ctx), in)
	if gerr != nil {
		return repr.WorkRecord{}, workRecordErr(gerr)
	}
	if scan.needed && rec.Rating != nil {
		c.scanRatingSummary(rec.Rating.ID, uid, rec.Rating.ShortSummary, scan.checkSkipped)
	}
	return workRecordRepr(*rec), nil
}

func workRecordErr(err error) error {
	switch err {
	case nil:
		return nil
	case catsvc.ErrWorkRecordWorkUnavailable:
		return problem.New(problem.CodeNotFound, "", "", "work is not available for records.")
	case catsvc.ErrWorkRecordActorRequired, catsvc.ErrPlaytimeClientRequired:
		return problem.New(problem.CodeUserIdentityRequired, "", "", err.Error())
	case catsvc.ErrPlaytimeMinutesRange:
		p := playtimeErr(err).(*problem.Problem)
		p.Errors[0].Pointer = "/playtime/minutes"
		return p
	case catsvc.ErrWorkStateBadState, catsvc.ErrWorkStateBadCompletion, catsvc.ErrWorkStateCompletionOnWish:
		p := workStateErr(err).(*problem.Problem)
		p.Errors[0].Pointer = "/work_state" + p.Errors[0].Pointer
		return p
	}
	return ratingErr(err, "/rating")
}
