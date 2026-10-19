package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/require"
)

type liveLinkRow struct {
	Source     string `json:"source"`
	URL        string `json:"url"`
	IsIdentity bool   `json:"is_identity"`
	PageID     string `json:"page_id,omitempty"`
}

func liveCreditNameLinks(t *testing.T, env *liveEnv, id int64) []liveLinkRow {
	t.Helper()
	status, _, body := liveDo(t, env, http.MethodGet,
		"/v2/catalog/credit-names/"+idstr(id)+"?include=links", liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	var out struct {
		Links *[]liveLinkRow `json:"links"`
	}
	require.NoError(t, json.Unmarshal(body, &out))
	require.NotNil(t, out.Links)
	return *out.Links
}

func TestLivePersonExactAnchorsMintLinks(t *testing.T) {
	env := liveCatalog(t)

	links := liveCreditNameLinks(t, env, env.fx.AnchoredCredit)
	require.Equal(t, []liveLinkRow{
		{Source: "vndb", URL: "https://vndb.org/s131", IsIdentity: true, PageID: "s131"},
		{Source: "bangumi", URL: "https://bgm.tv/person/4423", IsIdentity: true, PageID: "4423"},
		{Source: "erogamescape", URL: "https://erogamescape.dyndns.org/~ap2/ero/toukei_kaiseki/creater.php?creater=8080", IsIdentity: true, PageID: "8080"},
	}, links, "the name's alias resolves to s131, which the person's own anchor repeats; dlsite has no public page")

	for _, l := range links {
		require.NotEqual(t, "https://vndb.org/v777", l.URL, "probable anchors never mint")
		require.NotEqual(t, "https://vndb.org/1234", l.URL,
			"the credit-name ref is a staff-alias id; minting it reaches an unrelated VN")
		require.NotEqual(t, "https://vndb.org/s1234", l.URL, "an alias id is not a staff id")
	}
}

func TestLivePersonRelatedOnlyKeepsOneLink(t *testing.T) {
	env := liveCatalog(t)
	links := liveCreditNameLinks(t, env, env.fx.Credit)
	require.Equal(t, []liveLinkRow{{Source: "twitter", URL: "https://x.com/live_person"}}, links,
		"a person with only a related anchor is unchanged by the exact-anchor lane")
}

func TestLiveWorkTagBlockIsNotTruncatedAt100(t *testing.T) {
	env := liveCatalog(t)

	status, _, body := liveDo(t, env, http.MethodGet,
		"/v2/catalog/works/"+idstr(env.fx.BulkWork)+"?include=tags", liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	var detail struct {
		Tags *[]struct {
			DisplayName string `json:"display_name"`
		} `json:"tags"`
	}
	require.NoError(t, json.Unmarshal(body, &detail))
	require.NotNil(t, detail.Tags)
	require.Len(t, *detail.Tags, liveBulkRows, "the embedded block used to stop at 100")

	paged := 0
	path := "/v2/catalog/works/" + idstr(env.fx.BulkWork) + "/tags?limit=100"
	for {
		status, _, body = liveDo(t, env, http.MethodGet, path, liveAppKey, "")
		require.Equal(t, 200, status, string(body))
		var page struct {
			Items      []json.RawMessage `json:"items"`
			NextCursor *string           `json:"next_cursor"`
		}
		require.NoError(t, json.Unmarshal(body, &page))
		paged += len(page.Items)
		if page.NextCursor == nil {
			break
		}
		path = "/v2/catalog/works/" + idstr(env.fx.BulkWork) + "/tags?limit=100&cursor=" + *page.NextCursor
	}
	require.Equal(t, liveBulkRows, paged, "the block and the paged sub-face must agree")
}

func TestLiveCharacterTraitBlockIsNotTruncatedAt100(t *testing.T) {
	env := liveCatalog(t)
	status, _, body := liveDo(t, env, http.MethodGet,
		"/v2/catalog/characters/"+idstr(env.fx.BulkCharacter)+"?include=traits", liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	var detail struct {
		Traits *[]struct {
			DisplayName string `json:"display_name"`
			Spoiler     string `json:"spoiler"`
		} `json:"traits"`
	}
	require.NoError(t, json.Unmarshal(body, &detail))
	require.NotNil(t, detail.Traits)
	require.Len(t, *detail.Traits, liveBulkRows,
		"every fixture trait is spoiler none, so the default ceiling admits all of them")
}

func liveWorksTotal(t *testing.T, env *liveEnv, path string) (*int64, map[string]json.RawMessage) {
	t.Helper()
	status, _, body := liveDo(t, env, http.MethodGet, path, liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &raw))
	var out struct {
		Total *int64 `json:"total"`
	}
	require.NoError(t, json.Unmarshal(body, &out))
	return out.Total, raw
}

func TestLiveWorksSQLLaneIncludeTotal(t *testing.T) {
	env := liveCatalog(t)

	_, raw := liveWorksTotal(t, env, "/v2/catalog/works?limit=1")
	_, present := raw["total"]
	require.False(t, present, "total must be absent, not null, without include_total")

	var want int64
	require.NoError(t, env.db.Raw(
		`SELECT count(*) FROM catalog_work w
		 WHERE w.deleted_at IS NULL AND w.status = ? AND w.medium_id = 1 AND w.content_rating <> ?
		   AND EXISTS (SELECT 1 FROM catalog_work_label wl WHERE wl.work_id = w.id AND wl.label_id = ?)`,
		model.WorkStatusLive, model.ContentRatingR18, env.fx.Company).Scan(&want).Error)
	require.Positive(t, want)

	total, _ := liveWorksTotal(t, env,
		"/v2/catalog/works?limit=1&include_total=true&company_id="+idstr(env.fx.Company))
	require.NotNil(t, total)
	require.Equal(t, want, *total, "the label-filtered total counts the whole result set, not the page")
}

func TestLiveWorksIncludeTotalFollowsNSFW(t *testing.T) {
	env := liveCatalog(t)

	sfw, _ := liveWorksTotal(t, env, "/v2/catalog/works?limit=1&include_total=true")
	nsfw, _ := liveWorksTotal(t, env, "/v2/catalog/works?limit=1&include_total=true&nsfw=true")
	require.NotNil(t, sfw)
	require.NotNil(t, nsfw)
	require.Equal(t, *sfw+1, *nsfw, "exactly the one r18 fixture work joins under nsfw=true")
}

func TestLiveWorksBatchIncludeTotal(t *testing.T) {
	env := liveCatalog(t)

	total, _ := liveWorksTotal(t, env,
		"/v2/catalog/works?include_total=true&ids="+idstr(env.fx.Work)+","+idstr(env.fx.Claimable))
	require.NotNil(t, total)
	require.Equal(t, int64(2), *total, "the batch ids flow through the same WHERE the count reads")

	total, _ = liveWorksTotal(t, env, "/v2/catalog/works?include_total=true&refs=vndb:v00000000")
	require.NotNil(t, total)
	require.Zero(t, *total, "the all-miss batch publishes 0, like every other entity lane")
}
