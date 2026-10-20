package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func liveJSON(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var out map[string]any
	require.NoError(t, json.Unmarshal(body, &out), string(body))
	delete(out, "$schema")
	return out
}

func TestLiveEditBundleIsTheSingleFacesInOneCall(t *testing.T) {
	env := liveCatalog(t)
	id := idstr(env.fx.Work)
	status, _, body := liveDo(t, env, http.MethodGet, "/v2/me/edit-bundle/work/"+id, livePlainToken, "")
	require.Equal(t, 200, status, string(body))
	bundle := liveJSON(t, body)
	assert.Equal(t, "edit_bundle", bundle["object"])

	faces := map[string]string{
		"schema":       "/v2/catalog/schemas/work",
		"snapshot":     "/v2/moderation/snapshots/work/" + id,
		"capabilities": "/v2/me/edit-capabilities/work?entity_id=" + id,
		"context":      "/v2/me/edit-context/work/" + id,
		"quota":        "/v2/me/edit-quota",
	}
	for member, path := range faces {
		status, _, body := liveDo(t, env, http.MethodGet, path, livePlainToken, "")
		require.Equal(t, 200, status, "%s: %s", path, body)
		got, ok := bundle[member].(map[string]any)
		require.True(t, ok, "bundle.%s is an object", member)
		assert.Equal(t, liveJSON(t, body), got, "bundle.%s must be the body %s answers", member, path)
	}
}

func TestLiveEditBundleRefusals(t *testing.T) {
	env := liveCatalog(t)
	for path, want := range map[string]int{
		"/v2/me/edit-bundle/work/999999999": http.StatusNotFound,
		"/v2/me/edit-bundle/nonsense/1":     http.StatusNotFound,
	} {
		status, _, body := liveDo(t, env, http.MethodGet, path, livePlainToken, "")
		assert.Equal(t, want, status, "%s: %s", path, body)
	}
	status, _, body := liveDo(t, env, http.MethodGet, "/v2/me/edit-bundle/work/"+idstr(env.fx.Work), "", "")
	assert.Equal(t, http.StatusUnauthorized, status, string(body))
}

type liveNameList struct {
	Items []struct {
		Object      string  `json:"object"`
		Key         string  `json:"key"`
		Family      string  `json:"family"`
		ID          string  `json:"id"`
		DisplayName string  `json:"display_name"`
		NameZh      *string `json:"name_zh"`
	} `json:"items"`
	Missing []string `json:"missing"`
}

func liveLaneNames(t *testing.T, env *liveEnv, collection string, ids []int64, nsfw string) map[string]string {
	t.Helper()
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = idstr(id)
	}
	status, _, body := liveDo(t, env, http.MethodGet,
		"/v2/catalog/"+collection+"?ids="+strings.Join(parts, ",")+"&nsfw="+nsfw, liveSecondReadToken, "")
	require.Equal(t, 200, status, string(body))
	var list struct {
		Items []struct {
			ID          string  `json:"id"`
			DisplayName *string `json:"display_name"`
			Title       *string `json:"title"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(body, &list))
	out := map[string]string{}
	for _, it := range list.Items {
		switch {
		case it.DisplayName != nil:
			out[it.ID] = *it.DisplayName
		case it.Title != nil:
			out[it.ID] = *it.Title
		default:
			out[it.ID] = ""
		}
	}
	return out
}

func TestLiveCatalogNamesMatchEachFamilysBatchLane(t *testing.T) {
	env := liveCatalog(t)
	fx := env.fx
	families := []struct {
		family, collection string
		ids                []int64
	}{
		{"work", "works", []int64{fx.Work, fx.NSFWMember, fx.ENWork}},
		{"tag", "tags", []int64{fx.Tag, fx.TagSexual}},
		{"company", "companies", []int64{fx.Company, fx.CompanyLogo}},
		{"engine", "engines", []int64{fx.Engine}},
		{"series", "series", []int64{fx.Series}},
		{"character", "characters", []int64{fx.Character}},
		{"credit_name", "credit-names", []int64{fx.Credit}},
		{"person", "persons", []int64{fx.Person}},
		{"role", "roles", []int64{fx.Role}},
		{"trait", "traits", []int64{fx.Trait, fx.TraitMinor}},
		{"release", "releases", []int64{fx.Release, fx.ENRelease}},
	}
	var keys []string
	for _, f := range families {
		for _, id := range f.ids {
			keys = append(keys, fmt.Sprintf("%s:%d", f.family, id))
		}
	}
	keys = append(keys, "tag:999999999", keys[0])

	visible := map[string]map[string]bool{}
	for _, nsfw := range []string{"false", "true"} {
		status, _, body := liveDo(t, env, http.MethodGet,
			"/v2/catalog/names?keys="+strings.Join(keys, ",")+"&nsfw="+nsfw, liveSecondReadToken, "")
		require.Equal(t, 200, status, string(body))
		var got liveNameList
		require.NoError(t, json.Unmarshal(body, &got), string(body))

		names := map[string]string{}
		for _, it := range got.Items {
			assert.Equal(t, "ref_name", it.Object)
			assert.Equal(t, it.Family+":"+it.ID, it.Key)
			names[it.Key] = it.DisplayName
		}
		assert.Len(t, got.Items, len(names), "a repeated key answers once")
		assert.Contains(t, got.Missing, "tag:999999999")

		visible[nsfw] = map[string]bool{}
		for _, f := range families {
			lane := liveLaneNames(t, env, f.collection, f.ids, nsfw)
			for _, id := range f.ids {
				key := fmt.Sprintf("%s:%d", f.family, id)
				want, ok := lane[idstr(id)]
				name, present := names[key]
				require.Equalf(t, ok, present, "nsfw=%s %s: names and the %s lane disagree on visibility", nsfw, key, f.collection)
				if ok {
					assert.Equalf(t, want, name, "nsfw=%s %s", nsfw, key)
				} else {
					assert.Containsf(t, got.Missing, key, "nsfw=%s", nsfw)
				}
				visible[nsfw][key] = present
			}
		}
	}
	flipped := false
	for key, shown := range visible["true"] {
		if shown && !visible["false"][key] {
			flipped = true
		}
	}
	assert.True(t, flipped, "the fixture set must include something nsfw=true reveals, or the gate is untested")
}

func TestLiveCatalogNamesPrefersARecordedChineseName(t *testing.T) {
	env := liveCatalog(t)
	require.NoError(t, env.db.Exec(`UPDATE catalog_character_trait SET name_zh = ? WHERE id = ?`, "测试特质", env.fx.Trait).Error)
	t.Cleanup(func() {
		env.db.Exec(`UPDATE catalog_character_trait SET name_zh = '' WHERE id = ?`, env.fx.Trait)
	})
	status, _, body := liveDo(t, env, http.MethodGet, "/v2/catalog/names?keys=trait:"+idstr(env.fx.Trait)+"&nsfw=true", liveSecondReadToken, "")
	require.Equal(t, 200, status, string(body))
	var got liveNameList
	require.NoError(t, json.Unmarshal(body, &got), string(body))
	require.Len(t, got.Items, 1)
	require.NotNil(t, got.Items[0].NameZh)
	assert.Equal(t, "测试特质", *got.Items[0].NameZh)
	assert.NotEqual(t, "测试特质", got.Items[0].DisplayName, "display_name stays what the traits lane answers")
}

func TestLiveCatalogNamesRefusals(t *testing.T) {
	env := liveCatalog(t)
	many := make([]string, maxNameKeys+1)
	for i := range many {
		many[i] = fmt.Sprintf("tag:%d", i+1)
	}
	for _, tc := range []struct{ query, code string }{
		{"keys=" + strings.Join(many, ","), "TOO_MANY_IDS"},
		{"keys=nonsense:1", "INVALID_PARAMETER"},
		{"keys=tag:abc", "INVALID_PARAMETER"},
		{"keys=tag:1&nsfw=maybe", "INVALID_PARAMETER"},
	} {
		status, _, body := liveDo(t, env, http.MethodGet, "/v2/catalog/names?"+tc.query, liveSecondReadToken, "")
		assert.Equal(t, http.StatusBadRequest, status, "%s: %s", tc.query, body)
		assert.Contains(t, string(body), tc.code, tc.query)
	}
	status, _, body := liveDo(t, env, http.MethodGet, "/v2/catalog/names?keys=", "", "")
	assert.Equal(t, 200, status, string(body))
	assert.Contains(t, string(body), `"items":[]`)
}
