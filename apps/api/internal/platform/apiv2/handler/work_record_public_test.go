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

type liveWorkRecordList struct {
	Items []struct {
		Object    string `json:"object"`
		WorkID    string `json:"work_id"`
		WorkState *struct {
			State      string  `json:"state"`
			Completion *string `json:"completion"`
		} `json:"work_state"`
		Playtime *struct {
			Minutes int `json:"minutes"`
		} `json:"playtime"`
		Rating *struct {
			Overall int    `json:"overall"`
			UserID  string `json:"user_id"`
		} `json:"rating"`
	} `json:"items"`
	NextCursor *string `json:"next_cursor"`
	Total      *int64  `json:"total"`
	Facets     map[string][]struct {
		Value string `json:"value"`
		Count int    `json:"count"`
	} `json:"facets"`
}

type livePrivacy struct {
	Object      string  `json:"object"`
	PlayRecords string  `json:"play_records"`
	UpdatedAt   *string `json:"updated_at"`
}

func TestLivePublicWorkRecords(t *testing.T) {
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

	all := model.WorkCompletionAll
	_, err := env.cat.WorkRecords.Put(ctx, uid, fx.Work, liveClient, liveSite, catsvc.WorkRecordInput{
		WorkState: &catsvc.WorkStateInput{State: model.WorkStateDone, Completion: &all},
		Minutes:   ptrInt(720),
		Rating:    &catsvc.RatingInput{Overall: 9, Recommend: model.RecommendYes},
	})
	require.NoError(t, err)
	_, err = env.cat.WorkStates.Set(ctx, uid, fx.NSFWMember, model.WorkStateWish, nil)
	require.NoError(t, err)

	empty := datatypes.JSON([]byte("{}"))
	gone := &model.CatalogWork{
		MediumID: 1, OLang: "ja", DisplayName: "Quarantined Library Work",
		ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive,
		Extra: empty, FieldProvenance: empty,
	}
	require.NoError(t, env.db.Create(gone).Error)
	t.Cleanup(func() {
		require.NoError(t, env.db.Exec(`DELETE FROM catalog_user_work_state WHERE work_id = ?`, gone.ID).Error)
		require.NoError(t, env.db.Delete(&model.CatalogWork{}, gone.ID).Error)
	})
	_, err = env.cat.WorkStates.Set(ctx, uid, gone.ID, model.WorkStateDropped, nil)
	require.NoError(t, err)
	require.NoError(t, env.db.Model(&model.CatalogWork{}).Where("id = ?", gone.ID).
		Update("status", model.WorkStatusQuarantine).Error)

	list := func(token string, q url.Values) (int, []byte, liveWorkRecordList) {
		t.Helper()
		q.Set("user_id", idstr(uid))
		status, _, raw := liveDo(t, env, http.MethodGet, "/v2/work-records?"+q.Encode(), token, "")
		var out liveWorkRecordList
		if status == 200 {
			require.NoError(t, json.Unmarshal(raw, &out), string(raw))
		}
		return status, raw, out
	}

	status, _, raw := liveDo(t, env, http.MethodGet, "/v2/work-records?user_id="+idstr(uid), "", "")
	require.Equal(t, 401, status, string(raw))
	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/work-records", liveAppKey, "")
	require.Equal(t, 400, status, string(raw))
	for _, bad := range []url.Values{{"state": {"finished"}}, {"content_limit": {"r18"}}, {"facets": {"site"}}} {
		status, raw, _ = list(liveAppKey, bad)
		require.Equalf(t, 400, status, "%v -> %s", bad, raw)
	}

	status, raw, page := list(liveAppKey, url.Values{})
	require.Equal(t, 200, status, string(raw))
	require.Len(t, page.Items, 2, "the quarantined work stays off the library")
	require.Equal(t, idstr(fx.NSFWMember), page.Items[0].WorkID, "newest state first")
	require.Equal(t, "wish", page.Items[0].WorkState.State)
	require.Nil(t, page.Items[0].Playtime)
	require.Nil(t, page.Items[0].Rating)
	done := page.Items[1]
	require.Equal(t, "work_record", done.Object)
	require.Equal(t, idstr(fx.Work), done.WorkID)
	require.Equal(t, "done", done.WorkState.State)
	require.Equal(t, "all", *done.WorkState.Completion)
	require.Equal(t, 720, done.Playtime.Minutes)
	require.Equal(t, 9, done.Rating.Overall)
	require.Equal(t, idstr(uid), done.Rating.UserID)

	status, raw, page = list(liveAppKey, url.Values{"limit": {"1"}})
	require.Equal(t, 200, status, string(raw))
	require.Len(t, page.Items, 1)
	require.NotNil(t, page.NextCursor)
	status, raw, page = list(liveAppKey, url.Values{"limit": {"1"}, "cursor": {*page.NextCursor}})
	require.Equal(t, 200, status, string(raw))
	require.Len(t, page.Items, 1)
	require.Equal(t, idstr(fx.Work), page.Items[0].WorkID)
	require.Nil(t, page.NextCursor)

	status, raw, page = list(liveAppKey, url.Values{"state": {"done,dropped"}})
	require.Equal(t, 200, status, string(raw))
	require.Len(t, page.Items, 1)
	require.Equal(t, idstr(fx.Work), page.Items[0].WorkID)

	counts := func(p liveWorkRecordList) map[string]int {
		out := map[string]int{}
		var order []string
		for _, b := range p.Facets["state"] {
			out[b.Value] = b.Count
			order = append(order, b.Value)
		}
		require.Equal(t, []string{"wish", "doing", "done", "on_hold", "dropped"}, order)
		return out
	}
	status, raw, page = list(liveAppKey, url.Values{"facets": {"state"}, "include_total": {"true"}})
	require.Equal(t, 200, status, string(raw))
	require.Equal(t, int64(2), *page.Total)
	require.Equal(t, map[string]int{"wish": 1, "doing": 0, "done": 1, "on_hold": 0, "dropped": 0}, counts(page))

	status, raw, page = list(liveAppKey, url.Values{"content_limit": {"sfw"}, "facets": {"state"}, "include_total": {"true"}})
	require.Equal(t, 200, status, string(raw))
	require.Len(t, page.Items, 1)
	require.Equal(t, idstr(fx.Work), page.Items[0].WorkID)
	require.Equal(t, int64(1), *page.Total)
	require.Equal(t, map[string]int{"wish": 0, "doing": 0, "done": 1, "on_hold": 0, "dropped": 0}, counts(page))

	status, raw, page = list(liveAppKey, url.Values{"content_limit": {"nsfw"}})
	require.Equal(t, 200, status, string(raw))
	require.Len(t, page.Items, 1)
	require.Equal(t, idstr(fx.NSFWMember), page.Items[0].WorkID)

	privacy := func(method, token, body string) (int, []byte, livePrivacy) {
		t.Helper()
		status, _, raw := liveDo(t, env, method, "/v2/me/privacy", token, body)
		var out livePrivacy
		if status == 200 {
			require.NoError(t, json.Unmarshal(raw, &out), string(raw))
		}
		return status, raw, out
	}
	status, raw, p := privacy(http.MethodGet, liveSecondPlainToken, "")
	require.Equal(t, 200, status, string(raw))
	require.Equal(t, livePrivacy{Object: "privacy", PlayRecords: "public"}, p)

	status, raw, _ = privacy(http.MethodPatch, liveSecondPlainToken, `{"play_records":"friends"}`)
	require.Equal(t, 422, status, string(raw))

	status, raw, p = privacy(http.MethodPatch, liveSecondPlainToken, `{"play_records":"private"}`)
	require.Equal(t, 200, status, string(raw))
	require.Equal(t, "private", p.PlayRecords)
	require.NotNil(t, p.UpdatedAt)

	status, raw, p = privacy(http.MethodPatch, liveSecondPlainToken, `{}`)
	require.Equal(t, 200, status, string(raw))
	require.Equal(t, "private", p.PlayRecords, "an empty body changes nothing")

	status, raw, _ = list(liveAppKey, url.Values{})
	require.Equal(t, 403, status, string(raw))
	require.Equal(t, "PLAY_RECORDS_PRIVATE", problemCode(t, raw))
	status, raw, page = list(liveSecondReadToken, url.Values{})
	require.Equal(t, 200, status, "the account reads its own private records: %s", raw)
	require.Len(t, page.Items, 2)

	status, raw, p = privacy(http.MethodPatch, liveSecondPlainToken, `{"play_records":"public"}`)
	require.Equal(t, 200, status, string(raw))
	require.Equal(t, "public", p.PlayRecords)
	status, raw, page = list(liveAppKey, url.Values{})
	require.Equal(t, 200, status, string(raw))
	require.Len(t, page.Items, 2)
}

func ptrInt(v int) *int { return &v }
