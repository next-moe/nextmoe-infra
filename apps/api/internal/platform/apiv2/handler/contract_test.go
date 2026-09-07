package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/devapi"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

// A nil LookupCredential used to mean "gate open", which is what these tests
// leaned on when they sent `Bearer test`. It now answers 503, so the shared
// test app carries a real credential store.
var testAPIKey = func() string {
	k, err := devapi.GenerateV2Key(false)
	if err != nil {
		panic(err)
	}
	return k
}()

func testCredentialLookup(_ context.Context, raw string) (*devapi.Credential, error) {
	if raw == testAPIKey {
		return &devapi.Credential{
			KeyID: 99, ClientID: "test-app", Tier: devapi.TierInternal,
			Scopes: []string{devapi.ScopeCatalogRead, devapi.ScopeStoreRead},
		}, nil
	}
	return nil, nil
}

func testApp(t *testing.T) *fiber.App {
	t.Helper()
	app := fiber.New(fiber.Config{ErrorHandler: problem.WriteFiberError})
	SetupWith(app, Options{LookupCredential: testCredentialLookup})
	return app
}

func do(t *testing.T, app *fiber.App, method, path string) (int, string, []byte) {
	t.Helper()
	return doAs(t, app, method, path, "")
}

func doAs(t *testing.T, app *fiber.App, method, path, token string) (int, string, []byte) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := app.Test(req)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, resp.Header.Get("Content-Type"), body
}

func contractURL(t *testing.T, path string) string {
	t.Helper()
	url := path
	url = strings.ReplaceAll(url, "{code}", problem.CodeRateLimited)
	url = strings.ReplaceAll(url, "{name}", "medium")
	url = strings.ReplaceAll(url, "{id}", "1")
	url = strings.ReplaceAll(url, "{object}", "work")
	url = strings.ReplaceAll(url, "{work_id}", "1")
	url = strings.ReplaceAll(url, "{cover_id}", "1")
	url = strings.ReplaceAll(url, "{product_id}", "RJ01000000")
	url = strings.ReplaceAll(url, "{uid}", "1")
	if strings.Contains(url, "{") {
		t.Fatalf("unsubstituted path param in %s", url)
	}
	return url
}

// contractWalk drives every operation the document declares and returns the
// status each one answered, keyed "METHOD path".
func contractWalk(t *testing.T, app *fiber.App, doc *huma.OpenAPI, token string) map[string]int {
	t.Helper()
	got := map[string]int{}
	for path, item := range doc.Paths {
		for _, op := range pathOps(item) {
			method := op.Method
			if method == "" && item.Get == op {
				method = http.MethodGet
			}
			status, ct, body := doAs(t, app, method, contractURL(t, path), token)
			got[method+" "+path] = status
			declared := make([]string, 0, len(op.Responses))
			for code := range op.Responses {
				declared = append(declared, code)
			}
			if _, ok := op.Responses[itoa(status)]; !ok {
				t.Errorf("%s %s returned %d which is not in the declared set %v body=%s", method, path, status, declared, body)
			}
			if status >= 400 {
				if !strings.Contains(ct, "application/problem+json") {
					t.Errorf("%s %s error content-type %q", method, path, ct)
				}
				var p problem.Problem
				require.NoError(t, json.Unmarshal(body, &p), string(body))
				if p.Code == "" || p.Type == "" || p.Status != status {
					t.Errorf("%s %s problem %+v", method, path, p)
				}
			} else if status != http.StatusNoContent && !strings.Contains(ct, "json") {
				t.Errorf("%s %s success content-type %q", method, path, ct)
			}
		}
	}
	require.NotEmpty(t, got)
	return got
}

func TestContractHitsEverySpecOperation(t *testing.T) {
	app := fiber.New(fiber.Config{ErrorHandler: problem.WriteFiberError})
	doc := Setup(app).OpenAPI()
	require.NotEmpty(t, doc.Paths)
	contractWalk(t, app, doc, "")
}

const contractUserToken = "contract-user-token"

// The walk above sends no credential, so everything it asserts is an assertion
// about the anonymous projection — the credentialed contract had no test at
// all, and a gate that refused a valid credential would have looked exactly
// like the surface working. All three arms run against the SAME app, so the
// only variable is the header. There are two credential kinds and an operation
// takes one of them, never both: an application key on /v2/me is a 401 by
// design, which is why the assertion is "some credential opens it" rather than
// "the key opens it".
func TestContractCredentialedArm(t *testing.T) {
	app := fiber.New(fiber.Config{ErrorHandler: problem.WriteFiberError})
	doc := SetupWith(app, Options{
		// Three walks of 88 operations from one httptest address, most of them
		// a 401: the default store's auth-failure budget is 120 a minute, so
		// arms two and three came back 429 across the board and the comparison
		// was between two blocked runs.
		Store:            &liveUnlimitedStore{},
		LookupCredential: testCredentialLookup,
		LookupUser: func(_ context.Context, raw string) (UserIdentity, error) {
			if raw == contractUserToken {
				return UserIdentity{UID: 7, ClientID: "test-app", Roles: []string{"admin"}}, nil
			}
			return UserIdentity{}, os.ErrPermission
		},
		LookupSite: func(context.Context, string) (SiteBinding, error) {
			return SiteBinding{Site: "kungal"}, nil
		},
	}).OpenAPI()

	anon := contractWalk(t, app, doc, "")
	keyed := contractWalk(t, app, doc, testAPIKey)
	user := contractWalk(t, app, doc, contractUserToken)
	require.Equal(t, len(anon), len(keyed))
	require.Equal(t, len(anon), len(user))

	var gated, moved []string
	for op, status := range anon {
		if keyed[op] != status || user[op] != status {
			moved = append(moved, fmt.Sprintf("%s anon=%d key=%d user=%d", op, status, keyed[op], user[op]))
		}
		if status != http.StatusUnauthorized {
			continue
		}
		gated = append(gated, op)
		require.Falsef(t,
			keyed[op] == http.StatusUnauthorized && user[op] == http.StatusUnauthorized,
			"%s answers MISSING_CREDENTIAL to every credential this API issues", op)
	}
	sort.Strings(gated)
	sort.Strings(moved)
	require.NotEmpty(t, gated, "control: the walk must reach at least one credential-gated operation")
	t.Logf("credential-gated operations: %d of %d", len(gated), len(anon))
	t.Logf("status differs between arms on %d operations:\n%s", len(moved), strings.Join(moved, "\n"))
}

func TestProblemsAndVocabularies(t *testing.T) {
	app := testApp(t)

	status, _, body := do(t, app, http.MethodGet, "/v2/problems?limit=100")
	require.Equal(t, 200, status)
	var list struct {
		Object string `json:"object"`
		Items  []struct {
			Code   string `json:"code"`
			Type   string `json:"type"`
			Status int    `json:"status"`
			Domain string `json:"domain"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(body, &list))
	require.Equal(t, "list", list.Object)
	require.Len(t, list.Items, len(problem.Codes))

	status, ct, body := do(t, app, http.MethodGet, "/v2/problems/"+problem.CodeRateLimited)
	require.Equal(t, 200, status)
	require.Contains(t, ct, "json")
	var one struct {
		Object string `json:"object"`
		Code   string `json:"code"`
		Type   string `json:"type"`
		Status int    `json:"status"`
	}
	require.NoError(t, json.Unmarshal(body, &one))
	require.Equal(t, "problem_type", one.Object)
	require.Equal(t, problem.CodeRateLimited, one.Code)
	require.Equal(t, 429, one.Status)
	require.True(t, strings.HasSuffix(one.Type, "/platform/rate-limited"))

	status, ct, body = do(t, app, http.MethodGet, "/v2/problems/NOT_A_CODE")
	require.Equal(t, 404, status)
	require.Contains(t, ct, "application/problem+json")
	var p problem.Problem
	require.NoError(t, json.Unmarshal(body, &p), string(body))
	require.Equal(t, problem.CodeNotFound, p.Code)
	require.Equal(t, 404, p.Status)
	require.True(t, strings.HasPrefix(p.RequestID, "req_"))
	require.Len(t, strings.TrimPrefix(p.RequestID, "req_"), 26)

	status, _, body = do(t, app, http.MethodGet, "/v2/problems/reasons")
	require.Equal(t, 200, status)
	var reasons struct {
		Items []struct {
			Reason string `json:"reason"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(body, &reasons))
	require.Len(t, reasons.Items, len(problem.Reasons))

	status, _, body = do(t, app, http.MethodGet, "/v2/vocabularies?limit=100")
	require.Equal(t, 200, status)
	var vocabs struct {
		Items []struct {
			Name   string `json:"name"`
			Closed bool   `json:"closed"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(body, &vocabs))
	require.Greater(t, len(vocabs.Items), 0)

	status, _, _ = do(t, app, http.MethodGet, "/v2/vocabularies/medium")
	require.Equal(t, 200, status)
	status, ct, body = do(t, app, http.MethodGet, "/v2/vocabularies/nope")
	require.Equal(t, 404, status)
	require.Contains(t, ct, "application/problem+json")
	require.NoError(t, json.Unmarshal(body, &p))
	require.Equal(t, problem.CodeNotFound, p.Code)

	status, h := func() (int, http.Header) {
		req := httptest.NewRequest(http.MethodGet, "/v2/problems", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		return resp.StatusCode, resp.Header
	}()
	require.Equal(t, 200, status)
	require.True(t, strings.HasPrefix(h.Get("X-Request-ID"), "req_"))
	require.Equal(t, "public, max-age=300, s-maxage=1800, stale-while-revalidate=3600", h.Get("Cache-Control"))
	require.Equal(t, "Authorization, Accept-Encoding", h.Get("Vary"))
	require.Contains(t, h.Get("Link"), `rel="service-desc"`)
	require.NotEmpty(t, h.Get("ETag"))
	require.Equal(t, "*", h.Get("Access-Control-Allow-Origin"))

	status, ct, body = do(t, app, http.MethodPost, "/v2/problems")
	require.Equal(t, 405, status)
	require.Contains(t, ct, "application/problem+json")
	require.NoError(t, json.Unmarshal(body, &p), string(body))
	require.Equal(t, problem.CodeMethodNotAllowed, p.Code)

	status, ct, body = do(t, app, http.MethodGet, "/v2/does-not-exist")
	require.Equal(t, 404, status)
	require.Contains(t, ct, "application/problem+json")
	require.NoError(t, json.Unmarshal(body, &p), string(body))
	require.Equal(t, problem.CodeNotFound, p.Code)
}

func TestCollectionPaginationAndBatch(t *testing.T) {
	app := testApp(t)

	status, _, body := do(t, app, http.MethodGet, "/v2/problems?limit=2")
	require.Equal(t, 200, status)
	var page struct {
		Items []struct {
			Code string `json:"code"`
		} `json:"items"`
		NextCursor *string `json:"next_cursor"`
		Total      *int64  `json:"total"`
	}
	require.NoError(t, json.Unmarshal(body, &page))
	require.Len(t, page.Items, 2)
	require.NotNil(t, page.NextCursor)
	require.Nil(t, page.Total)

	status, _, body = do(t, app, http.MethodGet, "/v2/problems?limit=2&cursor="+*page.NextCursor+"&include_total=true")
	require.Equal(t, 200, status)
	require.NoError(t, json.Unmarshal(body, &page))
	require.Len(t, page.Items, 2)
	require.NotNil(t, page.Total)
	require.Equal(t, int64(len(problem.Codes)), *page.Total)

	status, ct, body := do(t, app, http.MethodGet, "/v2/problems?limit=101")
	require.Equal(t, 400, status)
	require.Contains(t, ct, "application/problem+json")
	var p problem.Problem
	require.NoError(t, json.Unmarshal(body, &p))
	require.Equal(t, problem.CodeLimitTooLarge, p.Code)

	status, _, body = do(t, app, http.MethodGet, "/v2/vocabularies?ids=medium,nope")
	require.Equal(t, 200, status)
	var batch struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
		Missing []string `json:"missing"`
	}
	require.NoError(t, json.Unmarshal(body, &batch))
	require.Len(t, batch.Items, 1)
	require.Equal(t, "medium", batch.Items[0].Name)
	require.Equal(t, []string{"nope"}, batch.Missing)
}

func TestWorksRequiresCredential(t *testing.T) {
	app := testApp(t)
	status, ct, body := do(t, app, http.MethodGet, "/v2/catalog/works")
	require.Equal(t, 401, status)
	require.Contains(t, ct, "application/problem+json")
	var p problem.Problem
	require.NoError(t, json.Unmarshal(body, &p))
	require.Equal(t, problem.CodeMissingCredential, p.Code)

	req := httptest.NewRequest(http.MethodGet, "/v2/catalog/works?limit=101", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, 400, resp.StatusCode)

	req = httptest.NewRequest(http.MethodGet, "/v2/catalog/works?nsfw=true", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	resp, err = app.Test(req)
	require.NoError(t, err)
	body, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, 503, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "application/problem+json")
	require.NoError(t, json.Unmarshal(body, &p))
	require.Equal(t, problem.CodeServiceUnavailable, p.Code)

	req = httptest.NewRequest(http.MethodGet, "/v2/catalog/works/1?nsfw=yes", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	resp, err = app.Test(req)
	require.NoError(t, err)
	body, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, 400, resp.StatusCode)
	require.NoError(t, json.Unmarshal(body, &p))
	require.Equal(t, problem.CodeInvalidParameter, p.Code)
}

func TestCatalogStatsAndNewsUnauthenticated(t *testing.T) {
	app := testApp(t)

	status, ct, body := do(t, app, http.MethodGet, "/v2/catalog/stats")
	require.Equal(t, 503, status)
	require.Contains(t, ct, "application/problem+json")
	var p problem.Problem
	require.NoError(t, json.Unmarshal(body, &p))
	require.Equal(t, problem.CodeServiceUnavailable, p.Code)

	status, ct, body = do(t, app, http.MethodGet, "/v2/catalog/schemas/work")
	require.Equal(t, 503, status)
	require.Contains(t, ct, "application/problem+json")
	require.NoError(t, json.Unmarshal(body, &p))
	require.Equal(t, problem.CodeServiceUnavailable, p.Code)

	status, ct, body = do(t, app, http.MethodGet, "/v2/news/sources")
	require.Equal(t, 503, status)
	require.Contains(t, ct, "application/problem+json")
	require.NoError(t, json.Unmarshal(body, &p))
	require.Equal(t, problem.CodeServiceUnavailable, p.Code)

	status, _, body = do(t, app, http.MethodGet, "/v2/news")
	require.Equal(t, 503, status)
	require.NoError(t, json.Unmarshal(body, &p))
	require.Equal(t, problem.CodeServiceUnavailable, p.Code)

	status, ct, body = do(t, app, http.MethodGet, "/v2/catalog/works/1")
	require.Equal(t, 401, status)
	require.Contains(t, ct, "application/problem+json")
	require.NoError(t, json.Unmarshal(body, &p))
	require.Equal(t, problem.CodeMissingCredential, p.Code)

	req := httptest.NewRequest(http.MethodGet, "/v2/catalog/works/1", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	resp, err := app.Test(req)
	require.NoError(t, err)
	body, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, 503, resp.StatusCode)
	require.NoError(t, json.Unmarshal(body, &p))
	require.Equal(t, problem.CodeServiceUnavailable, p.Code)

	req = httptest.NewRequest(http.MethodGet, "/v2/catalog/companies/1", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	resp, err = app.Test(req)
	require.NoError(t, err)
	require.Equal(t, 503, resp.StatusCode)

	status, ct, body = do(t, app, http.MethodGet, "/v2/catalog/companies/1/graph")
	require.Equal(t, 401, status)
	require.Contains(t, ct, "application/problem+json")
	require.NoError(t, json.Unmarshal(body, &p))
	require.Equal(t, problem.CodeMissingCredential, p.Code)

	req = httptest.NewRequest(http.MethodGet, "/v2/catalog/companies/1/graph", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	resp, err = app.Test(req)
	require.NoError(t, err)
	require.Equal(t, 503, resp.StatusCode)
}

func TestCatalogCollectionsRequireCredential(t *testing.T) {
	app := testApp(t)
	for _, path := range []string{
		"/v2/catalog/companies",
		"/v2/catalog/tags",
		"/v2/catalog/series",
		"/v2/catalog/engines",
		"/v2/catalog/releases",
		"/v2/catalog/changes",
		"/v2/catalog/redirects",
		"/v2/catalog/calendar",
		"/v2/catalog/search?object=work",
		"/v2/catalog/characters",
		"/v2/catalog/credit-names",
		"/v2/catalog/persons",
		"/v2/catalog/traits",
	} {
		status, ct, body := do(t, app, http.MethodGet, path)
		require.Equal(t, 401, status, path)
		require.Contains(t, ct, "application/problem+json", path)
		var p problem.Problem
		require.NoError(t, json.Unmarshal(body, &p), path)
		require.Equal(t, problem.CodeMissingCredential, p.Code, path)

		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+testAPIKey)
		resp, err := app.Test(req)
		require.NoError(t, err, path)
		require.Equal(t, 503, resp.StatusCode, path)
	}
}

func TestWorkSubresourcesRequireCredential(t *testing.T) {
	app := testApp(t)
	status, ct, body := do(t, app, http.MethodGet, "/v2/catalog/works/1/tags")
	require.Equal(t, 401, status)
	require.Contains(t, ct, "application/problem+json")
	var p problem.Problem
	require.NoError(t, json.Unmarshal(body, &p))
	require.Equal(t, problem.CodeMissingCredential, p.Code)

	req := httptest.NewRequest(http.MethodGet, "/v2/catalog/works/1/covers", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, 503, resp.StatusCode)

	req = httptest.NewRequest(http.MethodGet, "/v2/catalog/works/1?include=nope", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	resp, err = app.Test(req)
	require.NoError(t, err)
	body, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, 400, resp.StatusCode)
	require.NoError(t, json.Unmarshal(body, &p))
	require.Equal(t, problem.CodeUnknownInclude, p.Code)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
