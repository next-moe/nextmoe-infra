package handler

import (
	"context"
	"net/http"

	"api/internal/platform/apiv2/repr"

	"github.com/danielgtaylor/huma/v2"
)

type getCapabilitiesInput struct {
	Object   string `path:"object" maxLength:"32" enum:"work,company,character,release,tag,engine,series" doc:"Family these capabilities describe. Unknown family is 404 NOT_FOUND."`
	EntityID int64  `query:"entity_id" minimum:"0" doc:"Evaluate against one entity. Omit for the type-level answer, which cannot express the owner channel: a site that grants owner-review or owner-automerge answers would_automerge=false without it."`
}

type getCapabilitiesOutput struct {
	Body repr.EditCapabilities
}

func registerMeCapabilities(api huma.API, cat *Catalog) {
	huma.Register(api, huma.Operation{
		OperationID: "getMyEditCapabilities",
		Method:      http.MethodGet,
		Path:        "/v2/me/edit-capabilities/{object}",
		Summary:     "What I may edit on one family",
		Description: "Per-field can_propose / can_review / would_automerge for the bearer, evaluated by the editing engine. " +
			"The capability axis cannot ride on /v2/catalog/schemas/{object}: that face is credential-less and B34 forbids varying one URL's field set by credential, so the two are separate URLs and join on fields[].key. " +
			"Pass entity_id= wherever the site grants the owner channel, or would_automerge answers the type-level question instead. " +
			"Requires a user access token bound to a catalog site. The token must carry the catalog:edit scope.",
		Tags: []string{"me"},
		Errors: collectionErrors(http.StatusUnauthorized, http.StatusForbidden,
			http.StatusNotFound, http.StatusServiceUnavailable),
		SkipValidateParams: true,
	}, getMyEditCapabilities(cat))
}

func getMyEditCapabilities(cat *Catalog) func(context.Context, *getCapabilitiesInput) (*getCapabilitiesOutput, error) {
	return func(ctx context.Context, in *getCapabilitiesInput) (*getCapabilitiesOutput, error) {
		if in == nil {
			in = &getCapabilitiesInput{}
		}
		rec, err := cat.GetEditCapabilities(ctx, in.Object, in.EntityID)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &getCapabilitiesOutput{Body: rec}, nil
	}
}
