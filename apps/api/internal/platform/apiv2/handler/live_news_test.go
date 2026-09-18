package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	newsmodel "api/internal/platform/news/model"

	"github.com/stretchr/testify/require"
)

type newsBodyView struct {
	Object     string   `json:"object"`
	ID         string   `json:"id"`
	Lane       string   `json:"lane"`
	Status     string   `json:"status"`
	Title      string   `json:"title"`
	Summary    string   `json:"summary"`
	SourceURL  string   `json:"source_url"`
	BannerHash string   `json:"banner_hash"`
	WorkIDs    []string `json:"work_ids"`
	Source     struct {
		Name string `json:"name"`
	} `json:"source"`
}

func liveDoFull(t *testing.T, env *liveEnv, method, path, token, body string, extra map[string]string) (int, http.Header, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	resp, err := env.app.Test(req)
	require.NoError(t, err)
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, resp.Header, raw
}

func liveProblem(t *testing.T, raw []byte) problem.Problem {
	t.Helper()
	var p problem.Problem
	require.NoError(t, json.Unmarshal(raw, &p), string(raw))
	return p
}

func liveNewsBody(t *testing.T, raw []byte) newsBodyView {
	t.Helper()
	var v newsBodyView
	require.NoError(t, json.Unmarshal(raw, &v), string(raw))
	return v
}

func liveSubmit(t *testing.T, env *liveEnv, title string) newsBodyView {
	t.Helper()
	status, _, raw := liveDo(t, env, http.MethodPost, "/v2/me/news", liveUserToken,
		`{"source":"`+liveNewsSource+`","title":"`+title+`","summary":"A lede","source_url":"https://example.test/x"}`)
	require.Equal(t, 201, status, string(raw))
	return liveNewsBody(t, raw)
}

func TestLiveNewsSourceIsTheGrant(t *testing.T) {
	env := liveCatalog(t)

	status, _, raw := liveDo(t, env, http.MethodGet, "/v2/me/news", liveAppKey, "")
	require.Equal(t, 401, status, string(raw))

	for _, source := range []string{"no_such_source", "ymgal"} {
		status, _, raw = liveDo(t, env, http.MethodPost, "/v2/me/news", liveUserToken,
			`{"source":"`+source+`","title":"t","summary":"s","source_url":"https://example.test/x"}`)
		require.Equal(t, 403, status, source+": "+string(raw))
		require.Equal(t, problem.CodeSourceNotYours, liveProblem(t, raw).Code, source)
	}

	require.NoError(t, env.db.Exec(`
		INSERT INTO news_source (key, display_name, homepage_url, attribution, publisher_uid, column_url, active)
		VALUES ('dozing', 'Dozing', 'https://example.test', 'a', ?, '', false)
		ON CONFLICT (key) DO UPDATE SET publisher_uid = EXCLUDED.publisher_uid, active = false`, liveUID).Error)
	status, _, raw = liveDo(t, env, http.MethodPost, "/v2/me/news", liveUserToken,
		`{"source":"dozing","title":"t","summary":"s","source_url":"https://example.test/x"}`)
	require.Equal(t, 422, status, string(raw))
	require.Equal(t, problem.CodeSourceInactive, liveProblem(t, raw).Code)
}

func TestLiveNewsCreateLandsPending(t *testing.T) {
	env := liveCatalog(t)

	status, hdr, raw := liveDoFull(t, env, http.MethodPost, "/v2/me/news", liveUserToken,
		`{"source":"`+liveNewsSource+`","lane":"column","title":"Fresh","summary":"A lede",`+
			`"source_url":"https://example.test/fresh","published_at":"2026-08-25T12:00:00Z",`+
			`"work_ids":["`+idstr(env.fx.Work)+`"]}`, nil)
	require.Equal(t, 201, status, string(raw))
	rec := liveNewsBody(t, raw)
	require.Equal(t, "news_submission", rec.Object)
	require.Equal(t, "pending", rec.Status)
	require.Equal(t, "column", rec.Lane)
	require.Equal(t, liveNewsSource, rec.Source.Name)
	require.Equal(t, []string{idstr(env.fx.Work)}, rec.WorkIDs)
	require.Equal(t, "/v2/me/news/"+rec.ID, hdr.Get("Location"))
	require.NotEmpty(t, hdr.Get("ETag"))
	require.Equal(t, "private, no-store", hdr.Get("Cache-Control"))

	status, hdr, raw = liveDoFull(t, env, http.MethodGet, "/v2/me/news/"+rec.ID, liveUserToken, "", nil)
	require.Equal(t, 200, status, string(raw))
	require.NotEmpty(t, hdr.Get("ETag"))
	require.Equal(t, "private, no-store", hdr.Get("Cache-Control"))
	require.Equal(t, rec.ID, liveNewsBody(t, raw).ID)

	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/me/news?limit=100", liveUserToken, "")
	require.Equal(t, 200, status, string(raw))
	var page struct {
		Items []newsBodyView `json:"items"`
	}
	require.NoError(t, json.Unmarshal(raw, &page))
	seen := false
	for _, it := range page.Items {
		if it.ID == rec.ID {
			seen = true
		}
	}
	require.True(t, seen, "a publisher must see their own pending item")
}

// TestLiveNewsPendingIsInvisibleOnThePublicFace is the same obligation the read
// face already enforces, asserted from the write side: a POST is not a publish.
func TestLiveNewsPendingIsInvisibleOnThePublicFace(t *testing.T) {
	env := liveCatalog(t)
	rec := liveSubmit(t, env, "Still pending")

	status, _, raw := liveDo(t, env, http.MethodGet, "/v2/news/"+rec.ID, "", "")
	require.Equal(t, 404, status, string(raw))

	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/news?limit=100", "", "")
	require.Equal(t, 200, status, string(raw))
	require.NotContains(t, string(raw), `"id":"`+rec.ID+`"`)
	require.Contains(t, string(raw), `"id":"`+idstr(env.fx.NewsItem)+`"`,
		"control: the seeded published item is on the public face")
}

func TestLiveNewsValidationIsFieldLevel(t *testing.T) {
	env := liveCatalog(t)

	status, _, raw := liveDo(t, env, http.MethodPost, "/v2/me/news", liveUserToken,
		`{"source":"`+liveNewsSource+`","title":"t","summary":"`+strings.Repeat("a", newsmodel.PreviewMaxRunes+1)+`","source_url":"https://example.test/x"}`)
	require.Equal(t, 422, status, string(raw))
	p := liveProblem(t, raw)
	require.Equal(t, problem.CodeValidationFailed, p.Code)
	require.NotEmpty(t, p.Errors)
	require.Equal(t, "/summary", p.Errors[0].Pointer)
	require.Equal(t, problem.ReasonTooLong, p.Errors[0].Reason)

	status, _, raw = liveDo(t, env, http.MethodPost, "/v2/me/news", liveUserToken,
		`{"source":"`+liveNewsSource+`","title":"t","summary":"`+strings.Repeat("a", newsmodel.PreviewMaxRunes)+`","source_url":"https://example.test/x"}`)
	require.Equal(t, 201, status, "exactly 200 runes is the ceiling, not one past it: "+string(raw))

	// ftp:// satisfies huma's format:"uri" (it only asks for a non-empty scheme)
	// and is refused by newsSourceURLErrors, which wants http or https — so both
	// this and work_ids are handler-stage failures and must arrive together.
	// A scheme-less "not a url" would instead fail huma's schema stage, which
	// returns before the handler runs and can therefore report only that field.
	status, _, raw = liveDo(t, env, http.MethodPost, "/v2/me/news", liveUserToken,
		`{"source":"`+liveNewsSource+`","title":"t","summary":"s","source_url":"ftp://example.test/x","work_ids":["abc","`+idstr(env.fx.Work)+`"]}`)
	require.Equal(t, 422, status, string(raw))
	p = liveProblem(t, raw)
	require.Equal(t, problem.CodeValidationFailed, p.Code)
	pointers := map[string]string{}
	for _, e := range p.Errors {
		pointers[e.Pointer] = e.Reason
	}
	require.Equal(t, problem.ReasonInvalidFormat, pointers["/source_url"])
	require.Equal(t, problem.ReasonInvalidFormat, pointers["/work_ids/0"], "every failure at once, not one round trip each")

	// The schema stage reports every field it rejects at once, too.
	status, _, raw = liveDo(t, env, http.MethodPost, "/v2/me/news", liveUserToken,
		`{"source":"`+liveNewsSource+`","title":"","summary":"s","source_url":"not a url"}`)
	require.Equal(t, 422, status, string(raw))
	p = liveProblem(t, raw)
	require.Equal(t, problem.CodeValidationFailed, p.Code)
	pointers = map[string]string{}
	var titleParams *problem.FieldParams
	for _, e := range p.Errors {
		pointers[e.Pointer] = e.Reason
		if e.Pointer == "/title" {
			titleParams = e.Params
		}
	}
	require.Equal(t, problem.ReasonInvalidFormat, pointers["/source_url"])
	require.Equal(t, problem.ReasonTooShort, pointers["/title"], "every schema failure at once, not one round trip each")
	require.NotNil(t, titleParams)
	require.NotNil(t, titleParams.MinLength)
	require.Equal(t, 1, *titleParams.MinLength)

	// banner_hash is refused by the generated schema before the handler runs, so
	// it is asserted on its own: mixing it into the case above hides every other
	// field's error behind huma's single schema failure.
	status, _, raw = liveDo(t, env, http.MethodPost, "/v2/me/news", liveUserToken,
		`{"source":"`+liveNewsSource+`","title":"t","summary":"s","source_url":"https://example.test/x","banner_hash":"zz"}`)
	require.Equal(t, 422, status, string(raw))
	p = liveProblem(t, raw)
	require.Equal(t, problem.CodeValidationFailed, p.Code)
	require.NotEmpty(t, p.Errors)
	require.Equal(t, "/banner_hash", p.Errors[0].Pointer)
}

func TestLiveNewsIdempotencyKeyReplays(t *testing.T) {
	env := liveCatalog(t)
	body := `{"source":"` + liveNewsSource + `","title":"Once","summary":"A lede","source_url":"https://example.test/once"}`
	key := map[string]string{"Idempotency-Key": "01K3QY8Z9T7M4A2BXWQ7RN2C4H"}

	status, hdr, raw := liveDoFull(t, env, http.MethodPost, "/v2/me/news", liveUserToken, body, key)
	require.Equal(t, 201, status, string(raw))
	require.Empty(t, hdr.Get("Idempotency-Replayed"))
	first := liveNewsBody(t, raw).ID

	status, hdr, raw = liveDoFull(t, env, http.MethodPost, "/v2/me/news", liveUserToken, body, key)
	require.Equal(t, 201, status, string(raw))
	require.Equal(t, "true", hdr.Get("Idempotency-Replayed"))
	require.Equal(t, first, liveNewsBody(t, raw).ID, "a replay must not mint a second item")
}

func TestLiveNewsPendingEditThenWithdraw(t *testing.T) {
	env := liveCatalog(t)
	rec := liveSubmit(t, env, "Draft title")

	status, _, raw := liveDo(t, env, http.MethodPatch, "/v2/me/news/"+rec.ID, liveUserToken, `{}`)
	require.Equal(t, 422, status, "an empty patch changes nothing: "+string(raw))

	status, hdr, raw := liveDoFull(t, env, http.MethodPatch, "/v2/me/news/"+rec.ID, liveUserToken,
		`{"title":"Edited title","work_ids":[]}`, nil)
	require.Equal(t, 200, status, string(raw))
	require.Equal(t, "Edited title", liveNewsBody(t, raw).Title)
	require.Equal(t, "pending", liveNewsBody(t, raw).Status)
	require.Equal(t, "private, no-store", hdr.Get("Cache-Control"))

	status, _, raw = liveDo(t, env, http.MethodPatch, "/v2/me/news/"+rec.ID, liveUserToken, `{"status":"withdrawn"}`)
	require.Equal(t, 428, status, "If-Match is mandatory on the withdrawal: "+string(raw))
	require.Equal(t, problem.CodePreconditionRequired, liveProblem(t, raw).Code)

	id, ok := repr.ParseID(rec.ID)
	require.True(t, ok)
	require.NoError(t, env.db.Exec(`UPDATE news_item SET status = ? WHERE id = ?`, newsmodel.StatusPublished, id).Error)

	status, hdr, raw = liveDoFull(t, env, http.MethodGet, "/v2/me/news/"+rec.ID, liveUserToken, "", nil)
	require.Equal(t, 200, status, string(raw))
	etag := hdr.Get("ETag")
	require.NotEmpty(t, etag)

	status, _, raw = liveDoFull(t, env, http.MethodPatch, "/v2/me/news/"+rec.ID, liveUserToken,
		`{"title":"Too late"}`, nil)
	require.Equal(t, 409, status, "text is editable only while pending: "+string(raw))
	require.Equal(t, problem.CodeInvalidStateTransition, liveProblem(t, raw).Code)

	status, _, raw = liveDoFull(t, env, http.MethodPatch, "/v2/me/news/"+rec.ID, liveUserToken,
		`{"status":"withdrawn"}`, map[string]string{"If-Match": `"n` + rec.ID + `.0"`})
	require.Equal(t, 412, status, string(raw))
	require.Equal(t, problem.CodePreconditionFailed, liveProblem(t, raw).Code)

	status, _, raw = liveDoFull(t, env, http.MethodPatch, "/v2/me/news/"+rec.ID, liveUserToken,
		`{"status":"withdrawn"}`, map[string]string{"If-Match": etag})
	require.Equal(t, 200, status, string(raw))
	require.Equal(t, "withdrawn", liveNewsBody(t, raw).Status)

	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/news?limit=100", "", "")
	require.Equal(t, 200, status)
	require.NotContains(t, string(raw), `"id":"`+rec.ID+`"`, "a withdrawn item leaves the public list")
}

func TestLiveNewsRejectedIsTerminal(t *testing.T) {
	env := liveCatalog(t)
	rec := liveSubmit(t, env, "Doomed")
	id, ok := repr.ParseID(rec.ID)
	require.True(t, ok)
	require.NoError(t, env.db.Exec(`UPDATE news_item SET status = ? WHERE id = ?`, newsmodel.StatusRejected, id).Error)

	status, hdr, raw := liveDoFull(t, env, http.MethodGet, "/v2/me/news/"+rec.ID, liveUserToken, "", nil)
	require.Equal(t, 200, status, string(raw))
	require.Equal(t, "rejected", liveNewsBody(t, raw).Status)
	etag := hdr.Get("ETag")

	for _, body := range []string{`{"title":"Reborn"}`, `{"status":"withdrawn"}`} {
		status, _, raw = liveDoFull(t, env, http.MethodPatch, "/v2/me/news/"+rec.ID, liveUserToken, body,
			map[string]string{"If-Match": etag})
		require.Equal(t, 409, status, body+": "+string(raw))
		require.Equal(t, problem.CodeInvalidStateTransition, liveProblem(t, raw).Code, body)
	}
}

// Neither news face had a single assertion about ids=, refs= or facets=, and
// both declare all three. The public face was the worse of the two: it parsed
// ids=, had no hydration lane, and collect.Parse's batch branch zeroed the
// limit, so GET /v2/news?ids=<a published id> answered 200 with an empty
// items[], no missing[] and no next_cursor. The two faces refuse for different
// reasons — NewsSpec and NewsSubmissionSpec are separate specs — so each gets
// its own arm.
func TestLiveNewsCollectionParams(t *testing.T) {
	env := liveCatalog(t)
	published := idstr(env.fx.NewsItem)

	for _, face := range []struct{ path, token string }{
		{"/v2/news", ""},
		{"/v2/me/news", liveUserToken},
	} {
		for _, tc := range []struct{ query, param string }{
			{"ids=" + published, "ids"},
			{"ids=1,2,3", "ids"},
			{"refs=vndb:v1", "refs"},
		} {
			status, ct, raw := liveDo(t, env, http.MethodGet, face.path+"?"+tc.query, face.token, "")
			require.Equalf(t, 400, status, "%s?%s: %s", face.path, tc.query, raw)
			require.Contains(t, ct, "application/problem+json")
			p := liveProblem(t, raw)
			require.Equal(t, problem.CodeInvalidParameter, p.Code, face.path)
			require.NotEmpty(t, p.Errors, face.path)
			require.Equal(t, tc.param, p.Errors[0].Parameter, face.path)
			require.Equal(t, problem.ReasonNotAllowedValue, p.Errors[0].Reason, face.path)
		}

		status, _, raw := liveDo(t, env, http.MethodGet, face.path+"?facets=source", face.token, "")
		require.Equalf(t, 400, status, "%s: %s", face.path, raw)
		require.Equal(t, problem.CodeUnknownFacet, liveProblem(t, raw).Code, face.path)

		// The documented batch bound is checked before the lane refusal, so it
		// is observable on a NoBatch face and must stay the same 400 the
		// hydrating lanes answer.
		over := make([]string, collect.MaxBatchItems+1)
		for i := range over {
			over[i] = published
		}
		status, _, raw = liveDo(t, env, http.MethodGet, face.path+"?ids="+strings.Join(over, ","), face.token, "")
		require.Equalf(t, 400, status, "%s: %s", face.path, raw)
		require.Equal(t, problem.CodeTooManyIDs, liveProblem(t, raw).Code, face.path)

		status, _, raw = liveDo(t, env, http.MethodGet,
			face.path+"?ids="+strings.Join(over[:collect.MaxBatchItems], ","), face.token, "")
		require.Equalf(t, 400, status, "%s: %s", face.path, raw)
		require.Equal(t, problem.CodeInvalidParameter, liveProblem(t, raw).Code,
			face.path+": exactly 100 is under the bound, so it reaches the lane refusal instead")
	}

	// Control: the parameters that ARE implemented on the public face still work,
	// so the refusals above are about the batch lane and not about the face
	// having stopped parsing its query string.
	status, _, raw := liveDo(t, env, http.MethodGet, "/v2/news?include_total=true&limit=1", "", "")
	require.Equal(t, 200, status, string(raw))
	var page struct {
		Items []json.RawMessage `json:"items"`
		Total *int64            `json:"total"`
	}
	require.NoError(t, json.Unmarshal(raw, &page))
	require.NotNil(t, page.Total, "include_total= is declared on /v2/news and must be answered")
	require.GreaterOrEqual(t, *page.Total, int64(1))
	require.Len(t, page.Items, 1)
}

// newsFeedIDs reads /v2/news with an arbitrary query and returns the ids and
// the total, so a filter can be judged by what it KEEPS as well as by what it
// drops. An empty result set is not evidence a filter ran.
func newsFeedIDs(t *testing.T, env *liveEnv, query string) (map[string]bool, *int64) {
	t.Helper()
	status, _, raw := liveDo(t, env, http.MethodGet, "/v2/news?limit=100&"+query, "", "")
	require.Equal(t, 200, status, query+": "+string(raw))
	var page struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		Total *int64 `json:"total"`
	}
	require.NoError(t, json.Unmarshal(raw, &page), query)
	ids := map[string]bool{}
	for _, it := range page.Items {
		ids[it.ID] = true
	}
	return ids, page.Total
}

const (
	newsFiltEarly = "2026-03-01T00:00:00Z"
	newsFiltMid   = "2026-04-01T00:00:00Z"
	newsFiltLate  = "2026-05-01T00:00:00Z"
)

// GET /v2/news accepted lane=, source=, published_after= and published_before=
// and dropped all four: they were never declared, so SkipValidateParams ate
// them and the handler passed a hard-coded empty FeedFilter. The forum sends
// every one of them (newsclient.FeedQuery.values), so its 情报 page has been
// rendering the unfiltered feed. Each arm below asserts both halves of the
// partition — a filter that returned nothing would pass a "the wrong rows are
// gone" test and fail this one.
func TestLiveNewsFeedFilters(t *testing.T) {
	env := liveCatalog(t)
	require.NoError(t, env.db.Exec(`DELETE FROM news_item WHERE external_id LIKE 'filt-%'`).Error)
	for _, key := range []string{"filt_a", "filt_b"} {
		require.NoError(t, env.db.Exec(`
			INSERT INTO news_source (key, display_name, homepage_url, attribution, publisher_uid, column_url, active)
			VALUES (?, ?, 'https://example.test', 'a', ?, '', true)
			ON CONFLICT (key) DO UPDATE SET active = true`, key, key, liveUID).Error)
	}
	seed := func(external, source, lane, published string) string {
		t.Helper()
		require.NoError(t, env.db.Exec(`
			INSERT INTO news_item (source_key, lane, upstream_category, external_id, title, preview,
				source_url, banner_hash, banner_origin_url, published_at, status)
			VALUES (?, ?, '', ?, 't', 'p', 'https://example.test/f', '', '', ?::timestamptz, ?)`,
			source, lane, external, published, newsmodel.StatusPublished).Error)
		var id int64
		require.NoError(t, env.db.Raw(`SELECT id FROM news_item WHERE external_id = ?`, external).Scan(&id).Error)
		return idstr(id)
	}
	a1 := seed("filt-a1", "filt_a", newsmodel.LaneNews, newsFiltEarly)
	a2 := seed("filt-a2", "filt_a", newsmodel.LaneColumn, newsFiltMid)
	b1 := seed("filt-b1", "filt_b", newsmodel.LaneNews, newsFiltLate)

	all, _ := newsFeedIDs(t, env, "")
	for _, id := range []string{a1, a2, b1} {
		require.Truef(t, all[id], "control: %s is on the unfiltered feed to begin with", id)
	}

	for _, tc := range []struct {
		query      string
		keep, drop []string
	}{
		{"source=filt_a", []string{a1, a2}, []string{b1}},
		{"source=filt_b", []string{b1}, []string{a1, a2}},
		{"source=filt_a,filt_b", []string{a1, a2, b1}, nil},
		{"lane=news", []string{a1, b1}, []string{a2}},
		{"lane=column", []string{a2}, []string{a1, b1}},
		{"lane=news,column&source=filt_a", []string{a1, a2}, []string{b1}},
		{"published_after=" + newsFiltMid, []string{a2, b1}, []string{a1}},
		{"published_before=" + newsFiltMid, []string{a1, a2}, []string{b1}},
		{"published_after=" + newsFiltMid + "&published_before=" + newsFiltMid, []string{a2}, []string{a1, b1}},
		{"published_after=" + newsFiltEarly + "&published_before=" + newsFiltLate, []string{a1, a2, b1}, nil},
	} {
		got, _ := newsFeedIDs(t, env, tc.query)
		require.NotEmptyf(t, tc.keep, "%s: a filter arm with nothing to keep proves nothing", tc.query)
		for _, id := range tc.keep {
			require.Truef(t, got[id], "%s must keep %s", tc.query, id)
		}
		for _, id := range tc.drop {
			require.Falsef(t, got[id], "%s must drop %s", tc.query, id)
		}
	}

	// include_total counts the NARROWED population, which is the assertion that
	// separates "the filter ran" from "the filter ran and returned nothing" and
	// from "the filter was ignored": ignored would report the whole feed.
	_, narrowed := newsFeedIDs(t, env, "source=filt_a&include_total=true")
	require.NotNil(t, narrowed)
	require.Equal(t, int64(2), *narrowed)
	_, whole := newsFeedIDs(t, env, "include_total=true")
	require.NotNil(t, whole)
	require.Greater(t, *whole, *narrowed, "control: the unfiltered population is strictly larger")

	// The exact wire format the forum emits, which is why published_* is an
	// RFC 3339 instant rather than the YYYY-MM-DD released_after= on works.
	forum := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	got, _ := newsFeedIDs(t, env, "published_after="+forum)
	require.True(t, got[a2] && got[b1] && !got[a1], "forum wire format %q", forum)

	// Accept-and-ignore is the defect; every unusable value is refused.
	for _, tc := range []struct{ query, code, param string }{
		{"lane=bogus", problem.CodeUnknownEnumValue, "lane"},
		{"lane=news,bogus", problem.CodeUnknownEnumValue, "lane"},
		{"published_after=2026-04-01", problem.CodeInvalidParameter, "published_after"},
		{"published_before=2026-04-01T00:00:00%2B08:00", problem.CodeInvalidParameter, "published_before"},
		{"published_after=" + newsFiltLate + "&published_before=" + newsFiltEarly, problem.CodeInvalidParameter, "published_after"},
		{"source=" + strings.TrimSuffix(strings.Repeat("s,", newsMaxSources+1), ","), problem.CodeInvalidParameter, "source"},
	} {
		status, ct, raw := liveDo(t, env, http.MethodGet, "/v2/news?"+tc.query, "", "")
		require.Equalf(t, 400, status, "%s: %s", tc.query, raw)
		require.Contains(t, ct, "application/problem+json", tc.query)
		p := liveProblem(t, raw)
		require.Equal(t, tc.code, p.Code, tc.query)
		require.NotEmpty(t, p.Errors, tc.query)
		require.Equal(t, tc.param, p.Errors[0].Parameter, tc.query)
	}

	// An unknown source key matches nothing rather than failing: the vocabulary
	// is the news_source table, which is open.
	empty, total := newsFeedIDs(t, env, "source=no_such_source&include_total=true")
	require.Empty(t, empty)
	require.NotNil(t, total)
	require.Equal(t, int64(0), *total)

	require.NoError(t, env.db.Exec(`DELETE FROM news_item WHERE external_id LIKE 'filt-%'`).Error)
}

func TestLiveNewsIsFencedToTheCaller(t *testing.T) {
	env := liveCatalog(t)
	require.NoError(t, env.db.Exec(`
		INSERT INTO news_source (key, display_name, homepage_url, attribution, publisher_uid, column_url, active)
		VALUES ('stranger', 'Stranger', 'https://example.test', 'a', 987654, '', true)
		ON CONFLICT (key) DO UPDATE SET publisher_uid = EXCLUDED.publisher_uid, active = true`).Error)
	require.NoError(t, env.db.Exec(`
		INSERT INTO news_item (source_key, lane, upstream_category, external_id, title, preview,
			source_url, banner_hash, banner_origin_url, published_at, status)
		VALUES ('stranger', 'news', '', 'stranger-1', 't', 'p', 'https://example.test/s', '', '', now(), ?)`,
		newsmodel.StatusPublished).Error)
	var strangerID int64
	require.NoError(t, env.db.Raw(`SELECT id FROM news_item WHERE external_id = 'stranger-1'`).Scan(&strangerID).Error)

	status, _, raw := liveDo(t, env, http.MethodGet, "/v2/me/news/"+idstr(strangerID), liveUserToken, "")
	require.Equal(t, 404, status, string(raw))
	require.Equal(t, problem.CodeNotFound, liveProblem(t, raw).Code)

	status, _, raw = liveDo(t, env, http.MethodPatch, "/v2/me/news/"+idstr(strangerID), liveUserToken, `{"title":"mine now"}`)
	require.Equal(t, 404, status, string(raw))

	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/news/"+idstr(strangerID), "", "")
	require.Equal(t, 200, status, "control: the item exists and is public, it is just not the caller's: "+string(raw))
}
