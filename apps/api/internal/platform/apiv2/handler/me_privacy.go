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
		PlayRecords *string `json:"play_records,omitempty" enum:"public,private" example:"private" doc:"Who may read the account's play states and playtimes on /v2/work-records. Absent leaves it as it is."`
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
		Description: "play_records says who may read the bearer's play states and playtimes on /v2/work-records. An account that never changed it answers public with a null updated_at. Requires a user access token. Any app may call this; no scope is required.",
		Tags:        me, Errors: errs, SkipValidateParams: true,
	}, getMyPrivacy(cat))
	huma.Register(api, huma.Operation{
		OperationID: "patchMyPrivacy", Method: http.MethodPatch, Path: "/v2/me/privacy",
		Summary:     "Change my privacy switches",
		Description: "Sets the switches the body names and leaves the others as they are; an empty body changes nothing. The change applies to the next read of /v2/work-records. Requires a user access token. Any app may call this; no scope is required.",
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
		out, err := cat.PatchMyPrivacy(ctx, in.Body.PlayRecords)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &privacyOutput{Body: out}, nil
	}
}

func privacyRepr(p catsvc.PlayRecordsPrivacy) repr.UserPrivacy {
	out := repr.UserPrivacy{Object: "privacy", PlayRecords: "public"}
	if p.Private {
		out.PlayRecords = "private"
	}
	if p.UpdatedAt != nil {
		s := p.UpdatedAt.UTC().Format(time.RFC3339)
		out.UpdatedAt = &s
	}
	return out
}

func (c *Catalog) GetMyPrivacy(ctx context.Context) (repr.UserPrivacy, error) {
	if c == nil || c.WorkRecords == nil {
		return repr.UserPrivacy{}, problem.New(problem.CodeServiceUnavailable, "", "", "privacy is not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return repr.UserPrivacy{}, err
	}
	p, err := c.WorkRecords.Privacy(ctx, uid)
	if err != nil {
		return repr.UserPrivacy{}, err
	}
	return privacyRepr(p), nil
}

func (c *Catalog) PatchMyPrivacy(ctx context.Context, playRecords *string) (repr.UserPrivacy, error) {
	if c == nil || c.WorkRecords == nil {
		return repr.UserPrivacy{}, problem.New(problem.CodeServiceUnavailable, "", "", "privacy is not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return repr.UserPrivacy{}, err
	}
	var p catsvc.PlayRecordsPrivacy
	if playRecords == nil {
		p, err = c.WorkRecords.Privacy(ctx, uid)
	} else {
		p, err = c.WorkRecords.SetPrivacy(ctx, uid, *playRecords == "private")
	}
	if err != nil {
		return repr.UserPrivacy{}, err
	}
	return privacyRepr(p), nil
}
