package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type myWorksPage struct {
	Object string `json:"object"`
	Items  []struct {
		Object    string   `json:"object"`
		WorkID    string   `json:"work_id"`
		FolderIDs []string `json:"folder_ids"`
		Playtime  *struct {
			Object  string `json:"object"`
			WorkID  string `json:"work_id"`
			Minutes int    `json:"minutes"`
		} `json:"playtime"`
		WorkState *struct {
			Object     string  `json:"object"`
			WorkID     string  `json:"work_id"`
			State      string  `json:"state"`
			Completion *string `json:"completion"`
		} `json:"work_state"`
	} `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

func seedPlaytime(t *testing.T, db *gorm.DB, uid, workID int64, clientID string, minutes int) {
	t.Helper()
	row := &model.CatalogUserPlaytime{ActorUID: uid, WorkID: workID, ClientID: clientID, Minutes: minutes}
	require.NoError(t, db.Create(row).Error)
	t.Cleanup(func() { db.Delete(row) })
}

func seedWorkState(t *testing.T, db *gorm.DB, uid, workID int64, state int16, completion *int16) {
	t.Helper()
	row := &model.CatalogUserWorkState{ActorUID: uid, WorkID: workID, State: state, Completion: completion}
	require.NoError(t, db.Create(row).Error)
	t.Cleanup(func() { db.Delete(row) })
}

func TestLiveMyWorksAnswersEveryAskedWork(t *testing.T) {
	env := liveCatalog(t)

	full := seedHoldableWork(t, env.db, "My Works Full")
	a := seedFolderHolding(t, env.db, liveUID, full, model.FolderVisibilityPrivate, "My Works A")
	b := seedFolderHolding(t, env.db, liveUID, full, model.FolderVisibilityPublic, "My Works B")
	seedPlaytime(t, env.db, liveUID, full, "desktop", 30)
	seedPlaytime(t, env.db, liveUID, full, "phone", 90)
	main := model.WorkCompletionMain
	seedWorkState(t, env.db, liveUID, full, model.WorkStateDone, &main)

	stateOnly := seedHoldableWork(t, env.db, "My Works State Only")
	seedWorkState(t, env.db, liveUID, stateOnly, model.WorkStateWish, nil)

	strangers := seedHoldableWork(t, env.db, "My Works Stranger")
	seedFolderHolding(t, env.db, livePlainUID, strangers, model.FolderVisibilityPublic, "Stranger's")
	seedPlaytime(t, env.db, livePlainUID, strangers, "desktop", 45)
	seedWorkState(t, env.db, livePlainUID, strangers, model.WorkStateDoing, nil)

	asked := []string{idstr(stateOnly), idstr(full), "99887766", idstr(strangers), idstr(full)}
	status, _, raw := liveDo(t, env, http.MethodGet, "/v2/me/works?work_ids="+strings.Join(asked, ","), liveUserToken, "")
	require.Equal(t, 200, status, string(raw))

	var page myWorksPage
	require.NoError(t, json.Unmarshal(raw, &page), string(raw))
	require.Equal(t, "list", page.Object)
	require.Nil(t, page.NextCursor, "this lane does not paginate")
	require.Len(t, page.Items, 4, "one item per distinct work, the repeated id answered once")
	var order []string
	for _, it := range page.Items {
		require.Equal(t, "user_work", it.Object)
		order = append(order, it.WorkID)
	}
	require.Equal(t, []string{idstr(stateOnly), idstr(full), "99887766", idstr(strangers)}, order)

	st := page.Items[0]
	require.Empty(t, st.FolderIDs)
	require.NotNil(t, st.FolderIDs, "folder_ids is an empty array, never null")
	require.Nil(t, st.Playtime)
	require.NotNil(t, st.WorkState)
	require.Equal(t, "wish", st.WorkState.State)
	require.Nil(t, st.WorkState.Completion)

	f := page.Items[1]
	require.Equal(t, []string{idstr(min(a, b)), idstr(max(a, b))}, f.FolderIDs)
	require.NotNil(t, f.Playtime)
	require.Equal(t, 90, f.Playtime.Minutes, "the most minutes any client reported, as /v2/me/playtimes answers")
	require.Equal(t, idstr(full), f.Playtime.WorkID)
	require.NotNil(t, f.WorkState)
	require.Equal(t, "done", f.WorkState.State)
	require.Equal(t, "main", *f.WorkState.Completion)

	for _, it := range page.Items[2:] {
		require.Empty(t, it.FolderIDs, "%s: another account's rows are never the bearer's", it.WorkID)
		require.Nil(t, it.Playtime, it.WorkID)
		require.Nil(t, it.WorkState, it.WorkID)
	}

	// The batch lanes this face stands in for answer the same values.
	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/me/playtimes?work_ids="+idstr(full), liveUserToken, "")
	require.Equal(t, 200, status, string(raw))
	require.Contains(t, string(raw), `"minutes":90`)

	status, _, raw = liveDo(t, env, http.MethodGet,
		"/v2/me/work-states?work_ids="+idstr(full)+",99887766,"+idstr(stateOnly), liveUserToken, "")
	require.Equal(t, 200, status, string(raw))
	var states struct {
		Items []struct {
			WorkID string `json:"work_id"`
			State  string `json:"state"`
		} `json:"items"`
		Missing []string `json:"missing"`
	}
	require.NoError(t, json.Unmarshal(raw, &states), string(raw))
	require.Len(t, states.Items, 2)
	require.Equal(t, idstr(full), states.Items[0].WorkID, "request order")
	require.Equal(t, "done", states.Items[0].State)
	require.Equal(t, idstr(stateOnly), states.Items[1].WorkID)
	require.Equal(t, []string{"99887766"}, states.Missing)
}

func TestLiveMyWorksBatchBound(t *testing.T) {
	env := liveCatalog(t)

	ids := make([]string, 0, 101)
	for i := range 100 {
		ids = append(ids, strconv.Itoa(900000+i))
	}
	status, _, raw := liveDo(t, env, http.MethodGet, "/v2/me/works?work_ids="+strings.Join(ids, ","), liveUserToken, "")
	require.Equal(t, 200, status, "100 is the bound, not one past it: %s", raw)

	ids = append(ids, "999999")
	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/me/works?work_ids="+strings.Join(ids, ","), liveUserToken, "")
	require.Equal(t, 400, status, string(raw))
	require.Equal(t, problem.CodeTooManyIDs, liveProblem(t, raw).Code)

	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/me/works", liveUserToken, "")
	require.Equal(t, 400, status, string(raw))
	p := liveProblem(t, raw)
	require.Equal(t, problem.CodeInvalidParameter, p.Code)
	require.NotEmpty(t, p.Errors)
	require.Equal(t, "work_ids", p.Errors[0].Parameter)

	status, _, raw = liveDo(t, env, http.MethodGet, "/v2/me/works?work_ids=abc", liveUserToken, "")
	require.Equal(t, 400, status, string(raw))
	require.Equal(t, problem.CodeInvalidParameter, liveProblem(t, raw).Code)
}
