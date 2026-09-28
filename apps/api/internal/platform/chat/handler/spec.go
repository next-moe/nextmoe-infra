package handler

import (
	"reflect"

	"api/internal/platform/apiv2/problem"
	"api/pkg/wireshape"

	"github.com/danielgtaylor/huma/v2"
)

// Huma published every error as its own ErrorModel; chat sends problem.Problem.
func publishWireShapes(doc *huma.OpenAPI) {
	if wireshape.Skip {
		return
	}
	problemRef := doc.Components.Schemas.Schema(reflect.TypeFor[problem.Problem](), true, "Problem")
	wireshape.Publish(doc)
	for _, item := range doc.Paths {
		for _, op := range wireshape.Operations(item) {
			for status, resp := range op.Responses {
				if status >= "400" && resp != nil {
					resp.Content = map[string]*huma.MediaType{"application/problem+json": {Schema: problemRef}}
				}
			}
		}
	}
	delete(doc.Components.Schemas.Map(), "ErrorModel")
	delete(doc.Components.Schemas.Map(), "ErrorDetail")
}
