package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"api/internal/platform/catalog/editspec"

	"github.com/stretchr/testify/require"
)

func w3Header(t *testing.T, env *liveEnv, method, path, token string) (int, http.Header) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := env.app.Test(req)
	require.NoError(t, err)
	return resp.StatusCode, resp.Header
}

func w3Keys(t *testing.T, body []byte) []string {
	t.Helper()
	var page struct {
		Items []map[string]any `json:"items"`
	}
	require.NoError(t, json.Unmarshal(body, &page), string(body))
	require.NotEmpty(t, page.Items, "an empty page cannot prove a projection: %s", body)
	out := make([]string, 0, len(page.Items[0]))
	for k := range page.Items[0] {
		out = append(out, k)
	}
	return out
}

func w3ProposalIDs(t *testing.T, body []byte) []string {
	t.Helper()
	var page struct {
		Items []struct {
			EntityID string `json:"entity_id"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(body, &page), string(body))
	out := make([]string, 0, len(page.Items))
	for _, it := range page.Items {
		out = append(out, it.EntityID)
	}
	return out
}

// All three consumers send entity_id= here and forum also sends
// entity_type=catalog.work; none of the three was declared or read, so "existing
// proposals on this work" answered with the caller's proposals on every entity
// they had ever edited.
func TestLiveMyProposalsHonorTheEntityFilter(t *testing.T) {
	env := liveCatalog(t)
	a := liveDraftClaim(t, env, "W3 Filter A", 30901)
	b := liveDraftClaim(t, env, "W3 Filter B", 30902)
	liveOpenProposal(t, env, a, "W3 Filter A renamed")
	liveOpenProposal(t, env, b, "W3 Filter B renamed")

	status, _, all := liveDo(t, env, http.MethodGet, "/v2/me/proposals", livePlainToken, "")
	require.Equal(t, 200, status, string(all))
	require.Subset(t, w3ProposalIDs(t, all), []string{idstr(a), idstr(b)},
		"positive control: both proposals are in the unfiltered page")

	status, _, body := liveDo(t, env, http.MethodGet,
		"/v2/me/proposals?entity_type="+editspec.TypeWork+"&entity_id="+idstr(a), livePlainToken, "")
	require.Equal(t, 200, status, string(body))
	got := w3ProposalIDs(t, body)
	require.NotEmpty(t, got)
	for _, id := range got {
		require.Equal(t, idstr(a), id, "entity_id= must narrow to one entity: %s", body)
	}

	// object= is the family spelling of the same filter; disagreeing with
	// entity_type= is a contradiction, not an intersection.
	status, _, body = liveDo(t, env, http.MethodGet,
		"/v2/me/proposals?object=company&entity_type="+editspec.TypeWork, livePlainToken, "")
	require.Equal(t, 400, status, string(body))
	require.Equal(t, "MUTUALLY_EXCLUSIVE_PARAMETERS", liveProblemCode(t, body))
}

// `if ok {}` with no else: an unknown state was dropped and the face answered
// every state, while the public twin 400s on the same helper.
func TestLiveMyProposalsRefuseAnUnknownState(t *testing.T) {
	env := liveCatalog(t)
	status, _, body := liveDo(t, env, http.MethodGet, "/v2/me/proposals?state=bogus", livePlainToken, "")
	require.Equal(t, 400, status, string(body))
	require.Equal(t, "UNKNOWN_ENUM_VALUE", liveProblemCode(t, body))

	status, _, body = liveDo(t, env, http.MethodGet, "/v2/me/proposals?state=open", livePlainToken, "")
	require.Equal(t, 200, status, "positive control: a known state still pages: %s", body)
}

// GET /v2/moderation/claims?ids=1234 answered 200 with the first 20 pending
// claims, no missing[] and no cursor: q.Batch was set, finishList suppressed the
// cursor and zeroed the limit, and no code on these four lanes ever read q.IDs.
func TestLiveLanesWithoutABatchReadRefuseIt(t *testing.T) {
	env := liveCatalog(t)
	for _, path := range []string{
		"/v2/me/proposals?ids=1", "/v2/moderation/proposals?ids=1",
		"/v2/moderation/claims?ids=1", "/v2/me/claims?ids=1",
		"/v2/me/playtimes?ids=1", "/v2/me/work-states?ids=1", "/v2/me/news?ids=1",
		"/v2/me/claims?refs=vndb:v1",
	} {
		status, _, body := liveDo(t, env, http.MethodGet, path, liveUserToken, "")
		require.Equalf(t, 400, status, "%s must refuse a batch it cannot serve: %s", path, body)
		require.Equal(t, "INVALID_PARAMETER", liveProblemCode(t, body), path)
	}
	// Control: the catalog lanes every consumer hydrates through must keep
	// working. A refusal that also hit these would be the outage, not the fix.
	for _, path := range []string{
		"/v2/catalog/works?ids=" + idstr(env.fx.Work),
		"/v2/catalog/companies?ids=" + idstr(env.fx.Company),
		"/v2/catalog/proposals?ids=1",
	} {
		status, _, body := liveDo(t, env, http.MethodGet, path, liveAppKey, "")
		require.Equalf(t, 200, status, "positive control %s: %s", path, body)
	}
}

// fields= was declared on 40 operations, validated to 400 on an unknown token,
// and then never applied: the response always carried the full projection.
func TestLiveFieldsProjectsAndOnlyWhereDeclared(t *testing.T) {
	env := liveCatalog(t)

	status, _, full := liveDo(t, env, http.MethodGet, "/v2/catalog/works?limit=1", liveAppKey, "")
	require.Equal(t, 200, status, string(full))
	require.Greater(t, len(w3Keys(t, full)), 2, "positive control: the unprojected item is wide")

	status, _, body := liveDo(t, env, http.MethodGet, "/v2/catalog/works?limit=1&fields=display_name", liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	require.ElementsMatch(t, []string{"object", "id", "display_name"}, w3Keys(t, body),
		"object and id are always kept and nothing else survives: %s", body)

	// fields= absent must be byte-identical to before the projection existed.
	status, _, again := liveDo(t, env, http.MethodGet, "/v2/catalog/works?limit=1", liveAppKey, "")
	require.Equal(t, 200, status)
	require.Equal(t, string(full), string(again))

	// The ETag has to move with the token set, or a client that switches
	// projections is served its own 304.
	_, wide := w3Header(t, env, http.MethodGet, "/v2/catalog/works?limit=1", liveAppKey)
	_, thin := w3Header(t, env, http.MethodGet, "/v2/catalog/works?limit=1&fields=display_name", liveAppKey)
	require.NotEmpty(t, wide.Get("ETag"))
	require.NotEqual(t, wide.Get("ETag"), thin.Get("ETag"))

	// An operation that never declared fields= must not start honoring it:
	// projecting /v2/catalog/stats — or, in production, the OpenAPI document —
	// down to {object,id} would be the same defect with the sign flipped.
	status, _, stats := liveDo(t, env, http.MethodGet, "/v2/catalog/stats", "", "")
	require.Equal(t, 200, status, string(stats))
	status, _, projected := liveDo(t, env, http.MethodGet, "/v2/catalog/stats?fields=object", "", "")
	require.Equal(t, 200, status, string(projected))
	require.Equal(t, string(stats), string(projected), "an undeclared fields= must be ignored, not applied")
}

// tokens() skipped its allow-list when the list was nil, and Facets is the one
// member every spec but the works spec leaves nil — so the works face 400s
// facets=bogus_facet and all seven entity list faces answered 200.
func TestLiveUnknownFacetIs400OnEveryListFace(t *testing.T) {
	env := liveCatalog(t)
	for _, path := range []string{
		"/v2/catalog/companies", "/v2/catalog/persons", "/v2/catalog/characters",
		"/v2/catalog/series", "/v2/catalog/tags", "/v2/catalog/traits",
		"/v2/catalog/credit-names",
	} {
		status, _, body := liveDo(t, env, http.MethodGet, path+"?facets=bogus_facet", liveAppKey, "")
		require.Equalf(t, 400, status, "%s: %s", path, body)
		require.Equal(t, "UNKNOWN_FACET", liveProblemCode(t, body), path)

		status, _, body = liveDo(t, env, http.MethodGet, path+"?limit=1", liveAppKey, "")
		require.Equalf(t, 200, status, "positive control %s: %s", path, body)
	}
	// The face that always answered correctly still does, and a declared facet
	// on it is still accepted: the fix is "nil means none", not "facets are
	// gone". A declared facet routes to the search backend, which this
	// fixture does not bind, so the discriminant is "not 400", not "200".
	status, _, body := liveDo(t, env, http.MethodGet, "/v2/catalog/works?facets=bogus_facet", liveAppKey, "")
	require.Equal(t, 400, status, string(body))
	status, _, body = liveDo(t, env, http.MethodGet, "/v2/catalog/works?limit=1&facets=olang", liveAppKey, "")
	require.NotEqual(t, 400, status, "a declared facet must survive the token check: %s", body)
}

// WorksSearchFilter has no Site and no Platform member, so both were accepted
// and dropped on the way to the search engine — and facets= alone is enough to switch
// the collection to search, which is how all three consumers reach that lane.
func TestLiveSearchRefusesTheFiltersItCannotApply(t *testing.T) {
	env := liveCatalog(t)
	for _, q := range []string{
		"q=anything&site=" + liveSite,
		"facets=olang&site=" + liveSite,
		"sort=popularity&platform=win",
	} {
		status, _, body := liveDo(t, env, http.MethodGet, "/v2/catalog/works?"+q, liveAppKey, "")
		require.Equalf(t, 400, status, "%s: %s", q, body)
		require.Equal(t, "MUTUALLY_EXCLUSIVE_PARAMETERS", liveProblemCode(t, body), q)
	}
	// Control: the same filters on the registry lane are honored, not refused.
	status, _, body := liveDo(t, env, http.MethodGet, "/v2/catalog/works?limit=1&site="+liveSite, liveAppKey, "")
	require.Equal(t, 200, status, string(body))
}

// Every /v2/catalog/* body varies by credential and declared no cache intent, so
// Cloudflare invented one and kept two of them for four hours.
func TestLiveEveryCredentialedResponseDeclaresPrivateNoStore(t *testing.T) {
	env := liveCatalog(t)
	for _, path := range []string{
		"/v2/catalog/works?limit=1", "/v2/catalog/claim-events", "/v2/me/claims",
	} {
		token := liveAppKey
		if strings.HasPrefix(path, "/v2/me/") {
			token = liveUserToken
		} else if strings.HasPrefix(path, "/v2/catalog/claim-events") {
			token = liveAppKeyEvent
		}
		status, h := w3Header(t, env, http.MethodGet, path, token)
		require.Equalf(t, 200, status, path)
		require.Equalf(t, "private, no-store", h.Get("Cache-Control"), path)
	}
	// Control: the public vocabulary lanes still opt in, so this is a policy and
	// not a blanket no-store.
	for _, path := range []string{"/v2/vocabularies", "/v2/problems", "/v2/catalog/schemas/work"} {
		status, h := w3Header(t, env, http.MethodGet, path, "")
		require.Equalf(t, 200, status, path)
		require.Containsf(t, h.Get("Cache-Control"), "max-age=300", path)
	}
}

// The four proposal write ops declared an ETag response header and left it
// empty, and huma drops an empty-string header — so the validator the next write
// needs was only obtainable with a second GET.
func TestLiveProposalWritesCarryTheirValidator(t *testing.T) {
	env := liveCatalog(t)
	work := liveDraftClaim(t, env, "W3 ETag Target", 30903)

	req := httptest.NewRequest(http.MethodPost, "/v2/me/proposals", strings.NewReader(fmt.Sprintf(
		`{"entity_type":%q,"entity_id":%q,"patch":{%q:%q},"note":"w3"}`,
		editspec.TypeWork, idstr(work), editspec.FieldWorkDisplayName, "W3 ETag Renamed")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+livePlainToken)
	resp, err := env.app.Test(req)
	require.NoError(t, err)
	require.Equal(t, 201, resp.StatusCode)
	created := resp.Header.Get("ETag")
	require.NotEmpty(t, created, "createMyProposal declares an ETag and must send one")

	var prop liveProposalBody
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&prop))

	// The validator from the create is enough to amend: no second GET. The
	// amender is the reviewer, not the proposer — the engine resolves amendment
	// standing per field as review standing, which a plain claimant lacks.
	status, _, body := liveDoHeader(t, env, http.MethodPost,
		"/v2/me/proposals/"+prop.ID+"/amendments", liveUserToken,
		fmt.Sprintf(`{"set":{%q:%q},"note":"amend one"}`, editspec.FieldWorkDisplayName, "W3 Amended"),
		map[string]string{"If-Match": created})
	require.Equal(t, 201, status, string(body))

	var amended liveProposalBody
	require.NoError(t, json.Unmarshal(body, &amended))
	require.NotNil(t, amended.Amendments, "the amendment response must say what it appended")
	require.NotEmpty(t, *amended.Amendments)
	last := (*amended.Amendments)[len(*amended.Amendments)-1]
	require.Equal(t, "amendment", last.Object)
	require.NotEqual(t, prop.ID, last.ID, "the id must be the amendment's, not the proposal's")

	// An amendment only wrote the child row, so the proposal's validator never
	// moved: reviewer A's stale If-Match still matched after B amended.
	after := liveETag(t, env, "/v2/me/proposals/"+prop.ID, livePlainToken)
	require.NotEqual(t, created, after, "an amendment must advance the proposal validator")

	status, _, body = liveDoHeader(t, env, http.MethodPost,
		"/v2/me/proposals/"+prop.ID+"/amendments", liveUserToken,
		fmt.Sprintf(`{"set":{%q:%q},"note":"stale"}`, editspec.FieldWorkDisplayName, "W3 Stale"),
		map[string]string{"If-Match": created})
	require.Equal(t, 412, status, "the superseded validator must be refused: %s", body)
}

// unban has no fixed target — it restores the state the claim was hidden from —
// so the forum hardcodes to := "live" and tells a moderator "published" for a
// claim that went back to pending.
func TestLiveClaimDecisionReportsTheResultingState(t *testing.T) {
	env := liveCatalog(t)
	work := liveDraftClaim(t, env, "W3 Decision Target", 30904)
	id := idstr(work)

	status, _, body := liveDoHeader(t, env, http.MethodPatch, "/v2/me/claims/"+id, liveUserToken,
		`{"state":"pending"}`, map[string]string{"If-Match": liveETag(t, env, "/v2/me/claims/"+id, liveUserToken)})
	require.Equal(t, 200, status, string(body))

	decide := func(decision string) (from *string, to string) {
		t.Helper()
		tag := liveETag(t, env, "/v2/moderation/claims/"+id, liveUserToken)
		st, _, raw := liveDoHeader(t, env, http.MethodPost, "/v2/moderation/claims/"+id+"/decisions",
			liveUserToken, fmt.Sprintf(`{"decision":%q,"note":"w3"}`, decision),
			map[string]string{"If-Match": tag})
		require.Equal(t, 201, st, string(raw))
		var rec struct {
			FromState *string `json:"from_state"`
			ToState   string  `json:"to_state"`
		}
		require.NoError(t, json.Unmarshal(raw, &rec), string(raw))
		return rec.FromState, rec.ToState
	}

	from, to := decide("ban")
	require.NotNil(t, from)
	require.Equal(t, "pending", *from)
	require.Equal(t, "hidden", to)

	// The whole point: unban lands back on pending, not on live.
	from, to = decide("unban")
	require.NotNil(t, from)
	require.Equal(t, "hidden", *from)
	require.Equal(t, "pending", to, "unban restores the state the claim was hidden from")
}

// The queue was claim_state = pending only while the decision face acts on
// hidden too, so no GET under /v2/moderation/ could list a banned claim and
// unban was reachable only by already knowing the work id.
func TestLiveModerationQueueListsTheStatesItCanDecide(t *testing.T) {
	env := liveCatalog(t)
	work := liveDraftClaim(t, env, "W3 Hidden Target", 30905)
	id := idstr(work)

	status, _, body := liveDoHeader(t, env, http.MethodPost, "/v2/moderation/claims/"+id+"/decisions",
		liveUserToken, `{"decision":"ban","note":"w3 hide"}`,
		map[string]string{"If-Match": liveETag(t, env, "/v2/moderation/claims/"+id, liveUserToken)})
	require.Equal(t, 201, status, string(body))

	status, _, body = liveDo(t, env, http.MethodGet, "/v2/moderation/claims?claim_state=hidden&limit=100", liveUserToken, "")
	require.Equal(t, 200, status, string(body))
	require.Contains(t, w3ClaimIDs(t, body), id, "a banned claim must be findable for unban: %s", body)

	// Control: the default queue is still pending-only, so the widening is a
	// parameter and not a silent change of what the queue means.
	status, _, body = liveDo(t, env, http.MethodGet, "/v2/moderation/claims?limit=100", liveUserToken, "")
	require.Equal(t, 200, status, string(body))
	require.NotContains(t, w3ClaimIDs(t, body), id)

	status, _, body = liveDo(t, env, http.MethodGet, "/v2/moderation/claims?claim_state=bogus", liveUserToken, "")
	require.Equal(t, 400, status, string(body))
}

func w3ClaimIDs(t *testing.T, body []byte) []string {
	t.Helper()
	var page struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(body, &page), string(body))
	out := make([]string, 0, len(page.Items))
	for _, it := range page.Items {
		out = append(out, it.ID)
	}
	return out
}

// total was counted on the cursor-narrowed query, so it shrank on every page of
// the three proposal lanes and the moderation claim queue — the exact thing
// deviation 10 says a total must not do.
func TestLiveIncludeTotalDoesNotShrinkWhilePaging(t *testing.T) {
	env := liveCatalog(t)
	for i := 0; i < 3; i++ {
		work := liveDraftClaim(t, env, fmt.Sprintf("W3 Total %d", i), int64(30910+i))
		liveOpenProposal(t, env, work, fmt.Sprintf("W3 Total %d renamed", i))
	}
	readTotal := func(path string) (int64, string) {
		t.Helper()
		status, _, body := liveDo(t, env, http.MethodGet, path, livePlainToken, "")
		require.Equal(t, 200, status, string(body))
		var page struct {
			Total      *int64  `json:"total"`
			NextCursor *string `json:"next_cursor"`
		}
		require.NoError(t, json.Unmarshal(body, &page), string(body))
		require.NotNil(t, page.Total, string(body))
		next := ""
		if page.NextCursor != nil {
			next = *page.NextCursor
		}
		return *page.Total, next
	}
	first, cursor := readTotal("/v2/me/proposals?include_total=true&limit=1")
	require.GreaterOrEqual(t, first, int64(3))
	require.NotEmpty(t, cursor, "positive control: there has to be a second page to compare")
	second, _ := readTotal("/v2/me/proposals?include_total=true&limit=1&cursor=" + cursor)
	require.Equal(t, first, second, "total is the size of the collection, not of the page's remainder")
}
