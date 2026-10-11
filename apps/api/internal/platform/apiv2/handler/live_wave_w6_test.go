package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"

	"github.com/stretchr/testify/require"
)

type w6Work struct {
	ID        string             `json:"id"`
	Latin     *string            `json:"latin"`
	Localized map[string]w6Local `json:"localized"`
	Tags      *[]w6Tag           `json:"tags"`
	Credits   *[]w6CreditGroup   `json:"credits"`
	Intros    *[]struct {
		Lang string `json:"lang"`
	} `json:"intros"`
	Companies *[]struct {
		ID string `json:"id"`
	} `json:"companies"`
	Ratings *[]struct {
		Source string `json:"source"`
	} `json:"ratings"`
	Refs *[]struct {
		Source string `json:"source"`
	} `json:"refs"`
	Playtimes *[]struct {
		Source    string `json:"source"`
		Minutes   int    `json:"minutes"`
		VoteCount int    `json:"vote_count"`
	} `json:"playtimes"`
}

type w6Local struct {
	Value string `json:"value"`
}

type w6Tag struct {
	ID          *string `json:"id"`
	DisplayName string  `json:"display_name"`
	Source      string  `json:"source"`
	Spoiler     string  `json:"spoiler"`
}

type w6CreditGroup struct {
	RoleKey string `json:"role_key"`
	Credits []struct {
		ID       string `json:"id"`
		Identity string `json:"identity"`
	} `json:"credits"`
}

func w6ListItem(t *testing.T, env *liveEnv, path, id string) w6Work {
	t.Helper()
	var page struct {
		Items []w6Work `json:"items"`
	}
	w4Get(t, env, path, &page)
	for _, it := range page.Items {
		if it.ID == id {
			return it
		}
	}
	t.Fatalf("%s: work %s is missing from the page", path, id)
	return w6Work{}
}

func w6Problem(t *testing.T, env *liveEnv, path string) *problem.Problem {
	t.Helper()
	status, _, body := liveDo(t, env, http.MethodGet, path, liveAppKey, "")
	require.Equal(t, 400, status, "%s -> %s", path, body)
	var p problem.Problem
	require.NoError(t, json.Unmarshal(body, &p), string(body))
	return &p
}

// The works list declared all eighteen detail include tokens and hydrated four
// blocks, so include=tags,credits was a 200 with neither block present and no
// error. A consumer probed for a 400, got none, and shipped a per-work detail
// read on its busiest page to recover what the list had accepted an ask for.
func TestLiveWorksListHydratesTagsAndCredits(t *testing.T) {
	env := liveCatalog(t)
	id := idstr(env.fx.Work)

	var detail w6Work
	w4Get(t, env, "/v2/catalog/works/"+id+"?include=tags,credits", &detail)
	require.NotNil(t, detail.Tags)
	require.NotEmpty(t, *detail.Tags)
	require.NotNil(t, detail.Credits)
	require.NotEmpty(t, *detail.Credits)

	for _, path := range []string{
		"/v2/catalog/works?include=tags,credits&limit=100",
		"/v2/catalog/works?include=tags,credits&ids=" + id,
	} {
		it := w6ListItem(t, env, path, id)
		require.NotNil(t, it.Tags, "%s published no tags block", path)
		require.Equal(t, *detail.Tags, *it.Tags, "%s: the list and detail tags blocks must agree", path)
		require.NotNil(t, it.Credits, "%s published no credits block", path)
		require.Equal(t, *detail.Credits, *it.Credits, "%s: the list and detail credits blocks must agree", path)
	}

	// Not asking still means not answering: the blocks are include-gated, and a
	// consumer that reads their absence as "this work has none" must be right.
	bare := w6ListItem(t, env, "/v2/catalog/works?limit=100", id)
	require.Nil(t, bare.Tags)
	require.Nil(t, bare.Credits)
}

// The credits block carries the D9 row identity and the D24/roster suppression
// rules. Widening one query rather than writing a batch copy is what keeps the
// two faces byte-identical; this pins that they are.
func TestLiveWorksListCreditsMatchTheSubResource(t *testing.T) {
	env := liveCatalog(t)
	id := idstr(env.fx.Work)

	var sub struct {
		Items []w6CreditGroup `json:"items"`
	}
	w4Get(t, env, "/v2/catalog/works/"+id+"/credits?limit=100", &sub)
	require.NotEmpty(t, sub.Items)

	it := w6ListItem(t, env, "/v2/catalog/works?include=credits&ids="+id, id)
	require.NotNil(t, it.Credits)
	require.Equal(t, sub.Items, *it.Credits)
}

// The tokens the list cannot serve used to be accepted and answered by
// nothing. They are refused through the vocabulary the collection declares, so
// the refusal is the same UNKNOWN_INCLUDE any other unknown token gets.
func TestLiveWorksListRefusesDetailOnlyIncludes(t *testing.T) {
	env := liveCatalog(t)
	id := idstr(env.fx.Work)

	detailOnly := []string{
		"relations", "releases", "popularity", "series",
		"platforms", "screenshots", "characters", "engines", "links",
	}
	for _, tok := range detailOnly {
		require.NotContains(t, collect.WorkListInclude, tok)
		require.Contains(t, collect.WorkInclude, tok, "the token must still exist on the detail face")

		p := w6Problem(t, env, "/v2/catalog/works?include="+tok)
		require.Equal(t, problem.CodeUnknownInclude, p.Code, tok)
		p = w6Problem(t, env, "/v2/catalog/calendar?month=2024-01&include="+tok)
		require.Equal(t, problem.CodeUnknownInclude, p.Code, tok)

		status, _, body := liveDo(t, env, http.MethodGet,
			"/v2/catalog/works/"+id+"?include="+tok, liveAppKey, "")
		require.Equal(t, 200, status, "%s must stay a detail token: %s", tok, body)
	}
}

// view=full unioned a twelve-token FULL_SET into a face that emitted four, so
// the one request that means "everything you have" was the widest silent gap.
func TestLiveWorksListViewFullIsTheListFullSet(t *testing.T) {
	env := liveCatalog(t)
	id := idstr(env.fx.Work)

	require.NotContains(t, collect.WorkListFullSet, "credits")
	require.NotContains(t, collect.WorkListFullSet, "playtimes")
	for _, tok := range collect.WorkListFullSet {
		require.Contains(t, collect.WorkListInclude, tok)
	}

	it := w6ListItem(t, env, "/v2/catalog/works?view=full&ids="+id, id)
	require.NotNil(t, it.Intros)
	require.NotNil(t, it.Companies)
	require.NotNil(t, it.Ratings)
	require.NotNil(t, it.Refs)
	require.NotNil(t, it.Tags)
	require.Contains(t, it.Localized, "zh-Hans")
	require.Nil(t, it.Credits, "credits is an explicit ask on the list, exactly as on the detail face")
	require.Nil(t, it.Playtimes, "playtimes is an explicit ask on the list, exactly as on the detail face")
}

// A game manager's integration (2026-10-11) planned three requests per game.
// Playtime was the one block it needed that the ids= batch could not carry.
func TestLiveWorksListHydratesPlaytimes(t *testing.T) {
	env := liveCatalog(t)
	id := idstr(env.fx.Work)

	var detail w6Work
	w4Get(t, env, "/v2/catalog/works/"+id+"?include=playtimes", &detail)
	require.NotNil(t, detail.Playtimes)
	require.Len(t, *detail.Playtimes, 2, "the fixture must carry playtimes or this test cannot fail")

	for _, path := range []string{
		"/v2/catalog/works?include=playtimes&limit=100",
		"/v2/catalog/works?include=playtimes&ids=" + id,
		"/v2/catalog/works?view=full&include=playtimes&ids=" + id,
	} {
		it := w6ListItem(t, env, path, id)
		require.NotNil(t, it.Playtimes, "%s published no playtimes block", path)
		require.Equal(t, *detail.Playtimes, *it.Playtimes, "%s: the list and detail playtimes blocks must agree", path)
	}

	bare := w6ListItem(t, env, "/v2/catalog/works?limit=100", id)
	require.Nil(t, bare.Playtimes)
}

// The calendar shares workFromListItem and enrichWorkListItems with the works
// list but had its own include mapper, which never learned titles. include=
// titles on the calendar therefore dropped latin and localized[] — the two
// fields every card renders a Chinese name from.
func TestLiveCalendarHonoursTheListVocabulary(t *testing.T) {
	env := liveCatalog(t)
	id := idstr(env.fx.Work)

	bare := w6ListItem(t, env, "/v2/catalog/calendar?month=2024-01", id)
	require.Empty(t, bare.Localized)
	require.Nil(t, bare.Tags)

	// Compared against the works list rather than against fixture literals: the
	// claim is that one work reads the same through either collection, and the
	// two reads are taken together so an unrelated edit cannot decide it.
	for _, tok := range collect.WorkListInclude {
		cal := w6ListItem(t, env, "/v2/catalog/calendar?month=2024-01&include="+tok, id)
		list := w6ListItem(t, env, "/v2/catalog/works?include="+tok+"&ids="+id, id)
		require.Equal(t, list, cal, "include=%s reads differently on the calendar", tok)
	}

	named := w6ListItem(t, env, "/v2/catalog/calendar?month=2024-01&include=titles", id)
	require.NotEmpty(t, named.Localized, "the fixture must carry a localized title or this test cannot fail")

	tagged := w6ListItem(t, env, "/v2/catalog/calendar?month=2024-01&include=tags", id)
	require.NotNil(t, tagged.Tags)
	require.NotEmpty(t, *tagged.Tags)
}

// A machine consumer discovers the vocabulary here. Publishing only the detail
// set would hand it tokens the collection face refuses.
func TestLiveWorkSchemaPublishesBothVocabularies(t *testing.T) {
	env := liveCatalog(t)

	var got struct {
		Include     []string `json:"include"`
		FullSet     []string `json:"full_set"`
		ListInclude []string `json:"list_include"`
		ListFullSet []string `json:"list_full_set"`
	}
	w4Get(t, env, "/v2/catalog/schemas/work", &got)
	require.Equal(t, collect.WorkInclude, got.Include)
	require.Equal(t, collect.WorkFullSet, got.FullSet)
	require.Equal(t, collect.WorkListInclude, got.ListInclude)
	require.Equal(t, collect.WorkListFullSet, got.ListFullSet)
	require.NotEqual(t, got.Include, got.ListInclude,
		"work is the family whose two faces differ; equal lists here mean the split was lost")

	for _, object := range []string{"company", "character", "tag", "series", "engine", "release"} {
		var same struct {
			Include     []string `json:"include"`
			ListInclude []string `json:"list_include"`
		}
		w4Get(t, env, "/v2/catalog/schemas/"+object, &same)
		require.Equal(t, same.Include, same.ListInclude, object)
	}
}
