package handler

import (
	"context"
	"net/http"

	"api/internal/platform/apiv2/repr"

	"github.com/danielgtaylor/huma/v2"
)

type getEditContextInput struct {
	Object string `path:"object" maxLength:"32" enum:"work,company,character,release,tag,engine,series,person,credit_name,trait" doc:"Family of the entity. Unknown family is 404 NOT_FOUND."`
	ID     string `path:"id" pattern:"^[0-9]+$" maxLength:"20" doc:"Catalog id of the entity."`
}

type getEditContextOutput struct {
	Body repr.EditContext
}

func registerMeEditContext(api huma.API, cat *Catalog) {
	huma.Register(api, huma.Operation{
		OperationID: "getMyEditContext",
		Method:      http.MethodGet,
		Path:        "/v2/me/edit-context/{object}/{id}",
		Summary:     "What an editor sees behind one entity",
		Description: "Per field: the value a proposal is written against, who wrote each scalar column (field provenance, newest first), and, for list fields whose importers keep their own rows, every stored row grouped by source with its identity key, whether it is hidden, whether it is the human lane, plus suppressions whose row is gone. " +
			"An editor shows what each upstream says from this face and sends rows[].identity in the field's .suppressed companion to hide a row. " +
			"Requires a user access token bound to a catalog site. The token must carry the catalog:edit scope.",
		Tags: []string{"me"},
		Errors: collectionErrors(http.StatusUnauthorized, http.StatusForbidden,
			http.StatusNotFound, http.StatusServiceUnavailable),
		SkipValidateParams: true,
	}, getMyEditContext(cat))
}

func getMyEditContext(cat *Catalog) func(context.Context, *getEditContextInput) (*getEditContextOutput, error) {
	return func(ctx context.Context, in *getEditContextInput) (*getEditContextOutput, error) {
		rec, err := cat.GetEditContext(ctx, in.Object, in.ID)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &getEditContextOutput{Body: rec}, nil
	}
}
