package handler

import (
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

const idempotencyKeyDescription = "Makes the request safe to retry. The same key with the same body within 24 hours replays the first response with Idempotency-Replayed: true; the same key with a different body is 409 IDEMPOTENCY_KEY_REUSED; a retry while the first request is still running is 409 IDEMPOTENCY_REQUEST_IN_PROGRESS. Scoped to the caller and the path."

func declareIdempotency(path string, item *huma.PathItem) {
	if item == nil || item.Post == nil || !strings.HasPrefix(path, "/v2") {
		return
	}
	op := item.Post
	already := false
	for _, p := range op.Parameters {
		if p != nil && p.Name == "Idempotency-Key" && p.In == "header" {
			already = true
			break
		}
	}
	if !already {
		minLen, maxLen := 1, 255
		op.Parameters = append(op.Parameters, &huma.Param{
			Name:        "Idempotency-Key",
			In:          "header",
			Required:    false,
			Description: idempotencyKeyDescription,
			Schema: &huma.Schema{
				Type:        "string",
				MinLength:   &minLen,
				MaxLength:   &maxLen,
				Description: idempotencyKeyDescription,
			},
		})
	}
	if op.Responses == nil {
		op.Responses = map[string]*huma.Response{}
	}
	if _, ok := op.Responses["409"]; !ok {
		op.Responses["409"] = &huma.Response{Description: http.StatusText(http.StatusConflict)}
	}
}

func missingV2PostIdempotency(doc *huma.OpenAPI) []string {
	if doc == nil {
		return []string{"nil document"}
	}
	var missing []string
	for path, item := range doc.Paths {
		if item == nil || item.Post == nil || !strings.HasPrefix(path, "/v2") {
			continue
		}
		op := item.Post
		hasHeader := false
		for _, p := range op.Parameters {
			if p != nil && p.Name == "Idempotency-Key" && p.In == "header" {
				hasHeader = true
				break
			}
		}
		if !hasHeader {
			missing = append(missing, path+" missing Idempotency-Key")
		}
		if op.Responses == nil || op.Responses["409"] == nil {
			missing = append(missing, path+" missing 409")
		}
	}
	return missing
}
