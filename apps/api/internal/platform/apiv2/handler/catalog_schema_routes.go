package handler

import (
	"context"
	"net/http"

	"api/internal/platform/apiv2/repr"

	"github.com/danielgtaylor/huma/v2"
)

type getSchemaInput struct {
	Object string `path:"object" maxLength:"32" enum:"work,company,character,release,tag,engine,series,person,credit_name,trait,walkthrough" doc:"Family this schema describes. Unknown family is 404 NOT_FOUND."`
}

type getSchemaOutput struct {
	Body repr.ObjectSchema
}

func registerCatalogSchemas(api huma.API, cat *Catalog) {
	huma.Register(api, huma.Operation{
		OperationID:        "getCatalogSchema",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/schemas/{object}",
		Summary:            "Editable-field schema for one family",
		Description:        "Unauthenticated metadata: include tokens, FULL_SET, and editing-engine fields. include/full_set describe the family's DETAIL face and list_include/list_full_set its collection face, which is narrower on work. Actor capabilities are not evaluated. Unknown object is 404 NOT_FOUND. schemas/release sets creation_disabled.",
		Tags:               []string{"catalog"},
		Errors:             collectionErrors(http.StatusNotFound, http.StatusServiceUnavailable),
		SkipValidateParams: true,
	}, getCatalogSchema(cat))
}

func getCatalogSchema(cat *Catalog) func(context.Context, *getSchemaInput) (*getSchemaOutput, error) {
	return func(ctx context.Context, in *getSchemaInput) (*getSchemaOutput, error) {
		if in == nil {
			in = &getSchemaInput{}
		}
		rec, err := cat.GetSchema(ctx, in.Object)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &getSchemaOutput{Body: rec}, nil
	}
}
