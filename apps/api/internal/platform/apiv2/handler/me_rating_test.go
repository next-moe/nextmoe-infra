package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

type liveRating struct {
	Object       string         `json:"object"`
	ID           string         `json:"id"`
	WorkID       string         `json:"work_id"`
	UserID       string         `json:"user_id"`
	Overall      int            `json:"overall"`
	AspectScores map[string]any `json:"aspect_scores"`
	Recommend    string         `json:"recommend"`
	SpoilerLevel string         `json:"spoiler_level"`
	ShortSummary string         `json:"short_summary"`
	ClientID     string         `json:"client_id"`
	Site         string         `json:"site"`
	WorkState    map[string]any `json:"work_state"`
}

func cleanLiveRatings(t *testing.T, env *liveEnv, uids ...int64) {
	t.Helper()
	for _, uid := range uids {
		require.NoError(t, env.db.Exec(`DELETE FROM catalog_user_work_state WHERE actor_uid = ?`, uid).Error)
		require.NoError(t, env.db.Exec(`DELETE FROM catalog_user_playtime WHERE actor_uid = ?`, uid).Error)
	}
	require.NoError(t, env.db.Exec(`TRUNCATE catalog_user_work_rating, catalog_user_work_rating_tombstone`).Error)
	require.NoError(t, env.db.Exec(`DELETE FROM catalog_work_rating WHERE source_id = (SELECT id FROM catalog_source WHERE key = 'nextmoe')`).Error)
}

func problemCode(t *testing.T, body []byte) string {
	t.Helper()
	var p struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.Unmarshal(body, &p))
	return p.Code
}

func TestLiveRatingLifecycle(t *testing.T) {
	env := liveCatalog(t)
	fx := env.fx
	path := "/v2/me/ratings/" + idstr(fx.Work)
	cleanLiveRatings(t, env, liveUID, livePlainUID)
	t.Cleanup(func() { cleanLiveRatings(t, env, liveUID, livePlainUID) })

	body := `{"overall":8,"recommend":"yes","spoiler_level":"portion","short_summary":"好","aspect_scores":{"story":9}}`
	status, _, raw := liveDo(t, env, http.MethodPut, path, "", body)
	require.Equal(t, 401, status, string(raw))

	for _, bad := range []string{
		`{"overall":11,"recommend":"yes"}`,
		`{"overall":8,"recommend":"maybe"}`,
		`{"overall":8,"recommend":"yes","aspect_scores":{"voice":0}}`,
	} {
		status, _, raw = liveDo(t, env, http.MethodPut, path, liveUserToken, bad)
		require.Equalf(t, 422, status, "%s -> %s", bad, raw)
	}

	status, _, raw = liveDo(t, env, http.MethodPut, path, liveUserToken, body)
	require.Equal(t, 200, status, string(raw))
	var rec liveRating
	require.NoError(t, json.Unmarshal(raw, &rec))
	require.Equal(t, "rating", rec.Object)
	require.Equal(t, idstr(liveUID), rec.UserID)
	require.Equal(t, idstr(fx.Work), rec.WorkID)
	require.Equal(t, 8, rec.Overall)
	require.EqualValues(t, 9, rec.AspectScores["story"])
	require.Contains(t, rec.AspectScores, "art")
	require.Nil(t, rec.AspectScores["art"])
	require.Equal(t, "portion", rec.SpoilerLevel)
	require.Equal(t, liveClient, rec.ClientID)
	require.Equal(t, liveSite, rec.Site)
	require.Nil(t, rec.WorkState)

	status, _, raw = liveDo(t, env, http.MethodGet, path, liveUserToken, "")
	require.Equal(t, 200, status, string(raw))
	var got liveRating
	require.NoError(t, json.Unmarshal(raw, &got))
	require.Equal(t, rec, got)

	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/me/ratings?work_ids="+idstr(fx.Work)+",99887766", liveUserToken, "")
	require.Equal(t, 200, status, string(raw))
	var batch struct {
		Items   []liveRating `json:"items"`
		Missing []string     `json:"missing"`
	}
	require.NoError(t, json.Unmarshal(raw, &batch))
	require.Len(t, batch.Items, 1)
	require.Equal(t, []string{"99887766"}, batch.Missing)

	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/me/works?work_ids="+idstr(fx.Work), liveUserToken, "")
	require.Equal(t, 200, status, string(raw))
	var works struct {
		Items []struct {
			Rating *liveRating `json:"rating"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(raw, &works))
	require.Len(t, works.Items, 1)
	require.NotNil(t, works.Items[0].Rating)
	require.Equal(t, rec.ID, works.Items[0].Rating.ID)

	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/ratings?work_id="+idstr(fx.Work), liveAppKey, "")
	require.Equal(t, 200, status, string(raw))
	var page struct {
		Items []liveRating `json:"items"`
	}
	require.NoError(t, json.Unmarshal(raw, &page))
	require.Len(t, page.Items, 1)
	require.Equal(t, rec, page.Items[0])

	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/ratings", liveAppKey, "")
	require.Equal(t, 400, status, string(raw))

	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/ratings/"+rec.ID, liveAppKey, "")
	require.Equal(t, 200, status, string(raw))

	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/catalog/works/"+idstr(fx.Work)+"/ratings", liveAppKey, "")
	require.Equal(t, 200, status, string(raw))
	require.Contains(t, string(raw), `"source":"nextmoe"`, "the write refreshes the work's nextmoe score at once")

	status, _, raw = liveDo(t, env, http.MethodDelete, "/v2/moderation/ratings/"+rec.ID, livePlainToken, "")
	require.Equal(t, 403, status, string(raw))
	status, _, raw = liveDo(t, env, http.MethodDelete, "/v2/moderation/ratings/"+rec.ID, liveUserToken, "")
	require.Equal(t, 204, status, string(raw))
	status, _, raw = liveDo(t, env, http.MethodDelete, "/v2/moderation/ratings/"+rec.ID, liveUserToken, "")
	require.Equal(t, 404, status, string(raw))
	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/ratings/"+rec.ID, liveAppKey, "")
	require.Equal(t, 404, status, string(raw))
	status, _, raw = liveDo(t, env, http.MethodGet, path, liveUserToken, "")
	require.Equal(t, 404, status, string(raw))

	status, _, raw = liveDo(t, env, http.MethodDelete, path, liveUserToken, "")
	require.Equal(t, 204, status, string(raw))
}

type liveWorkRecord struct {
	Object    string `json:"object"`
	WorkID    string `json:"work_id"`
	WorkState *struct {
		State      string  `json:"state"`
		Completion *string `json:"completion"`
	} `json:"work_state"`
	Playtime *struct {
		Minutes int `json:"minutes"`
	} `json:"playtime"`
	Rating *liveRating `json:"rating"`
}

func TestLiveWorkRecord(t *testing.T) {
	env := liveCatalog(t)
	fx := env.fx
	path := "/v2/me/work-records/" + idstr(fx.Work)
	cleanLiveRatings(t, env, livePlainUID, liveUID)
	t.Cleanup(func() { cleanLiveRatings(t, env, livePlainUID, liveUID) })

	status, _, raw := liveDo(t, env, http.MethodGet, path, livePlainToken, "")
	require.Equal(t, 200, status, string(raw))
	var rec liveWorkRecord
	require.NoError(t, json.Unmarshal(raw, &rec))
	require.Equal(t, liveWorkRecord{Object: "work_record", WorkID: idstr(fx.Work)}, rec)

	full := `{"work_state":{"state":"done","completion":"all"},"playtime":{"minutes":90},"rating":{"overall":7,"recommend":"neutral"}}`
	status, _, raw = liveDo(t, env, http.MethodPut, path, livePlainToken, full)
	require.Equal(t, 200, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &rec))
	require.Equal(t, "done", rec.WorkState.State)
	require.Equal(t, "all", *rec.WorkState.Completion)
	require.Equal(t, 90, rec.Playtime.Minutes)
	require.Equal(t, 7, rec.Rating.Overall)
	require.Equal(t, "none", rec.Rating.SpoilerLevel)
	require.Equal(t, map[string]any{"state": "done", "completion": "all"}, rec.Rating.WorkState,
		"a rating carries its author's play state")

	status, _, raw = liveDo(t, env, http.MethodPut, path, livePlainToken,
		`{"work_state":{"state":"wish","completion":"all"},"rating":{"overall":9,"recommend":"yes"}}`)
	require.Equal(t, 422, status, string(raw))
	require.Contains(t, string(raw), `"/work_state/completion"`)
	status, _, raw = liveDo(t, env, http.MethodGet, path, livePlainToken, "")
	require.Equal(t, 200, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &rec))
	require.Equal(t, 7, rec.Rating.Overall, "a refused PUT writes no part")

	status, _, raw = liveDo(t, env, http.MethodPut, path, liveThirdPartyToken, full)
	require.Equal(t, 200, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &rec))
	require.Equal(t, 7, rec.Rating.Overall, "a third-party app writes the rating too")
	require.Equal(t, liveThirdPartyClient, rec.Rating.ClientID)
	status, _, raw = liveDo(t, env, http.MethodPut, path, liveUserToken, full)
	require.Equal(t, 200, status, string(raw))
	status, _, raw = liveDo(t, env, http.MethodPut, path, liveThirdPartyToken, `{"work_state":{"state":"doing"}}`)
	require.Equal(t, 200, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &rec))
	require.Equal(t, "doing", rec.WorkState.State)
	require.NotNil(t, rec.Rating, "a third-party PUT without a rating leaves the rating alone")

	status, _, raw = liveDo(t, env, http.MethodPut, path, livePlainToken, `{}`)
	require.Equal(t, 200, status, string(raw))
	require.NoError(t, json.Unmarshal(raw, &rec))
	require.Nil(t, rec.WorkState)
	require.Nil(t, rec.Playtime)
	require.Nil(t, rec.Rating)
}
