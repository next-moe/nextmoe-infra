package handler

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

func liveOpMethod(item *huma.PathItem, op *huma.Operation) string {
	if op.Method != "" {
		return op.Method
	}
	switch {
	case item.Get == op:
		return http.MethodGet
	case item.Put == op:
		return http.MethodPut
	case item.Post == op:
		return http.MethodPost
	case item.Delete == op:
		return http.MethodDelete
	case item.Patch == op:
		return http.MethodPatch
	default:
		return http.MethodGet
	}
}

// liveReadPaths is templated, not concrete, so TestLiveReadsCoverEveryGet can
// compare it against the document. It was a hand-written list of concrete URLs
// and had fallen seventeen routes behind the route table, including the whole
// news lane and characters/{id}/appearances.
var liveReadPaths = []string{
	"/v2/problems",
	"/v2/problems/reasons",
	"/v2/problems/{code}",
	"/v2/vocabularies",
	"/v2/vocabularies/{name}",
	"/v2/catalog/stats",
	"/v2/catalog/schemas/{object}",
	"/v2/me/edit-capabilities/{object}",
	"/v2/catalog/works",
	"/v2/catalog/works/{id}",
	"/v2/catalog/works/{id}/covers",
	"/v2/catalog/works/{id}/screenshots",
	"/v2/catalog/works/{id}/tags",
	"/v2/catalog/works/{id}/characters",
	"/v2/catalog/works/{id}/credits",
	"/v2/catalog/works/{id}/releases",
	"/v2/catalog/works/{id}/intros",
	"/v2/catalog/works/{id}/ratings",
	"/v2/catalog/works/{id}/relations",
	"/v2/catalog/works/{id}/series",
	"/v2/catalog/works/{id}/links",
	"/v2/catalog/works/{id}/engines",
	"/v2/catalog/companies",
	"/v2/catalog/companies/{id}",
	"/v2/catalog/companies/{id}/graph",
	"/v2/catalog/tags",
	"/v2/catalog/tags/{id}",
	"/v2/catalog/series",
	"/v2/catalog/series/{id}",
	"/v2/catalog/engines",
	"/v2/catalog/engines/{id}",
	"/v2/catalog/roles",
	"/v2/catalog/roles/{id}",
	"/v2/catalog/releases",
	"/v2/catalog/releases/{id}",
	"/v2/catalog/characters",
	"/v2/catalog/characters/{id}",
	"/v2/catalog/characters/{id}/appearances",
	"/v2/catalog/credit-names",
	"/v2/catalog/credit-names/{id}",
	"/v2/catalog/credit-names/{id}/credits",
	"/v2/catalog/persons",
	"/v2/catalog/persons/{id}",
	"/v2/catalog/persons/{id}/credit-names",
	"/v2/catalog/traits",
	"/v2/catalog/traits/{id}",
	"/v2/catalog/calendar",
	"/v2/catalog/changes",
	"/v2/catalog/redirects",
	"/v2/catalog/claim-events",
	"/v2/catalog/proposals",
	"/v2/catalog/revisions",
	"/v2/news",
	"/v2/news/sources",
	"/v2/news/{id}",
	"/v2/folders",
	"/v2/folders/{id}",
	"/v2/folders/{id}/items",
	"/v2/me/playtimes",
	"/v2/me/folders",
	"/v2/me/folders/{id}",
	"/v2/me/folders/{id}/items",
	"/v2/me/cover-votes",
	"/v2/me/claims",
	"/v2/me/claims/{id}",
	"/v2/me/proposals",
	"/v2/me/news",
	"/v2/me/news/{id}",
	"/v2/moderation/claims",
	"/v2/moderation/claims/{id}",
	"/v2/moderation/proposals",
	"/v2/moderation/snapshots/{object}/{id}",
}

// The GET operations the fixture cannot bring to 200, each with the reason it
// is out. A new read route belongs in liveReadPaths or here; the completeness
// test refuses both "in neither" and "in both".
var liveReadsNotSwept = map[string]string{
	"/v2/catalog/proposals/{id}":            "no proposal survives seedLiveFixtures; TestLiveWrites200 creates the only one",
	"/v2/catalog/revisions/{id}":            "no revision survives seedLiveFixtures",
	"/v2/me/proposals/{id}":                 "same: no proposal id to address",
	"/v2/moderation/proposals/{id}":         "same: no proposal id to address",
	"/v2/me/playtimes/{work_id}":            "404 until a playtime is written, which is TestLiveWrites200's job",
	"/v2/catalog/search":                    "the live env binds no OpenSearch, so this face is 503 here",
	"/v2/store/purchase-links/{product_id}": "the live env binds no store service",
	"/v2/store/stats":                       "the live env binds no store service",
	"/v2/store/prices/{id}":                 "the live env binds no price service; live_wave_prices_test.go mounts one over fake fetchers",
	"/v2/store/prices":                      "the live env binds no price service; live_wave_prices_test.go mounts one over fake fetchers",
}

// A face whose contract makes a filter mandatory would answer the bare sweep
// with 400. Carrying the filter here keeps it swept; the alternative is a
// liveReadsNotSwept entry, which would exclude a route the fixture can reach.
var liveReadRequiredQuery = map[string]func(fx liveFix) string{
	"/v2/folders": func(liveFix) string { return "?owner_uid=" + idstr(liveUID) },
}

func liveReadURL(t *testing.T, tmpl string, fx liveFix) string {
	t.Helper()
	url := strings.NewReplacer(
		"{code}", problem.CodeRateLimited, "{name}", "medium", "{object}", "work",
		"{uid}", idstr(liveEmptyUID),
	).Replace(tmpl)
	// Longest-prefix, not liveSubstitute's substring switch: that one matches
	// "/tags" inside /v2/catalog/works/{id}/tags and addresses a tag id as if it
	// were a work.
	for _, e := range []struct {
		prefix string
		id     int64
	}{
		{"/v2/catalog/works/", fx.Work},
		{"/v2/catalog/companies/", fx.Company},
		{"/v2/catalog/tags/", fx.Tag},
		{"/v2/catalog/series/", fx.Series},
		{"/v2/catalog/engines/", fx.Engine},
		{"/v2/catalog/roles/", fx.Role},
		{"/v2/catalog/releases/", fx.Release},
		{"/v2/catalog/characters/", fx.Character},
		{"/v2/catalog/credit-names/", fx.Credit},
		{"/v2/catalog/persons/", fx.Person},
		{"/v2/catalog/traits/", fx.Trait},
		{"/v2/folders/", fx.FolderPublic},
		{"/v2/me/folders/", fx.Folder},
		{"/v2/me/claims/", fx.Pending},
		{"/v2/moderation/claims/", fx.Pending},
		{"/v2/moderation/snapshots/work/", fx.Work},
		{"/v2/me/news/", fx.NewsItem},
		{"/v2/news/", fx.NewsItem},
	} {
		if strings.HasPrefix(url, e.prefix) {
			url = strings.ReplaceAll(url, "{id}", idstr(e.id))
			break
		}
	}
	require.NotContainsf(t, url, "{", "unsubstituted path param in %s", tmpl)
	if q, ok := liveReadRequiredQuery[tmpl]; ok {
		url += q(fx)
	}
	return url
}

func TestLiveReads200(t *testing.T) {
	env := liveCatalog(t)
	for _, tmpl := range liveReadPaths {
		path := liveReadURL(t, tmpl, env.fx)
		status, ct, body := liveDo(t, env, http.MethodGet, path, liveAuthPath(tmpl), "")
		if status != 200 {
			t.Errorf("GET %s -> %d %s", path, status, body)
			continue
		}
		if !strings.Contains(ct, "json") {
			t.Errorf("GET %s content-type %q", path, ct)
		}
	}
	full := liveReadURL(t, "/v2/catalog/characters/{id}", env.fx) + "?view=full"
	status, _, body := liveDo(t, env, http.MethodGet, full, liveAppKey, "")
	require.Equal(t, 200, status, string(body))
}

func TestLiveReadsCoverEveryGet(t *testing.T) {
	env := liveCatalog(t)
	specApp := fiber.New(fiber.Config{ErrorHandler: problem.WriteFiberError})
	doc := SetupWith(specApp, Options{Catalog: env.cat}).OpenAPI()

	swept := map[string]bool{}
	for _, p := range liveReadPaths {
		require.Falsef(t, swept[p], "%s is listed twice", p)
		swept[p] = true
	}
	declared := map[string]bool{}
	for path, item := range doc.Paths {
		if item.Get == nil {
			continue
		}
		declared[path] = true
		reason, named := liveReadsNotSwept[path]
		require.Falsef(t, swept[path] && named, "%s is both swept and excluded", path)
		require.Truef(t, swept[path] || named,
			"GET %s is registered and neither swept by TestLiveReads200 nor named in liveReadsNotSwept", path)
		if named {
			require.NotEmptyf(t, reason, "%s needs a reason, not an empty string", path)
		}
	}
	for p := range swept {
		require.Truef(t, declared[p], "%s is swept but no longer a registered GET", p)
	}
	for p := range liveReadsNotSwept {
		require.Truef(t, declared[p], "%s is excluded but no longer a registered GET", p)
	}
	require.Equal(t, len(declared), len(swept)+len(liveReadsNotSwept))
	t.Logf("GET operations: %d swept, %d named exclusions", len(swept), len(liveReadsNotSwept))
}

func TestLiveWrites200(t *testing.T) {
	env := liveCatalog(t)
	fx := env.fx

	status, _, body := liveDo(t, env, http.MethodPut, "/v2/me/playtimes/"+idstr(fx.Work), liveUserToken, `{"minutes":12}`)
	require.Equal(t, 200, status, string(body))
	status, _, body = liveDo(t, env, http.MethodGet, "/v2/me/playtimes/"+idstr(fx.Work), liveUserToken, "")
	require.Equal(t, 200, status, string(body))
	status, _, body = liveDo(t, env, http.MethodPost, "/v2/me/playtimes", liveUserToken,
		`{"items":[{"work_id":"`+idstr(fx.Work)+`","minutes":15}]}`)
	require.Equal(t, 207, status, string(body))
	status, _, _ = liveDo(t, env, http.MethodDelete, "/v2/me/playtimes/"+idstr(fx.Work), liveUserToken, "")
	require.Equal(t, 204, status)

	status, _, body = liveDo(t, env, http.MethodPut, "/v2/me/cover-votes/"+idstr(fx.Cover), liveUserToken, `{"vote":"up"}`)
	require.Equal(t, 200, status, string(body))
	status, _, body = liveDo(t, env, http.MethodGet, "/v2/me/cover-votes", liveUserToken, "")
	require.Equal(t, 200, status, string(body))
	status, _, _ = liveDo(t, env, http.MethodDelete, "/v2/me/cover-votes/"+idstr(fx.Cover), liveUserToken, "")
	require.Equal(t, 204, status)

	status, _, body = liveDo(t, env, http.MethodPost, "/v2/me/claims", liveUserToken,
		`{"work_id":"`+idstr(fx.Claimable)+`","site_work_id":"20001"}`)
	require.Equal(t, 201, status, string(body))

	empty := datatypes.JSON([]byte("{}"))
	mintTarget := &model.CatalogWork{
		MediumID: 1, OLang: "ja", DisplayName: "Mint Target",
		ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive,
		Extra: empty, FieldProvenance: empty,
	}
	require.NoError(t, env.db.Create(mintTarget).Error)
	require.NoError(t, env.db.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeWork, EntityID: mintTarget.ID, SourceID: 2,
		ExternalID: "v77777", LinkKind: model.LinkKindExact, MatchedBy: "test",
	}).Error)
	status, _, body = liveDo(t, env, http.MethodPost, "/v2/me/claims", liveUserToken,
		`{"refs":[{"source":"vndb","external_id":"v77777"}],"site_work_id":"20002"}`)
	require.Equal(t, 201, status, string(body))

	status, _, body = liveDo(t, env, http.MethodPost, "/v2/me/claims", liveUserToken,
		`{"refs":[{"source":"vndb","external_id":"v123456"}],"display_name":"Minted From Refs","site_work_id":"20003"}`)
	require.Equal(t, 201, status, string(body))

	status, _, body = liveDo(t, env, http.MethodGet, "/v2/me/claims?limit=1", liveUserToken, "")
	require.Equal(t, 200, status, string(body))
	var page struct {
		Items      []json.RawMessage `json:"items"`
		NextCursor *string           `json:"next_cursor"`
	}
	require.NoError(t, json.Unmarshal(body, &page))
	require.Len(t, page.Items, 1)
	require.NotNil(t, page.NextCursor)
	status, _, body = liveDo(t, env, http.MethodGet, "/v2/me/claims?limit=1&cursor="+*page.NextCursor, liveUserToken, "")
	require.Equal(t, 200, status, string(body))

	status, _, body = liveDo(t, env, http.MethodGet, "/v2/moderation/claims/"+idstr(fx.Pending), liveUserToken, "")
	require.Equal(t, 200, status, string(body))

	etag := liveETag(t, env, "/v2/moderation/claims/"+idstr(fx.Pending), liveUserToken)
	status, _, body = liveDo(t, env, http.MethodPost, "/v2/moderation/claims/"+idstr(fx.Pending)+"/decisions", liveUserToken,
		`{"decision":"approve","note":"ok"}`)
	// missing If-Match
	require.Equal(t, 428, status, string(body))
	reqBody := `{"decision":"approve","note":"ok"}`
	status, _, body = liveDoHeader(t, env, http.MethodPost, "/v2/moderation/claims/"+idstr(fx.Pending)+"/decisions", liveUserToken, reqBody, map[string]string{"If-Match": etag})
	require.Equal(t, 201, status, string(body))

	status, _, body = liveDo(t, env, http.MethodPost, "/v2/me/proposals", liveUserToken, fmt.Sprintf(
		`{"entity_type":%q,"entity_id":%q,"patch":{%q:"Renamed Live Work"}}`,
		editspec.TypeWork, idstr(fx.Work), editspec.FieldWorkDisplayName))
	if status != 201 && status != 403 {
		t.Fatalf("create proposal %d %s", status, body)
	}
}

func TestLiveSpecWalk(t *testing.T) {
	env := liveCatalog(t)
	specApp := fiber.New(fiber.Config{ErrorHandler: problem.WriteFiberError})
	doc := SetupWith(specApp, Options{Catalog: env.cat}).OpenAPI()
	require.NotEmpty(t, doc.Paths)

	for path, item := range doc.Paths {
		for _, op := range pathOps(item) {
			method := liveOpMethod(item, op)
			url := liveSubstitute(path, env.fx)
			if strings.Contains(url, "{") {
				t.Fatalf("unsubstituted path param in %s", url)
			}
			token := liveAuthPath(path)
			var body string
			switch method {
			case http.MethodPost, http.MethodPut, http.MethodPatch:
				body = "{}"
			}
			status, ct, raw := liveDo(t, env, method, url, token, body)
			declared := make([]string, 0, len(op.Responses))
			for code := range op.Responses {
				declared = append(declared, code)
			}
			if _, ok := op.Responses[itoa(status)]; !ok {
				t.Errorf("%s %s returned %d not in %v body=%s", method, path, status, declared, raw)
			}
			if status >= 400 && !strings.Contains(ct, "application/problem+json") {
				t.Errorf("%s %s error content-type %q", method, path, ct)
			}
		}
	}
}

func TestLiveG11SameBytesAcrossKeys(t *testing.T) {
	env := liveCatalog(t)
	path := "/v2/catalog/works/" + idstr(env.fx.Work)
	statusA, _, a := liveDo(t, env, http.MethodGet, path, liveAppKey, "")
	statusB, _, b := liveDo(t, env, http.MethodGet, path, liveAppKeyB, "")
	require.Equal(t, 200, statusA, string(a))
	require.Equal(t, 200, statusB, string(b))
	require.Equal(t, a, b)
}

// Both folder lanes first shipped their raw keyset keys as next_cursor — a
// bare decimal id on the list, RFC3339Nano|work_id on items — against the
// ^cur_ pattern repr.List publishes, and no fixture overflowed a page, so
// nothing saw the emitted form until a limit=1 crawl.
// The prefix alone does not separate a correct cursor from one wrapped twice:
// a double-wrapped cursor round-trips and crawls green, and 2.12.0 shipped one
// on both folder lanes. The payload has to be opened to see it.
func requireOpaqueCursor(t *testing.T, where, cursor string) {
	t.Helper()
	payload, ok := repr.ParseCursor(cursor)
	require.True(t, ok, "raw cursor leaked on %s: %s", where, cursor)
	key, err := base64.RawURLEncoding.DecodeString(payload)
	require.NoError(t, err, "cursor payload is not base64 on %s: %s", where, cursor)
	require.NotEmpty(t, key, "empty cursor payload on %s", where)
	_, nested := repr.ParseCursor(string(key))
	require.False(t, nested, "double-wrapped cursor on %s: %s", where, string(key))
}

func TestFolderCursorsAreOpaque(t *testing.T) {
	env := liveCatalog(t)
	fx := env.fx

	// Own rows: the shared fixtures don't survive this package intact (the
	// spec walk deletes fx.FolderSpare), so the crawl seeds what it counts.
	f := &model.CatalogUserFolder{OwnerUID: liveUID, Name: "Cursor Crawl",
		Visibility: model.FolderVisibilityPrivate, ItemCount: 2}
	require.NoError(t, env.db.Create(f).Error)
	require.NoError(t, env.db.Create(&model.CatalogUserFolderItem{
		FolderID: f.ID, WorkID: fx.Work, OwnerUID: liveUID}).Error)
	require.NoError(t, env.db.Create(&model.CatalogUserFolderItem{
		FolderID: f.ID, WorkID: fx.ENWork, OwnerUID: liveUID}).Error)
	t.Cleanup(func() {
		env.db.Where("folder_id = ?", f.ID).Delete(&model.CatalogUserFolderItem{})
		env.db.Delete(f)
	})

	crawl := func(base string) int {
		t.Helper()
		total, cursor := 0, ""
		for i := 0; i < 25; i++ {
			url := base
			if cursor != "" {
				url += "&cursor=" + cursor
			}
			status, _, body := liveDo(t, env, http.MethodGet, url, liveUserToken, "")
			require.Equal(t, 200, status, string(body))
			var page struct {
				Items      []json.RawMessage `json:"items"`
				NextCursor *string           `json:"next_cursor"`
			}
			require.NoError(t, json.Unmarshal(body, &page))
			total += len(page.Items)
			if page.NextCursor == nil {
				return total
			}
			requireOpaqueCursor(t, base, *page.NextCursor)
			cursor = *page.NextCursor
		}
		t.Fatalf("crawl of %s did not terminate", base)
		return total
	}

	require.GreaterOrEqual(t, crawl("/v2/me/folders?limit=1"), 2)
	require.Equal(t, 2, crawl("/v2/me/folders/"+idstr(f.ID)+"/items?limit=1"))

	status, _, body := liveDo(t, env, http.MethodGet,
		"/v2/me/folders?limit=1&cursor="+idstr(f.ID), liveUserToken, "")
	require.Equal(t, 400, status, string(body))
}
