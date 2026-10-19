package handler

import (
	"context"
	"net/http"

	"api/internal/platform/apiv2/repr"

	"github.com/danielgtaylor/huma/v2"
)

type getCapabilitiesInput struct {
	Object   string `path:"object" maxLength:"32" enum:"work,company,character,release,tag,engine,series" doc:"Family these capabilities describe. Unknown family is 404 NOT_FOUND."`
	EntityID int64  `query:"entity_id" minimum:"0" doc:"Evaluate against one entity. Omit for the type-level answer, which cannot express the owner channel: a policy that grants owner-review or owner-automerge answers would_automerge=false without it."`
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
			"Pass entity_id= for the owner channel, or would_automerge answers the type-level question instead. " +
			"The answer folds in the bearer's quota: an exhausted tier proposes nothing, and an editor past the daily direct-edit allowance no longer automerges. " +
			"Requires a user access token bound to a catalog site. The token must carry the catalog:edit scope.",
		Tags: []string{"me"},
		Errors: collectionErrors(http.StatusUnauthorized, http.StatusForbidden,
			http.StatusNotFound, http.StatusServiceUnavailable),
		SkipValidateParams: true,
	}, getMyEditCapabilities(cat))

	huma.Register(api, huma.Operation{
		OperationID: "getMyEditQuota",
		Method:      http.MethodGet,
		Path:        "/v2/me/edit-quota",
		Summary:     "My editing quota",
		Description: "The bearer's quota tier and, per counter, the limit, how much of it is used and what remains: proposals filed inside the sliding window, edits applied without review inside it, and proposals still awaiting a decision. " +
			"Counted per person across every catalog site; the limits are runtime configuration. " +
			"Requires a user access token bound to a catalog site. The token must carry the catalog:edit scope.",
		Tags: []string{"me"},
		Errors: collectionErrors(http.StatusUnauthorized, http.StatusForbidden,
			http.StatusServiceUnavailable),
	}, getMyEditQuota(cat))
}

type getEditQuotaOutput struct {
	Body repr.EditQuota
}

func getMyEditQuota(cat *Catalog) func(context.Context, *struct{}) (*getEditQuotaOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*getEditQuotaOutput, error) {
		rec, err := cat.GetEditQuota(ctx)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &getEditQuotaOutput{Body: rec}, nil
	}
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
