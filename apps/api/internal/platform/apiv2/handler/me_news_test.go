package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

func TestMeNewsRequiresUserToken(t *testing.T) {
	app := testApp(t)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/v2/me/news"},
		{http.MethodPost, "/v2/me/news"},
		{http.MethodGet, "/v2/me/news/1"},
		{http.MethodPatch, "/v2/me/news/1"},
		{http.MethodPost, "/v2/me/news-images"},
	} {
		status, ct, body := do(t, app, tc.method, tc.path)
		require.Equal(t, 401, status, tc.path)
		require.Contains(t, ct, "application/problem+json", tc.path)
		var p problem.Problem
		require.NoError(t, json.Unmarshal(body, &p), string(body))
		require.Equal(t, problem.CodeMissingCredential, p.Code, tc.path)
	}
}

func TestMeNewsUnbound(t *testing.T) {
	ctx := contextWithUser(t.Context(), 7, "client-a")
	_, err := (*Catalog)(nil).ListMyNews(ctx, collect.Query{})
	p, ok := err.(*problem.Problem)
	require.True(t, ok, "%v", err)
	require.Equal(t, problem.CodeServiceUnavailable, p.Code)

	_, _, err = (&Catalog{}).GetMyNews(ctx, 1)
	p, ok = err.(*problem.Problem)
	require.True(t, ok, "%v", err)
	require.Equal(t, problem.CodeServiceUnavailable, p.Code)

	_, _, err = (&Catalog{}).CreateMyNews(ctx, newsSubmissionBody{})
	p, ok = err.(*problem.Problem)
	require.True(t, ok, "%v", err)
	require.Equal(t, problem.CodeServiceUnavailable, p.Code)

	_, _, err = (&Catalog{}).PatchMyNews(ctx, 1, newsPatchBody{}, "")
	p, ok = err.(*problem.Problem)
	require.True(t, ok, "%v", err)
	require.Equal(t, problem.CodeServiceUnavailable, p.Code)
}

// TestMeNewsOpsAreDeclared names the operations: the contract walk asserts
// every declared operation answers, so a lost registration would make it pass by
// having nothing to walk.
func TestMeNewsOpsAreDeclared(t *testing.T) {
	app := fiber.New(fiber.Config{ErrorHandler: problem.WriteFiberError})
	doc := Setup(app).OpenAPI()

	want := map[string]struct{ path, method string }{
		"listMyNews":        {"/v2/me/news", http.MethodGet},
		"createMyNews":      {"/v2/me/news", http.MethodPost},
		"getMyNewsItem":     {"/v2/me/news/{id}", http.MethodGet},
		"patchMyNewsItem":   {"/v2/me/news/{id}", http.MethodPatch},
		"uploadMyNewsImage": {"/v2/me/news-images", http.MethodPost},
	}
	found := map[string]bool{}
	for path, item := range doc.Paths {
		for _, op := range pathOps(item) {
			w, ok := want[op.OperationID]
			if !ok {
				continue
			}
			require.Equal(t, w.path, path, op.OperationID)
			require.Equal(t, w.method, opMethodOf(item, op), op.OperationID)
			found[op.OperationID] = true
		}
	}
	for id := range want {
		require.True(t, found[id], "operation %s is missing from the spec", id)
	}

	post := doc.Paths["/v2/me/news"].Post
	require.NotNil(t, post.Responses["201"], "create must declare 201")
	require.NotNil(t, post.Responses["201"].Headers["Location"], "201 must carry Location")
	require.NotNil(t, post.Responses["403"], "SOURCE_NOT_YOURS is a 403 on this path")
	require.NotNil(t, post.Responses["422"], "SOURCE_INACTIVE and VALIDATION_FAILED are 422 on this path")
	patch := doc.Paths["/v2/me/news/{id}"].Patch
	require.NotNil(t, patch.Responses["409"], "INVALID_STATE_TRANSITION")
	require.NotNil(t, patch.Responses["428"], "If-Match is mandatory to withdraw")
	require.NotNil(t, doc.Paths["/v2/me/news/{id}"].Get.Responses["200"].Headers["ETag"])
	upload := doc.Paths["/v2/me/news-images"].Post
	require.NotNil(t, upload.Responses["201"].Headers["Location"], "201 must carry Location")
	require.NotNil(t, upload.Responses["422"], "a refused file is a 422 on this path")
	require.NotNil(t, upload.Responses["429"], "the per-account cap is a 429 on this path")
}

func TestNewsDomainCodesAreRegistered(t *testing.T) {
	for _, code := range []string{problem.CodeSourceNotYours, problem.CodeSourceInactive} {
		def, ok := problem.Lookup(code)
		require.True(t, ok, code)
		require.Equal(t, problem.DomainNews, def.Domain, code)
		require.Contains(t, def.TypeURI(), "/problems/news/", code)
	}
	notYours, _ := problem.Lookup(problem.CodeSourceNotYours)
	require.Equal(t, http.StatusForbidden, notYours.Status)
	inactive, _ := problem.Lookup(problem.CodeSourceInactive)
	require.Equal(t, http.StatusUnprocessableEntity, inactive.Status)
}

func opMethodOf(item *huma.PathItem, op *huma.Operation) string {
	if op.Method != "" {
		return op.Method
	}
	switch op {
	case item.Get:
		return http.MethodGet
	case item.Post:
		return http.MethodPost
	case item.Patch:
		return http.MethodPatch
	case item.Put:
		return http.MethodPut
	case item.Delete:
		return http.MethodDelete
	}
	return ""
}
