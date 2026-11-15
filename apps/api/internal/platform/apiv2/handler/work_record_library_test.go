package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"api/internal/platform/catalog/model"
	catsvc "api/internal/platform/catalog/service"

	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

type liveLibraryPage struct {
	liveWorkRecordList
	PlaytimeTotalMinutes *int64 `json:"playtime_total_minutes"`
}

func TestLiveWorkRecordsSortAndRecordSets(t *testing.T) {
	env := liveCatalog(t)
	fx := env.fx
	ctx := context.Background()
	uid := liveSecondPlainUID
	cleanup := func() {
		cleanLiveRatings(t, env, uid)
		require.NoError(t, env.db.Exec(`DELETE FROM catalog_user_privacy WHERE actor_uid = ?`, uid).Error)
	}
	cleanup()
	t.Cleanup(cleanup)

	newWork := func(name string) int64 {
		t.Helper()
		empty := datatypes.JSON([]byte("{}"))
		w := &model.CatalogWork{
			MediumID: 1, OLang: "ja", DisplayName: name,
			ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive,
			Extra: empty, FieldProvenance: empty,
		}
		require.NoError(t, env.db.Create(w).Error)
		t.Cleanup(func() {
			cleanLiveRatings(t, env, uid)
			require.NoError(t, env.db.Delete(&model.CatalogWork{}, w.ID).Error)
		})
		return w.ID
	}
	playtimeOnly := newWork("Library Playtime Only")
	ratingOnly := newWork("Library Rating Only")
	doing := newWork("Library Doing")

	put := func(work int64, in catsvc.WorkRecordInput) {
		t.Helper()
		_, err := env.cat.WorkRecords.Put(ctx, uid, work, liveClient, liveSite, in)
		require.NoError(t, err)
	}
	all := model.WorkCompletionAll
	put(fx.Work, catsvc.WorkRecordInput{
		WorkState: &catsvc.WorkStateInput{State: model.WorkStateDone, Completion: &all},
		Minutes:   ptrInt(720),
		Rating:    &catsvc.RatingInput{Overall: 9, Recommend: model.RecommendYes},
	})
	put(fx.NSFWMember, catsvc.WorkRecordInput{WorkState: &catsvc.WorkStateInput{State: model.WorkStateWish}})
	put(playtimeOnly, catsvc.WorkRecordInput{Minutes: ptrInt(300)})
	put(ratingOnly, catsvc.WorkRecordInput{Rating: &catsvc.RatingInput{Overall: 5, Recommend: model.RecommendYes}})
	put(doing, catsvc.WorkRecordInput{
		WorkState: &catsvc.WorkStateInput{State: model.WorkStateDoing},
		Minutes:   ptrInt(720),
	})

	list := func(q url.Values) (int, []byte, liveLibraryPage) {
		t.Helper()
		q.Set("user_id", idstr(uid))
		status, _, raw := liveDo(t, env, http.MethodGet, "/v2/work-records?"+q.Encode(), liveAppKey, "")
		var out liveLibraryPage
		if status == 200 {
			require.NoError(t, json.Unmarshal(raw, &out), string(raw))
		}
		return status, raw, out
	}
	ids := func(p liveLibraryPage) []string {
		out := make([]string, 0, len(p.Items))
		for _, it := range p.Items {
			out = append(out, it.WorkID)
		}
		return out
	}
	walk := func(q url.Values, limit string) []string {
		t.Helper()
		var out []string
		q.Set("limit", limit)
		for range 10 {
			status, raw, page := list(q)
			require.Equal(t, 200, status, string(raw))
			out = append(out, ids(page)...)
			if page.NextCursor == nil {
				return out
			}
			q.Set("cursor", *page.NextCursor)
		}
		t.Fatal("cursor never ran out")
		return nil
	}
	sortBoth := func(a, b int64) (int64, int64) {
		if a > b {
			return a, b
		}
		return b, a
	}
	hi720, lo720 := sortBoth(fx.Work, doing)

	status, raw, page := list(url.Values{"include_total": {"true"}, "include_playtime_total": {"true"}})
	require.Equal(t, 200, status, string(raw))
	require.Equal(t, int64(3), *page.Total, "record=state stays the default and lists only works with a state")
	require.Equal(t, int64(1440), *page.PlaytimeTotalMinutes)

	want := []string{idstr(hi720), idstr(lo720), idstr(playtimeOnly)}
	require.Equal(t, want, walk(url.Values{"record": {"playtime"}, "sort": {"playtime_desc"}}, "1"),
		"equal minutes tie-break on work_id in the sort's direction, page by page")
	require.Equal(t, []string{idstr(playtimeOnly), idstr(lo720), idstr(hi720)},
		walk(url.Values{"record": {"playtime"}, "sort": {"playtime_asc"}}, "2"))

	nullsDesc := []int64{fx.NSFWMember, playtimeOnly, doing}
	require.Equal(t, append([]string{idstr(fx.Work), idstr(ratingOnly)}, idsDesc(nullsDesc)...),
		walk(url.Values{"record": {"any"}, "sort": {"rating_desc"}}, "2"),
		"works with no rating come last and page through on work_id")
	require.Equal(t, append([]string{idstr(ratingOnly), idstr(fx.Work)}, idsAsc(nullsDesc)...),
		walk(url.Values{"record": {"any"}, "sort": {"rating_asc"}}, "1"))

	desc := walk(url.Values{"record": {"any"}}, "2")
	require.Len(t, desc, 5)
	asc := walk(url.Values{"record": {"any"}, "sort": {"updated_asc"}}, "3")
	for i := range desc {
		require.Equal(t, desc[i], asc[len(asc)-1-i])
	}

	status, raw, page = list(url.Values{"record": {"any"}, "state": {"none"}, "facets": {"state"}, "include_total": {"true"}, "include_playtime_total": {"true"}})
	require.Equal(t, 200, status, string(raw))
	require.ElementsMatch(t, []string{idstr(playtimeOnly), idstr(ratingOnly)}, ids(page))
	for _, it := range page.Items {
		require.Nil(t, it.WorkState)
	}
	require.Equal(t, int64(2), *page.Total)
	require.Equal(t, int64(300), *page.PlaytimeTotalMinutes)
	buckets := map[string]int{}
	var order []string
	for _, b := range page.Facets["state"] {
		buckets[b.Value] = b.Count
		order = append(order, b.Value)
	}
	require.Equal(t, []string{"wish", "doing", "done", "on_hold", "dropped", "none"}, order)
	require.Equal(t, 2, buckets["none"])

	status, raw, page = list(url.Values{"record": {"playtime"}, "facets": {"state"}})
	require.Equal(t, 200, status, string(raw))
	require.Len(t, page.Items, 3)
	byValue := map[string]int{}
	for _, b := range page.Facets["state"] {
		byValue[b.Value] = b.Count
	}
	require.Equal(t, map[string]int{"wish": 0, "doing": 1, "done": 1, "on_hold": 0, "dropped": 0, "none": 1}, byValue)

	status, raw, page = list(url.Values{"facets": {"state"}})
	require.Equal(t, 200, status, string(raw))
	require.Len(t, page.Facets["state"], 5, "record=state keeps its five buckets")

	for _, bad := range []url.Values{
		{"state": {"none"}},
		{"record": {"all"}},
		{"sort": {"minutes_desc"}},
		{"include_playtime_total": {"yes"}},
	} {
		status, raw, _ = list(bad)
		require.Equalf(t, 400, status, "%v -> %s", bad, raw)
	}

	status, raw, page = list(url.Values{"record": {"playtime"}, "sort": {"playtime_desc"}, "limit": {"1"}})
	require.Equal(t, 200, status, string(raw))
	require.NotNil(t, page.NextCursor)
	status, raw, _ = list(url.Values{"record": {"playtime"}, "sort": {"rating_desc"}, "cursor": {*page.NextCursor}})
	require.Equal(t, 400, status, "a cursor belongs to the sort that issued it: %s", raw)
	status, raw, _ = list(url.Values{"record": {"playtime"}, "cursor": {*page.NextCursor}})
	require.Equal(t, 400, status, "the default sort rejects another sort's cursor too: %s", raw)
}

func idsDesc(v []int64) []string {
	out := idsAsc(v)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func idsAsc(v []int64) []string {
	s := append([]int64(nil), v...)
	for i := range s {
		for j := i + 1; j < len(s); j++ {
			if s[j] < s[i] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
	out := make([]string, len(s))
	for i, n := range s {
		out[i] = idstr(n)
	}
	return out
}

func TestLivePublicCompanyFollows(t *testing.T) {
	env := liveCatalog(t)
	fx := env.fx
	ctx := context.Background()
	uid := liveSecondPlainUID
	cleanup := func() {
		require.NoError(t, env.db.Exec(`DELETE FROM catalog_user_entity_follow WHERE actor_uid = ?`, uid).Error)
		require.NoError(t, env.db.Exec(`DELETE FROM catalog_user_privacy WHERE actor_uid = ?`, uid).Error)
	}
	cleanup()
	t.Cleanup(cleanup)
	_, err := env.cat.CompanyFollows.FollowCompany(ctx, uid, fx.Company, liveClient, liveSite)
	require.NoError(t, err)

	list := func(token string, q url.Values) (int, []byte) {
		t.Helper()
		status, _, raw := liveDo(t, env, http.MethodGet, "/v2/followed-companies?"+q.Encode(), token, "")
		return status, raw
	}
	mine := url.Values{"user_id": {idstr(uid)}, "include_total": {"true"}}

	status, raw := list(liveAppKey, url.Values{})
	require.Equal(t, 400, status, string(raw))
	status, raw = list("", mine)
	require.Equal(t, 401, status, string(raw))

	status, raw = list(liveAppKey, mine)
	require.Equal(t, 403, status, "private until the account opts in: %s", raw)
	require.Equal(t, "FOLLOWED_COMPANIES_PRIVATE", problemCode(t, raw))
	status, raw = list(liveSecondReadToken, mine)
	require.Equal(t, 200, status, "the account reads its own list: %s", raw)

	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/me/privacy", liveSecondPlainToken, "")
	require.Equal(t, 200, status, string(raw))
	var p struct {
		PlayRecords       string `json:"play_records"`
		FollowedCompanies string `json:"followed_companies"`
	}
	require.NoError(t, json.Unmarshal(raw, &p))
	require.Equal(t, "public", p.PlayRecords)
	require.Equal(t, "private", p.FollowedCompanies)

	status, _, raw = liveDo(t, env, http.MethodPatch, "/v2/me/privacy", liveSecondPlainToken, `{"followed_companies":"public"}`)
	require.Equal(t, 200, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &p))
	require.Equal(t, "public", p.FollowedCompanies)
	require.Equal(t, "public", p.PlayRecords, "a switch the body does not name is left as it is")

	status, raw = list(liveAppKey, mine)
	require.Equal(t, 200, status, string(raw))
	var page struct {
		Items []struct {
			Object    string `json:"object"`
			CompanyID string `json:"company_id"`
		} `json:"items"`
		Total *int64 `json:"total"`
	}
	require.NoError(t, json.Unmarshal(raw, &page))
	require.Len(t, page.Items, 1)
	require.Equal(t, "company_follow", page.Items[0].Object)
	require.Equal(t, idstr(fx.Company), page.Items[0].CompanyID)
	require.Equal(t, int64(1), *page.Total)

	status, _, raw = liveDo(t, env, http.MethodPatch, "/v2/me/privacy", liveSecondPlainToken, `{"play_records":"private"}`)
	require.Equal(t, 200, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &p))
	require.Equal(t, "public", p.FollowedCompanies, "the two switches are independent")
	status, raw = list(liveAppKey, mine)
	require.Equal(t, 200, status, string(raw))
}
