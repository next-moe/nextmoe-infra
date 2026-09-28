package handler

import (
	"encoding/json"
	"net/http"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

type liveCalendarPage struct {
	Items []struct {
		ID string `json:"id"`
	} `json:"items"`
	Meta *struct {
		Today    string  `json:"today"`
		MinMonth *string `json:"min_month"`
		MaxMonth *string `json:"max_month"`
		HasPrev  *bool   `json:"has_prev"`
		HasNext  *bool   `json:"has_next"`
	} `json:"meta"`
}

func liveCalendar(t *testing.T, env *liveEnv, query string) liveCalendarPage {
	t.Helper()
	status, _, body := liveDo(t, env, http.MethodGet, "/v2/catalog/calendar?"+query, liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	var out liveCalendarPage
	require.NoError(t, json.Unmarshal(body, &out))
	return out
}

func TestLiveCalendarContentLimitAndOLang(t *testing.T) {
	env := liveCatalog(t)

	status, _, body := liveDo(t, env, http.MethodGet,
		"/v2/catalog/calendar?month=2024-01&content_limit=bogus", liveAppKey, "")
	require.Equal(t, 400, status, string(body))

	page := liveCalendar(t, env, "month=2024-01&content_limit=sfw")
	require.Len(t, page.Items, 1)
	require.Equal(t, idstr(env.fx.Work), page.Items[0].ID)

	page = liveCalendar(t, env, "month=2024-01&content_limit=nsfw")
	require.Empty(t, page.Items, "the only dated work is unclaimed all_ages, outside the nsfw display axis")

	page = liveCalendar(t, env, "month=2024-01&olang=en")
	require.Empty(t, page.Items, "olang= must gate the window, not be silently ignored")

	page = liveCalendar(t, env, "month=2024-01&olang=all")
	require.Len(t, page.Items, 1)
}

func TestLiveCalendarMeta(t *testing.T) {
	env := liveCatalog(t)
	today := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

	page := liveCalendar(t, env, "month=2024-01")
	require.NotNil(t, page.Meta)
	require.Regexp(t, today, page.Meta.Today)
	require.NotNil(t, page.Meta.MinMonth)
	require.Equal(t, "2024-01", *page.Meta.MinMonth)
	require.NotNil(t, page.Meta.MaxMonth)
	require.Equal(t, "2024-01", *page.Meta.MaxMonth)
	require.NotNil(t, page.Meta.HasPrev)
	require.False(t, *page.Meta.HasPrev)
	require.NotNil(t, page.Meta.HasNext)
	require.False(t, *page.Meta.HasNext)

	page = liveCalendar(t, env, "month=2030-05")
	require.Empty(t, page.Items)
	require.NotNil(t, page.Meta)
	require.NotNil(t, page.Meta.HasPrev)
	require.True(t, *page.Meta.HasPrev, "2024-01 has dated releases behind 2030-05")
	require.NotNil(t, page.Meta.HasNext)
	require.False(t, *page.Meta.HasNext)

	page = liveCalendar(t, env, "precision=year&year=2024")
	require.NotNil(t, page.Meta)
	require.Regexp(t, today, page.Meta.Today)
	require.Nil(t, page.Meta.HasPrev, "month navigation belongs to the dated month window only")
	require.Nil(t, page.Meta.MinMonth)
}

func TestLiveWorkRosterVoicesAndIdentity(t *testing.T) {
	env := liveCatalog(t)
	status, _, body := liveDo(t, env, http.MethodGet,
		"/v2/catalog/works/"+idstr(env.fx.Work)+"?include=characters", liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	var out struct {
		Characters *[]json.RawMessage `json:"characters"`
	}
	require.NoError(t, json.Unmarshal(body, &out))
	require.NotNil(t, out.Characters)

	type rosterRow struct {
		ID         string  `json:"id"`
		RosterRole string  `json:"roster_role"`
		Spoiler    string  `json:"spoiler"`
		Identity   *string `json:"identity"`
		Voices     []struct {
			Object      string `json:"object"`
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"voices"`
	}
	rows := map[string]rosterRow{}
	rawByID := map[string]map[string]json.RawMessage{}
	for _, raw := range *out.Characters {
		var row rosterRow
		require.NoError(t, json.Unmarshal(raw, &row))
		var m map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(raw, &m))
		rows[row.ID] = row
		rawByID[row.ID] = m
	}

	rostered := rows[idstr(env.fx.Character)]
	require.Equal(t, "main", rostered.RosterRole)
	require.NotNil(t, rostered.Identity)
	require.NotEmpty(t, *rostered.Identity)
	require.Len(t, rostered.Voices, 1)
	require.Equal(t, "credit_name", rostered.Voices[0].Object)
	require.Equal(t, idstr(env.fx.Credit), rostered.Voices[0].ID)
	require.Equal(t, "Live Credit", rostered.Voices[0].DisplayName)

	vaOnly := rows[idstr(env.fx.VAOnlyCharacter)]
	require.Equal(t, "unknown", vaOnly.RosterRole)
	require.Equal(t, "none", vaOnly.Spoiler)
	_, present := rawByID[idstr(env.fx.VAOnlyCharacter)]["identity"]
	require.False(t, present, "a voice-credit-only row has no roster row to suppress: identity absent, not null")
	require.Len(t, vaOnly.Voices, 1)
	require.Equal(t, "Second Voice", vaOnly.Voices[0].DisplayName)
}

func TestLiveWorkCoversCarryKind(t *testing.T) {
	env := liveCatalog(t)
	status, _, body := liveDo(t, env, http.MethodGet,
		"/v2/catalog/works/"+idstr(env.fx.Work)+"?include=covers", liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	var out struct {
		Covers *[]struct {
			ID   string `json:"id"`
			Kind string `json:"cover_kind"`
			Hash string `json:"hash"`
		} `json:"covers"`
	}
	require.NoError(t, json.Unmarshal(body, &out))
	require.NotNil(t, out.Covers)
	require.Len(t, *out.Covers, 1)
	require.Equal(t, idstr(env.fx.Cover), (*out.Covers)[0].ID)
	require.Equal(t, "main", (*out.Covers)[0].Kind)
	require.Equal(t, liveCoverHash, (*out.Covers)[0].Hash)
}

func TestLiveWorksRollupViaCompany(t *testing.T) {
	env := liveCatalog(t)
	status, _, body := liveDo(t, env, http.MethodGet,
		"/v2/catalog/works?company_id="+idstr(env.fx.RollupCo)+"&company_rollup=true", liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	var page struct {
		Items []json.RawMessage `json:"items"`
	}
	require.NoError(t, json.Unmarshal(body, &page))
	require.Len(t, page.Items, 2)

	type viaRow struct {
		ID  string `json:"id"`
		Via *struct {
			Object      string `json:"object"`
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"via_company"`
	}
	byID := map[string]viaRow{}
	keys := map[string]map[string]json.RawMessage{}
	for _, raw := range page.Items {
		var row viaRow
		require.NoError(t, json.Unmarshal(raw, &row))
		var m map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(raw, &m))
		byID[row.ID] = row
		keys[row.ID] = m
	}

	_, present := keys[idstr(env.fx.RollupDirect)]["via_company"]
	require.False(t, present, "a directly attributed row carries no via_company key")

	via := byID[idstr(env.fx.RollupVia)].Via
	require.NotNil(t, via, "the imprint-reached row names the intermediate company")
	require.Equal(t, "company", via.Object)
	require.Equal(t, idstr(env.fx.RollupImprint), via.ID)
	require.Equal(t, "Rollup Imprint", via.DisplayName)

	status, _, body = liveDo(t, env, http.MethodGet,
		"/v2/catalog/works?company_id="+idstr(env.fx.RollupCo), liveAppKey, "")
	require.Equal(t, 200, status)
	require.NoError(t, json.Unmarshal(body, &page))
	require.Len(t, page.Items, 1, "without rollup only the direct attribution matches")
}

func TestLiveTagsHasWorks(t *testing.T) {
	env := liveCatalog(t)

	status, _, body := liveDo(t, env, http.MethodGet, "/v2/catalog/tags?has_works=bogus", liveAppKey, "")
	require.Equal(t, 400, status, string(body))

	var page struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		Total *int64 `json:"total"`
	}
	status, _, body = liveDo(t, env, http.MethodGet, "/v2/catalog/tags", liveAppKey, "")
	require.Equal(t, 200, status)
	require.NoError(t, json.Unmarshal(body, &page))
	require.Len(t, page.Items, 2, "without the flag every canonical tag lists")

	status, _, body = liveDo(t, env, http.MethodGet,
		"/v2/catalog/tags?has_works=true&include_total=true", liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	require.NoError(t, json.Unmarshal(body, &page))
	require.Len(t, page.Items, 1)
	require.Equal(t, idstr(env.fx.Tag), page.Items[0].ID)
	require.NotNil(t, page.Total)
	require.EqualValues(t, 1, *page.Total, "the total converges with the filter")
}

// The forum's diff saw works?ids= answer a row whose detail 404s; that pair is
// only reachable by mixing nsfw= across lanes. Pin all four lanes to one gate.
func TestLiveWorksBatchHonorsNSFWGate(t *testing.T) {
	env := liveCatalog(t)

	var page struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		Missing *[]string `json:"missing"`
	}
	status, _, body := liveDo(t, env, http.MethodGet,
		"/v2/catalog/works?ids="+idstr(env.fx.NSFWMember), liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	require.NoError(t, json.Unmarshal(body, &page))
	require.Empty(t, page.Items, "an r18 work must not leak through the ids= batch without nsfw=true")
	require.NotNil(t, page.Missing)
	require.Contains(t, *page.Missing, idstr(env.fx.NSFWMember))

	status, _, body = liveDo(t, env, http.MethodGet,
		"/v2/catalog/works?ids="+idstr(env.fx.NSFWMember)+"&nsfw=true", liveAppKey, "")
	require.Equal(t, 200, status)
	require.NoError(t, json.Unmarshal(body, &page))
	require.Len(t, page.Items, 1)

	status, _, _ = liveDo(t, env, http.MethodGet,
		"/v2/catalog/works/"+idstr(env.fx.NSFWMember), liveAppKey, "")
	require.Equal(t, 404, status)
	status, _, _ = liveDo(t, env, http.MethodGet,
		"/v2/catalog/works/"+idstr(env.fx.NSFWMember)+"?nsfw=true", liveAppKey, "")
	require.Equal(t, 200, status)
}

func TestLiveCalendarExcludeCompanyKind(t *testing.T) {
	env := liveCatalog(t)

	status, _, body := liveDo(t, env, http.MethodGet,
		"/v2/catalog/calendar?month=2024-01&exclude_company_kind=circle", liveAppKey, "")
	require.Equal(t, 400, status, string(body))
	require.Contains(t, string(body), "exclude_company_kind")

	status, _, body = liveDo(t, env, http.MethodGet,
		"/v2/catalog/calendar?month=2024-01&exclude_company_kind=none,doujin_circle", liveAppKey, "")
	require.Equal(t, 400, status, "none only stands alone: %s", body)

	for _, q := range []string{"month=2024-01", "month=2024-01&exclude_company_kind=doujin_circle", "month=2024-01&exclude_company_kind=none"} {
		page := liveCalendar(t, env, q)
		require.Len(t, page.Items, 1, "%s: the dated work's companies are game brands, so no circle rule drops it", q)
		require.Equal(t, idstr(env.fx.Work), page.Items[0].ID)
	}

	page := liveCalendar(t, env, "month=2024-01&exclude_company_kind=game_brand,doujin_circle")
	require.Empty(t, page.Items, "every company of the only dated work is an excluded kind")
	require.NotNil(t, page.Meta)
	require.Nil(t, page.Meta.MinMonth, "month navigation must follow the excluded population")
	require.NotNil(t, page.Meta.HasPrev)
	require.False(t, *page.Meta.HasPrev)
	require.NotNil(t, page.Meta.HasNext)
	require.False(t, *page.Meta.HasNext)
}
