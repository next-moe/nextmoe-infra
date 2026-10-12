package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

type liveRatingBlock struct {
	Source       string `json:"source"`
	VoteCount    int    `json:"vote_count"`
	Distribution *[]struct {
		Score float64 `json:"score"`
		Count int     `json:"count"`
	} `json:"distribution"`
	Stats *struct {
		Average *float64 `json:"average"`
		Stdev   *float64 `json:"stdev"`
		Min     *float64 `json:"min"`
		Max     *float64 `json:"max"`
	} `json:"stats"`
}

func liveRatingsBySource(t *testing.T, raw []byte) map[string]liveRatingBlock {
	t.Helper()
	var rows []liveRatingBlock
	require.NoError(t, json.Unmarshal(raw, &rows))
	out := make(map[string]liveRatingBlock, len(rows))
	for _, r := range rows {
		out[r.Source] = r
	}
	return out
}

func TestLiveWorkRatingsCarryHistogram(t *testing.T) {
	env := liveCatalog(t)
	status, _, body := liveDo(t, env, http.MethodGet,
		"/v2/catalog/works/"+idstr(env.fx.Work)+"?include=ratings", liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	var detail struct {
		Ratings json.RawMessage `json:"ratings"`
	}
	require.NoError(t, json.Unmarshal(body, &detail))
	bySource := liveRatingsBySource(t, detail.Ratings)

	bgm, ok := bySource["bangumi"]
	require.True(t, ok, string(body))
	require.NotNil(t, bgm.Distribution)
	require.Equal(t, []float64{7, 9, 10}, []float64{
		(*bgm.Distribution)[0].Score, (*bgm.Distribution)[1].Score, (*bgm.Distribution)[2].Score,
	}, "buckets ascend on the source-native scale")
	require.Equal(t, []int{3, 7, 2}, []int{
		(*bgm.Distribution)[0].Count, (*bgm.Distribution)[1].Count, (*bgm.Distribution)[2].Count,
	})
	require.NotNil(t, bgm.Stats)
	require.NotNil(t, bgm.Stats.Average)
	require.InDelta(t, 8.1, *bgm.Stats.Average, 0.001)
	require.NotNil(t, bgm.Stats.Stdev)
	require.Nil(t, bgm.Stats.Min)

	dl, ok := bySource["dlsite"]
	require.True(t, ok)
	require.Nil(t, dl.Distribution, "a rating row with no histogram must omit the key")
	require.Nil(t, dl.Stats)

	status, _, body = liveDo(t, env, http.MethodGet,
		"/v2/catalog/works/"+idstr(env.fx.Work)+"/ratings", liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	var page struct {
		Items json.RawMessage `json:"items"`
	}
	require.NoError(t, json.Unmarshal(body, &page))
	sub := liveRatingsBySource(t, page.Items)
	require.NotNil(t, sub["bangumi"].Distribution)
	require.NotNil(t, sub["bangumi"].Stats)
}

func TestLiveWorksListRatingsHaveNoHistogram(t *testing.T) {
	env := liveCatalog(t)
	status, _, body := liveDo(t, env, http.MethodGet,
		"/v2/catalog/works?ids="+idstr(env.fx.Work)+"&include=ratings", liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	var out struct {
		Items []struct {
			Ratings []map[string]json.RawMessage `json:"ratings"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(body, &out))
	require.Len(t, out.Items, 1)
	require.NotEmpty(t, out.Items[0].Ratings)
	for _, r := range out.Items[0].Ratings {
		_, hasDist := r["distribution"]
		_, hasStats := r["stats"]
		require.False(t, hasDist, "the list ratings block must not carry the histogram")
		require.False(t, hasStats, "the list ratings block must not carry the spread")
	}
}

func TestLiveWorkCompaniesCarryLogo(t *testing.T) {
	env := liveCatalog(t)
	status, _, body := liveDo(t, env, http.MethodGet,
		"/v2/catalog/works/"+idstr(env.fx.Work)+"?include=companies", liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	var out struct {
		Companies []map[string]json.RawMessage `json:"companies"`
	}
	require.NoError(t, json.Unmarshal(body, &out))
	require.Len(t, out.Companies, 2)
	logos := map[string]json.RawMessage{}
	for _, c := range out.Companies {
		var id string
		require.NoError(t, json.Unmarshal(c["id"], &id))
		logos[id] = c["logo"]
	}
	withLogo, ok := logos[idstr(env.fx.CompanyLogo)]
	require.True(t, ok)
	require.NotNil(t, withLogo)
	var img struct {
		URL  string `json:"url"`
		Hash string `json:"hash"`
	}
	require.NoError(t, json.Unmarshal(withLogo, &img))
	require.Equal(t, liveLogoHash, img.Hash)
	require.Contains(t, img.URL, liveCDNBase)

	plain, ok := logos[idstr(env.fx.Company)]
	require.True(t, ok)
	require.Nil(t, plain, "a company without a logo hash must have no logo key")
}

func TestLiveTagIncludeIntros(t *testing.T) {
	env := liveCatalog(t)
	status, _, body := liveDo(t, env, http.MethodGet,
		"/v2/catalog/tags/"+idstr(env.fx.Tag)+"?include=intros", liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	var out struct {
		Intros *[]struct {
			Lang      string `json:"lang"`
			Value     string `json:"value"`
			IsMachine bool   `json:"is_machine"`
		} `json:"intros"`
	}
	require.NoError(t, json.Unmarshal(body, &out))
	require.NotNil(t, out.Intros)
	require.Len(t, *out.Intros, 1)
	require.Equal(t, "zh-Hans", (*out.Intros)[0].Lang)
	require.Equal(t, "标签说明", (*out.Intros)[0].Value)
	require.False(t, (*out.Intros)[0].IsMachine)

	status, _, body = liveDo(t, env, http.MethodGet, "/v2/catalog/tags/"+idstr(env.fx.Tag), liveAppKey, "")
	require.Equal(t, 200, status)
	var bare map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &bare))
	_, ok := bare["intros"]
	require.False(t, ok, "intros must be absent without include=intros")

	status, _, _ = liveDo(t, env, http.MethodGet,
		"/v2/catalog/tags/"+idstr(env.fx.Tag)+"?include=nope", liveAppKey, "")
	require.Equal(t, 400, status)
}

func TestLiveSeriesNSFWAndBlocks(t *testing.T) {
	env := liveCatalog(t)
	status, _, body := liveDo(t, env, http.MethodGet,
		"/v2/catalog/series/"+idstr(env.fx.Series)+"?include=intros,refs", liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	var out struct {
		WorkCount int  `json:"work_count"`
		HasNSFW   bool `json:"has_nsfw"`
		Intros    *[]struct {
			Lang      string `json:"lang"`
			Value     string `json:"value"`
			IsMachine bool   `json:"is_machine"`
		} `json:"intros"`
		Refs *[]struct {
			Source     string `json:"source"`
			ExternalID string `json:"external_id"`
		} `json:"refs"`
	}
	require.NoError(t, json.Unmarshal(body, &out))
	require.Equal(t, 2, out.WorkCount, "the r18 member stays out of the sfw count")
	require.True(t, out.HasNSFW)
	require.NotNil(t, out.Intros)
	require.Len(t, *out.Intros, 1)
	require.Equal(t, "シリーズ説明", (*out.Intros)[0].Value)
	require.False(t, (*out.Intros)[0].IsMachine)
	require.NotNil(t, out.Refs)
	require.Len(t, *out.Refs, 1)
	require.Equal(t, "s-live-1", (*out.Refs)[0].ExternalID)

	status, _, body = liveDo(t, env, http.MethodGet, "/v2/catalog/series/"+idstr(env.fx.Series), liveAppKey, "")
	require.Equal(t, 200, status)
	var bare map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &bare))
	for _, k := range []string{"intros", "refs"} {
		_, ok := bare[k]
		require.False(t, ok, "unrequested block %s must be absent", k)
	}
	require.Contains(t, bare, "has_nsfw")

	status, _, body = liveDo(t, env, http.MethodGet, "/v2/catalog/series", liveAppKey, "")
	require.Equal(t, 200, status)
	var list struct {
		Items []struct {
			ID      string `json:"id"`
			HasNSFW bool   `json:"has_nsfw"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(body, &list))
	require.Len(t, list.Items, 1)
	require.Equal(t, idstr(env.fx.Series), list.Items[0].ID)
	require.True(t, list.Items[0].HasNSFW)
}

func TestLiveCreditNameStaffFace(t *testing.T) {
	env := liveCatalog(t)
	status, _, body := liveDo(t, env, http.MethodGet,
		"/v2/catalog/credit-names/"+idstr(env.fx.Credit), liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	var bare map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &bare))
	var gender string
	require.NoError(t, json.Unmarshal(bare["gender"], &gender))
	require.Equal(t, "female", gender)
	var year, month int
	require.NoError(t, json.Unmarshal(bare["birth_year"], &year))
	require.NoError(t, json.Unmarshal(bare["birth_month"], &month))
	require.Equal(t, 1979, year)
	require.Equal(t, 4, month)
	_, hasDay := bare["birth_day"]
	require.False(t, hasDay, "the birth parts are independently fuzzy")
	for _, k := range []string{"aliases", "photo", "siblings", "intros", "links", "refs"} {
		_, ok := bare[k]
		require.False(t, ok, "unrequested block %s must be absent", k)
	}

	status, _, body = liveDo(t, env, http.MethodGet,
		"/v2/catalog/credit-names/"+idstr(env.fx.Credit)+"?include=aliases,photo,siblings,intros,links,refs",
		liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	var full struct {
		Aliases *[]struct {
			Value     string `json:"value"`
			AliasKind string `json:"alias_kind"`
		} `json:"aliases"`
		Photo *struct {
			Hash string `json:"hash"`
			URL  string `json:"url"`
		} `json:"photo"`
		Siblings *[]struct {
			Object      string  `json:"object"`
			ID          string  `json:"id"`
			DisplayName string  `json:"display_name"`
			PersonID    *string `json:"person_id"`
		} `json:"siblings"`
		Intros *[]struct {
			Lang  string `json:"lang"`
			Value string `json:"value"`
		} `json:"intros"`
		Links *[]struct {
			Source string `json:"source"`
			URL    string `json:"url"`
		} `json:"links"`
		Refs *[]struct {
			Source     string `json:"source"`
			ExternalID string `json:"external_id"`
		} `json:"refs"`
	}
	require.NoError(t, json.Unmarshal(body, &full))
	require.NotNil(t, full.Aliases)
	require.Len(t, *full.Aliases, 1)
	require.Equal(t, "Live Credit Alias", (*full.Aliases)[0].Value)
	require.Equal(t, "translation", (*full.Aliases)[0].AliasKind)
	require.NotNil(t, full.Photo)
	require.Equal(t, livePhotoHash, full.Photo.Hash)
	require.Contains(t, full.Photo.URL, liveCDNBase)
	require.NotNil(t, full.Siblings)
	require.Len(t, *full.Siblings, 1)
	require.Equal(t, "credit_name", (*full.Siblings)[0].Object)
	require.Equal(t, idstr(env.fx.Sibling), (*full.Siblings)[0].ID)
	require.NotNil(t, (*full.Siblings)[0].PersonID)
	require.Equal(t, idstr(env.fx.Person), *(*full.Siblings)[0].PersonID)
	require.NotNil(t, full.Intros)
	require.Len(t, *full.Intros, 1)
	require.Equal(t, "人物简介", (*full.Intros)[0].Value)
	require.NotNil(t, full.Links)
	require.Len(t, *full.Links, 1)
	require.Equal(t, "https://x.com/live_person", (*full.Links)[0].URL)
	require.NotNil(t, full.Refs)
	require.Len(t, *full.Refs, 1)
	require.Equal(t, "s-live-va", (*full.Refs)[0].ExternalID)

	status, _, _ = liveDo(t, env, http.MethodGet,
		"/v2/catalog/credit-names/"+idstr(env.fx.Credit)+"?include=nope", liveAppKey, "")
	require.Equal(t, 400, status)
}

func TestLiveCreditNameCreditsCarryCharacterName(t *testing.T) {
	env := liveCatalog(t)
	status, _, body := liveDo(t, env, http.MethodGet,
		"/v2/catalog/credit-names/"+idstr(env.fx.Credit)+"/credits", liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	var page struct {
		Items []struct {
			Roles []struct {
				RoleKey       string  `json:"role_key"`
				CharacterID   *string `json:"character_id"`
				CharacterName *string `json:"character_name"`
			} `json:"roles"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(body, &page))
	require.Len(t, page.Items, 1)
	roles := map[string]struct {
		id   *string
		name *string
	}{}
	for _, r := range page.Items[0].Roles {
		roles[r.RoleKey] = struct {
			id   *string
			name *string
		}{r.CharacterID, r.CharacterName}
	}
	voice, ok := roles["voice-actor"]
	require.True(t, ok, string(body))
	require.NotNil(t, voice.id)
	require.Equal(t, idstr(env.fx.Character), *voice.id)
	require.NotNil(t, voice.name)
	require.Equal(t, "Live Char", *voice.name)

	other, ok := roles["other-staff"]
	require.True(t, ok, string(body))
	require.Nil(t, other.id)
	require.Nil(t, other.name, "a non-voice credit names no character")
}
