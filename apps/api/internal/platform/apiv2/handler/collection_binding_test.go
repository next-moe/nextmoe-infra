package handler

import (
	"context"
	"encoding/json"
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

// The parameter set of the shared CollectionInput. A face declaring all of
// these is a full collection lane.
var fullCollectionParams = []string{
	"cursor", "limit", "view", "include", "fields",
	"ids", "refs", "include_total", "facets", "sort",
}

// WorkSubInput, the narrow lane the twelve work sub-faces plus appearances and
// credit-names/{id}/credits share.
var subCollectionParams = []string{"cursor", "limit", "nsfw"}

// The three faces with an input of their own. Named rather than inferred: the
// list this file used to carry was six hand-written paths against a document
// that declares forty collection GETs, so /v2/me/news and thirty-three others
// were never checked at all. A fourth custom face is a failure here until it is
// named and its reason written down.
var customCollectionFaces = map[string]string{
	"/v2/catalog/claim-events": "operator feed: ids= but no refs=, and no nsfw/facets/include",
	"/v2/catalog/proposals":    "edit-history feed: no refs=, nsfw or facets",
	"/v2/catalog/revisions":    "edit-history feed: no refs=, nsfw or facets",
	"/v2/folders/holders":      "s2s fan-out lane: work_id=, cursor= and limit= and nothing else — every other parameter would widen what the uid list discloses",
}

func bindingApp(t *testing.T) (*fiber.App, huma.API, string, string) {
	t.Helper()
	appKey := mustV2Key(t)
	userToken := "binding-user-token"
	app := fiber.New(fiber.Config{ErrorHandler: problem.WriteFiberError})
	api := SetupWith(app, Options{
		Store: &liveUnlimitedStore{},
		LookupCredential: func(_ context.Context, raw string) (*devapi.Credential, error) {
			if raw == appKey {
				return &devapi.Credential{
					KeyID: 1,
					Scopes: []string{
						devapi.ScopeCatalogRead, devapi.ScopeStoreRead, devapi.ScopeClaimEventsRead,
						devapi.ScopeFolderHoldersRead,
					},
				}, nil
			}
			return nil, nil
		},
		LookupUser: func(_ context.Context, raw string) (UserIdentity, error) {
			if raw != userToken {
				return UserIdentity{}, os.ErrPermission
			}
			// This walk covers every collection face, so the token has to clear
			// both scope gates on /v2/me: folders and the editing plane.
			return UserIdentity{UID: 7, ClientID: "kungal-client",
				Scopes: []string{devapi.ScopeFolderRead, devapi.ScopeCatalogEdit}}, nil
		},
		LookupSite: func(context.Context, string) (SiteBinding, error) { return SiteBinding{Site: "kungal"}, nil },
	})
	return app, api, appKey, userToken
}

func queryParams(op *huma.Operation) map[string]bool {
	out := map[string]bool{}
	for _, p := range op.Parameters {
		if p != nil && p.In == "query" {
			out[p.Name] = true
		}
	}
	return out
}

func declaresAll(got map[string]bool, want []string) bool {
	for _, name := range want {
		if !got[name] {
			return false
		}
	}
	return true
}

// collectionFaces classifies every GET that declares a cursor. A path in
// neither family and not named in customCollectionFaces fails the test.
func collectionFaces(t *testing.T, doc *huma.OpenAPI) (full, sub, custom []string) {
	t.Helper()
	for path, item := range doc.Paths {
		if item.Get == nil {
			continue
		}
		got := queryParams(item.Get)
		if !got["cursor"] {
			continue
		}
		switch {
		case declaresAll(got, fullCollectionParams) && got["nsfw"]:
			full = append(full, path)
		case declaresAll(got, subCollectionParams):
			sub = append(sub, path)
		default:
			reason, named := customCollectionFaces[path]
			require.Truef(t, named,
				"GET %s declares cursor but neither the full nor the sub collection parameter set: %v",
				path, sortedKeys(got))
			require.NotEmpty(t, reason, path)
			require.Truef(t, got["limit"], "%s must at least page", path)
			custom = append(custom, path)
		}
	}
	sort.Strings(full)
	sort.Strings(sub)
	sort.Strings(custom)
	require.NotEmpty(t, full)
	require.NotEmpty(t, sub)
	require.Len(t, custom, len(customCollectionFaces),
		"every named custom face must still be a registered collection GET")
	return full, sub, custom
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func bindingToken(path, appKey, userToken string) string {
	if strings.HasPrefix(path, "/v2/me/") || strings.HasPrefix(path, "/v2/moderation/") {
		return userToken
	}
	return appKey
}

func bindingGet(t *testing.T, app *fiber.App, path, token string) (int, problem.Problem, []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := app.Test(req)
	require.NoError(t, err, path)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, path)
	var p problem.Problem
	_ = json.Unmarshal(body, &p)
	return resp.StatusCode, p, body
}

func TestEmbeddedCollectionParamsAreDocumented(t *testing.T) {
	_, api, _, _ := bindingApp(t)
	full, sub, custom := collectionFaces(t, api.OpenAPI())
	want := append(append([]string{}, fullCollectionParams...), "nsfw")
	for _, path := range full {
		got := queryParams(api.OpenAPI().Paths[path].Get)
		for _, name := range want {
			require.Truef(t, got[name], "%s is missing the %s query parameter", path, name)
		}
	}
	t.Logf("collection GETs: %d full, %d sub, %d custom", len(full), len(sub), len(custom))
}

// A parameter huma declares and never binds is the failure this asserts:
// limit=101 is the one value with a dedicated problem code, so a 400
// LIMIT_TOO_LARGE proves the query string reached collect.Parse.
func TestEmbeddedCollectionParamsBind(t *testing.T) {
	app, api, appKey, userToken := bindingApp(t)
	full, sub, custom := collectionFaces(t, api.OpenAPI())
	for _, tmpl := range append(append(append([]string{}, full...), sub...), custom...) {
		path := concretePath(t, tmpl)
		status, p, body := bindingGet(t, app, path+"?limit=101", bindingToken(tmpl, appKey, userToken))
		require.Equalf(t, http.StatusBadRequest, status, "%s %s", tmpl, body)
		require.Equalf(t, problem.CodeLimitTooLarge, p.Code, "%s %s", tmpl, body)
	}
}

// Deviation 82 traded a shared CollectionInput for a batch lane declared on
// every list operation, including the ones that answer 400 to it. That trade is
// only safe while a declared ids= is either honoured or refused: a face that
// declares it, parses it and ignores it answers 200 to a hydration request with
// items it did not ask for — which is what /v2/news did until this test.
var batchRefusingFaces = map[string]string{
	"/v2/catalog/calendar":      "feedNoBatch: the calendar is a window, not a set of ids",
	"/v2/catalog/changes":       "feedNoBatch: mirror feed, cursor only",
	"/v2/catalog/redirects":     "feedNoBatch: mirror feed, cursor only",
	"/v2/folders":               "collect.FolderSpec is NoBatch; owner_uid= is how this face is addressed",
	"/v2/folders/{id}/items":    "collect.FolderItemSpec is NoBatch, the same spec the owner lane uses",
	"/v2/me/claims":             "collect.ClaimSpec is NoBatch",
	"/v2/me/folders":            "collect.FolderSpec is NoBatch",
	"/v2/me/folders/{id}/items": "collect.FolderItemSpec is NoBatch; POST items is this lane's batch",
	"/v2/me/news":               "collect.NewsSubmissionSpec is NoBatch",
	"/v2/me/playtimes":          "collect.PlaytimeSpec is NoBatch; work_ids= is this lane's batch",
	"/v2/me/work-states":        "collect.WorkStateSpec is NoBatch; work_ids= is this lane's batch",
	"/v2/me/proposals":          "collect.ProposalListSpec is NoBatch; the list lane has no hydration",
	"/v2/moderation/claims":     "collect.ClaimSpec is NoBatch",
	"/v2/moderation/proposals":  "collect.ProposalListSpec is NoBatch; the list lane has no hydration",
	"/v2/news":                  "collect.NewsSpec is NoBatch: the public feed has no hydration lane either",

	"/v2/moderation/users/{uid}/folders": "collect.FolderSpec is NoBatch, the same spec the owner lane uses",
}

// The sibling of the ids= guard, and it caught the same face: /v2/news built
// its page with repr.NewList instead of finishList, so it was the one operation
// of twenty-six declaring include_total= that never answered a total — on a
// parameter the forum's news client sets on every request.
func TestDeclaredIncludeTotalIsHonoured(t *testing.T) {
	env := liveCatalog(t)
	specApp := fiber.New(fiber.Config{ErrorHandler: problem.WriteFiberError})
	doc := SetupWith(specApp, Options{Catalog: env.cat}).OpenAPI()

	checked, skipped := 0, 0
	for path, item := range doc.Paths {
		if item.Get == nil || !queryParams(item.Get)["include_total"] {
			continue
		}
		url := liveReadURL(t, path, env.fx)
		join := "?"
		if strings.Contains(url, "?") {
			join = "&"
		}
		status, _, raw := liveDo(t, env, http.MethodGet, url+join+"include_total=true&limit=1", liveAuthPath(path), "")
		if _, ok := batchUnobservable[path]; ok {
			require.Equal(t, http.StatusServiceUnavailable, status, path)
			skipped++
			continue
		}
		require.Equalf(t, http.StatusOK, status, "%s %s", path, raw)
		var page struct {
			Items []json.RawMessage `json:"items"`
			Total *int64            `json:"total"`
		}
		require.NoError(t, json.Unmarshal(raw, &page), path)
		require.NotNilf(t, page.Total, "%s declares include_total= and answered no total", path)
		// A total below the page it came with is a count that is not counting —
		// the failure deviation 89 fixed, one page earlier.
		require.GreaterOrEqualf(t, *page.Total, int64(len(page.Items)),
			"%s: total %d is smaller than the page it came with", path, *page.Total)
		checked++
	}
	require.NotZero(t, checked)
	t.Logf("include_total honoured on %d faces, %d unbound here", checked, skipped)
}

// The two proposal LIST lanes parsed with collect.ClaimSpec, so fields=
// validated against the claim record's keys on a face whose items are
// proposals: half the real keys were 400 UNKNOWN_FIELD and half the claim keys
// were accepted and then projected to nothing. The refusal set must not move
// with the vocabulary — ProposalListSpec keeps NoBatch, because these lanes
// build their items with proposalFrom and have no hydration lane.
func TestProposalListFieldVocabulary(t *testing.T) {
	env := liveCatalog(t)
	for _, path := range []string{"/v2/me/proposals", "/v2/moderation/proposals"} {
		for _, field := range []string{"state", "entity_id", "entity_type", "note", "proposer_uid", "decided_at"} {
			status, _, raw := liveDo(t, env, http.MethodGet, path+"?fields="+field, liveUserToken, "")
			require.Equalf(t, http.StatusOK, status, "%s?fields=%s must be a proposal key: %s", path, field, raw)
		}
		// The claim vocabulary is gone, not merely unused: accepting a key this
		// face never emits is the same silent projection-to-nothing.
		for _, field := range []string{"acted_count", "product_work_id", "last_event", "display_name"} {
			status, _, raw := liveDo(t, env, http.MethodGet, path+"?fields="+field, liveUserToken, "")
			require.Equalf(t, http.StatusBadRequest, status, "%s?fields=%s is a claim key: %s", path, field, raw)
			require.Equal(t, problem.CodeUnknownField, liveProblem(t, raw).Code, path)
		}
		// patch and amendments stay out: proposalFrom never fills them on a list.
		for _, field := range []string{"patch", "effective_patch", "amendments"} {
			status, _, raw := liveDo(t, env, http.MethodGet, path+"?fields="+field, liveUserToken, "")
			require.Equalf(t, http.StatusBadRequest, status, "%s?fields=%s: %s", path, field, raw)
		}
		status, _, raw := liveDo(t, env, http.MethodGet, path+"?sort=filed_desc", liveUserToken, "")
		require.Equalf(t, http.StatusOK, status, "%s: the one sort the lane actually applies: %s", path, raw)
		status, _, raw = liveDo(t, env, http.MethodGet, path+"?sort=bogus", liveUserToken, "")
		require.Equal(t, http.StatusBadRequest, status, path)
		require.Equal(t, problem.CodeUnknownSort, liveProblem(t, raw).Code, path)
	}
}

// The refusal is real in production and unreachable here: catalog_search.go
// answers 503 for an unbound service before it reaches feedNoBatch, and the
// live fixture binds no OpenSearch.
var batchUnobservable = map[string]string{
	"/v2/catalog/search": "feedNoBatch(\"search\") sits behind the unbound-service 503 in this fixture",
	"/v2/store/prices":   "the live env binds no price service, so the lane answers 503 here; TestLiveStorePrices covers missing[] over fake fetchers",
}

func TestDeclaredBatchLaneIsHonouredOrRefused(t *testing.T) {
	env := liveCatalog(t)
	specApp := fiber.New(fiber.Config{ErrorHandler: problem.WriteFiberError})
	doc := SetupWith(specApp, Options{Catalog: env.cat}).OpenAPI()

	refused := map[string]bool{}
	unobservable := map[string]bool{}
	var honoured []string
	for path, item := range doc.Paths {
		if item.Get == nil || !queryParams(item.Get)["ids"] {
			continue
		}
		url := liveReadURL(t, path, env.fx)
		join := "?"
		if strings.Contains(url, "?") {
			join = "&"
		}
		status, ct, raw := liveDo(t, env, http.MethodGet, url+join+"ids=98765432", liveAuthPath(path), "")
		if reason, ok := batchUnobservable[path]; ok {
			require.NotEmpty(t, reason, path)
			require.Equal(t, http.StatusServiceUnavailable, status, path)
			unobservable[path] = true
			continue
		}
		switch status {
		case http.StatusBadRequest:
			p := liveProblem(t, raw)
			require.Equal(t, problem.CodeInvalidParameter, p.Code, path)
			require.NotEmpty(t, p.Errors, path)
			require.Equal(t, "ids", p.Errors[0].Parameter, path)
			refused[path] = true
		case http.StatusOK:
			require.Contains(t, ct, "json", path)
			var page struct {
				Items      []json.RawMessage `json:"items"`
				Missing    *[]string         `json:"missing"`
				NextCursor *string           `json:"next_cursor"`
			}
			require.NoError(t, json.Unmarshal(raw, &page), path)
			require.Emptyf(t, page.Items, "%s answered a batch request for an absent id with items", path)
			require.NotNilf(t, page.Missing,
				"%s declares ids=, answered 200 and reported no missing[]: the lane is declared and ignored", path)
			require.Nil(t, page.NextCursor, path)
			honoured = append(honoured, path)
		default:
			t.Errorf("GET %s?ids= -> %d %s", path, status, raw)
		}
	}
	for path := range batchRefusingFaces {
		require.Truef(t, refused[path], "%s is listed as refusing ids= and did not", path)
	}
	for path := range refused {
		_, named := batchRefusingFaces[path]
		require.Truef(t, named, "%s refuses ids= and is not in batchRefusingFaces", path)
	}
	require.Len(t, unobservable, len(batchUnobservable),
		"every named unobservable face must still declare ids=")
	sort.Strings(honoured)
	require.NotEmpty(t, honoured, "control: at least one face must actually hydrate by ids=")
	t.Logf("ids= declared on %d faces: %d refuse, %d hydrate, %d unobservable here",
		len(refused)+len(honoured)+len(unobservable), len(refused), len(honoured), len(unobservable))
}
