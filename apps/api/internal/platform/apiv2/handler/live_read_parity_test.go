package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"api/internal/platform/apiv2/problem"

	"github.com/stretchr/testify/require"
)

func TestLiveWorksRefsAllMissIsEmpty(t *testing.T) {
	env := liveCatalog(t)
	status, _, body := liveDo(t, env, http.MethodGet, "/v2/catalog/works?refs=vndb:v00000000", liveAppKey, "")
	require.Equal(t, 200, status)
	var out struct {
		Items   []json.RawMessage `json:"items"`
		Missing []string          `json:"missing"`
	}
	require.NoError(t, json.Unmarshal(body, &out))
	require.Empty(t, out.Items)
	require.Equal(t, []string{"vndb:v00000000"}, out.Missing)
}

func TestLiveWorksRefsPartialMiss(t *testing.T) {
	env := liveCatalog(t)
	status, _, body := liveDo(t, env, http.MethodGet,
		"/v2/catalog/works?refs=vndb:"+env.fx.AnchorExt+",vndb:v00000000", liveAppKey, "")
	require.Equal(t, 200, status)
	var out struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		Missing []string `json:"missing"`
	}
	require.NoError(t, json.Unmarshal(body, &out))
	require.Len(t, out.Items, 1)
	require.Equal(t, idstr(env.fx.Anchored), out.Items[0].ID)
	require.Equal(t, []string{"vndb:v00000000"}, out.Missing)
}

func TestLiveCompaniesHasWorks(t *testing.T) {
	env := liveCatalog(t)
	var out struct {
		Items []struct {
			ID        string `json:"id"`
			WorkCount int    `json:"work_count"`
		} `json:"items"`
		Total *int64 `json:"total"`
	}
	for _, nsfw := range []string{"", "&nsfw=true"} {
		status, _, body := liveDo(t, env, http.MethodGet, "/v2/catalog/companies?has_works=true&include_total=true"+nsfw, liveAppKey, "")
		require.Equal(t, 200, status)
		require.NoError(t, json.Unmarshal(body, &out))
		require.Len(t, out.Items, 4, "every company but the empty one has a work, claimed or not")
		require.NotNil(t, out.Total)
		require.EqualValues(t, 4, *out.Total)
		for _, it := range out.Items {
			require.NotEqual(t, idstr(env.fx.CompanyEmpty), it.ID)
			total, _ := liveWorksTotal(t, env, "/v2/catalog/works?limit=1&include_total=true&company_id="+it.ID+nsfw)
			require.NotNil(t, total)
			require.EqualValues(t, *total, it.WorkCount, "company %s%s: work_count against its works list", it.ID, nsfw)
		}
	}

	status, _, body := liveDo(t, env, http.MethodGet, "/v2/catalog/companies", liveAppKey, "")
	require.Equal(t, 200, status)
	require.NoError(t, json.Unmarshal(body, &out))
	require.Len(t, out.Items, 5, "without the flag every company lists, including the rollup pair")
}

func TestLiveCompanyIncludeBlocks(t *testing.T) {
	env := liveCatalog(t)
	status, _, body := liveDo(t, env, http.MethodGet,
		"/v2/catalog/companies/"+idstr(env.fx.Company)+"?include=aliases,intros,links", liveAppKey, "")
	require.Equal(t, 200, status)
	var out struct {
		Aliases *[]struct {
			Value     string `json:"value"`
			Lang      string `json:"lang"`
			AliasKind string `json:"alias_kind"`
		} `json:"aliases"`
		Intros *[]json.RawMessage `json:"intros"`
		Links  *[]json.RawMessage `json:"links"`
		Logo   *json.RawMessage   `json:"logo"`
	}
	require.NoError(t, json.Unmarshal(body, &out))
	require.NotNil(t, out.Aliases)
	require.Len(t, *out.Aliases, 1)
	require.Equal(t, "ライブブランド", (*out.Aliases)[0].Value)
	require.Equal(t, "spelling_variant", (*out.Aliases)[0].AliasKind)
	require.NotNil(t, out.Intros)
	require.NotNil(t, out.Links)
	require.Nil(t, out.Logo)

	status, _, body = liveDo(t, env, http.MethodGet, "/v2/catalog/companies/"+idstr(env.fx.Company), liveAppKey, "")
	require.Equal(t, 200, status)
	var bare map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &bare))
	for _, k := range []string{"aliases", "intros", "links", "logo"} {
		_, ok := bare[k]
		require.False(t, ok, "unrequested block %s must be absent", k)
	}
}

func TestLiveCompaniesBatchDetailLane(t *testing.T) {
	env := liveCatalog(t)
	status, _, body := liveDo(t, env, http.MethodGet,
		"/v2/catalog/companies?ids="+idstr(env.fx.Company)+"&include=aliases,links", liveAppKey, "")
	require.Equal(t, 200, status)
	var out struct {
		Items []struct {
			ID      string             `json:"id"`
			Aliases *[]json.RawMessage `json:"aliases"`
			Links   *[]json.RawMessage `json:"links"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(body, &out))
	require.Len(t, out.Items, 1)
	require.NotNil(t, out.Items[0].Aliases)
	require.Len(t, *out.Items[0].Aliases, 1)
	require.NotNil(t, out.Items[0].Links)
}

func TestLiveTagSexualFlag(t *testing.T) {
	env := liveCatalog(t)
	status, _, body := liveDo(t, env, http.MethodGet, "/v2/catalog/tags/"+idstr(env.fx.TagSexual), liveAppKey, "")
	require.Equal(t, 200, status)
	var tag struct {
		IsSexual bool `json:"is_sexual"`
	}
	require.NoError(t, json.Unmarshal(body, &tag))
	require.True(t, tag.IsSexual)

	status, _, body = liveDo(t, env, http.MethodGet, "/v2/catalog/tags", liveAppKey, "")
	require.Equal(t, 200, status)
	var list struct {
		Items []struct {
			ID       string `json:"id"`
			IsSexual bool   `json:"is_sexual"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(body, &list))
	flags := map[string]bool{}
	for _, it := range list.Items {
		flags[it.ID] = it.IsSexual
	}
	require.False(t, flags[idstr(env.fx.Tag)])
	require.True(t, flags[idstr(env.fx.TagSexual)])
}

func TestLiveSeriesWorkCount(t *testing.T) {
	env := liveCatalog(t)
	status, _, body := liveDo(t, env, http.MethodGet, "/v2/catalog/series/"+idstr(env.fx.Series), liveAppKey, "")
	require.Equal(t, 200, status)
	var ser struct {
		WorkCount int `json:"work_count"`
	}
	require.NoError(t, json.Unmarshal(body, &ser))
	require.Equal(t, 2, ser.WorkCount, "the claimed and the unclaimed all-ages members; the r18 one stays hidden")

	status, _, body = liveDo(t, env, http.MethodGet, "/v2/catalog/series", liveAppKey, "")
	require.Equal(t, 200, status)
	var list struct {
		Items []struct {
			ID        string `json:"id"`
			WorkCount int    `json:"work_count"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(body, &list))
	require.Len(t, list.Items, 1)
	require.Equal(t, 2, list.Items[0].WorkCount)
}

func TestLiveEngineCarriesDescriptionAliases(t *testing.T) {
	env := liveCatalog(t)
	status, _, body := liveDo(t, env, http.MethodGet, "/v2/catalog/engines/"+idstr(env.fx.Engine), liveAppKey, "")
	require.Equal(t, 200, status)
	var eng struct {
		Description string    `json:"description"`
		Aliases     *[]string `json:"aliases"`
	}
	require.NoError(t, json.Unmarshal(body, &eng))
	require.Equal(t, "test", eng.Description)
	require.NotNil(t, eng.Aliases)
}

func TestLiveRoleFace(t *testing.T) {
	env := liveCatalog(t)
	// The seeded registry sits under this face, so the default page is a full
	// 20 with a cursor, not the fixture row alone.
	status, _, body := liveDo(t, env, http.MethodGet, "/v2/catalog/roles", liveAppKey, "")
	require.Equal(t, 200, status)
	var list struct {
		Items []struct {
			Object      string `json:"object"`
			ID          string `json:"id"`
			Key         string `json:"key"`
			Category    string `json:"category"`
			DisplayName string `json:"display_name"`
			Localized   map[string]struct {
				Value string `json:"value"`
			} `json:"localized"`
		} `json:"items"`
		NextCursor *string `json:"next_cursor"`
	}
	require.NoError(t, json.Unmarshal(body, &list), string(body))
	require.Len(t, list.Items, 20)
	require.NotNil(t, list.NextCursor)
	require.Equal(t, "role", list.Items[0].Object)

	status, _, body = liveDo(t, env, http.MethodGet,
		"/v2/catalog/roles?ids="+idstr(env.fx.Role), liveAppKey, "")
	require.Equal(t, 200, status)
	require.NoError(t, json.Unmarshal(body, &list))
	require.Len(t, list.Items, 1)
	require.Equal(t, "live原画", list.Items[0].Key)
	require.Equal(t, "原画", list.Items[0].DisplayName)
	require.Equal(t, "原画", list.Items[0].Localized["zh-Hans"].Value)
	require.Equal(t, "原画", list.Items[0].Localized["ja"].Value)

	status, _, body = liveDo(t, env, http.MethodGet, "/v2/catalog/roles/"+idstr(env.fx.Role), liveAppKey, "")
	require.Equal(t, 200, status)
	var det struct {
		Key string `json:"key"`
	}
	require.NoError(t, json.Unmarshal(body, &det))
	require.Equal(t, "live原画", det.Key)

	status, _, body = liveDo(t, env, http.MethodGet, "/v2/catalog/roles/424242", liveAppKey, "")
	require.Equal(t, 404, status)
	var p problem.Problem
	require.NoError(t, json.Unmarshal(body, &p))
	require.Equal(t, problem.CodeNotFound, p.Code)
}

func TestLiveCharacterIncludeTraits(t *testing.T) {
	env := liveCatalog(t)
	status, _, body := liveDo(t, env, http.MethodGet,
		"/v2/catalog/characters/"+idstr(env.fx.Character)+"?include=traits,image,figure", liveAppKey, "")
	require.Equal(t, 200, status)
	var out struct {
		Traits *[]struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
			Spoiler     string `json:"spoiler"`
		} `json:"traits"`
	}
	require.NoError(t, json.Unmarshal(body, &out))
	require.NotNil(t, out.Traits)
	require.Len(t, *out.Traits, 1)
	require.Equal(t, idstr(env.fx.Trait), (*out.Traits)[0].ID)
	require.Equal(t, "none", (*out.Traits)[0].Spoiler)

	status, _, body = liveDo(t, env, http.MethodGet, "/v2/catalog/characters/"+idstr(env.fx.Character), liveAppKey, "")
	require.Equal(t, 200, status)
	var bare map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &bare))
	_, ok := bare["traits"]
	require.False(t, ok, "traits must be absent without include=")
}

func TestLiveCompanyUnknownIncludeRejected(t *testing.T) {
	env := liveCatalog(t)
	status, _, _ := liveDo(t, env, http.MethodGet,
		"/v2/catalog/companies/"+idstr(env.fx.Company)+"?include=nope", liveAppKey, "")
	require.Equal(t, 400, status)
}
