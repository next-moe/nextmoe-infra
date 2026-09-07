package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	kunapp "api/internal/app"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/protocol"
	"api/internal/platform/devapi"
	"api/pkg/routepath"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// specV2Paths reads the published path table rather than the in-process
// document: the walk below is a promise about the surface the world is told
// exists, and it has to keep holding when the two drift.
func specV2Paths(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "..", "docs", "catalog", "v2-openapi.yaml"))
	require.NoError(t, err)
	var doc struct {
		Paths map[string]yaml.Node `yaml:"paths"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &doc))
	out := make([]string, 0, len(doc.Paths))
	for p := range doc.Paths {
		out = append(out, p)
	}
	require.NotEmpty(t, out)
	return out
}

var specPathParams = strings.NewReplacer(
	"{code}", "RATE_LIMITED",
	"{name}", "medium",
	"{id}", "1",
	"{object}", "work",
	"{work_id}", "1",
	"{cover_id}", "1",
	"{product_id}", "RJ01000000",
	"{uid}", "1",
)

func concretePath(t *testing.T, specPath string) string {
	t.Helper()
	p := specPathParams.Replace(specPath)
	require.NotContains(t, p, "{", "unsubstituted path param in %s", specPath)
	return p
}

// caseVariantPath flips the first letter of the first segment after /v2 —
// exactly the production probe that answered 200: GET /v2/Catalog/works.
func caseVariantPath(p string) (string, bool) {
	rest, ok := strings.CutPrefix(p, "/v2/")
	if !ok || rest == "" {
		return "", false
	}
	c := rest[0]
	switch {
	case c >= 'a' && c <= 'z':
		c -= 'a' - 'A'
	case c >= 'A' && c <= 'Z':
		c += 'a' - 'A'
	default:
		return "", false
	}
	return "/v2/" + string(c) + rest[1:], true
}

// The walk fires several hundred credential-less requests from one address,
// which is exactly the shape protocol's anti-abuse bucket blocks: sharing one
// bucket with itself turned the walk's own positive control into a 429 where
// it asserts 401. Only the block marker is dropped; the counters still run.
type unblockedStore struct{ *protocol.Memory }

func (unblockedStore) Get(context.Context, string) ([]byte, error) { return nil, nil }

func gateWalkApp(t *testing.T, cred *devapi.Credential) *fiber.App {
	t.Helper()
	// The production config, not one this test chose: CaseSensitive lives there,
	// and a test that set its own would prove nothing about the running service.
	app := fiber.New(kunapp.FiberConfig("kun-catalog"))
	SetupWith(app, Options{
		Store:            unblockedStore{protocol.NewMemory()},
		LookupCredential: func(context.Context, string) (*devapi.Credential, error) { return cred, nil },
	})
	return app
}

// walkGet returns the status AND the problem code, because "not a 2xx" is not
// the question. Every face here is unbound in a unit test, so a request that
// sails past the gate still answers 503 — an earlier draft of this walk was
// green against the unfixed gate for exactly that reason. The only safe
// signal is that the answer is still the gate's own.
func walkGet(t *testing.T, app *fiber.App, path, bearer string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := app.Test(req)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	var p problem.Problem
	if len(body) > 0 && json.Unmarshal(body, &p) == nil {
		return resp.StatusCode, p.Code
	}
	return resp.StatusCode, ""
}

// refusedBeforeTheHandler is true when the gate answered (401 MISSING_CREDENTIAL
// / 403 SCOPE_REQUIRED) or the router never matched (404 NOT_FOUND). Anything
// else means the request reached a face it had no credential for.
func refusedBeforeTheHandler(status int, code string) bool {
	switch code {
	case problem.CodeMissingCredential, problem.CodeInvalidCredential, problem.CodeScopeRequired:
		return true
	case problem.CodeNotFound, problem.CodeMethodNotAllowed:
		return status == http.StatusNotFound || status == http.StatusMethodNotAllowed
	}
	return false
}

// The whole /v2/catalog surface answered 200 to an anonymous GET whose first
// segment after /v2 was capitalised, because the gate read c.Path() while fiber
// routed on its lowercased detectionPath. This walks every published path in
// both variant forms so no future face can reintroduce the class.
func TestNoV2PathVariantIsReachableWithoutCredentials(t *testing.T) {
	app := gateWalkApp(t, nil)
	paths := specV2Paths(t)

	gated := 0
	for _, specPath := range paths {
		path := concretePath(t, specPath)
		status, code := walkGet(t, app, path, "")
		if status != http.StatusUnauthorized || code != problem.CodeMissingCredential {
			continue
		}
		gated++

		variants := []string{path + "/", path + "//"}
		if v, ok := caseVariantPath(path); ok {
			variants = append(variants, v)
		}
		for _, v := range variants {
			vs, vc := walkGet(t, app, v, "")
			if !refusedBeforeTheHandler(vs, vc) {
				t.Errorf("GET %s answered %d %s with no credential; canonical %s is 401 %s",
					v, vs, vc, path, problem.CodeMissingCredential)
			}
		}
	}

	// Positive control: a walk that matched nothing would pass green. These two
	// are the paths production actually served anonymously.
	status, code := walkGet(t, app, "/v2/catalog/works", "")
	require.Equal(t, http.StatusUnauthorized, status)
	require.Equal(t, problem.CodeMissingCredential, code)
	status, code = walkGet(t, app, "/v2/catalog/claim-events", "")
	require.Equal(t, http.StatusUnauthorized, status)
	require.Equal(t, problem.CodeMissingCredential, code)
	require.Greater(t, gated, 40, "the walk exercised too few gated paths to mean anything")

	// Negative control on the other side: the unauthenticated faces and the
	// keyless allowlist must still reach their handlers, or "everything 401s"
	// would masquerade as a fix.
	status, _ = walkGet(t, app, "/v2/problems", "")
	require.Equal(t, http.StatusOK, status)
	status, _ = walkGet(t, app, "/v2/vocabularies", "")
	require.Equal(t, http.StatusOK, status)
	_, code = walkGet(t, app, "/v2/catalog/schemas/work", "")
	require.NotEqual(t, problem.CodeMissingCredential, code)
}

// /v2/catalog/claim-events carries actor uids and decline reasons across every
// tenant. The extra scope was checked with `path == "/v2/catalog/claim-events"`,
// so one trailing slash routed to the same handler and skipped the check.
func TestClaimEventsOperatorScopeSurvivesPathVariants(t *testing.T) {
	selfService := &devapi.Credential{
		KeyID: 1, ClientID: "some-app", Tier: devapi.TierFree,
		Scopes: []string{devapi.ScopeCatalogRead},
	}
	app := gateWalkApp(t, selfService)
	key := mustV2Key(t)

	status, code := walkGet(t, app, "/v2/catalog/claim-events", key)
	require.Equal(t, http.StatusForbidden, status, "positive control: catalog:read alone must not read the operator feed")
	require.Equal(t, problem.CodeScopeRequired, code)

	for _, variant := range []string{
		"/v2/catalog/claim-events/",
		"/v2/catalog/claim-events//",
		"/v2/Catalog/claim-events",
	} {
		vs, vc := walkGet(t, app, variant, key)
		require.True(t, refusedBeforeTheHandler(vs, vc),
			"GET %s answered %d %s and reached the operator feed with %v", variant, vs, vc, selfService.Scopes)
	}

	// The same key still reads the ordinary catalog faces, so the guard is not
	// simply refusing everything.
	_, code = walkGet(t, app, "/v2/catalog/works", key)
	require.NotEqual(t, problem.CodeScopeRequired, code)
}

// A nil credential store used to mean "let everyone through".
func TestCatalogAuthFailsClosedWithoutACredentialStore(t *testing.T) {
	app := fiber.New(kunapp.FiberConfig("kun-catalog"))
	SetupWith(app, Options{})
	key := mustV2Key(t)

	for _, path := range []string{"/v2/catalog/works", "/v2/store/stats"} {
		status, code := walkGet(t, app, path, key)
		require.Equal(t, http.StatusServiceUnavailable, status, path)
		require.Equal(t, problem.CodeServiceUnavailable, code, path)
	}
	// Still 401 without a token at all, and the keyless allowlist is unaffected.
	status, code := walkGet(t, app, "/v2/catalog/works", "")
	require.Equal(t, http.StatusUnauthorized, status)
	require.Equal(t, problem.CodeMissingCredential, code)
	_, code = walkGet(t, app, "/v2/catalog/schemas/work", "")
	require.NotEqual(t, problem.CodeMissingCredential, code)
}

func TestRoutedPathMirrorsFiberDetectionPath(t *testing.T) {
	for raw, want := range map[string]string{
		"/v2/catalog/works":         "/v2/catalog/works",
		"/v2/Catalog/works":         "/v2/catalog/works",
		"/v2/CATALOG/WORKS":         "/v2/catalog/works",
		"/v2/catalog/claim-events/": "/v2/catalog/claim-events",
		"/v2/catalog/works//":       "/v2/catalog/works",
		"/":                         "/",
	} {
		require.Equal(t, want, routepath.Normalize(raw), "routepath.Normalize(%q)", raw)
	}
}
