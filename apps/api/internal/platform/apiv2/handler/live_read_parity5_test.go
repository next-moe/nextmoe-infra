package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

type liveCompanyRow struct {
	ID    string  `json:"id"`
	Latin *string `json:"latin"`
}

func TestLiveCompanyLatinOnEveryLane(t *testing.T) {
	env := liveCatalog(t)

	status, _, body := liveDo(t, env, http.MethodGet,
		"/v2/catalog/companies/"+idstr(env.fx.Company), liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	var detail liveCompanyRow
	require.NoError(t, json.Unmarshal(body, &detail))
	require.NotNil(t, detail.Latin)
	require.Equal(t, "Raibu Burando", *detail.Latin)

	for _, path := range []string{
		"/v2/catalog/companies?limit=100",
		"/v2/catalog/companies?ids=" + idstr(env.fx.Company) + "," + idstr(env.fx.CompanyEmpty),
	} {
		status, _, body = liveDo(t, env, http.MethodGet, path, liveAppKey, "")
		require.Equal(t, 200, status, string(body))
		var page struct {
			Items []liveCompanyRow `json:"items"`
		}
		require.NoError(t, json.Unmarshal(body, &page))
		byID := map[string]*string{}
		for _, it := range page.Items {
			byID[it.ID] = it.Latin
		}
		require.NotNil(t, byID[idstr(env.fx.Company)], "%s: recorded latin must surface", path)
		require.Equal(t, "Raibu Burando", *byID[idstr(env.fx.Company)], path)
		empty, ok := byID[idstr(env.fx.CompanyEmpty)]
		if ok {
			require.Nil(t, empty, "%s: unrecorded latin stays null, never empty string", path)
		}
	}
}

func livePlaytimeList(t *testing.T, env *liveEnv, path string) (*int64, []json.RawMessage, *string, map[string]json.RawMessage) {
	t.Helper()
	status, _, body := liveDo(t, env, http.MethodGet, path, liveUserToken, "")
	require.Equal(t, 200, status, string(body))
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &raw))
	var out struct {
		Total      *int64            `json:"total"`
		Items      []json.RawMessage `json:"items"`
		NextCursor *string           `json:"next_cursor"`
	}
	require.NoError(t, json.Unmarshal(body, &out))
	return out.Total, out.Items, out.NextCursor, raw
}

func TestLiveMyPlaytimesIncludeTotal(t *testing.T) {
	env := liveCatalog(t)

	for _, id := range []int64{env.fx.Work, env.fx.BulkWork} {
		status, _, body := liveDo(t, env, http.MethodPut,
			"/v2/me/playtimes/"+idstr(id), liveUserToken, `{"minutes":30}`)
		require.Equal(t, 200, status, string(body))
	}

	_, _, _, raw := livePlaytimeList(t, env, "/v2/me/playtimes?limit=1")
	_, present := raw["total"]
	require.False(t, present, "total must be absent, not null, without include_total")

	crawled, pages := 0, 0
	path := "/v2/me/playtimes?limit=1"
	for {
		_, items, next, _ := livePlaytimeList(t, env, path)
		crawled += len(items)
		if next == nil {
			break
		}
		pages++
		require.LessOrEqual(t, pages, 50,
			"the second-truncated cursor used to re-serve the boundary row forever on same-second rows")
		requireOpaqueCursor(t, "/v2/me/playtimes", *next)
		path = "/v2/me/playtimes?limit=1&cursor=" + *next
	}
	require.GreaterOrEqual(t, crawled, 2)

	total, items, _, _ := livePlaytimeList(t, env, "/v2/me/playtimes?limit=1&include_total=true")
	require.Len(t, items, 1)
	require.NotNil(t, total)
	require.Equal(t, int64(crawled), *total, "the total counts every row, not the page and not the cursor tail")

	total, items, _, _ = livePlaytimeList(t, env,
		"/v2/me/playtimes?include_total=true&work_ids="+idstr(env.fx.Work))
	require.Len(t, items, 1)
	require.NotNil(t, total)
	require.Equal(t, int64(1), *total, "the batch total is the matched count")

	total, items, _, raw = livePlaytimeList(t, env,
		"/v2/me/playtimes?include_total=true&work_ids="+idstr(env.fx.Claimable))
	require.Empty(t, items)
	require.NotNil(t, total)
	require.Zero(t, *total, "the all-miss batch publishes 0, like every other lane")
	require.Contains(t, string(raw["missing"]), idstr(env.fx.Claimable))
}
