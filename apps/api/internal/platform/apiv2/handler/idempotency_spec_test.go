package handler

import (
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

func TestV2PostsDeclareIdempotencyKeyAnd409(t *testing.T) {
	doc := Setup(fiber.New()).OpenAPI()
	missing := missingV2PostIdempotency(doc)
	require.Empty(t, missing)

	bare := &huma.OpenAPI{Paths: map[string]*huma.PathItem{
		"/v2/probe": {Post: &huma.Operation{}},
	}}
	require.NotEmpty(t, missingV2PostIdempotency(bare), "positive control: a POST without the header or 409 must fail the census")
}
