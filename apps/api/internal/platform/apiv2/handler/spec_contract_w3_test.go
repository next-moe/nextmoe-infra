package handler

import (
	"strings"
	"testing"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

func w3Doc(t *testing.T) *huma.OpenAPI {
	t.Helper()
	app := fiber.New(fiber.Config{ErrorHandler: problem.WriteFiberError})
	doc := Setup(app).OpenAPI()
	require.NotEmpty(t, doc.Paths)
	return doc
}

// requireIfMatch has answered 428 on all five of these since they shipped while
// the document published the header as optional, so letmoe's withdraw button
// (an etag parameter its transport has no slot to send) is 428 on every click
// and a spec-generated SDK repeats the same omission.
func TestIfMatchIsRequiredWhereTheServerDemandsIt(t *testing.T) {
	want := map[string]bool{
		"patchMyClaim":             true,
		"patchMyProposal":          true,
		"amendMyProposal":          true,
		"decideModerationClaim":    true,
		"decideModerationProposal": true,
		// The odd one out, and deliberately so: me_news.go requires If-Match only
		// for the withdraw transition and accepts a text edit without it, so
		// required:true here would be a false statement.
		"patchMyNewsItem": false,
	}
	seen := map[string]bool{}
	for _, item := range w3Doc(t).Paths {
		for _, op := range pathOps(item) {
			for _, p := range op.Parameters {
				if p == nil || p.Name != "If-Match" {
					continue
				}
				seen[op.OperationID] = true
				expect, known := want[op.OperationID]
				require.Truef(t, known, "%s declares If-Match and this table does not know it", op.OperationID)
				require.Equalf(t, expect, p.Required, "%s If-Match required", op.OperationID)
			}
		}
	}
	require.Len(t, seen, len(want), "an If-Match header disappeared from the surface: %v", seen)
}

// The bound is a promise the prose already made; three request arrays never
// declared it, and one of them is the op whose whole contract is per-item
// partial failure. Asserted over every request-body array so the fourth one is
// caught the day it is added, not the day a client sends 5000 items.
func TestEveryRequestBodyArrayDeclaresTheBatchBound(t *testing.T) {
	doc := w3Doc(t)
	require.NotNil(t, doc.Components)
	checked := 0
	for name, schema := range doc.Components.Schemas.Map() {
		if !strings.HasSuffix(name, "InputBody") {
			continue
		}
		for prop, p := range schema.Properties {
			if p == nil || (p.Type != "array" && p.Items == nil) {
				continue
			}
			require.NotNilf(t, p.MaxItems, "%s.%s is an unbounded request array", name, prop)
			require.EqualValuesf(t, collect.MaxBatchItems, *p.MaxItems,
				"%s.%s must carry the shared batch bound", name, prop)
			checked++
		}
	}
	require.GreaterOrEqual(t, checked, 5, "the sweep matched nothing; the *InputBody naming changed")
}

// The document carried no components.securitySchemes and 0 of 88 operations
// declared security, so every generated client and the MCP surface had no auth
// parameter at all. Asserted against v2Security, which is the same function the
// runtime gate dispatches on — the point is that there is one authority, not
// two lists that agree today.
func TestEveryOperationDeclaresTheCredentialItsGateDemands(t *testing.T) {
	doc := w3Doc(t)
	require.NotNil(t, doc.Components)
	require.Contains(t, doc.Components.SecuritySchemes, securityAppKey)
	require.Contains(t, doc.Components.SecuritySchemes, securityUserToken)

	gated, keyless, dual := 0, 0, 0
	for path, item := range doc.Paths {
		schemes, _ := v2Security(path)
		for _, op := range pathOps(item) {
			if len(schemes) == 0 {
				require.Emptyf(t, op.Security, "%s takes no credential and must declare none", op.OperationID)
				keyless++
				continue
			}
			require.Lenf(t, op.Security, len(schemes), "%s", op.OperationID)
			// One requirement object per alternative, never two schemes inside
			// one object: the second shape means "both at once", which is not
			// what any face here does and which generators handle badly.
			for i, scheme := range schemes {
				require.Lenf(t, op.Security[i], 1, "%s requirement %d", op.OperationID, i)
				_, ok := op.Security[i][scheme]
				require.Truef(t, ok, "%s must declare %s", op.OperationID, scheme)
			}
			if len(schemes) > 1 {
				dual++
			}
			gated++
		}
	}
	require.Greater(t, gated, 0)
	require.Greater(t, keyless, 0, "no keyless operation was seen; the control is vacuous")
	require.Greater(t, dual, 40, "the catalog read surface takes either credential; too few operations said so")
}

// The gate keys on what fiber matched, and the document is generated from the
// same predicate, so an unreachable path in one of them is a silent hole in the
// other. Every published path must be one routepath.Normalize already agrees
// with, or the two authorities are describing different surfaces.
func TestPublishedPathsAreAlreadyNormalized(t *testing.T) {
	for path := range w3Doc(t).Paths {
		require.Equal(t, strings.ToLower(path), path, path)
		require.Falsef(t, len(path) > 1 && strings.HasSuffix(path, "/"), "%s has a trailing slash", path)
	}
}

// Two waves in a row reasoned about collectionErrors as if it were the
// document's error set. It is not: huma.Register appends 500 to every operation
// that declares any error, and 422 to every operation that takes a parameter or
// a body (huma.go:736-741). So the padded 500 cannot be removed from that list,
// and a "missing" 422 was never missing. Pinned as arithmetic so the next
// reader checks the numbers instead of the list.
func TestHumaOwnsThe500AndThe422NotCollectionErrors(t *testing.T) {
	doc := w3Doc(t)
	total, with500, with422 := 0, 0, 0
	for _, item := range doc.Paths {
		for _, op := range pathOps(item) {
			total++
			if _, ok := op.Responses["500"]; ok {
				with500++
			}
			if _, ok := op.Responses["422"]; ok {
				with422++
			}
		}
	}
	require.Equal(t, total, with500, "huma puts 500 on every operation; collectionErrors cannot take it off")
	require.Less(t, with422, total, "422 is not universal, so the count below is a real discriminant")
	require.Equal(t, total-4, with422,
		"only the four operations with no input parameters lack 422: getCatalogStats, listMyCoverVotes, listNewsSources, listProblemReasons")
}
