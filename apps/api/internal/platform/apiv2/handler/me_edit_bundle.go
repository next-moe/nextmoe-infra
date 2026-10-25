package handler

import (
	"context"
	"net/http"
	"strconv"

	"api/internal/platform/apiv2/repr"

	"github.com/danielgtaylor/huma/v2"
)

type getEditBundleInput struct {
	Object string `path:"object" maxLength:"32" enum:"work,company,character,release,tag,engine,series,person,credit_name,trait,walkthrough" doc:"Family of the entity. Unknown family is 404 NOT_FOUND."`
	ID     string `path:"id" pattern:"^[0-9]+$" maxLength:"20" doc:"Catalog id of the entity."`
}

type getEditBundleOutput struct {
	Body repr.EditBundle
}

func registerMeEditBundle(api huma.API, cat *Catalog) {
	huma.Register(api, huma.Operation{
		OperationID: "getMyEditBundle",
		Method:      http.MethodGet,
		Path:        "/v2/me/edit-bundle/{object}/{id}",
		Summary:     "Everything an editor needs to open one entity",
		Description: "One read for an editing screen: the family schema, the entity's snapshot, the bearer's capabilities on it, its edit context and the bearer's quota — each member the exact body its own face answers, so a client swaps five calls for one. " +
			"The schema and the snapshot decide the answer: an unknown family or entity is 404 as on GET /v2/moderation/snapshots/{object}/{id}. capabilities, context and quota are left out when their own face would have answered an error. " +
			"Requires a user access token bound to a catalog site. The token must carry the catalog:edit scope.",
		Tags: []string{"me"},
		Errors: collectionErrors(http.StatusUnauthorized, http.StatusForbidden,
			http.StatusNotFound, http.StatusServiceUnavailable),
		SkipValidateParams: true,
	}, getMyEditBundle(cat))
}

func getMyEditBundle(cat *Catalog) func(context.Context, *getEditBundleInput) (*getEditBundleOutput, error) {
	return func(ctx context.Context, in *getEditBundleInput) (*getEditBundleOutput, error) {
		if in == nil {
			in = &getEditBundleInput{}
		}
		schema, err := cat.GetSchema(ctx, in.Object)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		snapshot, err := cat.GetSnapshot(ctx, in.Object, in.ID)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		out := repr.EditBundle{Object: "edit_bundle", FamilySchema: schema, Snapshot: snapshot}
		if id, perr := strconv.ParseInt(in.ID, 10, 64); perr == nil {
			if caps, cerr := cat.GetEditCapabilities(ctx, in.Object, id); cerr == nil {
				out.Capabilities = &caps
			}
		}
		if edit, cerr := cat.GetEditContext(ctx, in.Object, in.ID); cerr == nil {
			out.Context = &edit
		}
		if quota, qerr := cat.GetEditQuota(ctx); qerr == nil {
			out.Quota = &quota
		}
		return &getEditBundleOutput{Body: out}, nil
	}
}
