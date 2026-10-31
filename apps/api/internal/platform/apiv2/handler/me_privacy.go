package handler

import (
	"context"
	"net/http"
	"time"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	catsvc "api/internal/platform/catalog/service"

	"github.com/danielgtaylor/huma/v2"
)

type patchPrivacyInput struct {
	Body struct {
		PlayRecords            *string `json:"play_records,omitempty" enum:"public,private" example:"private" doc:"Who may read the account's play states and playtimes on /v2/work-records. Absent leaves it as it is."`
		BrowsingHistory        *string `json:"browsing_history,omitempty" enum:"on,off" example:"off" doc:"off erases every app's browsing history and stops recording it; on starts recording again. A third-party app may only send off. Absent leaves it as it is."`
		BrowsingHistorySharing *string `json:"browsing_history_sharing,omitempty" enum:"per_app,shared" example:"per_app" doc:"shared lets an app holding history:read read every app's history. A third-party app may only send per_app. Absent leaves it as it is."`
	}
}

type privacyOutput struct {
	Body repr.UserPrivacy
}

func registerMePrivacy(api huma.API, cat *Catalog) {
	me := []string{"me"}
	errs := collectionErrors(http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable)
	huma.Register(api, huma.Operation{
		OperationID: "getMyPrivacy", Method: http.MethodGet, Path: "/v2/me/privacy",
		Summary:     "My privacy switches",
		Description: "play_records says who may read the bearer's play states and playtimes on /v2/work-records; browsing_history and browsing_history_sharing govern /v2/me/history. An account that never changed a switch answers public, on and per_app with a null updated_at. Requires a user access token. Any app may call this; no scope is required.",
		Tags:        me, Errors: errs, SkipValidateParams: true,
	}, getMyPrivacy(cat))
	huma.Register(api, huma.Operation{
		OperationID: "patchMyPrivacy", Method: http.MethodPatch, Path: "/v2/me/privacy",
		Summary:     "Change my privacy switches",
		Description: "Sets the switches the body names and leaves the others as they are; an empty body changes nothing. The change applies to the next request. browsing_history=off deletes every app's browsing history before it answers. A third-party app may switch browsing history off and sharing back to per_app, never the other way: that is 422 VALIDATION_FAILED with reason NOT_PERMITTED, and nothing in the body is applied. Requires a user access token. Any app may call this; no scope is required.",
		Tags:        me, Errors: append(errs, http.StatusUnprocessableEntity), SkipValidateParams: true,
	}, patchMyPrivacy(cat))
}

func getMyPrivacy(cat *Catalog) func(context.Context, *struct{}) (*privacyOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*privacyOutput, error) {
		out, err := cat.GetMyPrivacy(ctx)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &privacyOutput{Body: out}, nil
	}
}

func patchMyPrivacy(cat *Catalog) func(context.Context, *patchPrivacyInput) (*privacyOutput, error) {
	return func(ctx context.Context, in *patchPrivacyInput) (*privacyOutput, error) {
		out, err := cat.PatchMyPrivacy(ctx, in.Body.PlayRecords, in.Body.BrowsingHistory, in.Body.BrowsingHistorySharing)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &privacyOutput{Body: out}, nil
	}
}

func privacyRepr(p catsvc.PlayRecordsPrivacy, h catsvc.BrowsingHistorySettings) repr.UserPrivacy {
	out := repr.UserPrivacy{Object: "privacy", PlayRecords: "public", BrowsingHistory: "on", BrowsingHistorySharing: "per_app"}
	if p.Private {
		out.PlayRecords = "private"
	}
	if h.Off {
		out.BrowsingHistory = "off"
	}
	if h.Shared {
		out.BrowsingHistorySharing = "shared"
	}
	if p.UpdatedAt != nil {
		s := p.UpdatedAt.UTC().Format(time.RFC3339)
		out.UpdatedAt = &s
	}
	return out
}

func (c *Catalog) GetMyPrivacy(ctx context.Context) (repr.UserPrivacy, error) {
	if c == nil || c.WorkRecords == nil || c.BrowsingHistory == nil {
		return repr.UserPrivacy{}, problem.New(problem.CodeServiceUnavailable, "", "", "privacy is not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return repr.UserPrivacy{}, err
	}
	return c.privacyOf(ctx, uid)
}

func (c *Catalog) privacyOf(ctx context.Context, uid int64) (repr.UserPrivacy, error) {
	p, err := c.WorkRecords.Privacy(ctx, uid)
	if err != nil {
		return repr.UserPrivacy{}, err
	}
	h, err := c.BrowsingHistory.Settings(ctx, uid)
	if err != nil {
		return repr.UserPrivacy{}, err
	}
	return privacyRepr(p, h), nil
}

func (c *Catalog) PatchMyPrivacy(ctx context.Context, playRecords, browsingHistory, sharing *string) (repr.UserPrivacy, error) {
	if c == nil || c.WorkRecords == nil || c.BrowsingHistory == nil {
		return repr.UserPrivacy{}, problem.New(problem.CodeServiceUnavailable, "", "", "privacy is not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return repr.UserPrivacy{}, err
	}
	var off, shared *bool
	if browsingHistory != nil {
		v := *browsingHistory == "off"
		off = &v
	}
	if sharing != nil {
		v := *sharing == "shared"
		shared = &v
	}
	if thirdPartyFrom(ctx) && ((off != nil && !*off) || (shared != nil && *shared)) {
		current, err := c.BrowsingHistory.Settings(ctx, uid)
		if err != nil {
			return repr.UserPrivacy{}, err
		}
		if p := loosenedByThirdParty(current, off, shared); p != nil {
			return repr.UserPrivacy{}, p
		}
	}
	if playRecords != nil {
		if _, err := c.WorkRecords.SetPrivacy(ctx, uid, *playRecords == "private"); err != nil {
			return repr.UserPrivacy{}, err
		}
	}
	if _, err := c.BrowsingHistory.SetSettings(ctx, uid, off, shared); err != nil {
		return repr.UserPrivacy{}, err
	}
	return c.privacyOf(ctx, uid)
}

// Only the account may widen what is kept or who reads it; a third-party app
// that could, could also grant itself every app's history.
func loosenedByThirdParty(current catsvc.BrowsingHistorySettings, off, shared *bool) *problem.Problem {
	var errs []problem.FieldError
	if off != nil && !*off && current.Off {
		errs = append(errs, problem.FieldError{Pointer: "/browsing_history", Reason: problem.ReasonNotPermitted,
			Detail: "a third-party app cannot switch browsing history back on"})
	}
	if shared != nil && *shared && !current.Shared {
		errs = append(errs, problem.FieldError{Pointer: "/browsing_history_sharing", Reason: problem.ReasonNotPermitted,
			Detail: "a third-party app cannot share browsing history across apps"})
	}
	if len(errs) == 0 {
		return nil
	}
	p := problem.New(problem.CodeValidationFailed, "", "", "only the account, through a first-party app, can widen browsing history.")
	p.Errors = errs
	return p
}
